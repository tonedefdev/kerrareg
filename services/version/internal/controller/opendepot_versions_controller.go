/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	"github.com/google/uuid"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/storage"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
)

const (
	opendepotControllerName = "opendepot-versions-controller"
)

// VersionReconciler reconciles a Version object.
type VersionReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	ScanningEnabled bool
	ScanModules     bool
	TrivyCacheDir   string
	ScanOffline     bool
	BlockOnCritical bool
	BlockOnHigh     bool
	// scanSem limits the number of concurrent Trivy processes. Each Trivy
	// invocation loads the full vulnerability DB (~2 GiB) so running more than
	// one at a time risks OOMKill even with a generous container memory limit.
	scanSem chan struct{}
	// downloadSem limits the number of concurrent provider archive downloads.
	// Each download streams a ~700 MB zip to disk; allowing all four workers to
	// download simultaneously risks exhausting memory and disk I/O.
	downloadSem chan struct{}
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions/finalizers,verbs=update
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=modules,verbs=get
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=modules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=providers,verbs=get
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=providers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

func (r *VersionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	version := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, req.NamespacedName, version); err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("version resource not found. Ignoring since object must be deleted", "version", req.Name)
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get version", "version", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info(
		"Version found: starting reconciliation",
		"type", version.Spec.Type,
		"version", version.Spec.Version,
		"versionName", version.Name,
	)

	if version.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer) {
			controllerutil.AddFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer)
			if err := r.Update(ctx, version); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
	} else {
		return r.reconcileDeletion(ctx, version)
	}

	var prepareResult ctrl.Result
	var prepareErr error

	switch version.Spec.Type {
	case opendepotv1alpha1.OpenDepotModule:
		prepareResult, prepareErr = r.prepareModuleVersion(ctx, req, version)
	case opendepotv1alpha1.OpenDepotProvider:
		prepareResult, prepareErr = r.prepareProviderVersion(version)
	default:
		return ctrl.Result{}, fmt.Errorf("no usable type provided on Version '%s'", version.Name)
	}

	if prepareErr != nil {
		return prepareResult, prepareErr
	}

	if prepareResult.RequeueAfter > 0 {
		return prepareResult, nil
	}

	if version.Spec.ModuleConfigRef != nil && version.Spec.ProviderConfigRef != nil {
		version.Status.Synced = false
		version.Status.SyncStatus = "Only one of 'ModuleConfigRef' or 'ProviderConfigRef' can be provided: both are defined"
		if err := r.Status().Update(ctx, version); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, fmt.Errorf("invalid version config: both moduleConfigRef and providerConfigRef are defined")
	}

	var fileBytes []byte
	var archiveChecksum *string
	var providerTmpPath string

	switch version.Spec.Type {
	case opendepotv1alpha1.OpenDepotModule:
		moduleBytes, checksum, err := r.fetchModuleArchive(ctx, version)
		if err != nil {
			version.Status.SyncStatus = fmt.Sprintf("Failed to retrieve module archive: %v", err)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, err
		}

		fileBytes = moduleBytes
		archiveChecksum = checksum

		if version.Spec.ModuleConfigRef.Immutable != nil &&
			*version.Spec.ModuleConfigRef.Immutable &&
			version.Status.Checksum != nil &&
			archiveChecksum != nil &&
			*version.Status.Checksum != *archiveChecksum {

			statusMsg := fmt.Errorf("version is marked immutable: archive checksum doesn't match existing checksum: got '%s'", *archiveChecksum)
			version.Status.SyncStatus = statusMsg.Error()
			version.Status.Synced = false
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, statusMsg
		}
	case opendepotv1alpha1.OpenDepotProvider:
		// Fast path: if the Version has already been synced and the artifact exists in
		// storage with a matching checksum, there is nothing to download or upload.
		// Skipping the download is critical — /tmp is tmpfs (RAM-backed) in Linux
		// containers, so downloading 700MB per worker on every reconcile exhausts memory.
		if version.Status.Checksum != nil && version.Status.Synced && version.Spec.FileName != nil {
			existingFilePath, pathErr := getVersionFilePath(version)
			if pathErr == nil {
				earlySoi := &types.StorageObjectInput{
					Method:   types.Get,
					FilePath: existingFilePath,
					Version:  version,
				}
				if checkErr := r.InitStorageFactory(ctx, earlySoi); checkErr == nil &&
					earlySoi.FileExists &&
					earlySoi.ObjectChecksum != nil &&
					*earlySoi.ObjectChecksum == *version.Status.Checksum {
					return reconcile.Result{}, nil
				}
			}
		}

		// Serialize provider downloads: each is ~700 MB and concurrent downloads
		// exhaust memory. The semaphore ensures only one download runs at a time.
		select {
		case r.downloadSem <- struct{}{}:
		case <-ctx.Done():
			return ctrl.Result{}, ctx.Err()
		}
		tmpPath, cleanupArchive, checksum, fileName, err := r.fetchProviderArchive(ctx, version)
		<-r.downloadSem
		if err != nil {
			version.Status.SyncStatus = fmt.Sprintf("Failed to retrieve provider archive from HashiCorp releases API: %v", err)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, err
		}
		defer cleanupArchive()

		if version.Spec.FileName == nil {
			uuidFileName, err := generateProviderFileName(*fileName)
			if err != nil {
				version.Status.SyncStatus = fmt.Sprintf("Failed to generate UUID filename for provider archive: %v", err)
				_ = r.Status().Update(ctx, version)
				return ctrl.Result{}, err
			}
			version.Spec.FileName = uuidFileName
		}

		archiveChecksum = checksum
		providerTmpPath = tmpPath
	}

	filePath, err := getVersionFilePath(version)
	if err != nil {
		return ctrl.Result{}, err
	}

	// For providers the archive is on disk (providerTmpPath); open it as a ReadSeeker so
	// storage backends can stream it without ever loading the full zip into the Go heap.
	var providerFile *os.File
	if providerTmpPath != "" {
		pf, openErr := os.Open(providerTmpPath)
		if openErr != nil {
			version.Status.SyncStatus = fmt.Sprintf("Failed to open provider archive temp file: %v", openErr)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, openErr
		}
		defer pf.Close()
		providerFile = pf
	}

	// hasArtifact indicates whether we have bytes (module) or a temp file (provider) to upload.
	hasArtifact := len(fileBytes) > 0 || providerFile != nil

	soi := &types.StorageObjectInput{
		FileBytes:  fileBytes,
		FileReader: providerFile,
		FilePath:   filePath,
		Version:    version,
	}

	if version.Status.Checksum != nil {
		soi.Method = types.Get
		if err = r.InitStorageFactory(ctx, soi); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if !hasArtifact {
			version.Status.Synced = false
			version.Status.SyncStatus = "No artifact bytes available for upload yet"
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		soi.Method = types.Put
		if err = r.InitStorageFactory(ctx, soi); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !soi.FileExists || (soi.ObjectChecksum != nil && version.Status.Checksum != nil && *soi.ObjectChecksum != *version.Status.Checksum) {
		if !hasArtifact {
			version.Status.Synced = false
			version.Status.SyncStatus = "Artifact missing in storage and no bytes available to reconcile"
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// Seek back to the start so the storage backend can re-read the provider archive.
		if providerFile != nil {
			if _, seekErr := providerFile.Seek(0, io.SeekStart); seekErr != nil {
				return ctrl.Result{}, fmt.Errorf("failed to seek provider archive: %w", seekErr)
			}
		}

		soi.Method = types.Put
		if err = r.InitStorageFactory(ctx, soi); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		currentVersion.Spec.FileName = version.Spec.FileName
		currentVersion.Spec.ModuleConfigRef = version.Spec.ModuleConfigRef
		currentVersion.Spec.ProviderConfigRef = version.Spec.ProviderConfigRef

		if err := r.Update(ctx, currentVersion); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Run Trivy security scan for provider artifacts when scanning is enabled.
	// The binary scan result is returned here and written in the final status
	// update below so that all required fields (checksum, synced, syncStatus)
	// are set atomically and satisfy CRD required-field validation.
	//
	// Free the in-memory zip bytes before invoking Trivy so that the ~700 MB
	// provider archive and the ~2 GB Trivy vulnerability DB are never resident
	// in the Go heap simultaneously. The scan reads the zip directly from the
	// temp file on disk instead.
	var binaryScan *opendepotv1alpha1.ProviderBinaryScan
	if r.ScanningEnabled && version.Spec.Type == opendepotv1alpha1.OpenDepotProvider && providerTmpPath != "" {
		var scanErr error
		binaryScan, scanErr = r.runProviderScan(ctx, version, providerTmpPath, r.TrivyCacheDir, r.ScanOffline, r.BlockOnCritical, r.BlockOnHigh)
		if scanErr != nil {
			version.Status.Synced = false
			version.Status.SyncStatus = fmt.Sprintf("Scan policy violation: %v", scanErr)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, scanErr
		}
	}

	// Run Trivy IaC scan for module archives when scanning and module scanning are enabled.
	// The scan result is returned here and written atomically in the final status update below.
	var moduleScan *opendepotv1alpha1.ModuleSourceScan
	if r.ScanningEnabled && r.ScanModules && version.Spec.Type == opendepotv1alpha1.OpenDepotModule && len(fileBytes) > 0 {
		var scanErr error
		moduleScan, scanErr = r.runModuleScan(ctx, version, fileBytes, r.TrivyCacheDir, r.ScanOffline, r.BlockOnCritical, r.BlockOnHigh)
		if scanErr != nil {
			version.Status.Synced = false
			version.Status.SyncStatus = fmt.Sprintf("Scan policy violation: %v", scanErr)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, scanErr
		}
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		currentVersion.Status.Synced = true
		if archiveChecksum != nil {
			currentVersion.Status.Checksum = archiveChecksum
		}

		currentVersion.Status.SyncStatus = "Successfully synced version"
		if binaryScan != nil {
			currentVersion.Status.BinaryScan = binaryScan
		}

		if moduleScan != nil {
			currentVersion.Status.SourceScan = moduleScan
		}

		if err := r.Status().Update(ctx, currentVersion, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	return reconcile.Result{}, nil
}

// reconcileDeletion removes the stored artifact and finalizer when a Version is being deleted.
func (r *VersionReconciler) reconcileDeletion(ctx context.Context, version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer) {
		return ctrl.Result{}, nil
	}

	filePath, err := getVersionFilePath(version)
	if err != nil {
		return ctrl.Result{}, err
	}

	soi := &types.StorageObjectInput{
		Method:   types.Delete,
		FilePath: filePath,
		Version:  version,
	}
	if err := r.InitStorageFactory(ctx, soi); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer)
	if err := r.Update(ctx, version); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// prepareModuleVersion resolves the backing Module metadata required to reconcile a module Version.
// When moduleConfigRef.name is absent, or when no Module CR with that name exists in the namespace,
// the config is treated as fully inline: a UUID filename is generated so getVersionFilePath has a
// stable storage key, and the GitHub download proceeds using the fields already on moduleConfigRef.
func (r *VersionReconciler) prepareModuleVersion(ctx context.Context, req ctrl.Request, version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	if version.Spec.ModuleConfigRef == nil {
		return ctrl.Result{}, fmt.Errorf("moduleConfigRef is required for module version '%s'", version.Name)
	}

	// No Module CR name provided — the caller supplied all config inline.
	if version.Spec.ModuleConfigRef.Name == nil {
		if version.Spec.FileName == nil {
			uuidFileName, err := generateModuleFileName(version.Spec.ModuleConfigRef.FileFormat)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to generate UUID filename for module archive: %w", err)
			}
			version.Spec.FileName = uuidFileName
		}
		return ctrl.Result{}, nil
	}

	moduleObject := client.ObjectKey{Name: *version.Spec.ModuleConfigRef.Name, Namespace: req.Namespace}
	module := &opendepotv1alpha1.Module{}
	if err := r.Get(ctx, moduleObject, module); err != nil {
		if k8serr.IsNotFound(err) {
			// No backing Module CR — treat as inline config, using Name as the GitHub repo name.
			if version.Spec.FileName == nil {
				uuidFileName, err := generateModuleFileName(version.Spec.ModuleConfigRef.FileFormat)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to generate UUID filename for module archive: %w", err)
				}
				version.Spec.FileName = uuidFileName
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if module.Status.ModuleVersionRefs == nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	moduleRef, exists := module.Status.ModuleVersionRefs[version.Spec.Version]
	if !exists || moduleRef == nil || moduleRef.FileName == nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	version.Spec.ModuleConfigRef = &module.Spec.ModuleConfig
	version.Spec.FileName = moduleRef.FileName

	if version.Spec.ModuleConfigRef.Name == nil {
		version.Spec.ModuleConfigRef.Name = &module.ObjectMeta.Name
	}

	return ctrl.Result{}, nil
}

// prepareProviderVersion validates provider references and ensures required provider fields are present.
func (r *VersionReconciler) prepareProviderVersion(version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	if version.Spec.ProviderConfigRef == nil {
		return ctrl.Result{}, fmt.Errorf("providerConfigRef is required for provider version '%s'", version.Name)
	}

	if version.Spec.ProviderConfigRef.Name == nil {
		providerName := version.Labels["opendepot.defdev.io/provider"]
		if providerName == "" {
			return ctrl.Result{}, fmt.Errorf("providerConfigRef.name is required for provider version '%s'", version.Name)
		}
		version.Spec.ProviderConfigRef.Name = &providerName
	}

	return ctrl.Result{}, nil
}

// fetchModuleArchive downloads module source from GitHub and returns bytes with a checksum.
func (r *VersionReconciler) fetchModuleArchive(ctx context.Context, version *opendepotv1alpha1.Version) ([]byte, *string, error) {
	var githubClientConfig *opendepotGithub.GithubClientConfig
	var githubClient *github.Client

	useAuthClient := false
	if version.Spec.ModuleConfigRef.GithubClientConfig != nil {
		useAuthClient = version.Spec.ModuleConfigRef.GithubClientConfig.UseAuthenticatedClient
	}

	var err error
	if useAuthClient {
		githubClientConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, r.Client, version.Namespace)
		if err != nil {
			return nil, nil, err
		}
	}

	githubClient, err = opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubClientConfig)
	if err != nil {
		return nil, nil, err
	}

	var fileFormat github.ArchiveFormat
	if version.Spec.FileName != nil && strings.Contains(*version.Spec.FileName, "zip") {
		fileFormat = github.Zipball
	} else {
		fileFormat = github.Tarball
	}

	return opendepotGithub.GetModuleArchiveFromRef(ctx, r.Log, githubClient, version, fileFormat)
}

// generateModuleFileName returns a randomly generated UUID7 filename for a module archive.
// The default extension is .tar.gz; pass fileFormat = "zip" to get a .zip extension.
func generateModuleFileName(fileFormat *string) (*string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	ext := ".tar.gz"
	if fileFormat != nil && *fileFormat == "zip" {
		ext = ".zip"
	}
	name := fmt.Sprintf("%s%s", id, ext)
	return &name, nil
}

// generateProviderFileName returns a randomly generated UUID7 filename, preserving the original file extension.
func generateProviderFileName(originalFileName string) (*string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	ext := path.Ext(originalFileName)
	name := fmt.Sprintf("%s%s", id, ext)
	return &name, nil
}

// fetchProviderArchive resolves a provider binary download from the OpenTofu registry
// and streams the artifact to a temporary file on disk to avoid buffering the
// full provider zip (~700 MB) in the Go heap. The caller must invoke the returned
// cleanup function (typically via defer) to remove the temp file.
func (r *VersionReconciler) fetchProviderArchive(ctx context.Context, version *opendepotv1alpha1.Version) (archivePath string, cleanup func(), checksum *string, fileName *string, err error) {
	if version.Spec.ProviderConfigRef == nil || version.Spec.ProviderConfigRef.Name == nil {
		return "", func() {}, nil, nil, fmt.Errorf("providerConfigRef.name is required")
	}

	if strings.TrimSpace(version.Spec.OperatingSystem) == "" || strings.TrimSpace(version.Spec.Architecture) == "" {
		return "", func() {}, nil, nil, fmt.Errorf("provider operatingSystem and architecture are required")
	}

	providerName := strings.TrimSpace(*version.Spec.ProviderConfigRef.Name)
	providerVersion := strings.TrimPrefix(strings.TrimSpace(version.Spec.Version), "v")
	if providerVersion == "" {
		return "", func() {}, nil, nil, fmt.Errorf("provider version is empty")
	}

	providerNamespace := "hashicorp"
	if version.Spec.ProviderConfigRef.Namespace != nil {
		if ns := strings.TrimSpace(*version.Spec.ProviderConfigRef.Namespace); ns != "" {
			providerNamespace = ns
		}
	}

	download, err := lookupProviderDownload(ctx, providerNamespace, providerName, providerVersion,
		version.Spec.OperatingSystem, version.Spec.Architecture)
	if err != nil {
		return "", func() {}, nil, nil, err
	}

	tmpPath, checksumHex, cleanupFn, err := httpStreamToFile(ctx, download.DownloadURL)
	if err != nil {
		return "", func() {}, nil, nil, err
	}

	// Validate the downloaded archive against the registry-provided SHA256.
	if download.Shasum != "" {
		if checksumHex != strings.ToLower(download.Shasum) {
			cleanupFn()
			return "", func() {}, nil, nil, fmt.Errorf("checksum mismatch for provider archive %s: registry expected %s, got %s",
				download.Filename, download.Shasum, checksumHex)
		}
	}

	// Re-encode the hex SHA-256 as base64 for storage (matches the existing format).
	checksumBytes, _ := hex.DecodeString(checksumHex)
	checksumB64 := base64.StdEncoding.EncodeToString(checksumBytes)

	fn := download.Filename
	if fn == "" {
		fn = path.Base(download.DownloadURL)
	}

	if fn == "." || fn == "/" || fn == "" {
		cleanupFn()
		return "", func() {}, nil, nil, fmt.Errorf("unable to determine filename from provider download URL '%s'", download.DownloadURL)
	}

	return tmpPath, cleanupFn, &checksumB64, &fn, nil
}

// httpGetJSON performs an HTTP GET and unmarshals the response payload into out.
func httpGetJSON(ctx context.Context, requestURL string, out any) error {
	bytes, err := httpGetBytes(ctx, requestURL)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(bytes, out); err != nil {
		return fmt.Errorf("unable to parse JSON from '%s': %w", requestURL, err)
	}

	return nil
}

// httpGetBytes performs an HTTP GET and returns the raw response body bytes.
// Use httpStreamToFile for large downloads (e.g. provider binaries) to avoid
// buffering the full body in the Go heap.
func httpGetBytes(ctx context.Context, requestURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed for '%s': %w", requestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request to '%s' failed with status %d", requestURL, resp.StatusCode)
	}

	fileBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body for '%s': %w", requestURL, err)
	}

	return fileBytes, nil
}

