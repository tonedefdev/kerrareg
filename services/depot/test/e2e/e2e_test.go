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

var _ = Describe("Depot", Ordered, func() {
	const (
		depotNamespace    = "kerrareg-system"
		depotCRName       = "e2e-depot"
		moduleCRName      = "terraform-aws-key-pair"
		moduleVersion     = "2.0.0"
		moduleProvider    = "aws"
		providerCRName    = "random"
		providerVersion   = "3.6.0"
		moduleStoragePath = "/data/modules"
	)

	BeforeAll(func() {
		By("cleaning up any pre-existing Depot, Module, and Provider CRs from previous runs")
		func() {
			cmd := exec.Command("kubectl", "delete", "depot", depotCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "module", moduleCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "provider", providerCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}()

		By("applying the Depot CR")
		depotYAML := fmt.Sprintf(`
apiVersion: kerrareg.io/v1alpha1
kind: Depot
metadata:
  name: %s
  namespace: %s
spec:
  moduleConfigs:
    - name: %s
      provider: %s
      repoOwner: terraform-aws-modules
      versionConstraints: "= %s"
      fileFormat: zip
      storageConfig:
        fileSystem:
          directoryPath: %s
  providerConfigs:
    - name: %s
      operatingSystems:
        - linux
      architectures:
        - amd64
      versionConstraints: "= %s"
      storageConfig:
        fileSystem:
          directoryPath: %s
`, depotCRName, depotNamespace,
			moduleCRName, moduleProvider, moduleVersion, moduleStoragePath,
			providerCRName, providerVersion, moduleStoragePath)

		depotFile := filepath.Join(GinkgoT().TempDir(), "test-depot.yaml")
		Expect(os.WriteFile(depotFile, []byte(depotYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", depotFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply test Depot CR")
	})

	AfterAll(func() {
		cmd := exec.Command("kubectl", "delete", "depot", depotCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "module", moduleCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "provider", providerCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
	})

	It("should create a Module CR from the depot moduleConfigs", func() {
		By("waiting for the Module CR to be created")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", moduleCRName,
				"-n", depotNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "expected Module CR to exist")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct version on the Module CR", func() {
		By("verifying the Module CR has the expected version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", moduleCRName,
				"-n", depotNamespace,
				"-o", fmt.Sprintf(`jsonpath={.spec.versions[?(@.version=="%s")].version}`, moduleVersion),
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(moduleVersion), "expected version %s in Module spec", moduleVersion)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct repoOwner on the Module CR", func() {
		By("verifying the Module CR has the expected repoOwner")
		cmd := exec.Command("kubectl", "get", "module", moduleCRName,
			"-n", depotNamespace,
			"-o", `jsonpath={.spec.moduleConfig.repoOwner}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(Equal("terraform-aws-modules"))
	})

	It("should create a Provider CR from the depot providerConfigs", func() {
		By("waiting for the Provider CR to be created")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "provider", providerCRName,
				"-n", depotNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "expected Provider CR to exist")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct version on the Provider CR", func() {
		By("verifying the Provider CR has the expected version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "provider", providerCRName,
				"-n", depotNamespace,
				"-o", fmt.Sprintf(`jsonpath={.spec.versions[?(@.version=="%s")].version}`, providerVersion),
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(providerVersion), "expected version %s in Provider spec", providerVersion)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct operatingSystems on the Provider CR", func() {
		By("verifying the Provider CR has the expected operatingSystems")
		cmd := exec.Command("kubectl", "get", "provider", providerCRName,
			"-n", depotNamespace,
			"-o", `jsonpath={.spec.providerConfig.operatingSystems[0]}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(Equal("linux"))
	})

	It("should update the Depot status with managed module and provider names", func() {
		By("verifying Depot status.modules contains the module name")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "depot", depotCRName,
				"-n", depotNamespace,
				"-o", `jsonpath={.status.modules}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring(moduleCRName))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Depot status.providers contains the provider name")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "depot", depotCRName,
				"-n", depotNamespace,
				"-o", `jsonpath={.status.providers}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring(providerCRName))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should update an existing Module CR when the Depot is re-reconciled", func() {
		By("patching the Depot to change the version constraint")
		patchYAML := fmt.Sprintf(`{"spec":{"moduleConfigs":[{"name":%q,"provider":%q,"repoOwner":"terraform-aws-modules","versionConstraints":"= %s","fileFormat":"zip","storageConfig":{"fileSystem":{"directoryPath":%q}}}],"providerConfigs":[{"name":%q,"operatingSystems":["linux"],"architectures":["amd64"],"versionConstraints":"= %s","storageConfig":{"fileSystem":{"directoryPath":%q}}}]}}`,
			moduleCRName, moduleProvider, moduleVersion, moduleStoragePath,
			providerCRName, providerVersion, moduleStoragePath)

		cmd := exec.Command("kubectl", "patch", "depot", depotCRName,
			"-n", depotNamespace,
			"--type=merge",
			"-p", patchYAML,
		)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to patch Depot")

		By("verifying the Module CR still has the correct version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", moduleCRName,
				"-n", depotNamespace,
				"-o", fmt.Sprintf(`jsonpath={.spec.versions[?(@.version=="%s")].version}`, moduleVersion),
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(moduleVersion))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})
})

