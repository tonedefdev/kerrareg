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

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
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

	// versionImage is the version controller image to deploy for e2e tests.
	versionImage = "version-controller:e2e-test"

	// serverImage is the server image to deploy for e2e tests.
	serverImage = "server:e2e-test"
)

const (
	// helmReleaseName is the existing Helm release that owns module/version/server.
	helmReleaseName = "opendepot"
	// namespace is the namespace where all opendepot resources live.
	namespace = "opendepot-system"
)

// TestE2E runs the end-to-end test suite for the depot controller.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting opendepot depot e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	repoRoot, err := utils.GetRepoRoot()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to determine repo root")

	if _, inspectErr := exec.Command("docker", "image", "inspect", projectImage).Output(); inspectErr != nil {
		By("building the depot controller image")
		buildCmd := exec.Command("docker", "build",
			"-t", projectImage,
			"-f", "services/depot/Dockerfile",
			".",
		)
		_, err = utils.RunAt(buildCmd, repoRoot)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the depot controller image")
	} else {
		By("depot controller image already present, skipping build")
	}

	By("loading the depot controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the depot controller image into Kind")

	if _, inspectErr := exec.Command("docker", "image", "inspect", versionImage).Output(); inspectErr != nil {
		By("building the version controller image")
		versionBuildCmd := exec.Command("docker", "build",
			"-t", versionImage,
			"-f", "services/version/Dockerfile",
			".",
		)
		_, err = utils.RunAt(versionBuildCmd, repoRoot)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the version controller image")
	} else {
		By("version controller image already present, skipping build")
	}

	if _, inspectErr := exec.Command("docker", "image", "inspect", serverImage).Output(); inspectErr != nil {
		By("building the server image")
		serverBuildCmd := exec.Command("docker", "build",
			"-t", serverImage,
			"-f", "services/server/Dockerfile",
			".",
		)
		_, err = utils.RunAt(serverBuildCmd, repoRoot)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the server image")
	} else {
		By("server image already present, skipping build")
	}

	By("loading the version controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(versionImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the version controller image into Kind")

	By("loading the server image on Kind")
	err = utils.LoadImageToKindClusterWithName(serverImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the server image into Kind")

	By("ensuring all chart CRDs are installed")
	allCRDsPath := filepath.Join(repoRoot, "chart", "opendepot", "crds")
	cmd := exec.Command("kubectl", "apply", "--server-side", "--force-conflicts", "-f", allCRDsPath)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply chart CRDs")

	By("ensuring namespace exists")
	cmd = exec.Command("kubectl", "create", "namespace", namespace)
	_, _ = utils.Run(cmd) // ignore error if namespace already exists

	By("creating the Valkey authentication secret")
	err = utils.EnsureValkeyAuthSecret(namespace)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create Valkey authentication secret")

	By("upgrading Helm release to deploy depot controller with local image")
	chartPath, err := utils.GetChartPath()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	depotRepo, depotTag := utils.SplitImageRef(projectImage)
	versionRepo, versionTag := utils.SplitImageRef(versionImage)
	serverRepo, serverTag := utils.SplitImageRef(serverImage)

	cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
		"--install",
		"--create-namespace",
		"--namespace", namespace,
		"--skip-crds",
		"--set", "global.image.tag=",
		"--set", "depot.enabled=true",
		"--set", fmt.Sprintf("depot.image.repository=%s", depotRepo),
		"--set", fmt.Sprintf("depot.image.tag=%s", depotTag),
		"--set", fmt.Sprintf("version.image.repository=%s", versionRepo),
		"--set", fmt.Sprintf("version.image.tag=%s", versionTag),
		"--set", "module.enabled=false",
		"--set", "provider.enabled=false",
		"--set", "server.anonymousAuth=true",
		"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
		"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
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
	By("uninstalling Helm release to clean up depot e2e resources")
	cmd := exec.Command("helm", "uninstall", helmReleaseName,
		"--namespace", namespace,
		"--ignore-not-found",
	)
	_, _ = utils.Run(cmd)
})