// httpStreamToFile streams an HTTP GET response body to a temporary file on disk
// while computing its SHA-256 checksum via an io.TeeReader. It returns the temp
// file path, the hex-encoded checksum, a cleanup function that removes the file,
// and any error. This avoids buffering large provider binaries (~700 MB) in the
// Go heap, which is critical when multiple reconcilers run concurrently.
func httpStreamToFile(ctx context.Context, requestURL string) (filePath string, checksumHex string, cleanup func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", "", func() {}, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", func() {}, fmt.Errorf("request failed for '%s': %w", requestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", func() {}, fmt.Errorf("request to '%s' failed with status %d", requestURL, resp.StatusCode)
	}

	f, err := os.CreateTemp("", "opendepot-provider-*.zip")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("failed to create temp file for provider download: %w", err)
	}

	cleanupFn := func() {
		f.Close()
		os.Remove(f.Name())
	}

	h := sha256.New()
	if _, err = io.Copy(f, io.TeeReader(resp.Body, h)); err != nil {
		cleanupFn()
		return "", "", func() {}, fmt.Errorf("failed to stream provider archive from '%s': %w", requestURL, err)
	}

	if err = f.Sync(); err != nil {
		cleanupFn()
		return "", "", func() {}, fmt.Errorf("failed to sync provider archive temp file: %w", err)
	}

	return f.Name(), fmt.Sprintf("%x", h.Sum(nil)), cleanupFn, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VersionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Allow at most one Trivy process at a time to prevent concurrent DB loads
	// from exhausting container memory when multiple reconcilers run in parallel.
	r.scanSem = make(chan struct{}, 1)
	// Allow at most one provider archive download at a time. Each download is
	// ~700 MB; concurrent downloads exhaust memory and storage I/O.
	r.downloadSem = make(chan struct{}, 1)
	return ctrl.NewControllerManagedBy(mgr).
		For(&opendepotv1alpha1.Version{}).
		Named(opendepotControllerName).
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Complete(r)
}

