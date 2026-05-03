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
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// namespace where the project is deployed in.
const namespace = "opendepot-system"

var _ = Describe("Module", Ordered, func() {
	const (
		moduleNamespace       = "opendepot-system"
		serverPortForwardPort = "18080"
		// OpenTofu requires a module registry hostname with at least one dot.
		// opendepot.localtest.me resolves to 127.0.0.1 via public DNS (localtest.me service).
		registryHost        = "opendepot.localtest.me:18080"
		moduleCRName        = "terraform-aws-key-pair"
		moduleVersion       = "2.0.0"
		moduleVersionCRName = "terraform-aws-key-pair-2.0.0"
		moduleProvider      = "aws"
		moduleStoragePath   = "/data/modules"
	)

	var pfCancel context.CancelFunc

	BeforeAll(func() {
		By("applying the test Module CR")
		moduleYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
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
`, moduleCRName, moduleNamespace, moduleProvider, moduleStoragePath, moduleVersion)

		moduleFile := filepath.Join(GinkgoT().TempDir(), "test-module.yaml")
		Expect(os.WriteFile(moduleFile, []byte(moduleYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", moduleFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply test Module CR")

		By("starting port-forward to the opendepot server")
		pfCtx, cancel := context.WithCancel(context.Background())
		pfCancel = cancel
		pfCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
			"svc/server",
			fmt.Sprintf("%s:80", serverPortForwardPort),
			"-n", moduleNamespace,
		)
		Expect(pfCmd.Start()).To(Succeed(), "Failed to start port-forward")
		// Allow port-forward to establish.
		time.Sleep(3 * time.Second)
	})

	AfterAll(func() {
		if pfCancel != nil {
			pfCancel()
		}
		cmd := exec.Command("kubectl", "delete", "module", moduleCRName,
			"-n", moduleNamespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)
	})

	It("should create Version CRs for the module", func() {
		By("waiting for Version CRs to be created")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "versions",
				"-l", fmt.Sprintf("opendepot.defdev.io/module=%s", moduleCRName),
				"-n", moduleNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			lines := utils.GetNonEmptyLines(output)
			g.Expect(lines).NotTo(BeEmpty(), "expected at least one Version CR")
		}, 60*time.Second, 3*time.Second).Should(Succeed())
	})

	It("should sync the module artifact", func() {
		By("waiting for Version CR to report synced=true (downloads from GitHub)")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", moduleVersionCRName,
				"-n", moduleNamespace,
				"-o", `jsonpath={.status.synced}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 5*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should serve module registry API endpoints", func() {
		// The module sync downloads from GitHub, which can take several minutes.
		// The port-forward may have died during that wait, so restart it here.
		By("refreshing port-forward after artifact sync")
		if pfCancel != nil {
			pfCancel()
		}
		time.Sleep(2 * time.Second)
		pfCtx, pfCancelNew := context.WithCancel(context.Background())
		pfCancel = pfCancelNew
		pfRefreshCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
			"svc/server",
			fmt.Sprintf("%s:80", serverPortForwardPort),
			"-n", moduleNamespace,
		)
		Expect(pfRefreshCmd.Start()).To(Succeed(), "Failed to restart port-forward before API tests")

		base := fmt.Sprintf("http://localhost:%s", serverPortForwardPort)

		By("waiting for port-forward to become ready")
		Eventually(func() error {
			resp, err := http.Get(base + "/.well-known/terraform.json") //nolint:noctx
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			return nil
		}, 30*time.Second, 2*time.Second).Should(Succeed(), "port-forward did not become ready within 30s")

		By("checking /.well-known/terraform.json")
		body := httpGetBody(base + "/.well-known/terraform.json")
		Expect(body).To(ContainSubstring("modules.v1"))

		By("checking module versions list endpoint")
		body = httpGetBody(fmt.Sprintf("%s/opendepot/modules/v1/%s/%s/%s/versions",
			base, moduleNamespace, moduleCRName, moduleProvider))
		Expect(body).To(ContainSubstring(moduleVersion))

		By("checking module download endpoint returns X-Terraform-Get header")
		resp := httpGetRaw(fmt.Sprintf("%s/opendepot/modules/v1/%s/%s/%s/%s/download",
			base, moduleNamespace, moduleCRName, moduleProvider, moduleVersion))
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		Expect(resp.Header.Get("X-Terraform-Get")).To(ContainSubstring("/opendepot/modules/v1/download/"))
	})

	It("should successfully run tofu init against the opendepot registry", func() {
		By("creating a temp directory with a Terraform config")
		tmpDir := GinkgoT().TempDir()

		mainTF := fmt.Sprintf(`module "key_pair" {
  source  = "%s/%s/%s/%s"
  version = "%s"
}
`, registryHost, moduleNamespace, moduleCRName, moduleProvider, moduleVersion)
		Expect(os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())

		By("writing a .tofurc to point at the local module registry")
		tofuRC := fmt.Sprintf(`host "%s" {
  services = {
    "modules.v1" = "http://%s/opendepot/modules/v1/"
  }
}
`, registryHost, registryHost)
		tofuRCPath := filepath.Join(tmpDir, ".tofurc")
		Expect(os.WriteFile(tofuRCPath, []byte(tofuRC), 0600)).To(Succeed())

		By("running tofu init")
		cmd := exec.Command("tofu", "init", "-no-color")
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("TF_CLI_CONFIG_FILE=%s", tofuRCPath),
		)
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"tofu init failed; output:\n%s", string(output))
		Expect(string(output)).To(ContainSubstring("successfully initialized"))
	})

	It("should enforce Kubernetes RBAC when anonymousAuth is disabled", func() {
		const (
			authTestSA   = "opendepot-e2e-reader"
			authTestRole = "opendepot-e2e-reader"
			authTestRB   = "opendepot-e2e-reader"
		)

		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("restoring anonymous auth after auth test")
			restoreCmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--namespace", moduleNamespace,
				"--reuse-values",
				"--set", "server.anonymousAuth=true",
				"--set", "server.useBearerToken=false",
				"--wait",
				"--timeout", "2m",
			)
			_, _ = utils.Run(restoreCmd)

			By("restarting port-forward after restoring anonymous auth")
			if pfCancel != nil {
				pfCancel()
			}
			time.Sleep(2 * time.Second)
			pfCtx, cancel := context.WithCancel(context.Background())
			pfCancel = cancel
			pfRestoreCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
				"svc/server",
				fmt.Sprintf("%s:80", serverPortForwardPort),
				"-n", moduleNamespace,
			)
			_ = pfRestoreCmd.Start()
			time.Sleep(3 * time.Second)

			By("cleaning up auth test RBAC")
			_, _ = utils.Run(exec.Command("kubectl", "delete", "rolebinding", authTestRB, "-n", moduleNamespace, "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "role", authTestRole, "-n", moduleNamespace, "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "serviceaccount", authTestSA, "-n", moduleNamespace, "--ignore-not-found"))
		})

		By("disabling anonymous auth via Helm upgrade")
		cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", moduleNamespace,
			"--reuse-values",
			"--set", "server.anonymousAuth=false",
			"--set", "server.useBearerToken=true",
			"--wait",
			"--timeout", "2m",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to disable anonymous auth")

		By("restarting port-forward after server pod restart")
		if pfCancel != nil {
			pfCancel()
		}
		time.Sleep(2 * time.Second)
		pfCtx, pfCancelNew := context.WithCancel(context.Background())
		pfCancel = pfCancelNew
		pfCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
			"svc/server",
			fmt.Sprintf("%s:80", serverPortForwardPort),
			"-n", moduleNamespace,
		)
		Expect(pfCmd.Start()).To(Succeed(), "Failed to restart port-forward")
		time.Sleep(3 * time.Second)

		By("verifying unauthenticated request returns 401")
		unauthResp, err := http.Get(fmt.Sprintf( //nolint:noctx
			"http://localhost:%s/opendepot/modules/v1/%s/%s/%s/versions",
			serverPortForwardPort, moduleNamespace, moduleCRName, moduleProvider))
		Expect(err).NotTo(HaveOccurred())
		_ = unauthResp.Body.Close()
		Expect(unauthResp.StatusCode).To(Equal(http.StatusUnauthorized))

		By("creating a read-only ServiceAccount and RBAC for the auth test")
		_, _ = utils.Run(exec.Command("kubectl", "create", "serviceaccount", authTestSA, "-n", moduleNamespace))
		_, _ = utils.Run(exec.Command("kubectl", "create", "role", authTestRole,
			"-n", moduleNamespace,
			"--resource=modules.opendepot.defdev.io,versions.opendepot.defdev.io",
			"--verb=get,list,watch",
		))
		_, _ = utils.Run(exec.Command("kubectl", "create", "rolebinding", authTestRB,
			"-n", moduleNamespace,
			fmt.Sprintf("--role=%s", authTestRole),
			fmt.Sprintf("--serviceaccount=%s:%s", moduleNamespace, authTestSA),
		))

		By("generating a short-lived ServiceAccount token")
		tokenOutput, err := utils.Run(exec.Command("kubectl", "create", "token", authTestSA,
			"-n", moduleNamespace,
			"--duration=1h",
		))
		Expect(err).NotTo(HaveOccurred())
		token := strings.TrimSpace(tokenOutput)
		Expect(token).NotTo(BeEmpty())

		By("running tofu init with bearer token authentication")
		tmpDir := GinkgoT().TempDir()
		mainTF := fmt.Sprintf(`module "key_pair" {
  source  = "%s/%s/%s/%s"
  version = "%s"
}
`, registryHost, moduleNamespace, moduleCRName, moduleProvider, moduleVersion)
		Expect(os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())

		tofuRC := fmt.Sprintf(`host "%s" {
  services = {
    "modules.v1" = "http://%s/opendepot/modules/v1/"
  }
}
credentials "%s" {
  token = "%s"
}
`, registryHost, registryHost, registryHost, token)
		tofuRCPath := filepath.Join(tmpDir, ".tofurc")
		Expect(os.WriteFile(tofuRCPath, []byte(tofuRC), 0600)).To(Succeed())

		iniCmd := exec.Command("tofu", "init", "-no-color")
		iniCmd.Dir = tmpDir
		iniCmd.Env = append(os.Environ(),
			fmt.Sprintf("TF_CLI_CONFIG_FILE=%s", tofuRCPath),
		)
		output, initErr := iniCmd.CombinedOutput()
		Expect(initErr).NotTo(HaveOccurred(),
			"tofu init with auth failed; output:\n%s", string(output))
		Expect(string(output)).To(ContainSubstring("successfully initialized"))
	})
})

