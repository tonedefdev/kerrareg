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
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// namespace where the project is deployed in.
const namespace = "opendepot-system"

type providerMirrorVersionsResponse struct {
	Versions map[string]struct{} `json:"versions"`
}

type providerMirrorArchivesResponse struct {
	Archives map[string]providerMirrorArchive `json:"archives"`
}

type providerMirrorArchive struct {
	URL    string   `json:"url"`
	Hashes []string `json:"hashes"`
}

var _ = Describe("Provider", Ordered, func() {
	const (
		providerNamespace     = "opendepot-system"
		serverPortForwardPort = "18080"
		providerCRName        = "null"
		providerVersion       = "3.2.3"
		providerVersionCRName = "null-3-2-3-linux-amd64"
		providerStoragePath   = "/data/modules"
	)

	var (
		pfCancel          context.CancelFunc
		mirrorTLSServer   *httptest.Server
		mirrorTLSCertDir  string
		mirrorTLSCertPath string
	)

	BeforeAll(func() {
		By("applying the test Provider CR")
		operatingSystems := "      - linux"
		if runtime.GOOS != "linux" {
			operatingSystems += "\n      - " + runtime.GOOS
		}
		architectures := "      - amd64"
		if runtime.GOARCH != "amd64" {
			architectures += "\n      - " + runtime.GOARCH
		}

		providerYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
    name: "%s"
    operatingSystems:
%s
    architectures:
%s
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, providerCRName, providerNamespace, providerCRName, operatingSystems, architectures, providerStoragePath, providerVersion)

		providerFile := filepath.Join(GinkgoT().TempDir(), "test-provider.yaml")
		Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", providerFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply test Provider CR")

		By("starting port-forward to the opendepot server")
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

		upstream, err := url.Parse(fmt.Sprintf("http://localhost:%s", serverPortForwardPort))
		Expect(err).NotTo(HaveOccurred())
		mirrorTLSCertDir, err = os.MkdirTemp("", "opendepot-provider-mirror-ca-")
		Expect(err).NotTo(HaveOccurred())
		mirrorTLSCertPath = filepath.Join(mirrorTLSCertDir, "mirror.crt")
		mirrorTLSKeyPath := filepath.Join(mirrorTLSCertDir, "mirror.key")
		mkcertCmd := exec.Command("mkcert", "-cert-file", mirrorTLSCertPath, "-key-file", mirrorTLSKeyPath,
			"opendepot.localtest.me", "localhost", "127.0.0.1", "::1",
		)
		output, err := mkcertCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to generate network mirror TLS certificate; output:\n%s", string(output))
		certificate, err := tls.LoadX509KeyPair(mirrorTLSCertPath, mirrorTLSKeyPath)
		Expect(err).NotTo(HaveOccurred())
		mirrorTLSServer = httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(upstream))
		mirrorTLSServer.TLS = &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
		}
		mirrorTLSServer.StartTLS()
		_, mirrorPort, err := net.SplitHostPort(mirrorTLSServer.Listener.Addr().String())
		Expect(err).NotTo(HaveOccurred())
		mirrorTLSServer.URL = "https://opendepot.localtest.me:" + mirrorPort
	})

	AfterAll(func() {
		if pfCancel != nil {
			pfCancel()
		}
		if mirrorTLSServer != nil {
			mirrorTLSServer.Close()
		}
		if mirrorTLSCertDir != "" {
			Expect(os.RemoveAll(mirrorTLSCertDir)).To(Succeed())
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
				"-l", fmt.Sprintf("opendepot.defdev.io/provider=%s", providerCRName),
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
		}, 5*time.Minute, 5*time.Second).Should(Succeed())
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

		By("waiting for spec.fileName and status.checksum to be set on the Version CR")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", providerVersionCRName,
				"-n", providerNamespace,
				"-o", `jsonpath={.spec.fileName},{.status.checksum},{.status.synced}`,
			)
			out, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			parts := strings.SplitN(strings.TrimSpace(out), ",", 3)
			g.Expect(parts).To(HaveLen(3), "unexpected jsonpath output: %s", out)
			g.Expect(parts[0]).NotTo(BeEmpty(), "spec.fileName not yet set: %s", out)
			g.Expect(parts[1]).NotTo(BeEmpty(), "status.checksum not yet set: %s", out)
			g.Expect(parts[2]).To(Equal("true"), "status.synced not yet true: %s", out)
		}, 60*time.Second, 5*time.Second).Should(Succeed())

		By("checking /.well-known/terraform.json")
		body := httpGetBody(base + "/.well-known/terraform.json")
		Expect(body).To(ContainSubstring("providers.v1"))

		By("checking provider versions list endpoint")
		body = httpGetBody(fmt.Sprintf("%s/opendepot/providers/v1/%s/%s/versions",
			base, providerNamespace, providerCRName))
		Expect(body).To(ContainSubstring(providerVersion))

		By("checking provider download endpoint")
		var downloadBody string
		Eventually(func(g Gomega) {
			resp, err := http.Get(fmt.Sprintf( //nolint:noctx
				"%s/opendepot/providers/v1/%s/%s/%s/download/linux/amd64",
				base, providerNamespace, providerCRName, providerVersion,
			))
			g.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			raw, readErr := io.ReadAll(resp.Body)
			g.Expect(readErr).NotTo(HaveOccurred())
			g.Expect(resp.StatusCode).To(BeNumerically("<", 300),
				"unexpected status %d from download endpoint; body: %s", resp.StatusCode, string(raw))
			downloadBody = string(raw)
		}, 30*time.Second, 2*time.Second).Should(Succeed())
		Expect(downloadBody).To(ContainSubstring("download_url"))
		Expect(downloadBody).To(ContainSubstring("shasum"))
		Expect(downloadBody).To(ContainSubstring("signing_keys"))

		By("checking SHA256SUMS endpoint")
		body = httpGetBody(fmt.Sprintf("%s/opendepot/providers/v1/%s/%s/%s/SHA256SUMS/linux/amd64",
			base, providerNamespace, providerCRName, providerVersion))
		Expect(body).NotTo(BeEmpty())

		By("checking SHA256SUMS.sig endpoint")
		body = httpGetBody(fmt.Sprintf("%s/opendepot/providers/v1/%s/%s/%s/SHA256SUMS.sig/linux/amd64",
			base, providerNamespace, providerCRName, providerVersion))
		Expect(body).NotTo(BeEmpty())
	})

	It("should successfully run tofu init against the opendepot registry", func() {
		By("creating a temp directory with a Terraform config")
		tmpDir := GinkgoT().TempDir()

		mainTF := fmt.Sprintf(`terraform {
  required_providers {
    `+providerCRName+` = {
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
    "providers.v1" = "http://localhost:%s/opendepot/providers/v1/"
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

	It("should install the canonical provider identity through the network mirror", func() {
		currentVersionCRName := fmt.Sprintf("null-3-2-3-%s-%s", runtime.GOOS, runtime.GOARCH)
		By("waiting for the host platform provider artifact")
		Eventually(func(g Gomega) {
			output, err := utils.Run(exec.Command("kubectl", "get", "version", currentVersionCRName,
				"-n", providerNamespace,
				"-o", `jsonpath={.status.synced},{.status.checksum}`,
			))
			g.Expect(err).NotTo(HaveOccurred())
			parts := strings.SplitN(output, ",", 2)
			g.Expect(parts).To(HaveLen(2))
			g.Expect(parts[0]).To(Equal("true"))
			g.Expect(parts[1]).NotTo(BeEmpty())
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		mirrorBaseURL := fmt.Sprintf("%s/opendepot/providers/mirror/v1/%s/", mirrorTLSServer.URL, providerNamespace)
		providerBaseURL := mirrorBaseURL + "registry.opentofu.org/hashicorp/null/"

		By("checking the network mirror version index")
		response, err := mirrorTLSServer.Client().Get(providerBaseURL + "index.json")
		Expect(err).NotTo(HaveOccurred())
		defer response.Body.Close()
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		var index providerMirrorVersionsResponse
		Expect(json.NewDecoder(response.Body).Decode(&index)).To(Succeed())
		Expect(index.Versions).To(HaveKey(providerVersion))

		By("checking platform metadata and archive integrity")
		metadataURL := providerBaseURL + providerVersion + ".json"
		response, err = mirrorTLSServer.Client().Get(metadataURL)
		Expect(err).NotTo(HaveOccurred())
		defer response.Body.Close()
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		var metadata providerMirrorArchivesResponse
		Expect(json.NewDecoder(response.Body).Decode(&metadata)).To(Succeed())
		platform := runtime.GOOS + "_" + runtime.GOARCH
		archive, exists := metadata.Archives[platform]
		Expect(exists).To(BeTrue())
		Expect(archive.Hashes).To(HaveLen(1))
		Expect(archive.Hashes[0]).To(HavePrefix("zh:"))

		expectedChecksum, err := utils.Run(exec.Command("kubectl", "get", "version", currentVersionCRName,
			"-n", providerNamespace,
			"-o", `jsonpath={.status.checksum}`,
		))
		Expect(err).NotTo(HaveOccurred())
		expectedChecksumBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(expectedChecksum))
		Expect(err).NotTo(HaveOccurred())
		Expect(archive.Hashes[0]).To(Equal("zh:" + hex.EncodeToString(expectedChecksumBytes)))

		metadataLocation, err := url.Parse(metadataURL)
		Expect(err).NotTo(HaveOccurred())
		archiveLocation, err := metadataLocation.Parse(archive.URL)
		Expect(err).NotTo(HaveOccurred())
		archiveResponse, err := mirrorTLSServer.Client().Get(archiveLocation.String())
		Expect(err).NotTo(HaveOccurred())
		defer archiveResponse.Body.Close()
		Expect(archiveResponse.StatusCode).To(Equal(http.StatusOK))
		archiveBytes, err := io.ReadAll(archiveResponse.Body)
		Expect(err).NotTo(HaveOccurred())
		digest := sha256.Sum256(archiveBytes)
		Expect(base64.StdEncoding.EncodeToString(digest[:])).To(Equal(strings.TrimSpace(expectedChecksum)))

		By("running tofu init with direct installation disabled")
		workDir := GinkgoT().TempDir()
		mainTF := fmt.Sprintf(`terraform {
  required_providers {
    null = {
      source  = "registry.opentofu.org/hashicorp/null"
      version = "%s"
    }
  }
}
`, providerVersion)
		Expect(os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())
		tofuRC := fmt.Sprintf(`provider_installation {
  network_mirror {
    url     = "%s"
    include = ["registry.opentofu.org/hashicorp/null"]
  }
  direct {
    exclude = ["registry.opentofu.org/hashicorp/null"]
  }
}
`, mirrorBaseURL)
		tofuRCPath := filepath.Join(workDir, ".tofurc")
		Expect(os.WriteFile(tofuRCPath, []byte(tofuRC), 0600)).To(Succeed())
		initCmd := exec.Command("tofu", "init", "-no-color")
		initCmd.Dir = workDir
		initCmd.Env = append(os.Environ(), "TF_CLI_CONFIG_FILE="+tofuRCPath)
		output, err := initCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "network mirror tofu init failed; output:\n%s", string(output))
		Expect(string(output)).To(ContainSubstring("successfully initialized"))
		lockFile, err := os.ReadFile(filepath.Join(workDir, ".terraform.lock.hcl"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(lockFile)).To(ContainSubstring(`provider "registry.opentofu.org/hashicorp/null"`))
	})

	It("should enforce Kubernetes RBAC when anonymousAuth is disabled", func() {
		const (
			authTestSA   = "opendepot-e2e-provider-reader"
			authTestRole = "opendepot-e2e-provider-reader"
			authTestRB   = "opendepot-e2e-provider-reader"
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
			"http://localhost:%s/opendepot/providers/v1/%s/%s/versions",
			serverPortForwardPort, providerNamespace, providerCRName))
		Expect(err).NotTo(HaveOccurred())
		_ = unauthResp.Body.Close()
		Expect(unauthResp.StatusCode).To(Equal(http.StatusUnauthorized))

		mirrorVersionsURL := fmt.Sprintf("%s/opendepot/providers/mirror/v1/%s/registry.opentofu.org/hashicorp/null/index.json",
			mirrorTLSServer.URL, providerNamespace)
		By("verifying unauthenticated mirror metadata returns 401")
		unauthMirrorResp, err := mirrorTLSServer.Client().Get(mirrorVersionsURL)
		Expect(err).NotTo(HaveOccurred())
		_ = unauthMirrorResp.Body.Close()
		Expect(unauthMirrorResp.StatusCode).To(Equal(http.StatusUnauthorized))

		By("creating a read-only ServiceAccount and RBAC for the auth test")
		_, _ = utils.Run(exec.Command("kubectl", "create", "serviceaccount", authTestSA, "-n", providerNamespace))
		_, _ = utils.Run(exec.Command("kubectl", "create", "role", authTestRole,
			"-n", providerNamespace,
			"--resource=providers.opendepot.defdev.io,versions.opendepot.defdev.io",
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

		By("verifying read-only Kubernetes RBAC authorizes mirror metadata")
		authMirrorRequest, err := http.NewRequest(http.MethodGet, mirrorVersionsURL, nil)
		Expect(err).NotTo(HaveOccurred())
		authMirrorRequest.Header.Set("Authorization", "Bearer "+token)
		authMirrorResp, err := mirrorTLSServer.Client().Do(authMirrorRequest)
		Expect(err).NotTo(HaveOccurred())
		defer authMirrorResp.Body.Close()
		Expect(authMirrorResp.StatusCode).To(Equal(http.StatusOK))
		var mirrorVersions providerMirrorVersionsResponse
		Expect(json.NewDecoder(authMirrorResp.Body).Decode(&mirrorVersions)).To(Succeed())
		Expect(mirrorVersions.Versions).To(HaveKey(providerVersion))

		// OpenTofu sends credentials (from the .tofurc credentials block) over HTTP for
		// hostnames that are not the loopback address "localhost". We use
		// "opendepot.localtest.me:18080" — a public DNS wildcard that resolves to 127.0.0.1 —
		// so that the existing port-forward (localhost:18080) is reachable while OpenTofu
		// treats the host as a non-local name and forwards the bearer token on every HTTP
		// request (versions, download metadata, SHA256SUMS, binary). This is the same
		// pattern used in the module e2e auth test.
		const authRegistryHost = "opendepot.localtest.me:18080"

		By("running tofu init with bearer token authentication")
		tmpDir := GinkgoT().TempDir()
		mainTF := fmt.Sprintf(`terraform {
  required_providers {
    `+providerCRName+` = {
      source  = "%s/%s/%s"
      version = "%s"
    }
  }
}
`, authRegistryHost, providerNamespace, providerCRName, providerVersion)
		Expect(os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())

		tofuRC := fmt.Sprintf(`host "%s" {
  services = {
    "providers.v1" = "http://%s/opendepot/providers/v1/"
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

var _ = Describe("Provider Scanning", Ordered, func() {
	const (
		scanNamespace     = "opendepot-system"
		scanProviderName  = "null"
		scanVersion       = "3.2.3"
		scanVersionCRName = "null-3-2-3-linux-amd64"
		scanStoragePath   = "/data/modules"
	)

	BeforeAll(func() {
		By("upgrading Helm release to enable scanning with offline=false (Trivy fetches its own DB)")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())

		By("deleting any existing trivy cache PVC to avoid immutable field conflicts on re-enable")
		cmd := exec.Command("kubectl", "delete", "pvc", "opendepot-trivy-cache",
			"-n", scanNamespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)

		cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", scanNamespace,
			"--reuse-values",
			// providerScanning=true creates the Trivy cache PVC and passes
			// --scan-offline=false + --trivy-cache-dir to the controller so
			// Trivy downloads a real vulnerability DB and returns actual CVEs.
			"--set", "scanning.enabled=true",
			"--set", "scanning.providerScanning=true",
			"--set", "scanning.offline=false",
			// Kind's default storage class only supports ReadWriteOnce.
			"--set", "scanning.cache.accessMode=ReadWriteOnce",
			// Extra memory headroom for the version controller running Trivy.
			"--set", "version.resources.limits.memory=1Gi",
			// Enable verbose debug logging so Trivy output is visible in test logs.
			"--set", "version.zapLogLevel=5",
			"--wait",
			"--timeout", "3m",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to upgrade Helm release with scanning enabled")

		By("applying the null provider CR")
		providerYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
    name: "%s"
    operatingSystems:
      - linux
    architectures:
      - amd64
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, scanProviderName, scanNamespace, scanProviderName, scanStoragePath, scanVersion)

		providerFile := filepath.Join(GinkgoT().TempDir(), "scan-provider.yaml")
		Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())
		cmd = exec.Command("kubectl", "apply", "-f", providerFile)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply null Provider CR")
	})

	AfterAll(func() {
		cmd := exec.Command("kubectl", "delete", "provider", scanProviderName,
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
			"--set", "scanning.providerScanning=false",
			"--set", "version.zapLogLevel=",
			"--wait",
			"--timeout", "3m",
		)
		_, _ = utils.Run(cmd)
	})

	It("should sync the null provider Version", func() {
		// The null provider binary is ~20 MB; sync should be quick.
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", scanVersionCRName,
				"-n", scanNamespace,
				"-o", "jsonpath={.status.synced}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 3*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should populate binaryScan on the Version CR", func() {
		// Trivy downloads its vulnerability DB on first run (~200 MB); allow generous time.
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", scanVersionCRName,
				"-n", scanNamespace,
				"-o", "jsonpath={.status.binaryScan.scannedAt}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "binaryScan.scannedAt should be set after scan completes")
		}, 10*time.Minute, 15*time.Second).Should(Succeed())
	})

	It("should populate sourceScan on the Version CR", func() {
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

	It("should report at least one binary finding on the Version CR", func() {
		// null v3.2.3 was released 2021; its embedded Go deps are old enough to
		// guarantee known CVEs in the Trivy DB. A zero-finding result here means
		// the scan ran but the source-skip bug wrote a tombstone, or the DB is stale.
		cmd := exec.Command("kubectl", "get", "version", scanVersionCRName,
			"-n", scanNamespace,
			"-o", `jsonpath={.status.binaryScan.findings[0].vulnerabilityID}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(output)).NotTo(BeEmpty(),
			"binaryScan.findings should contain at least one finding for null v%s", scanVersion)
	})

	It("should report at least one source finding on the Version CR", func() {
		// go.mod for null v3.2.3 uses vintage sdk deps that carry known CVEs.
		cmd := exec.Command("kubectl", "get", "version", scanVersionCRName,
			"-n", scanNamespace,
			"-o", `jsonpath={.status.sourceScan.findings[0].vulnerabilityID}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(output)).NotTo(BeEmpty(),
			"sourceScan.findings should contain at least one finding for null v%s", scanVersion)
	})

})

var _ = Describe("Community Provider", Ordered, func() {
	const (
		communityNamespace    = "opendepot-system"
		communityProviderName = "github"
		// The integrations namespace on the OpenTofu registry hosts the GitHub provider.
		communityRegistryNS    = "integrations"
		communityVersion       = "6.6.0"
		communityVersionCRName = "github-6-6-0-linux-amd64"
		communityStoragePath   = "/data/modules"
	)

	BeforeAll(func() {
		By("upgrading Helm release to enable scanning before applying the community provider")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())

		By("deleting any existing trivy cache PVC to avoid immutable field conflicts on re-enable")
		cmd := exec.Command("kubectl", "delete", "pvc", "opendepot-trivy-cache",
			"-n", communityNamespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)

		cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", communityNamespace,
			"--reuse-values",
			// providerScanning=true creates the Trivy cache PVC and passes
			// --scan-offline=false + --trivy-cache-dir to the controller so
			// Trivy downloads a real vulnerability DB and returns actual CVEs.
			"--set", "scanning.enabled=true",
			"--set", "scanning.providerScanning=true",
			"--set", "scanning.offline=false",
			// Kind's default storage class only supports ReadWriteOnce.
			"--set", "scanning.cache.accessMode=ReadWriteOnce",
			// Extra memory headroom for Trivy running inside the version controller.
			"--set", "version.resources.limits.memory=1Gi",
			// Enable verbose debug logging so Trivy output is visible in test logs.
			"--set", "version.zapLogLevel=5",
			"--wait",
			"--timeout", "3m",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to upgrade Helm release with scanning enabled")

		By("applying the integrations/github Provider CR")
		providerYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
    name: "%s"
    namespace: "%s"
    operatingSystems:
      - linux
    architectures:
      - amd64
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, communityProviderName, communityNamespace, communityProviderName, communityRegistryNS, communityStoragePath, communityVersion)

		providerFile := filepath.Join(GinkgoT().TempDir(), "community-provider.yaml")
		Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())
		cmd = exec.Command("kubectl", "apply", "-f", providerFile)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply community Provider CR")
	})

	AfterAll(func() {
		cmd := exec.Command("kubectl", "delete", "provider", communityProviderName,
			"-n", communityNamespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)

		By("disabling scanning to restore baseline state after community provider test")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())
		cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", communityNamespace,
			"--reuse-values",
			"--set", "scanning.enabled=false",
			"--set", "scanning.providerScanning=false",
			"--set", "version.zapLogLevel=",
			"--wait",
			"--timeout", "3m",
		)
		_, _ = utils.Run(cmd)
	})

	It("should create a Version CR for the community provider", func() {
		By("waiting for the Version CR to be created")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "versions",
				"-l", fmt.Sprintf("opendepot.defdev.io/provider=%s", communityProviderName),
				"-n", communityNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(output)).NotTo(BeEmpty(), "expected at least one Version CR for the community provider")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should sync the community provider Version CR", func() {
		// The github provider binary is ~30 MB; allow generous time for download.
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", communityVersionCRName,
				"-n", communityNamespace,
				"-o", "jsonpath={.status.synced}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 5*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should populate binaryScan on the community provider Version CR", func() {
		// Validates that Trivy can scan binaries from community (non-HashiCorp) providers.
		// Trivy may need to download its DB on first run (~200 MB); allow generous time.
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", communityVersionCRName,
				"-n", communityNamespace,
				"-o", "jsonpath={.status.binaryScan.scannedAt}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "binaryScan.scannedAt should be set after scan completes")
		}, 10*time.Minute, 15*time.Second).Should(Succeed())
	})

	It("should populate sourceScan on the community provider Version CR", func() {
		// Validates that Trivy can clone and scan the source repository of a community provider.
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "version", communityVersionCRName,
				"-n", communityNamespace,
				"-o", "jsonpath={.status.sourceScan.scannedAt}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "sourceScan.scannedAt should be set after source scan completes")
		}, 10*time.Minute, 15*time.Second).Should(Succeed())
	})

	It("should report at least one binary finding on the community provider Version CR", func() {
		cmd := exec.Command("kubectl", "get", "version", communityVersionCRName,
			"-n", communityNamespace,
			"-o", `jsonpath={.status.binaryScan.findings[0].vulnerabilityID}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(output)).NotTo(BeEmpty(),
			"binaryScan.findings should contain at least one finding for github v%s", communityVersion)
	})

	It("should report at least one source finding on the community provider Version CR", func() {
		cmd := exec.Command("kubectl", "get", "version", communityVersionCRName,
			"-n", communityNamespace,
			"-o", `jsonpath={.status.sourceScan.findings[0].vulnerabilityID}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(output)).NotTo(BeEmpty(),
			"sourceScan.findings should contain at least one finding for github v%s", communityVersion)
	})

})

