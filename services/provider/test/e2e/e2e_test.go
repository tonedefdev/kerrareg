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

	utils "github.com/tonedefdev/kerrareg/pkg/testutils"
)

// namespace where the project is deployed in.
const namespace = "kerrareg-system"

var _ = Describe("Provider", Ordered, func() {
	const (
		providerNamespace     = "kerrareg-system"
		serverPortForwardPort = "18080"
		providerCRName        = "aws"
		providerVersion       = "5.80.0"
		providerVersionCRName = "aws-5-80-0-linux-amd64"
		providerStoragePath   = "/data/modules"
	)

	var pfCancel context.CancelFunc

	BeforeAll(func() {
		By("applying the test Provider CR")
		providerYAML := fmt.Sprintf(`
apiVersion: kerrareg.io/v1alpha1
kind: Provider
metadata:
  name: %s
  namespace: %s
spec:
  providerConfig:
    name: %s
    operatingSystems:
      - linux
    architectures:
      - amd64
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, providerCRName, providerNamespace, providerCRName, providerStoragePath, providerVersion)

		providerFile := filepath.Join(GinkgoT().TempDir(), "test-provider.yaml")
		Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", providerFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply test Provider CR")

		By("starting port-forward to the kerrareg server")
		pfCtx, cancel := context.WithCancel(context.Background())
		pfCancel = cancel
		pfCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
			"svc/server",
			fmt.Sprintf("%s:80", serverPortForwardPort),
			"-n", providerNamespace,
		)
		Expect(pfCmd.Start()).To(Succeed(), "Failed to start port-forward")
		// Allow port-forward to establish.
		time.Sleep(3 * time.Second)
	})

	AfterAll(func() {
		if pfCancel != nil {
			pfCancel()
		}
		cmd := exec.Command("kubectl", "delete", "provider", providerCRName,
			"-n", providerNamespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)
	})

	It("should create Version CRs for the provider", func() {
		By("waiting for Version CRs to be created")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "versions",
				"-l", fmt.Sprintf("kerrareg.io/provider=%s", providerCRName),
				"-n", providerNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			lines := utils.GetNonEmptyLines(output)
			g.Expect(lines).NotTo(BeEmpty(), "expected at least one Version CR")
		}, 60*time.Second, 3*time.Second).Should(Succeed())
	})

	It("should sync the provider artifact", func() {
		By("waiting for Version CR to report synced=true (downloads from HashiCorp)")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", providerVersionCRName,
				"-n", providerNamespace,
				"-o", `jsonpath={.status.synced}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 5*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should serve provider registry API endpoints", func() {
		// The provider sync downloads ~700MB, which can take several minutes.
		// The port-forward may have died during that wait, so restart it here.
		By("refreshing port-forward after long sync")
		if pfCancel != nil {
			pfCancel()
		}
		time.Sleep(2 * time.Second)
		pfCtx, pfCancelNew := context.WithCancel(context.Background())
		pfCancel = pfCancelNew
		pfRefreshCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
			"svc/server",
			fmt.Sprintf("%s:80", serverPortForwardPort),
			"-n", providerNamespace,
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
		Expect(body).To(ContainSubstring("providers.v1"))

		By("checking provider versions list endpoint")
		body = httpGetBody(fmt.Sprintf("%s/kerrareg/providers/v1/%s/%s/versions",
			base, providerNamespace, providerCRName))
		Expect(body).To(ContainSubstring(providerVersion))

		By("checking provider download endpoint")
		body = httpGetBody(fmt.Sprintf("%s/kerrareg/providers/v1/%s/%s/%s/download/linux/amd64",
			base, providerNamespace, providerCRName, providerVersion))
		Expect(body).To(ContainSubstring("download_url"))
		Expect(body).To(ContainSubstring("shasum"))
		Expect(body).To(ContainSubstring("signing_keys"))

		By("checking SHA256SUMS endpoint")
		body = httpGetBody(fmt.Sprintf("%s/kerrareg/providers/v1/%s/%s/%s/SHA256SUMS/linux/amd64",
			base, providerNamespace, providerCRName, providerVersion))
		Expect(body).NotTo(BeEmpty())

		By("checking SHA256SUMS.sig endpoint")
		body = httpGetBody(fmt.Sprintf("%s/kerrareg/providers/v1/%s/%s/%s/SHA256SUMS.sig/linux/amd64",
			base, providerNamespace, providerCRName, providerVersion))
		Expect(body).NotTo(BeEmpty())
	})

	It("should successfully run tofu init against the kerrareg registry", func() {
		By("creating a temp directory with a Terraform config")
		tmpDir := GinkgoT().TempDir()

		mainTF := fmt.Sprintf(`terraform {
  required_providers {
    aws = {
      source  = "localhost:%s/%s/%s"
      version = "%s"
    }
  }
}
`, serverPortForwardPort, providerNamespace, providerCRName, providerVersion)
		Expect(os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())

		By("writing a .tofurc to point at the local registry")
		tofuRC := fmt.Sprintf(`host "localhost:%s" {
  services = {
    "providers.v1" = "http://localhost:%s/kerrareg/providers/v1/"
  }
}
`, serverPortForwardPort, serverPortForwardPort)
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
			authTestSA   = "kerrareg-e2e-provider-reader"
			authTestRole = "kerrareg-e2e-provider-reader"
			authTestRB   = "kerrareg-e2e-provider-reader"
		)

		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("restoring anonymous auth after auth test")
			restoreCmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--namespace", providerNamespace,
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
				"-n", providerNamespace,
			)
			_ = pfRestoreCmd.Start()
			time.Sleep(3 * time.Second)

			By("cleaning up auth test RBAC")
			_, _ = utils.Run(exec.Command("kubectl", "delete", "rolebinding", authTestRB, "-n", providerNamespace, "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "role", authTestRole, "-n", providerNamespace, "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "serviceaccount", authTestSA, "-n", providerNamespace, "--ignore-not-found"))
		})

		By("disabling anonymous auth via Helm upgrade")
		cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", providerNamespace,
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
			"-n", providerNamespace,
		)
		Expect(pfCmd.Start()).To(Succeed(), "Failed to restart port-forward")
		time.Sleep(3 * time.Second)

		By("verifying unauthenticated request returns 401")
		unauthResp, err := http.Get(fmt.Sprintf( //nolint:noctx
			"http://localhost:%s/kerrareg/providers/v1/%s/%s/versions",
			serverPortForwardPort, providerNamespace, providerCRName))
		Expect(err).NotTo(HaveOccurred())
		_ = unauthResp.Body.Close()
		Expect(unauthResp.StatusCode).To(Equal(http.StatusUnauthorized))

		By("creating a read-only ServiceAccount and RBAC for the auth test")
		_, _ = utils.Run(exec.Command("kubectl", "create", "serviceaccount", authTestSA, "-n", providerNamespace))
		_, _ = utils.Run(exec.Command("kubectl", "create", "role", authTestRole,
			"-n", providerNamespace,
			"--resource=providers.kerrareg.io,versions.kerrareg.io",
			"--verb=get,list,watch",
		))
		_, _ = utils.Run(exec.Command("kubectl", "create", "rolebinding", authTestRB,
			"-n", providerNamespace,
			fmt.Sprintf("--role=%s", authTestRole),
			fmt.Sprintf("--serviceaccount=%s:%s", providerNamespace, authTestSA),
		))

		By("generating a short-lived ServiceAccount token")
		tokenOutput, err := utils.Run(exec.Command("kubectl", "create", "token", authTestSA,
			"-n", providerNamespace,
			"--duration=1h",
		))
		Expect(err).NotTo(HaveOccurred())
		token := strings.TrimSpace(tokenOutput)
		Expect(token).NotTo(BeEmpty())

		// OpenTofu sends credentials (from the .tofurc credentials block) over HTTP for
		// hostnames that are not the loopback address "localhost". We use
		// "kerrareg.localtest.me:18080" — a public DNS wildcard that resolves to 127.0.0.1 —
		// so that the existing port-forward (localhost:18080) is reachable while OpenTofu
		// treats the host as a non-local name and forwards the bearer token on every HTTP
		// request (versions, download metadata, SHA256SUMS, binary). This is the same
		// pattern used in the module e2e auth test.
		const authRegistryHost = "kerrareg.localtest.me:18080"

		By("running tofu init with bearer token authentication")
		tmpDir := GinkgoT().TempDir()
		mainTF := fmt.Sprintf(`terraform {
  required_providers {
    aws = {
      source  = "%s/%s/%s"
      version = "%s"
    }
  }
}
`, authRegistryHost, providerNamespace, providerCRName, providerVersion)
		Expect(os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())

		tofuRC := fmt.Sprintf(`host "%s" {
  services = {
    "providers.v1" = "http://%s/kerrareg/providers/v1/"
  }
}
credentials "%s" {
  token = "%s"
}
`, authRegistryHost, authRegistryHost, authRegistryHost, token)
		tofuRCPath := filepath.Join(tmpDir, ".tofurc")
		Expect(os.WriteFile(tofuRCPath, []byte(tofuRC), 0600)).To(Succeed())

		initCmd := exec.Command("tofu", "init", "-no-color")
		initCmd.Dir = tmpDir
		initCmd.Env = append(os.Environ(),
			fmt.Sprintf("TF_CLI_CONFIG_FILE=%s", tofuRCPath),
		)
		initOutput, initErr := initCmd.CombinedOutput()
		Expect(initErr).NotTo(HaveOccurred(),
			"tofu init with auth failed; output:\n%s", string(initOutput))
		Expect(string(initOutput)).To(ContainSubstring("successfully initialized"))
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

