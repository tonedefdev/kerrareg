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
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/go-github/v81/github"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
)

// trivyVulnerability is the subset of Trivy's per-vulnerability JSON output used here.
type trivyVulnerability struct {
	VulnerabilityID  string `json:"VulnerabilityID"`
	PkgName          string `json:"PkgName"`
	InstalledVersion string `json:"InstalledVersion"`
	FixedVersion     string `json:"FixedVersion"`
	Severity         string `json:"Severity"`
	Title            string `json:"Title"`
}

// trivyCauseMetadata holds the resource location for an IaC misconfiguration finding.
type trivyCauseMetadata struct {
	Resource string `json:"Resource"`
}

// trivyMisconfiguration is the subset of a single Trivy IaC misconfiguration finding.
type trivyMisconfiguration struct {
	ID            string             `json:"ID"`
	Title         string             `json:"Title"`
	Severity      string             `json:"Severity"`
	CauseMetadata trivyCauseMetadata `json:"CauseMetadata"`
}

// trivyResult is the subset of a single Trivy result target (file / layer).
type trivyResult struct {
	Target            string                  `json:"Target"`
	Class             string                  `json:"Class"`
	Type              string                  `json:"Type"`
	Vulnerabilities   []trivyVulnerability    `json:"Vulnerabilities"`
	Misconfigurations []trivyMisconfiguration `json:"Misconfigurations"`
}

// trivyReport is the top-level structure of `trivy --format json` output.
type trivyReport struct {
	Results []trivyResult `json:"Results"`
}

// runTrivy executes the trivy binary with the supplied arguments and returns raw stdout.
// Trivy exit code 1 means "vulnerabilities found" — this is not treated as an error here.
func runTrivy(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "trivy", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Exit code 1 = vulnerabilities found — not a fatal error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return stdout.Bytes(), nil
		}

		return nil, fmt.Errorf("trivy failed: %w — stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// parseTrivyReport converts raw Trivy JSON output into SecurityFinding slices.
// The optional filter function allows callers to select specific result targets (e.g. go.mod only).
func parseTrivyReport(data []byte, filter func(trivyResult) bool) ([]opendepotv1alpha1.SecurityFinding, error) {
	var report trivyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse trivy JSON output: %w", err)
	}

	var findings []opendepotv1alpha1.SecurityFinding
	for _, result := range report.Results {
		if filter != nil && !filter(result) {
			continue
		}

		for _, v := range result.Vulnerabilities {
			findings = append(findings, opendepotv1alpha1.SecurityFinding{
				VulnerabilityID:  v.VulnerabilityID,
				PkgName:          v.PkgName,
				InstalledVersion: v.InstalledVersion,
				FixedVersion:     v.FixedVersion,
				Severity:         v.Severity,
				Title:            v.Title,
			})
		}

		for _, m := range result.Misconfigurations {
			findings = append(findings, opendepotv1alpha1.SecurityFinding{
				VulnerabilityID: m.ID,
				PkgName:         m.CauseMetadata.Resource,
				Severity:        m.Severity,
				Title:           m.Title,
			})
		}
	}

	return findings, nil
}

// resolveProviderSourceRepository returns the VCS source URL for a provider.
// If ProviderConfig.SourceRepository is set it is used directly (explicit override).
// Otherwise the OpenTofu registry docs API (api.opentofu.org) is queried for the provider's
// repository link. If that lookup fails, a heuristic URL is derived from the namespace and name.
func resolveProviderSourceRepository(ctx context.Context, namespace, providerName string, cfg *opendepotv1alpha1.ProviderConfig) string {
	if cfg != nil && cfg.SourceRepository != nil && strings.TrimSpace(*cfg.SourceRepository) != "" {
		return strings.TrimSpace(*cfg.SourceRepository)
	}

	if strings.TrimSpace(namespace) == "" {
		namespace = "hashicorp"
	}

	repoURL, err := lookupProviderRepo(ctx, namespace, providerName)
	if err == nil && repoURL != "" {
		return repoURL
	}

	// Fall back to heuristic — scan degrades gracefully rather than blocking sync.
	return fmt.Sprintf("https://github.com/%s/terraform-provider-%s",
		strings.TrimSpace(namespace), strings.TrimSpace(providerName))
}