// RunStorageFactory is the runtime handler for managing storage objects received by 'soi'.
func RunStorageFactory(ctx context.Context, storageInterface storage.Storage, soi *types.StorageObjectInput) error {
	switch soi.Method {
	case types.Delete:
		if err := storageInterface.DeleteObject(ctx, soi); err != nil {
			return err
		}
	case types.Get:
		if err := storageInterface.GetObjectChecksum(ctx, soi); err != nil {
			return err
		}
	case types.Put:
		if err := storageInterface.PutObject(ctx, soi); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no usable method provided")
	}

	return nil
}

// InitStorageFactory prepares and initializes storage using the version's storage config.
func (r *VersionReconciler) InitStorageFactory(ctx context.Context, soi *types.StorageObjectInput) error {
	storageConfig, err := getVersionStorageConfig(soi.Version)
	if err != nil {
		return err
	}

	var storageInterface storage.Storage
	if storageConfig.FileSystem != nil {
		storageInterface = &storage.FileSystem{}
		return RunStorageFactory(ctx, storageInterface, soi)
	}

	if storageConfig.S3 != nil {
		amazonS3Storage := &storage.AmazonS3Storage{}
		if err := amazonS3Storage.NewClient(ctx, storageConfig.S3.Region); err != nil {
			return err
		}
		storageInterface = amazonS3Storage
		return RunStorageFactory(ctx, storageInterface, soi)
	}

	if storageConfig.AzureStorage != nil {
		azureBlobStorage := &storage.AzureBlobStorage{}
		if err := azureBlobStorage.NewClients(storageConfig.AzureStorage.SubscriptionID, storageConfig.AzureStorage.AccountUrl); err != nil {
			return err
		}
		storageInterface = azureBlobStorage
		return RunStorageFactory(ctx, storageInterface, soi)
	}

	if storageConfig.GCS != nil {
		gcsStorage := &storage.GoogleCloudStorage{}
		if err := gcsStorage.NewClient(ctx); err != nil {
			return err
		}
		storageInterface = gcsStorage
		return RunStorageFactory(ctx, storageInterface, soi)
	}

	return fmt.Errorf("at least one StorageConfig backend must be configured")
}

