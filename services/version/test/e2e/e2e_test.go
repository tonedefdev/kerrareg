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

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// namespace where the project is deployed in.
const namespace = "opendepot-system"

var _ = Describe("Version", Ordered, func() {
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

	Context("Version CR", Ordered, func() {
		const (
			versionCRName = "version-e2e-dual-config"
			storageDir    = "/data/modules"
			providerName  = "null"
			testFileName  = "null.zip"
			testVersion   = "3.2.3"
		)

		AfterAll(func() {
			By("removing the test Version CR")
			cmd := exec.Command("kubectl", "delete", "version", versionCRName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should add the opendepot finalizer to a Version CR", func() {
			By("applying a Provider-type Version CR with both moduleConfigRef and providerConfigRef")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Provider
  version: %q
  fileName: %q
  providerConfigRef:
    name: %q
    storageConfig:
      fileSystem:
        directoryPath: %s
  moduleConfigRef:
    storageConfig:
      fileSystem:
        directoryPath: %s
`, versionCRName, namespace, testVersion, testFileName, providerName, storageDir, storageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "test-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Version CR")

			By("waiting for the opendepot finalizer to be added")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.finalizers}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("opendepot.defdev.io/finalizer"),
					"expected opendepot finalizer to be present on the Version CR")
			}).Should(Succeed())
		})

		It("should not successfully sync a Version CR with conflicting moduleConfigRef and providerConfigRef", func() {
			By("waiting for the dual-config syncStatus message to be written")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"-o", "jsonpath={.status.syncStatus}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("both are defined"),
					"expected dual-config guard syncStatus message to be written")
			}).Should(Succeed())

			By("confirming the Version CR never reaches synced=true")
			Consistently(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(Equal("true"),
					"Version CR with both moduleConfigRef and providerConfigRef must not sync successfully")
			}, 15*time.Second, 3*time.Second).Should(Succeed())
		})

		It("should fully remove the Version CR after deletion", func() {
			By("deleting the Version CR")
			cmd := exec.Command("kubectl", "delete", "version", versionCRName,
				"-n", namespace,
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete Version CR")

			By("waiting for the Version CR to be fully removed")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"--ignore-not-found",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty(), "Version CR should be fully removed")
			}, 2*time.Minute).Should(Succeed())
		})
	})
})
