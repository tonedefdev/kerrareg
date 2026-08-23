/*
Copyright 2026 Anthony Owens.

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

// Package testutils provides shared helpers for OpenDepot e2e test suites.
// All test suites import this package (aliased as "utils") instead of
// maintaining per-service copies of the same code.
package testutils

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2" // nolint:revive,staticcheck
)

const (
	prometheusOperatorVersion = "v0.77.1"
	prometheusOperatorURL     = "https://github.com/prometheus-operator/prometheus-operator/" +
		"releases/download/%s/bundle.yaml"

	certmanagerVersion = "v1.16.3"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"
)

func warnError(err error) {
	_, _ = fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// Run executes the provided command from the project directory (the service root,
// i.e. the cwd with "/test/e2e" stripped).
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %q\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// RunAt executes the provided command from the given directory.
// Unlike Run, it does not override the directory with GetProjectDir.
func RunAt(cmd *exec.Cmd, dir string) (string, error) {
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()

	cmd.Dir = dir
	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %q\n", err)
	}
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}
	return string(output), nil
}

// LoadImageToKindClusterWithName loads a local docker image to the kind cluster.
// The cluster name defaults to "kind" and can be overridden via the KIND_CLUSTER
// environment variable.
func LoadImageToKindClusterWithName(name string) error {
	cluster := "kind"
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	cmd := exec.Command("kind", kindOptions...)
	_, err := Run(cmd)
	return err
}

// EnsureValkeyAuthSecret creates the Secret required by the chart's secure
// Valkey defaults. E2e clusters use a fixed, non-production test password.
func EnsureValkeyAuthSecret(namespace string) error {
	cmd := exec.Command(
		"kubectl", "create", "secret", "generic", "opendepot-valkey-auth",
		"--namespace", namespace,
		"--from-literal=default=opendepot-e2e-valkey-password",
	)
	_, err := Run(cmd)

	return err
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}
	return res
}

// GetProjectDir returns the service root directory by stripping "/test/e2e" from
// the current working directory. All OpenDepot e2e suites run from
// services/<name>/test/e2e, so this reliably returns services/<name>.
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get current working directory: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}

// GetChartPath returns the absolute path to the opendepot Helm chart
// (chart/opendepot at the repository root).
func GetChartPath() (string, error) {
	projDir, err := GetProjectDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(projDir, "..", "..", "chart", "opendepot"), nil
}

// GetRepoRoot returns the root directory of the opendepot repository.
func GetRepoRoot() (string, error) {
	projDir, err := GetProjectDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(projDir, "..", ".."), nil
}

// InstallPrometheusOperator installs the Prometheus Operator to be used to export enabled metrics.
func InstallPrometheusOperator() error {
	url := fmt.Sprintf(prometheusOperatorURL, prometheusOperatorVersion)
	cmd := exec.Command("kubectl", "create", "-f", url)
	_, err := Run(cmd)
	return err
}

// UninstallPrometheusOperator uninstalls the Prometheus Operator.
func UninstallPrometheusOperator() {
	url := fmt.Sprintf(prometheusOperatorURL, prometheusOperatorVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// IsPrometheusCRDsInstalled checks if any Prometheus CRDs are installed by
// verifying the existence of key CRDs related to Prometheus.
func IsPrometheusCRDsInstalled() bool {
	prometheusCRDs := []string{
		"prometheuses.monitoring.coreos.com",
		"prometheusrules.monitoring.coreos.com",
		"prometheusagents.monitoring.coreos.com",
	}

	cmd := exec.Command("kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range prometheusCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}
	return false
}

// NeedsRebuild returns true if the image is absent or its opendepot.build.hash
// label does not match wantHash.
func NeedsRebuild(image, wantHash string) bool {
	out, err := exec.Command(
		"docker", "inspect",
		"--format", `{{ index .Config.Labels "opendepot.build.hash" }}`,
		image,
	).Output()
	if err != nil {
		return true // image absent
	}
	return strings.TrimSpace(string(out)) != wantHash
}

// ComputeBuildContextHash computes a SHA-256 hash over the contents of all
// git-tracked files under the given paths (relative to repoRoot). This
// produces a deterministic fingerprint of the Docker build context without
// requiring a build.
func ComputeBuildContextHash(repoRoot string, paths []string) (string, error) {
	args := append([]string{"-C", repoRoot, "ls-files", "--"}, paths...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git ls-files: %w", err)
	}
	h := sha256.New()
	for rel := range strings.FieldsSeq(string(out)) {
		data, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", rel, err)
		}
		fmt.Fprintf(h, "%s\n", rel)
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16], nil
}

// SplitImageRef splits an image reference "repo:tag" into its components.
// If no tag is present, "latest" is returned as the tag.
func SplitImageRef(ref string) (repo, tag string) {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, "latest"
}