var _ = Describe("Module Scanning", Ordered, func() {
	const (
		scanNamespace      = "opendepot-system"
		scanModuleCRName   = "terraform-aws-key-pair"
		scanModuleVersion  = "2.0.0"
		scanVersionCRName  = "terraform-aws-key-pair-2.0.0"
		scanModuleProvider = "aws"
		scanStoragePath    = "/data/modules"
	)

	BeforeAll(func() {
		By("upgrading Helm release to enable scanning with scanModules=true")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())

		cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", scanNamespace,
			"--reuse-values",
			"--set", "scanning.enabled=true",
			"--set", "scanning.scanModules=true",
			"--set", "scanning.offline=false",
			// Kind's default storage class only supports ReadWriteOnce.
			"--set", "scanning.cache.accessMode=ReadWriteOnce",
			// Extra memory headroom for the version controller running Trivy.
			"--set", "version.resources.limits.memory=1Gi",
			"--wait",
			"--timeout", "3m",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to upgrade Helm release with module scanning enabled")

		By("applying the test Module CR")
		moduleYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
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
`, scanModuleCRName, scanNamespace, scanModuleProvider, scanStoragePath, scanModuleVersion)

		moduleFile := filepath.Join(GinkgoT().TempDir(), "scan-module.yaml")
		Expect(os.WriteFile(moduleFile, []byte(moduleYAML), 0600)).To(Succeed())
		cmd = exec.Command("kubectl", "apply", "-f", moduleFile)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply scan Module CR")
	})

	AfterAll(func() {
		cmd := exec.Command("kubectl", "delete", "module", scanModuleCRName,
			"-n", scanNamespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)

		By("disabling scanning to restore baseline state for other test blocks")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())
		cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", scanNamespace,
			"--reuse-values",
			"--set", "scanning.enabled=false",
			"--wait",
			"--timeout", "3m",
		)
		_, _ = utils.Run(cmd)
	})

	It("should sync the module Version CR", func() {
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", scanVersionCRName,
				"-n", scanNamespace,
				"-o", "jsonpath={.status.synced}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 5*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should populate sourceScan on the Version CR", func() {
		// Trivy downloads its vulnerability DB on first run (~200 MB); allow generous time.
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", scanVersionCRName,
				"-n", scanNamespace,
				"-o", "jsonpath={.status.sourceScan.scannedAt}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "sourceScan.scannedAt should be set after scan completes")
		}, 10*time.Minute, 15*time.Second).Should(Succeed())
	})
})

// httpGetBody performs an HTTP GET to the given URL and returns the response body as a string.
// The test fails immediately if the request returns a non-2xx status.
func httpGetBody(url string) string {
	resp, err := http.Get(url) //nolint:noctx
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "HTTP GET failed for %s", url)
	defer resp.Body.Close()
	ExpectWithOffset(1, resp.StatusCode).To(BeNumerically(">=", 200),
		"unexpected status %d for %s", resp.StatusCode, url)
	ExpectWithOffset(1, resp.StatusCode).To(BeNumerically("<", 300),
		"unexpected status %d for %s", resp.StatusCode, url)
	body, err := io.ReadAll(resp.Body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return string(body)
}

// httpGetRaw performs an HTTP GET and returns the raw response (without reading the body).
// The caller is responsible for closing resp.Body.
func httpGetRaw(url string) *http.Response {
	resp, err := http.Get(url) //nolint:noctx
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "HTTP GET failed for %s", url)
	return resp
}
