/*
Copyright 2025 Anthony Owens.

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

	"github.com/tonedefdev/kerrareg/services/provider/test/utils"
)

var (
	// projectImage is the provider controller image to deploy for e2e tests.
	// Override with the IMG environment variable.
	projectImage = func() string {
		if img := os.Getenv("IMG"); img != "" {
			return img
		}
		return "provider-controller:e2e-test"
	}()

	// versionImage is the version controller image to deploy for e2e tests.
	versionImage = "version-controller:e2e-test"

	// serverImage is the server image to deploy for e2e tests.
	serverImage = "server:e2e-test"

	// gpgHome is the temp directory used as GNUPGHOME for the test key pair.
	gpgHome string
)

const (
	// helmReleaseName is the existing Helm release that owns module/version/server.
	helmReleaseName = "kerrareg"
	// gpgSecretName is the k8s Secret created to hold the test GPG keys.
	gpgSecretName = "kerrareg-provider-gpg-test"
)

// TestE2E runs the end-to-end test suite for the provider controller.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting kerrareg provider e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("building the provider controller image")
	repoRoot, err := utils.GetRepoRoot()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to determine repo root")
	buildCmd := exec.Command("docker", "build",
		"-t", projectImage,
		"-f", "services/provider/Dockerfile",
		".",
	)
	_, err = utils.RunAt(buildCmd, repoRoot)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the provider controller image")

	By("building the version controller image")
	versionBuildCmd := exec.Command("docker", "build",
		"-t", versionImage,
		"-f", "services/version/Dockerfile",
		".",
	)
	_, err = utils.RunAt(versionBuildCmd, repoRoot)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the version controller image")

	By("building the server image")
	serverBuildCmd := exec.Command("docker", "build",
		"-t", serverImage,
		"-f", "services/server/Dockerfile",
		".",
	)
	_, err = utils.RunAt(serverBuildCmd, repoRoot)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the server image")

	By("loading the provider controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the provider controller image into Kind")

	By("loading the version controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(versionImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the version controller image into Kind")

	By("loading the server image on Kind")
	err = utils.LoadImageToKindClusterWithName(serverImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the server image into Kind")

	By("ensuring all chart CRDs are installed")
	allCRDsPath := filepath.Join(repoRoot, "chart", "kerrareg", "crds")
	cmd := exec.Command("kubectl", "apply", "--server-side", "--force-conflicts", "-f", allCRDsPath)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply chart CRDs")

	By("generating test GPG key pair")
	gpgHome, err = os.MkdirTemp("", "kerrareg-e2e-gpg-*")
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create temp GPG home")

	keyID, asciiArmor, privateKeyBase64, err := utils.GenerateTestGPGKeyPair(gpgHome)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to generate test GPG key pair")

	By("ensuring namespace exists before creating secrets")
	cmd = exec.Command("kubectl", "create", "namespace", namespace)
	_, _ = utils.Run(cmd) // ignore error if namespace already exists

	By("creating GPG secret in cluster")
	cmd = exec.Command("kubectl", "create", "secret", "generic", gpgSecretName,
		"--namespace", namespace,
		fmt.Sprintf("--from-literal=KERRAREG_PROVIDER_GPG_KEY_ID=%s", keyID),
		fmt.Sprintf("--from-literal=KERRAREG_PROVIDER_GPG_ASCII_ARMOR=%s", asciiArmor),
		fmt.Sprintf("--from-literal=KERRAREG_PROVIDER_GPG_PRIVATE_KEY_BASE64=%s", privateKeyBase64),
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create GPG secret")

	By("upgrading Helm release to add provider controller and configure GPG signing")
	chartPath, err := utils.GetChartPath()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	// Parse the repository and tag from projectImage (format: "repo:tag").
	providerRepo, providerTag := splitImageRef(projectImage)
	versionRepo, versionTag := splitImageRef(versionImage)
	serverRepo, serverTag := splitImageRef(serverImage)

	cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
		"--install",
		"--create-namespace",
		"--namespace", namespace,
		"--skip-crds",
		"--set", "provider.enabled=true",
		"--set", fmt.Sprintf("provider.image.repository=%s", providerRepo),
		"--set", fmt.Sprintf("provider.image.tag=%s", providerTag),
		"--set", fmt.Sprintf("version.image.repository=%s", versionRepo),
		"--set", fmt.Sprintf("version.image.tag=%s", versionTag),
		"--set", fmt.Sprintf("server.gpg.secretName=%s", gpgSecretName),
		"--set", "server.anonymousAuth=true",
		"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
		"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
		// Enable filesystem storage with a hostPath volume for Kind (no ReadWriteMany SC available).
		"--set", "storage.filesystem.enabled=true",
		"--set", "storage.filesystem.hostPath=/data/modules",
		// Increase version controller memory limit to handle large provider binaries (AWS ~700MB).
		"--set", "version.resources.limits.memory=2Gi",
		"--wait",
		"--timeout", "3m",
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to upgrade Helm release")
})

var _ = AfterSuite(func() {
	By("removing provider controller from Helm release")
	chartPath, err := utils.GetChartPath()
	if err == nil {
		cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", namespace,
			"--reuse-values",
			"--set", "provider.enabled=false",
			"--set", "server.gpg.secretName=",
			"--set", "server.anonymousAuth=false",
			"--set", fmt.Sprintf("server.image.repository=%s", "ghcr.io/tonedefdev/kerrareg/server"),
			"--set", "server.image.tag=",
			"--set", fmt.Sprintf("version.image.repository=%s", "ghcr.io/tonedefdev/kerrareg/version-controller"),
			"--set", "version.image.tag=",
			"--set", "storage.filesystem.enabled=false",
			"--set", "storage.filesystem.hostPath=",
			"--set", "version.resources.limits.memory=512Mi",
			"--wait",
			"--timeout", "2m",
		)
		_, _ = utils.Run(cmd)
	}

	By("deleting GPG secret")
	cmd := exec.Command("kubectl", "delete", "secret", gpgSecretName,
		"--namespace", namespace, "--ignore-not-found",
	)
	_, _ = utils.Run(cmd)

	By("cleaning up temp GPG home")
	if gpgHome != "" {
		os.RemoveAll(gpgHome)
	}
})

// splitImageRef splits an image reference "repo:tag" into its components.
// If no tag is present, "latest" is returned as the tag.
func splitImageRef(ref string) (repo, tag string) {
	lastColon := -1
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			lastColon = i
			break
		}
	}
	if lastColon < 0 {
		return ref, "latest"
	}
	return ref[:lastColon], ref[lastColon+1:]
}
