package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/storage"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

// sanitizeModuleVersionForLookup normalizes a module version string into the form
// used as the Version resource name suffix: strips a leading "v", lowercases, and
// replaces dots and underscores with hyphens (e.g. "v1.2.3" → "1-2-3").
func sanitizeModuleVersionForLookup(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	version = strings.ToLower(version)
	version = strings.ReplaceAll(version, ".", "-")
	version = strings.ReplaceAll(version, "_", "-")
	return version
}

// getModuleVersion fetches the Version resource for the module name and version
// encoded in the request URL parameters.
func getModuleVersion(clientset *kubernetes.Clientset, w http.ResponseWriter, r *http.Request) (*opendepotv1alpha1.Version, error) {
	name := chi.URLParam(r, "name")
	namespace := chi.URLParam(r, "namespace")
	version := chi.URLParam(r, "version")
	moduleName := fmt.Sprintf("%s-%s", name, sanitizeModuleVersionForLookup(version))

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		Name(moduleName).
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var moduleVersion opendepotv1alpha1.Version
	if err = json.Unmarshal(result, &moduleVersion); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return &moduleVersion, nil
}

// getDownloadModuleUrl handles the Terraform module download redirect endpoint.
// It resolves the caller's identity, enforces GroupBinding access control, looks up
// the Version resource, and responds with an X-Terraform-Get header that directs
// the Terraform client to the storage-backend-specific download URL.
func getDownloadModuleUrl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	if binding != nil {
		name := chi.URLParam(r, "name")
		namespace := chi.URLParam(r, "namespace")

		if !isResourceAllowed(binding, "module", name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
	}

	moduleVersion, err := getModuleVersion(clientset, w, r)
	if err != nil {
		logger.Error("unable to get module version", "error", err)
		return
	}

	if moduleVersion.Spec.ModuleConfigRef == nil ||
		moduleVersion.Spec.ModuleConfigRef.StorageConfig == nil ||
		moduleVersion.Spec.FileName == nil {
		http.Error(w, "module version artifact not yet available", http.StatusServiceUnavailable)
		return
	}

	var downloadPath string
	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage != nil {
		downloadPath = fmt.Sprintf("azure/%s/%s/%s/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.SubscriptionID,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.ResourceGroup,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountName,
			url.PathEscape(moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountUrl),
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.FileSystem != nil {
		downloadPath = fmt.Sprintf("fileSystem/%s/%s/%s",
			base64.RawURLEncoding.EncodeToString([]byte(*moduleVersion.Spec.ModuleConfigRef.StorageConfig.FileSystem.DirectoryPath)),
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.GCS != nil {
		downloadPath = fmt.Sprintf("gcs/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.GCS.Bucket,
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3 != nil {
		downloadPath = fmt.Sprintf("s3/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Region,
			*moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Key,
		)
	}

	if moduleVersion.Status.Checksum == nil {
		http.Error(w, "module version checksum not yet available", http.StatusServiceUnavailable)
		return
	}

	checksumQuery := url.QueryEscape(*moduleVersion.Status.Checksum)
	w.Header().Set("X-Terraform-Get", fmt.Sprintf("/opendepot/modules/v1/download/%s?fileChecksum=%s", downloadPath, checksumQuery))
	// Download is recorded here (at the protocol redirect step) rather than in the
	// serveModuleFrom* handlers because the Terraform module protocol requires clients
	// to call this endpoint first; namespace and version are only available here.
	// The serveModuleFrom* routes do not carry namespace or version URL params.
	_ = recordDownload(r.Context(), statsClient, chi.URLParam(r, "namespace"), "module", chi.URLParam(r, "name"), chi.URLParam(r, "version"))
	w.WriteHeader(http.StatusNoContent)
}

// getModuleVersions returns the list of available versions for a module, as required
// by the Terraform module registry protocol.
func getModuleVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if binding != nil {
		if !isResourceAllowed(binding, "module", name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
	}

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("modules").
		Name(name).
		DoRaw(r.Context())
	if err != nil {
		logger.Error("unable to get modules", "error", err, "namespace", namespace, "name", name, "responseBody", string(result))

		switch {
		case k8sApiErrors.IsNotFound(err):
			http.Error(w, "module not found", http.StatusNotFound)
		case k8sApiErrors.IsForbidden(err):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}

		return
	}

	var module opendepotv1alpha1.Module
	if err = json.Unmarshal(result, &module); err != nil {
		logger.Error("unable to unmarshal module", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	response := ModuleVersionsResponse{
		Modules: []ModuleVersions{
			{
				Versions: module.Spec.Versions,
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

// serveModuleFromAzureBlob proxies a module archive download from Azure Blob Storage.
func serveModuleFromAzureBlob(w http.ResponseWriter, r *http.Request) {
	accountName := chi.URLParam(r, "account")
	accountUrl := chi.URLParam(r, "accountUrl")
	rg := chi.URLParam(r, "rg")
	subID := chi.URLParam(r, "subID")

	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	accountUrl, err := url.PathUnescape(accountUrl)
	if err != nil {
		logger.Error("failed to unescape account url", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	azureStorage := &storage.AzureBlobStorage{}
	if err := azureStorage.NewClients(subID, accountUrl); err != nil {
		logger.Error("failed to init azure clients", "error", err, "storageAccountName", accountName)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath:      &fileName,
		Method:        storageTypes.Get,
		ContainerName: &name,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			AzureStorage: &opendepotv1alpha1.AzureStorageConfig{
				AccountName:    accountName,
				AccountUrl:     accountUrl,
				ResourceGroup:  rg,
				SubscriptionID: subID,
			},
		},
	}

	getObjectFromStorageSystem(w, r, azureStorage, soi, checksum)
}

// serveModuleFromFileSystem serves a module archive from the local filesystem.
// When the request carries terraform-get=1 (go-getter source detection), the handler
// returns an X-Terraform-Get header pointing to the actual download URL with the
// correct archive type rather than streaming the file directly.
func serveModuleFromFileSystem(w http.ResponseWriter, r *http.Request) {
	encodedDir := chi.URLParam(r, "directory")
	moduleName := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	// go-getter sends ?terraform-get=1 to detect source URLs via HTML meta tags.
	// We intercept this and return the X-Terraform-Get header pointing to the same
	// download URL. go-getter reads the header before parsing the body, then processes
	// the source URL through its full pipeline which detects the archive extension
	// and uses direct file download (no further terraform-get detection).
	if r.URL.Query().Get("terraform-get") == "1" {
		scheme := "https"
		if r.TLS == nil {
			if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto == "http" || fwdProto == "https" {
				scheme = fwdProto
			} else {
				scheme = "http"
			}
		}

		q := url.Values{}
		q.Set("fileChecksum", checksum)

		// GitHub tarballs are gzip-compressed despite having .tar extension.
		// go-getter uses the archive param to select the decompressor, so we
		// must specify tar.gz for gzipped tarballs.
		ext := path.Ext(fileName)
		archiveType := strings.TrimPrefix(ext, ".")
		if archiveType == "tar" {
			archiveType = "tar.gz"
		}

		q.Set("archive", archiveType)
		sourceURL := fmt.Sprintf("%s://%s/opendepot/modules/v1/download/fileSystem/%s/%s/%s?%s",
			scheme, r.Host, encodedDir, moduleName, fileName, q.Encode())

		w.Header().Set("X-Terraform-Get", sourceURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	dirBytes, err := base64.RawURLEncoding.DecodeString(encodedDir)
	if err != nil {
		logger.Error("failed to decode directory path", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}
	dir := string(dirBytes)

	filePath := path.Join(dir, moduleName, fileName)
	allowedRoot := path.Clean(*opendepotFilesystemMountPath)
	if !strings.HasPrefix(path.Clean(filePath)+"/", allowedRoot+"/") {
		logger.Error("filesystem download path escapes allowed root", "path", filePath, "allowedRoot", allowedRoot)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	logger.Info("filesystem download", "dir", dir, "module", moduleName, "file", fileName)

	fsStorage := &storage.FileSystem{}
	soi := &storageTypes.StorageObjectInput{
		FilePath: &filePath,
		Method:   storageTypes.Get,
	}

	getObjectFromStorageSystem(w, r, fsStorage, soi, checksum)
}

// serveModuleFromGCS proxies a module archive download from Google Cloud Storage.
func serveModuleFromGCS(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	gcsStorage := &storage.GoogleCloudStorage{}
	if err := gcsStorage.NewClient(r.Context()); err != nil {
		logger.Error("failed to init gcs client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{
				Bucket: bucket,
			},
		},
	}

	getObjectFromStorageSystem(w, r, gcsStorage, soi, checksum)
}

// serveModuleFromS3 proxies a module archive download from Amazon S3.
func serveModuleFromS3(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	region := chi.URLParam(r, "region")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	s3Storage := &storage.AmazonS3Storage{}
	if err := s3Storage.NewClient(r.Context(), region); err != nil {
		logger.Error("failed to init s3 client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			S3: &opendepotv1alpha1.AmazonS3Config{
				Bucket: bucket,
			},
		},
	}

	getObjectFromStorageSystem(w, r, s3Storage, soi, checksum)
}