// getVersionStorageConfig resolves storage configuration from module or provider config references.
func getVersionStorageConfig(version *opendepotv1alpha1.Version) (*opendepotv1alpha1.StorageConfig, error) {
	if version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.StorageConfig != nil {
		return version.Spec.ModuleConfigRef.StorageConfig, nil
	}

	if version.Spec.ProviderConfigRef != nil && version.Spec.ProviderConfigRef.StorageConfig != nil {
		return version.Spec.ProviderConfigRef.StorageConfig, nil
	}

	return nil, fmt.Errorf("storage config is not configured on moduleConfigRef or providerConfigRef")
}

// getVersionName resolves the logical resource name used as the storage prefix for a Version.
func getVersionName(version *opendepotv1alpha1.Version) (*string, error) {
	if version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.Name != nil {
		return version.Spec.ModuleConfigRef.Name, nil
	}

	if version.Spec.ProviderConfigRef != nil && version.Spec.ProviderConfigRef.Name != nil {
		return version.Spec.ProviderConfigRef.Name, nil
	}

	return nil, fmt.Errorf("unable to resolve version name from moduleConfigRef or providerConfigRef")
}

// getVersionFilePath computes the object key for module/provider artifacts.
func getVersionFilePath(version *opendepotv1alpha1.Version) (*string, error) {
	storageConfig, err := getVersionStorageConfig(version)
	if err != nil {
		return nil, err
	}

	name, err := getVersionName(version)
	if err != nil {
		return nil, err
	}

	if version.Spec.FileName == nil {
		return nil, fmt.Errorf("fileName is nil for version '%s'", version.Name)
	}

	if storageConfig.S3 != nil && storageConfig.S3.Key != nil {
		sanitized, err := storage.RemoveTrailingSlash(storageConfig.S3.Key)
		if err != nil {
			return nil, err
		}
		filePath := fmt.Sprintf("%s/%s/%s", *sanitized, *name, *version.Spec.FileName)
		return &filePath, nil
	}

	if storageConfig.FileSystem != nil && storageConfig.FileSystem.DirectoryPath != nil {
		sanitized, err := storage.RemoveTrailingSlash(storageConfig.FileSystem.DirectoryPath)
		if err != nil {
			return nil, err
		}
		filePath := fmt.Sprintf("%s/%s/%s", *sanitized, *name, *version.Spec.FileName)
		return &filePath, nil
	}

	filePath := fmt.Sprintf("%s/%s", *name, *version.Spec.FileName)
	return &filePath, nil
}
