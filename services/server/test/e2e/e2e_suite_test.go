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
	// serverImage is the server image to deploy for e2e tests.
	// Override with the IMG environment variable.
	serverImage = func() string {
		if img := os.Getenv("IMG"); img != "" {
			return img
		}
		return "server:e2e-test"
	}()
)

const (
	// helmReleaseName is the Helm release name used throughout all server e2e tests.
	helmReleaseName = "opendepot"
	// namespace is the namespace where all opendepot resources live.
	namespace = "opendepot-system"
)

// TestE2E runs the end-to-end test suite for the server.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting opendepot server e2e test suite\n")
	RunSpecs(t, "server e2e suite")
}

var _ = BeforeSuite(func() {
	repoRoot, err := utils.GetRepoRoot()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to determine repo root")

	if os.Getenv("SKIP_IMAGE_BUILD") != "true" {
		serverHash, err := utils.ComputeBuildContextHash(repoRoot, []string{
			"services/server",
			"api",
			"pkg",
			"go.work",
			"go.work.sum",
		})
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to compute server build hash")

		if utils.NeedsRebuild(serverImage, serverHash) {
			By("building the server image (context changed or image absent)")
			buildCmd := exec.Command("docker", "build",
				"-t", serverImage,
				"--label", "opendepot.build.hash="+serverHash,
				"-f", "services/server/Dockerfile",
				".",
			)
			_, err = utils.RunAt(buildCmd, repoRoot)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the server image")
		} else {
			By("server image up-to-date, skipping build")
		}
	} else {
		By("SKIP_IMAGE_BUILD=true: skipping image build, using pre-built image")
	}

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
})

var _ = AfterSuite(func() {
	By("uninstalling Helm release to clean up server e2e resources")
	cmd := exec.Command("helm", "uninstall", helmReleaseName,
		"--namespace", namespace,
		"--ignore-not-found",
	)
	_, _ = utils.Run(cmd)
})
