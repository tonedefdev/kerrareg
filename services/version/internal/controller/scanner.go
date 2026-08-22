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

	"github.com/google/go-github/v81/github"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/registry"
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
// An empty (non-nil) slice is returned when the report contains no matching findings.
func parseTrivyReport(data []byte, filter func(trivyResult) bool) ([]opendepotv1alpha1.SecurityFinding, error) {
	var report trivyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse trivy JSON output: %w", err)
	}

	findings := make([]opendepotv1alpha1.SecurityFinding, 0)
	seen := make(map[string]struct{})
	for _, result := range report.Results {
		if filter != nil && !filter(result) {
			continue
		}

		for _, v := range result.Vulnerabilities {
			key := v.VulnerabilityID + "|" + v.PkgName + "|" + v.InstalledVersion
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
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
			key := m.ID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
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
// For OpenTofu providers, the registry docs API is queried for the repository link.
// If that lookup is unavailable or the provider comes from another registry, a heuristic URL is derived.
func resolveProviderSourceRepository(ctx context.Context, namespace, providerName string, cfg *opendepotv1alpha1.ProviderConfig) string {
	if cfg != nil && cfg.SourceRepository != nil && strings.TrimSpace(*cfg.SourceRepository) != "" {
		return strings.TrimSpace(*cfg.SourceRepository)
	}

	if strings.TrimSpace(namespace) == "" {
		namespace = "hashicorp"
	}

	if opendepotv1alpha1.ProviderUpstreamRegistry(cfg) == opendepotv1alpha1.OpenTofuRegistryHost {
		repoURL, err := registry.LookupProviderRepo(ctx, namespace, providerName)
		if err == nil && repoURL != "" {
			return repoURL
		}
	}

	// Fall back to heuristic — scan degrades gracefully rather than blocking sync.
	return fmt.Sprintf("https://github.com/%s/terraform-provider-%s",
		strings.TrimSpace(namespace), strings.TrimSpace(providerName))
}

// extractBinaryFromZip extracts the provider executable from a HashiCorp release zip
// at archivePath on disk. The zip contains exactly one file: the compiled provider
// binary. We skip any accompanying README or LICENSE files by filtering on common
// non-binary suffixes and the absence of the executable bit.
func extractBinaryFromZip(archivePath string) ([]byte, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open provider zip archive: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		name := strings.ToLower(filepath.Base(f.Name))
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".json") {
			continue
		}

		if f.Mode()&0111 == 0 {
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

// scanProviderBinary runs `trivy rootfs` against the provider executable extracted
// from the zip at archivePath on disk. Reading from disk (rather than from a []byte
// held in the Go heap) ensures the full ~700 MB provider archive and the ~2 GB
// Trivy vulnerability DB are never resident in memory simultaneously.
// When offline is true, --offline-scan is passed to Trivy so it does not attempt network calls.
func (r *VersionReconciler) scanProviderBinary(ctx context.Context, archivePath string, cacheDir string, offline bool) ([]opendepotv1alpha1.SecurityFinding, error) {
	binaryBytes, err := extractBinaryFromZip(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract binary from provider archive: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "opendepot-binscan-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for binary scan: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "provider-binary")
	if err := os.WriteFile(binPath, binaryBytes, 0500); err != nil {
		return nil, fmt.Errorf("failed to write provider binary to temp dir: %w", err)
	}

	args := []string{"rootfs", "--format", "json"}
	if offline {
		args = append(args, "--offline-scan", "--skip-db-update")
	}
	args = append(args, "--cache-dir", cacheDir, "--quiet", tmpDir)

	// Serialise Trivy invocations: each process loads the full ~2 GiB DB.
	select {
	case r.scanSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	output, err := runTrivy(ctx, args...)
	<-r.scanSem
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
		args = append(args, "--offline-scan", "--skip-db-update")
	}
	args = append(args, "--cache-dir", cacheDir, "--quiet", tmpDir)

	// Serialise Trivy invocations: each process loads the full ~2 GiB DB.
	select {
	case r.scanSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	output, err := runTrivy(ctx, args...)
	<-r.scanSem
	if err != nil {
		return nil, fmt.Errorf("source scan failed: %w", err)
	}

	// Trivy ran but produced no output (e.g. DB not yet initialized on first
	// offline run). Return a non-nil empty slice so the caller writes a
	// tombstone — dedup then prevents an infinite retry loop.
	if len(output) == 0 {
		return make([]opendepotv1alpha1.SecurityFinding, 0), nil
	}

	return parseTrivyReport(output, func(res trivyResult) bool {
		return strings.HasSuffix(res.Target, "go.mod") || res.Type == "gomod"
	})
}

// runProviderScan orchestrates source and binary Trivy scans for a provider Version.
// archivePath is the path to the provider zip on disk (as written by httpStreamToFile).
// Both results are returned to the caller for atomic persistence alongside the other
// required status fields (checksum, synced, syncStatus).
//
// Source scan deduplication: if Version.Status.SourceScan is already set, the source
// scan is skipped — the result is specific to the provider version's go.mod and does
// not vary across OS/arch variants. Binary scan always runs (unique per OS/arch artifact).
//
// When blockOnCritical or blockOnHigh is true and findings of that severity are present,
// a non-nil error is returned to halt reconciliation.
func (r *VersionReconciler) runProviderScan(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	archivePath string,
	cacheDir string,
	offline bool,
	blockOnCritical bool,
	blockOnHigh bool,
) (string, *opendepotv1alpha1.BinaryScan, *opendepotv1alpha1.SourceScan, error) {
	providerName := version.Labels["opendepot.defdev.io/provider"]
	if providerName == "" {
		return "", nil, nil, nil
	}

	providerNamespace := "hashicorp"
	if version.Spec.ProviderConfigRef != nil && version.Spec.ProviderConfigRef.Namespace != nil {
		if ns := strings.TrimSpace(*version.Spec.ProviderConfigRef.Namespace); ns != "" {
			providerNamespace = ns
		}
	}

	repoURL := resolveProviderSourceRepository(ctx, providerNamespace, providerName, version.Spec.ProviderConfigRef)

	// Binary scan (always runs, unique per OS/arch)
	var binaryScan *opendepotv1alpha1.BinaryScan
	binaryFindings, err := r.scanProviderBinary(ctx, archivePath, cacheDir, offline)
	if err != nil {
		r.Log.Error(err, "Binary scan failed — continuing without scan results",
			"version", version.Name)
	} else {
		now := time.Now().UTC().Format(time.RFC3339)
		binaryScan = &opendepotv1alpha1.BinaryScan{
			ScannedAt: now,
			Findings:  binaryFindings,
		}

		if blockOnCritical || blockOnHigh {
			for _, f := range binaryFindings {
				if blockOnCritical && f.Severity == "CRITICAL" {
					return repoURL, binaryScan, nil, fmt.Errorf("blocking: CRITICAL vulnerability %s in binary (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
				}

				if blockOnHigh && f.Severity == "HIGH" {
					return repoURL, binaryScan, nil, fmt.Errorf("blocking: HIGH vulnerability %s in binary (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
				}
			}
		}
	}

	// Source scan (deduplicated per Version CR: skip if already scanned)
	if version.Status.SourceScan != nil {
		r.Log.V(5).Info("Source scan already present on this Version — skipping",
			"version", version.Name)
		return repoURL, binaryScan, nil, nil
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

	currentVersion := strings.TrimPrefix(version.Spec.Version, "v")
	sourceFindings, err := r.scanProviderSource(ctx, repoURL, currentVersion, cacheDir, offline, ghClient)
	if err != nil {
		r.Log.Error(err, "Source scan failed — continuing without scan results",
			"provider", providerName, "version", currentVersion)
		return repoURL, binaryScan, nil, nil
	}

	// nil findings + nil error = scanProviderSource silently skipped the download
	// (go.mod unavailable, e.g. private repo or transient network error). Return nil
	// so the caller does not persist a result — the next reconcile will retry.
	if sourceFindings == nil {
		return repoURL, binaryScan, nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sourceScan := &opendepotv1alpha1.SourceScan{
		ScannedAt: now,
		Findings:  sourceFindings,
	}

	if blockOnCritical || blockOnHigh {
		for _, f := range sourceFindings {
			if blockOnCritical && f.Severity == "CRITICAL" {
				return repoURL, binaryScan, sourceScan, fmt.Errorf("blocking: CRITICAL vulnerability %s in source (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
			}

			if blockOnHigh && f.Severity == "HIGH" {
				return repoURL, binaryScan, sourceScan, fmt.Errorf("blocking: HIGH vulnerability %s in source (%s %s)", f.VulnerabilityID, f.PkgName, f.InstalledVersion)
			}
		}
	}

	return repoURL, binaryScan, sourceScan, nil
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

// extractReadmeFromArchive scans a module archive (zip or gzip tarball) in-memory for a README
// file and returns its raw content. It matches case-insensitively against "readme" with any
// extension (or none), at the archive root or one path segment deep (GitHub tarballs nest all
// content under a single "<repo>-<sha>/" wrapper directory). Returns nil, nil if no README entry
// is found; this is a non-fatal condition for callers.
func extractReadmeFromArchive(archiveBytes []byte) ([]byte, error) {
	if zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err == nil {
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || !isReadmeEntry(f.Name) {
				continue
			}

			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open zip entry %s: %w", f.Name, err)
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("failed to read zip entry %s: %w", f.Name, err)
			}

			return data, nil
		}

		return nil, nil
	}

	gr, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, fmt.Errorf("archive is neither a valid zip nor a gzip tarball: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read tar entry: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg || !isReadmeEntry(hdr.Name) {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("failed to read tar entry %s: %w", hdr.Name, err)
		}

		return data, nil
	}

	return nil, nil
}

// isReadmeEntry reports whether the archive entry path is a README file at the archive root
// or one path segment deep (to account for GitHub's "<repo>-<sha>/" tarball wrapper directory).
func isReadmeEntry(name string) bool {
	segments := strings.Split(strings.Trim(name, "/"), "/")
	if len(segments) > 2 {
		return false
	}

	base := segments[len(segments)-1]
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.EqualFold(base, "readme")
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

	// --scanners misconfig is required: trivy fs defaults to vuln,secret only.
	// Config-class (IaC) rules are bundled in the Trivy binary and do not need
	// the vulnerability DB, so this works correctly with --offline-scan.
	args := []string{"fs", "--format", "json", "--scanners", "misconfig", tmpDir}

	// Serialise Trivy invocations: each process loads the full ~2 GiB DB.
	select {
	case r.scanSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	output, err := runTrivy(ctx, args...)
	<-r.scanSem
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
) (*opendepotv1alpha1.SourceScan, error) {
	findings, err := r.scanModuleArchive(ctx, archiveBytes, cacheDir, offline)
	if err != nil {
		r.Log.Error(err, "Module source scan failed — continuing without scan results",
			"version", version.Name)
		return nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sourceScan := &opendepotv1alpha1.SourceScan{
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
