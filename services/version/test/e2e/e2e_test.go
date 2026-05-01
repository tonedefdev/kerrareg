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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/kerrareg/pkg/testutils"
)

// namespace where the project is deployed in.
const namespace = "kerrareg-system"

var _ = Describe("Version", Ordered, func() {
	const (
		moduleCRName      = "terraform-aws-key-pair"
		moduleVersion     = "2.0.0"
		versionCRName     = "terraform-aws-key-pair-2.0.0"
		moduleProvider    = "aws"
		moduleStoragePath = "/data/modules"
	)

	var controllerPodName string

	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			if controllerPodName != "" {
				By("Fetching version controller pod logs")
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				controllerLogs, err := utils.Run(cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Version controller logs:\n %s", controllerLogs)
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get version controller logs: %s", err)
				}
			}

			By("Fetching Kubernetes events")
			cmd := exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Controller", func() {
		It("should run successfully", func() {
			By("validating that the version-controller pod is running")
			verifyControllerUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "app=version-controller",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)
				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve version-controller pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 version-controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("version-controller"))

				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect version-controller pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})
	})

	Context("Version CR", func() {
		AfterAll(func() {
			By("removing the test Module CR")
			cmd := exec.Command("kubectl", "delete", "module", moduleCRName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("removing the test Version CR")
			cmd = exec.Command("kubectl", "delete", "version", versionCRName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should reconcile a Version CR created by the Module controller", func() {
			By("applying the test Module CR")
			moduleYAML := fmt.Sprintf(`apiVersion: kerrareg.io/v1alpha1
kind: Module
metadata:
  name: %s
  namespace: %s
spec:
  moduleConfig:
    fileFormat: zip
    githubClientConfig:
      useAuthenticatedClient: false
    provider: %s
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-key-pair
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, moduleCRName, namespace, moduleProvider, moduleStoragePath, moduleVersion)

			moduleFile := filepath.Join(GinkgoT().TempDir(), "test-module.yaml")
			Expect(os.WriteFile(moduleFile, []byte(moduleYAML), 0600)).To(Succeed())
			cmd := exec.Command("kubectl", "apply", "-f", moduleFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Module CR")

			By("waiting for the Version CR to be created by the module controller")
			verifyVersionExists := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
				)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Version CR should exist")
			}
			Eventually(verifyVersionExists, 2*time.Minute).Should(Succeed())

			By("waiting for the Version CR to be synced by the version controller")
			verifyVersionSynced := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"), "Version CR should be synced")
			}
			Eventually(verifyVersionSynced, 5*time.Minute).Should(Succeed())
		})
	})
})