var _ = Describe("Provider Version History Limit", Ordered, func() {
	const (
		vhlNamespace    = "opendepot-system"
		vhlProviderName = "null-vhl"
		vhlStoragePath  = "/data/modules"
		// Two versions; the controller should keep only the newer one.
		vhlOlderVersion = "3.2.1"
		vhlNewerVersion = "3.2.2"
	)

	BeforeAll(func() {
		By("applying the null-vhl Provider CR with versionHistoryLimit: 1 and two versions")
		providerYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
    name: "%s"
    operatingSystems:
      - linux
    architectures:
      - amd64
    versionHistoryLimit: 1
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
    - version: "%s"
`, vhlProviderName, vhlNamespace, vhlProviderName, vhlStoragePath, vhlOlderVersion, vhlNewerVersion)

		providerFile := filepath.Join(GinkgoT().TempDir(), "vhl-provider.yaml")
		Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", providerFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply vhl Provider CR")
	})

	AfterAll(func() {
		cmd := exec.Command("kubectl", "delete", "provider", vhlProviderName,
			"-n", vhlNamespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)
	})

	It("should trim provider Spec.Versions to the history limit", func() {
		By("waiting for the provider controller to trim Spec.Versions to 1 entry")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "provider", vhlProviderName,
				"-n", vhlNamespace,
				"-o", "jsonpath={.spec.versions}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			// A single-element array serialises as e.g. [{"version":"v3.2.2"}]
			g.Expect(strings.Count(output, "version")).To(Equal(1), "expected exactly 1 version in Spec.Versions after trim")
		}, 60*time.Second, 3*time.Second).Should(Succeed())
	})

	It("should create only one Version CR for the newer version", func() {
		By("waiting for exactly one Version CR to exist for the provider")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "versions",
				"-l", fmt.Sprintf("opendepot.defdev.io/provider=%s", vhlProviderName),
				"-n", vhlNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			lines := utils.GetNonEmptyLines(output)
			g.Expect(lines).To(HaveLen(1), "expected exactly 1 Version CR after versionHistoryLimit enforcement")
			g.Expect(lines[0]).To(ContainSubstring(strings.ReplaceAll(vhlNewerVersion, ".", "-")),
				"surviving Version CR should be for the newer version %s", vhlNewerVersion)
		}, 60*time.Second, 3*time.Second).Should(Succeed())
	})
})