// extractBinaryFromZip extracts the provider executable from a HashiCorp release zip.
// The zip contains exactly one file: the compiled provider binary. We skip any
// accompanying README or LICENSE files by filtering on common non-binary suffixes.
func extractBinaryFromZip(archiveBytes []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to open provider zip archive: %w", err)
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		name := strings.ToLower(filepath.Base(f.Name))
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".json") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open %s in provider zip: %w", f.Name, err)
		}
		defer rc.Close()

		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(rc); err != nil {
			return nil, fmt.Errorf("failed to read %s from provider zip: %w", f.Name, err)
		}
		return buf.Bytes(), nil
	}

	return nil, fmt.Errorf("no executable found in provider zip archive")
}

// downloadGoMod fetches go.mod for a given provider version from its GitHub source repository
// using the provided GitHub client (authenticated or unauthenticated). The repoURL must be a
// https://github.com/owner/repo URL; version should be bare (no leading v).
func downloadGoMod(ctx context.Context, repoURL, version string, githubClient *github.Client) ([]byte, error) {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(repoURL, "https://github.com/"), "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("cannot parse owner/repo from URL %q", repoURL)
	}
	return opendepotGithub.GetProviderGoMod(ctx, githubClient, parts[0], parts[1], version)
}

// scanProviderBinary runs `trivy rootfs` against the provider executable extracted from archiveBytes.
// archiveBytes is the raw HashiCorp release zip. The binary is extracted before scanning.
// When offline is true, --offline-scan is passed to Trivy so it does not attempt network calls.
func (r *VersionReconciler) scanProviderBinary(ctx context.Context, archiveBytes []byte, cacheDir string, offline bool) ([]opendepotv1alpha1.SecurityFinding, error) {
	binaryBytes, err := extractBinaryFromZip(archiveBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to extract binary from provider archive: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "opendepot-binscan-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for binary scan: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "provider-binary")
	if err := os.WriteFile(binPath, binaryBytes, 0600); err != nil {
		return nil, fmt.Errorf("failed to write provider binary to temp dir: %w", err)
	}

	args := []string{"rootfs", "--format", "json"}
	if offline {
		args = append(args, "--offline-scan")
	}
	args = append(args, "--cache-dir", cacheDir, "--quiet", tmpDir)

	output, err := runTrivy(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("binary scan failed: %w", err)
	}

	if len(output) == 0 {
		return nil, nil
	}

	return parseTrivyReport(output, func(res trivyResult) bool {
		return res.Class == "lang-pkgs" || res.Class == "os-pkgs"
	})
}

// scanProviderSource runs `trivy fs` against a provider's go.mod fetched from its GitHub source repo.
// repoURL is the https://github.com/... URL of the provider's source repository.
// version is the provider version (bare semver; the v-prefix is tried automatically).
// When offline is true, --offline-scan is passed to Trivy so it does not attempt network calls.
func (r *VersionReconciler) scanProviderSource(ctx context.Context, repoURL, version, cacheDir string, offline bool, githubClient *github.Client) ([]opendepotv1alpha1.SecurityFinding, error) {
	goModBytes, err := downloadGoMod(ctx, repoURL, version, githubClient)
	if err != nil {
		// Non-fatal: source repo may be private or follow a non-standard layout.
		r.Log.Info("Skipping source scan: could not download go.mod",
			"repoURL", repoURL, "version", version, "reason", err.Error())
		return nil, nil
	}

	tmpDir, err := os.MkdirTemp("", "opendepot-srcscan-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for source scan: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, goModBytes, 0600); err != nil {
		return nil, fmt.Errorf("failed to write go.mod to temp dir: %w", err)
	}

	args := []string{"fs", "--format", "json"}
	if offline {
		args = append(args, "--offline-scan")
	}
	args = append(args, "--cache-dir", cacheDir, "--quiet", tmpDir)

	output, err := runTrivy(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("source scan failed: %w", err)
	}

	if len(output) == 0 {
		return nil, nil
	}

	return parseTrivyReport(output, func(res trivyResult) bool {
		return strings.HasSuffix(res.Target, "go.mod") || res.Type == "gomod"
	})
}

// runProviderScan orchestrates source and binary Trivy scans for a provider Version.
// It returns the binary scan result (if successful) to be persisted by the caller
// alongside the other status fields, avoiding partial status updates that would
// violate the CRD required-field validation (checksum, synced, syncStatus are all
// required and may not yet be set when this function is called).
//
// Source scan deduplication: if Provider.Status.SourceScan already covers this version,
// the source scan is skipped. Binary scan always runs (unique per OS/arch).
//
// When blockOnCritical or blockOnHigh is true and findings of that severity are present,
// the function returns a non-nil error to halt reconciliation.
func (r *VersionReconciler) runProviderScan(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	archiveBytes []byte,
	cacheDir string,
	offline bool,
	blockOnCritical bool,
	blockOnHigh bool,
) (*opendepotv1alpha1.ProviderBinaryScan, error) {
	providerName := version.Labels["opendepot.defdev.io/provider"]
	if providerName == "" {
		return nil, nil
	}

	// Binary scan (always runs, unique per OS/arch)
	var binaryScan *opendepotv1alpha1.ProviderBinaryScan
	binaryFindings, err := r.scanProviderBinary(ctx, archiveBytes, cacheDir, offline)
	if err != nil {
		r.Log.Error(err, "Binary scan failed — continuing without scan results",
			"version", version.Name)
	} else {
		now := time.Now().UTC().Format(time.RFC3339)
		binaryScan = &opendepotv1alpha1.ProviderBinaryScan{
			ScannedAt: now,
			Findings:  binaryFindings,
		}

		if blockOnCritical || blockOnHigh {
			for _, f := range binaryFindings {
				if blockOnCritical && f.Severity == "CRITICAL" {
					return binaryScan, fmt.Errorf("blocking: CRITICAL vulnerability %s in binary (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
				}

				if blockOnHigh && f.Severity == "HIGH" {
					return binaryScan, fmt.Errorf("blocking: HIGH vulnerability %s in binary (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
				}
			}
		}
	}

	// Source scan (deduplicated per provider version)
	providerNamespace := "hashicorp"
	if version.Spec.ProviderConfigRef != nil && version.Spec.ProviderConfigRef.Namespace != nil {
		if ns := strings.TrimSpace(*version.Spec.ProviderConfigRef.Namespace); ns != "" {
			providerNamespace = ns
		}
	}

	repoURL := resolveProviderSourceRepository(ctx, providerNamespace, providerName, version.Spec.ProviderConfigRef)
	providerObj := &opendepotv1alpha1.Provider{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      providerName,
		Namespace: version.Namespace,
	}, providerObj); err != nil {
		r.Log.Error(err, "Could not fetch parent Provider for source scan deduplication", "provider", providerName)
		return binaryScan, nil
	}

	currentVersion := strings.TrimPrefix(version.Spec.Version, "v")
	if providerObj.Status.SourceScan != nil && providerObj.Status.SourceScan.Version == currentVersion {
		r.Log.V(5).Info("Source scan already exists for this provider version — skipping",
			"provider", providerName, "version", currentVersion)
		return binaryScan, nil
	}

	useAuthClient := version.Spec.ProviderConfigRef != nil &&
		version.Spec.ProviderConfigRef.GithubClientConfig != nil &&
		version.Spec.ProviderConfigRef.GithubClientConfig.UseAuthenticatedClient

	var githubCfg *opendepotGithub.GithubClientConfig
	if useAuthClient {
		var cfgErr error
		githubCfg, cfgErr = opendepotGithub.GetGithubApplicationSecret(ctx, r.Client, version.Namespace)
		if cfgErr != nil {
			r.Log.Error(cfgErr, "Failed to load GitHub App secret — falling back to unauthenticated source scan",
				"provider", providerName)
			useAuthClient = false
		}
	}

	ghClient, ghErr := opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubCfg)
	if ghErr != nil {
		r.Log.Error(ghErr, "Failed to create GitHub client — falling back to unauthenticated source scan",
			"provider", providerName)
		ghClient, _ = opendepotGithub.CreateGithubClient(ctx, false, nil)
	}

	sourceFindings, err := r.scanProviderSource(ctx, repoURL, strings.TrimPrefix(version.Spec.Version, "v"), cacheDir, offline, ghClient)
	if err != nil {
		r.Log.Error(err, "Source scan failed — continuing without scan results",
			"provider", providerName, "version", currentVersion)
		return binaryScan, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sourceScan := &opendepotv1alpha1.ProviderSourceScan{
		ScannedAt: now,
		Version:   currentVersion,
		Findings:  sourceFindings,
	}

	if scanErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &opendepotv1alpha1.Provider{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(providerObj), current); err != nil {
			return err
		}

		current.Status.SourceScan = sourceScan
		return r.Status().Update(ctx, current, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	}); scanErr != nil {
		r.Log.Error(scanErr, "Failed to persist source scan results", "provider", providerName)
		return binaryScan, nil
	}

	if blockOnCritical || blockOnHigh {
		for _, f := range sourceFindings {
			if blockOnCritical && f.Severity == "CRITICAL" {
				return binaryScan, fmt.Errorf("blocking: CRITICAL vulnerability %s in source (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
			}

			if blockOnHigh && f.Severity == "HIGH" {
				return binaryScan, fmt.Errorf("blocking: HIGH vulnerability %s in source (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
			}
		}
	}

	return binaryScan, nil
}

// extractArchiveToDir extracts the contents of a module archive (zip or gzip tarball) into destDir.
// It defends against path traversal attacks by rejecting any entry whose cleaned path escapes destDir.
func extractArchiveToDir(archiveBytes []byte, destDir string) error {
	if zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err == nil {
		for _, f := range zr.File {
			if err := extractZipEntry(f, destDir); err != nil {
				return err
			}
		}
		return nil
	}

	gr, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return fmt.Errorf("archive is neither a valid zip nor a gzip tarball: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		if err := extractTarEntry(tr, hdr, destDir); err != nil {
			return err
		}
	}

	return nil
}

// extractZipEntry writes a single zip file entry into destDir, guarding against path traversal.
func extractZipEntry(f *zip.File, destDir string) error {
	dest := filepath.Join(destDir, f.Name)
	if !strings.HasPrefix(filepath.Clean(dest)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("zip entry %q escapes destination directory (path traversal)", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(dest, 0700)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", f.Name, err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip entry %s: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil { //nolint:gosec // size bounded by Trivy scan input
		return fmt.Errorf("failed to write zip entry %s: %w", f.Name, err)
	}
	return nil
}

// extractTarEntry writes a single tar entry into destDir, guarding against path traversal.
func extractTarEntry(tr *tar.Reader, hdr *tar.Header, destDir string) error {
	dest := filepath.Join(destDir, hdr.Name)
	if !strings.HasPrefix(filepath.Clean(dest)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("tar entry %q escapes destination directory (path traversal)", hdr.Name)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(dest, 0700)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", hdr.Name, err)
		}

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", dest, err)
		}
		defer out.Close()

		if _, err := io.Copy(out, tr); err != nil { //nolint:gosec // size bounded by Trivy scan input
			return fmt.Errorf("failed to write tar entry %s: %w", hdr.Name, err)
		}
	}

	return nil
}

// scanModuleArchive extracts a module archive into a temp directory and runs `trivy fs` against it.
// It returns IaC (config-class) findings from the HCL source.
func (r *VersionReconciler) scanModuleArchive(ctx context.Context, archiveBytes []byte, cacheDir string, offline bool) ([]opendepotv1alpha1.SecurityFinding, error) {
	tmpDir, err := os.MkdirTemp("", "opendepot-modscan-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for module scan: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractArchiveToDir(archiveBytes, tmpDir); err != nil {
		return nil, fmt.Errorf("failed to extract module archive: %w", err)
	}

	args := []string{"fs", "--format", "json"}
	if offline {
		args = append(args, "--offline-scan")
	}
	args = append(args, "--cache-dir", cacheDir, "--quiet", tmpDir)

	output, err := runTrivy(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("module source scan failed: %w", err)
	}

	if len(output) == 0 {
		return nil, nil
	}

	return parseTrivyReport(output, func(res trivyResult) bool {
		return res.Class == "config"
	})
}

// runModuleScan orchestrates a Trivy IaC scan for a module Version archive.
// It returns the scan result to be persisted atomically by the caller alongside
// the other required status fields (checksum, synced, syncStatus).
func (r *VersionReconciler) runModuleScan(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	archiveBytes []byte,
	cacheDir string,
	offline bool,
	blockOnCritical bool,
	blockOnHigh bool,
) (*opendepotv1alpha1.ModuleSourceScan, error) {
	findings, err := r.scanModuleArchive(ctx, archiveBytes, cacheDir, offline)
	if err != nil {
		r.Log.Error(err, "Module source scan failed — continuing without scan results",
			"version", version.Name)
		return nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sourceScan := &opendepotv1alpha1.ModuleSourceScan{
		ScannedAt: now,
		Findings:  findings,
	}

	if blockOnCritical || blockOnHigh {
		for _, f := range findings {
			if blockOnCritical && f.Severity == "CRITICAL" {
				return sourceScan, fmt.Errorf("blocking: CRITICAL finding %s in module source (%s)", f.VulnerabilityID, f.PkgName)
			}
			if blockOnHigh && f.Severity == "HIGH" {
				return sourceScan, fmt.Errorf("blocking: HIGH finding %s in module source (%s)", f.VulnerabilityID, f.PkgName)
			}
		}
	}

	return sourceScan, nil
}
