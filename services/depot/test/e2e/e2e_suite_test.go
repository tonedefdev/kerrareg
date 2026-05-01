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

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/kerrareg/pkg/testutils"
)

var (
	// projectImage is the depot controller image to deploy for e2e tests.
	// Override with the IMG environment variable.
	projectImage = func() string {
		if img := os.Getenv("IMG"); img != "" {
			return img
		}
		return "depot-controller:e2e-test"
	}()
)

const (
	// helmReleaseName is the existing Helm release that owns module/version/server.
	helmReleaseName = "kerrareg"
	// namespace is the namespace where all kerrareg resources live.
	namespace = "kerrareg-system"
)

// TestE2E runs the end-to-end test suite for the depot controller.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting kerrareg depot e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("building the depot controller image")
	repoRoot, err := utils.GetRepoRoot()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to determine repo root")
	buildCmd := exec.Command("docker", "build",
		"-t", projectImage,
		"-f", "services/depot/Dockerfile",
		".",
	)
	_, err = utils.RunAt(buildCmd, repoRoot)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the depot controller image")

	By("loading the depot controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the depot controller image into Kind")

	By("ensuring all chart CRDs are installed")
	allCRDsPath := filepath.Join(repoRoot, "chart", "kerrareg", "crds")
	cmd := exec.Command("kubectl", "apply", "--server-side", "--force-conflicts", "-f", allCRDsPath)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply chart CRDs")

	By("ensuring namespace exists")
	cmd = exec.Command("kubectl", "create", "namespace", namespace)
	_, _ = utils.Run(cmd) // ignore error if namespace already exists

	By("upgrading Helm release to deploy depot controller with local image")
	chartPath, err := utils.GetChartPath()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	depotRepo, depotTag := splitImageRef(projectImage)

	cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
		"--install",
		"--create-namespace",
		"--namespace", namespace,
		"--skip-crds",
		"--set", "depot.enabled=true",
		"--set", fmt.Sprintf("depot.image.repository=%s", depotRepo),
		"--set", fmt.Sprintf("depot.image.tag=%s", depotTag),
		"--set", "module.enabled=true",
		"--set", "provider.enabled=false",
		"--set", "server.anonymousAuth=true",
		// Enable filesystem storage with a hostPath volume for Kind.
		"--set", "storage.filesystem.enabled=true",
		"--set", "storage.filesystem.hostPath=/data/modules",
		"--wait",
		"--timeout", "3m",
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to upgrade Helm release")
})

var _ = AfterSuite(func() {
	By("resetting Helm release to remove depot e2e overrides")
	chartPath, err := utils.GetChartPath()
	if err == nil {
		cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", namespace,
			"--reuse-values",
			"--set", "depot.enabled=true",
			"--set", fmt.Sprintf("depot.image.repository=%s", "ghcr.io/tonedefdev/kerrareg/depot-controller"),
			"--set", "depot.image.tag=",
			"--set", "server.anonymousAuth=false",
			"--set", "storage.filesystem.enabled=false",
			"--set", "storage.filesystem.hostPath=",
			"--wait",
			"--timeout", "2m",
		)
		_, _ = utils.Run(cmd)
	}
})

// splitImageRef splits an image reference "repo:tag" into its components.
// If no tag is present, "latest" is returned as the tag.
func splitImageRef(ref string) (repo, tag string) {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, "latest"
}
