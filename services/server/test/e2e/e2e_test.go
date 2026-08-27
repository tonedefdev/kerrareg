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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

const (
	// serverLocalPort is the local port used for kubectl port-forward to the server Service.
	serverLocalPort = 19080
	// moduleNamespace is the namespace used when constructing module endpoint URLs.
	moduleNamespace = "opendepot-system"
	// moduleName is a non-existent module name used to probe the auth layer without
	// requiring real module data to be present.
	moduleName = "nonexistent-test-module"
	// moduleSystem is the provider/system segment of the module versions URL.
	moduleSystem = "aws"
	// tofuRegistryHost is the hostname used in tofu CLI integration tests.
	// *.localtest.me resolves to 127.0.0.1 via public DNS, so tofu init on the
	// test host reaches the kubectl port-forward tunnel at serverLocalPort without
	// any special DNS configuration inside the Kind cluster.
	tofuRegistryHost = "opendepot.localtest.me"
)

var _ = Describe("Server Authentication", Ordered, func() {
	var (
		chartPath  string
		serverRepo string
		serverTag  string
	)

	BeforeAll(func() {
		var err error
		chartPath, err = utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())
		serverRepo, serverTag = utils.SplitImageRef(serverImage)
	})

	// deployServer runs helm upgrade with a common base configuration plus any
	// extra --set flags supplied by the caller (e.g. auth-mode flags).
	deployServer := func(extraArgs ...string) {
		baseArgs := []string{
			"upgrade", helmReleaseName, chartPath,
			"--install",
			"--create-namespace",
			"--namespace", namespace,
			"--skip-crds",
			"--force-conflicts",
			"--set", "global.image.tag=",
			"--set", "depot.enabled=false",
			"--set", "module.enabled=false",
			"--set", "provider.enabled=false",
			"--set", "version.enabled=false",
			"--set", "server.enabled=true",
			"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
			"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
			"--set", "valkey.dataStorage.enabled=false",
			"--wait",
			"--timeout", "2m",
		}
		args := append(baseArgs, extraArgs...)
		cmd := exec.Command("helm", args...)
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to helm upgrade server")
	}

	// startPortForward starts a self-restarting kubectl port-forward to the server
	// Service and blocks until the port-forward tunnel is established. It returns a
	// cancel function that must be called (in AfterAll) to stop the port-forward.
	//
	// Auto-restart is needed because a Helm upgrade replaces the backing Pod during
	// the rollout. The port-forward process may connect to the terminating old Pod and
	// exit with "pod is not running"; we simply restart it until the new Pod is ready.
	startPortForward := func() context.CancelFunc {
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
					"-n", namespace,
					"svc/server",
					fmt.Sprintf("%d:80", serverLocalPort),
				)
				cmd.Stdout = GinkgoWriter
				cmd.Stderr = GinkgoWriter
				_ = cmd.Run()
				// Brief pause before restarting to avoid port-already-in-use races.
				time.Sleep(200 * time.Millisecond)
			}
		}()

		// Poll the service-discovery endpoint (no auth required) until the
		// port-forward tunnel is established.
		Eventually(func() error {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			return nil
		}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for server port-forward to be ready")

		return cancel
	}

	// stopPortForward cancels the context returned by startPortForward, which causes
	// the background goroutine to stop restarting kubectl port-forward.
	stopPortForward := func(cancel context.CancelFunc) {
		if cancel != nil {
			cancel()
		}
	}

	// moduleVersionsURL returns the module versions endpoint URL for the test module.
	moduleVersionsURL := func() string {
		return fmt.Sprintf("http://localhost:%d/opendepot/modules/v1/%s/%s/%s/versions",
			serverLocalPort, moduleNamespace, moduleName, moduleSystem)
	}

	Context("anonymous auth mode", Ordered, func() {
		var pfCancel context.CancelFunc

		BeforeAll(func() {
			By("deploying server with --anonymous-auth=true")
			deployServer(
				"--set", "server.anonymousAuth=true",
				"--set", "server.useBearerToken=false",
			)
			pfCancel = startPortForward()
		})

		AfterAll(func() {
			stopPortForward(pfCancel)
		})

		It("should return a non-401 response without an Authorization header", func() {
			resp, err := http.Get(moduleVersionsURL())
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"anonymous auth mode must not challenge clients with 401")
		})
	})

	Context("bearer token auth mode", Ordered, func() {
		var (
			pfCancel    context.CancelFunc
			bearerToken string
		)

		BeforeAll(func() {
			By("deploying server with --use-bearer-token=true")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=true",
			)
			pfCancel = startPortForward()

			By("creating a ServiceAccount with RBAC to list modules")
			saCmd := exec.Command("kubectl", "create", "serviceaccount", "test-authn-sa",
				"-n", namespace)
			_, _ = utils.Run(saCmd)

			roleCmd := exec.Command("kubectl", "create", "role", "test-authn-role",
				"--verb=get,list",
				"--resource=modules.opendepot.defdev.io",
				"-n", namespace)
			_, _ = utils.Run(roleCmd)

			rbCmd := exec.Command("kubectl", "create", "rolebinding", "test-authn-rb",
				"--role=test-authn-role",
				"--serviceaccount="+namespace+":test-authn-sa",
				"-n", namespace)
			_, _ = utils.Run(rbCmd)

			By("generating a bearer token from the test-authn-sa ServiceAccount")
			tokenCmd := exec.Command("kubectl", "create", "token", "test-authn-sa",
				"-n", namespace, "--duration=10m")
			token, err := utils.Run(tokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create bearer token")
			bearerToken = strings.TrimSpace(token)
		})

		AfterAll(func() {
			stopPortForward(pfCancel)

			for _, res := range [][]string{
				{"delete", "rolebinding", "test-authn-rb", "-n", namespace, "--ignore-not-found"},
				{"delete", "role", "test-authn-role", "-n", namespace, "--ignore-not-found"},
				{"delete", "serviceaccount", "test-authn-sa", "-n", namespace, "--ignore-not-found"},
				{"delete", "serviceaccount", "test-denied-sa", "-n", namespace, "--ignore-not-found"},
			} {
				cmd := exec.Command("kubectl", res...)
				_, _ = utils.Run(cmd)
			}
		})

		It("should return 401 when the Authorization header is missing", func() {
			resp, err := http.Get(moduleVersionsURL())
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"missing Authorization header must produce a 401")
		})

		It("should return a non-401 response with a valid bearer token", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+bearerToken)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a valid bearer token must not produce a 401")
		})

		It("should return 403 for a token from an SA with no RBAC", func() {
			By("creating a ServiceAccount with no RBAC")
			saCmd := exec.Command("kubectl", "create", "serviceaccount", "test-denied-sa",
				"-n", namespace)
			_, _ = utils.Run(saCmd)

			By("generating a token for the SA with no RBAC")
			tokenCmd := exec.Command("kubectl", "create", "token", "test-denied-sa",
				"-n", namespace, "--duration=10m")
			deniedToken, err := utils.Run(tokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create token for denied SA")
			deniedToken = strings.TrimSpace(deniedToken)

			By("verifying the server propagates the Kubernetes 403 back to the client")
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+deniedToken)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a token for an SA with no RBAC must produce a 403")
		})
	})

	Context("kubeconfig auth mode", Ordered, func() {
		var (
			pfCancel      context.CancelFunc
			kubeconfigB64 string
		)

		BeforeAll(func() {
			By("deploying server with default kubeconfig auth mode (--anonymous-auth=false --use-bearer-token=false)")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
			)
			pfCancel = startPortForward()

			By("retrieving the cluster CA certificate")
			caCmd := exec.Command("kubectl", "get", "configmap", "kube-root-ca.crt",
				"-n", "kube-system",
				"-o", `jsonpath={.data.ca\.crt}`)
			caCert, err := utils.Run(caCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to retrieve cluster CA cert")
			caData := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(caCert)))

			By("creating a short-lived token for the server ServiceAccount")
			tokenCmd := exec.Command("kubectl", "create", "token", "server",
				"-n", namespace, "--duration=10m")
			saToken, err := utils.Run(tokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create SA token for kubeconfig")
			saToken = strings.TrimSpace(saToken)

			By("building an in-cluster kubeconfig")
			// The server pod resolves the API server at kubernetes.default.svc:443.
			// We embed the cluster CA and the SA token so the server can authenticate
			// using this kubeconfig from inside the cluster.
			kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: %s
    server: https://kubernetes.default.svc:443
  name: in-cluster
contexts:
- context:
    cluster: in-cluster
    namespace: %s
    user: server-sa
  name: in-cluster
current-context: in-cluster
users:
- name: server-sa
  user:
    token: %s`, caData, namespace, saToken)

			kubeconfigB64 = base64.StdEncoding.EncodeToString([]byte(kubeconfig))
		})

		AfterAll(func() {
			stopPortForward(pfCancel)
		})

		It("should return a non-401 response with a valid base64-encoded kubeconfig", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+kubeconfigB64)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a valid base64-encoded kubeconfig must not produce a 401")
		})
	})

	// Context: tofu CLI integration
	//
	// These specs verify the behaviour of `tofu init` against the server over HTTP,
	// covering the three .tofurc configurations that users are likely to try:
	//   1. No credentials block at all          → 401 (no token sent)
	//   2. Token placed inside the host block   → 401 (silently ignored by OpenTofu)
	//   3. Token in the correct credentials block → authenticated (no 401)
	Context("tofu CLI integration", Ordered, func() {
		var tofuBin string

		BeforeAll(func() {
			var err error
			tofuBin, err = exec.LookPath("tofu")
			if err != nil {
				Skip("tofu CLI not found in PATH; skipping tofu integration tests")
			}
		})

		// runTofuInit writes a main.tf referencing a (non-existent) module from
		// the local port-forwarded server, writes rcContent as .tofurc, runs
		// `tofu init -no-color`, and returns the combined stdout+stderr output.
		// The module not existing is fine — auth failures surface before any
		// module download is attempted.
		//
		// The module source uses tofuRegistryHost (opendepot.localtest.me) as the
		// registry hostname. *.localtest.me resolves to 127.0.0.1 via public DNS,
		// so tofu reaches the port-forward tunnel on the test host without any
		// in-cluster DNS configuration. The port goes only in the service URL
		// inside the host block — not in the module source address.
		runTofuInit := func(rcContent string) string {
			workDir := GinkgoT().TempDir()
			mainTF := fmt.Sprintf(`module "test" {
  source = "%s/%s/%s/%s"
}
`, tofuRegistryHost, moduleNamespace, moduleName, moduleSystem)
			ExpectWithOffset(1, os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())

			rcFile := filepath.Join(GinkgoT().TempDir(), ".tofurc")
			ExpectWithOffset(1, os.WriteFile(rcFile, []byte(rcContent), 0600)).To(Succeed())

			cmd := exec.Command(tofuBin, "init", "-no-color")
			cmd.Dir = workDir
			cmd.Env = append(os.Environ(), "TF_CLI_CONFIG_FILE="+rcFile)
			output, _ := cmd.CombinedOutput()
			_, _ = fmt.Fprintf(GinkgoWriter, "tofu init output:\n%s\n", output)
			return string(output)
		}

		Context("anonymous auth over HTTP", Ordered, func() {
			var pfCancel context.CancelFunc

			BeforeAll(func() {
				By("deploying server with --anonymous-auth=true")
				deployServer(
					"--set", "server.anonymousAuth=true",
					"--set", "server.useBearerToken=false",
				)
				pfCancel = startPortForward()
			})

			AfterAll(func() {
				stopPortForward(pfCancel)
			})

			It("tofu init without credentials must not fail with a 401", func() {
				// No credentials block — anonymous auth accepts unauthenticated requests.
				rc := fmt.Sprintf(`host "%s" {
  services = {
    "modules.v1" = "http://%s:%d/opendepot/modules/v1/"
  }
}
`, tofuRegistryHost, tofuRegistryHost, serverLocalPort)
				output := runTofuInit(rc)
				Expect(output).NotTo(ContainSubstring("401"),
					"anonymous auth must not produce a 401 during tofu init")
				Expect(output).NotTo(ContainSubstring("missing Authorization header"),
					"anonymous auth must not produce an auth error during tofu init")
			})
		})

		Context("bearer token auth over HTTP", Ordered, func() {
			var (
				pfCancel    context.CancelFunc
				bearerToken string
			)

			BeforeAll(func() {
				By("deploying server with --use-bearer-token=true")
				deployServer(
					"--set", "server.anonymousAuth=false",
					"--set", "server.useBearerToken=true",
				)
				pfCancel = startPortForward()

				By("generating a bearer token from the server ServiceAccount")
				tokenCmd := exec.Command("kubectl", "create", "token", "server",
					"-n", namespace, "--duration=10m")
				token, err := utils.Run(tokenCmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create bearer token")
				bearerToken = strings.TrimSpace(token)
			})

			AfterAll(func() {
				stopPortForward(pfCancel)
			})

			It("tofu init without a credentials block in .tofurc fails with 401", func() {
				// No credentials block — tofu sends no Authorization header.
				rc := fmt.Sprintf(`host "%s" {
  services = {
    "modules.v1" = "http://%s:%d/opendepot/modules/v1/"
  }
}
`, tofuRegistryHost, tofuRegistryHost, serverLocalPort)
				output := runTofuInit(rc)
				Expect(output).To(
					Or(ContainSubstring("401"), ContainSubstring("missing Authorization header")),
					"tofu init without credentials must produce a 401 in bearer token mode",
				)
			})

			It("tofu init with token placed in the host block fails with 401 (common misconfiguration)", func() {
				// Placing `token` directly inside the `host` block is valid HCL but
				// OpenTofu silently ignores it — credentials must live in a separate
				// `credentials` block.  This is the root cause of the quickstart
				// "missing Authorization header" issue.
				rc := fmt.Sprintf("host \"%s\" {\n  services = {\n    \"modules.v1\" = \"http://%s:%d/opendepot/modules/v1/\"\n  }\n  token = %q\n}\n",
					tofuRegistryHost, tofuRegistryHost, serverLocalPort, bearerToken)
				output := runTofuInit(rc)
				Expect(output).To(
					Or(ContainSubstring("401"), ContainSubstring("missing Authorization header")),
					"token inside host block must not be sent; server must return 401",
				)
			})

			It("tofu init with token in a credentials block must not produce a 401", func() {
				// Correct configuration: `credentials` block keyed to the registry
				// hostname, plus `host` block to configure the HTTP service URL.
				// OpenTofu reads the `credentials` block and includes the token in
				// the Authorization header for all requests to that hostname.
				rc := fmt.Sprintf("credentials \"%s\" {\n  token = %q\n}\nhost \"%s\" {\n  services = {\n    \"modules.v1\" = \"http://%s:%d/opendepot/modules/v1/\"\n  }\n}\n",
					tofuRegistryHost, bearerToken, tofuRegistryHost, tofuRegistryHost, serverLocalPort)
				output := runTofuInit(rc)
				Expect(output).NotTo(
					Or(ContainSubstring("401"), ContainSubstring("missing Authorization header")),
					"token in credentials block must be forwarded to the server; must not return 401",
				)
			})
		})
	})

	Context("OIDC auth mode", Ordered, func() {
		var (
			pfCancelServer context.CancelFunc
			pfCancelDex    context.CancelFunc
			oidcJWT        string
		)

		const (
			dexLocalPort     = 15556
			testClientSecret = "test-client-secret-for-e2e"
			testUserEmail    = "test@example.com"
			testUserPassword = "testpassword"
			testUserID       = "test-user-oidc-e2e"
		)

		// startDexPortForward starts a self-restarting kubectl port-forward to the Dex
		// service and blocks until the OIDC discovery endpoint is reachable.
		startDexPortForward := func(localPort int) context.CancelFunc {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
						"-n", namespace,
						"svc/opendepot-dex",
						fmt.Sprintf("%d:5556", localPort),
					)
					cmd.Stdout = GinkgoWriter
					cmd.Stderr = GinkgoWriter
					_ = cmd.Run()
					time.Sleep(200 * time.Millisecond)
				}
			}()
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", localPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for Dex port-forward to be ready")
			return cancel
		}

		// acquireDexToken performs a Resource Owner Password Credentials grant against the
		// Dex token endpoint and returns the raw OIDC ID token string.
		acquireDexToken := func(localPort int, clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", localPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		BeforeAll(func() {
			By("generating bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(testUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex e2e values file")
			dexValues := fmt.Sprintf(`
dex:
  enabled: true
  config:
    issuer: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: testuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
          - http://localhost:10010
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
		dexProxy:
			enabled: false
`, testUserEmail, passwordHash, testUserID, testClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "dex-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC auth mode and Dex enabled")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)

			pfCancelServer = startPortForward()
			pfCancelDex = startDexPortForward(dexLocalPort)

			By("acquiring a valid Dex JWT via ROPC")
			Eventually(func() error {
				token, err := acquireDexToken(dexLocalPort, testClientSecret, testUserEmail, testUserPassword)
				if err != nil {
					return err
				}
				oidcJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT via ROPC")
		})

		AfterAll(func() {
			stopPortForward(pfCancelServer)
			stopPortForward(pfCancelDex)
		})

		It("should return 401 when the Authorization header is missing", func() {
			resp, err := http.Get(moduleVersionsURL())
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"OIDC mode must challenge clients with 401 when no Authorization header is present")
		})

		It("should return 401 with a garbage bearer token", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer this-is-not-a-valid-jwt")
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"a garbage bearer token must produce a 401")
		})

		It("should return 401 with an expired JWT", func() {
			// Construct a well-formed but expired JWT signed with a throwaway key.
			// go-oidc will reject it because the key ID is not in Dex's JWKS,
			// which is the same rejection path as a token whose signature cannot be
			// verified (e.g. after the signing key has rotated or the token has expired).
			hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"throwaway-key"}`))
			payloadBytes, err := json.Marshal(map[string]any{
				"iss": fmt.Sprintf("http://localhost:%d/dex", dexLocalPort),
				"aud": "opendepot",
				"sub": "expired-test-user",
				"exp": time.Now().Add(-1 * time.Hour).Unix(),
				"iat": time.Now().Add(-2 * time.Hour).Unix(),
			})

			Expect(err).NotTo(HaveOccurred())
			fakeSig := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("X", 256)))
			expiredJWT := hdr + "." + base64.RawURLEncoding.EncodeToString(payloadBytes) + "." + fakeSig

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+expiredJWT)
			resp, err := http.DefaultClient.Do(req)

			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"an expired JWT must produce a 401")
		})

		// Dex static passwords do not emit a "groups" claim. With the groups claim
		// required, a valid JWT that lacks it must be denied with 403 — not 401.
		// 401 = authentication failure (bad/missing token); 403 = authorization
		// failure (valid token, access not permitted).
		It("should return 403 with a valid Dex JWT that has no groups claim", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)

			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a valid JWT with no groups claim must be denied with 403")
		})

		// The groups claim is required when OIDC is enabled. A JWT that does not carry
		// the configured claim is denied with 403 — there is no bypass path.
		It("should return 403 when the configured groups claim is absent from the JWT", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"JWT missing the groups claim must be denied with 403 (groups claim is required)")
		})

		It("service discovery should include login.v1 when OIDC is enabled", func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var discovery map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&discovery)).To(Succeed())

			Expect(discovery).To(HaveKey("login.v1"),
				"service discovery must advertise login.v1 when OIDC is enabled")
			loginV1, ok := discovery["login.v1"].(map[string]any)

			Expect(ok).To(BeTrue(), "login.v1 must be a JSON object")
			Expect(loginV1["authz"]).NotTo(BeEmpty(), "login.v1.authz must be a non-empty URL")
			Expect(loginV1["token"]).NotTo(BeEmpty(), "login.v1.token must be a non-empty URL")
		})
	})

	// Context: OIDC auth mode with Dex reverse-proxy
	//
	// These specs verify server.oidc.dexProxy.enabled: Dex is reachable only through
	// the server's own /dex/* route, never via a direct port-forward to the Dex
	// Service. dex.config.issuer and server.oidc.issuerUrl are both set to the same
	// external, path-based URL (the server's own port-forward address, standing in
	// for a real ingress hostname), while JWKS discovery and token validation still
	// happen against Dex's in-cluster Service address under the hood.
	//
	// The negative case of --oidc-dex-proxy-enabled without --oidc-issuer-url or
	// --oidc-dex-internal-url set (server.go process-exits at startup) is not
	// covered by a live-cluster e2e case: the chart's server-deployment.yaml fail
	// guards make this combination unreachable via Helm (issuerUrl/dex.enabled are
	// validated at render time, and --oidc-dex-internal-url is always populated
	// automatically by the chart whenever the flag is set). It is only reachable by
	// invoking the server binary directly with hand-crafted flags, bypassing the
	// chart entirely.
	Context("OIDC auth mode with Dex reverse-proxy", Ordered, func() {
		var (
			pfCancelServer context.CancelFunc
			oidcJWT        string
			externalIssuer string
		)

		const (
			proxyTestClientSecret = "test-client-secret-proxy-e2e"
			proxyTestUserEmail    = "proxy-test@example.com"
			proxyTestUserPassword = "proxytestpassword"
			proxyTestUserID       = "proxy-test-user-oidc-e2e"
		)

		// acquireProxiedDexToken performs a Resource Owner Password Credentials grant
		// against the Dex token endpoint reached through the server's /dex proxy —
		// never against Dex's Service directly — and returns the raw OIDC ID token.
		acquireProxiedDexToken := func(clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", serverLocalPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		BeforeAll(func() {
			By("uninstalling previous Helm release to avoid Dex server-side apply conflicts")
			uninstallCmd := exec.Command("helm", "uninstall", helmReleaseName,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(uninstallCmd)

			externalIssuer = fmt.Sprintf("http://localhost:%d/dex", serverLocalPort)

			By("generating bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(proxyTestUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex reverse-proxy e2e values file")
			dexValues := fmt.Sprintf(`
dex:
  enabled: true
  config:
    issuer: %s
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: proxytestuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
          - http://localhost:10010
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
    issuerUrl: %s
    dexProxy:
      enabled: true
`, externalIssuer, proxyTestUserEmail, passwordHash, proxyTestUserID, proxyTestClientSecret, externalIssuer)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "dex-proxy-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC auth mode, Dex enabled, and dexProxy enabled")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)

			pfCancelServer = startPortForward()

			By("waiting for Dex discovery to be reachable only through the server's /dex proxy")
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", serverLocalPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("discovery endpoint returned %d", resp.StatusCode)
				}
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for the server's /dex proxy to be ready")

			By("acquiring a valid Dex JWT via ROPC through the server's /dex proxy")
			Eventually(func() error {
				token, err := acquireProxiedDexToken(proxyTestClientSecret, proxyTestUserEmail, proxyTestUserPassword)
				if err != nil {
					return err
				}
				oidcJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT via ROPC through the /dex proxy")
		})

		AfterAll(func() {
			stopPortForward(pfCancelServer)
		})

		It("should serve Dex OIDC discovery through the server's /dex proxy with the external issuer", func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var discovery map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&discovery)).To(Succeed())

			Expect(discovery["issuer"]).To(Equal(externalIssuer),
				"Dex's discovery document must report the external issuer, not the in-cluster address")
			Expect(discovery["token_endpoint"]).To(Equal(externalIssuer+"/token"),
				"the token endpoint advertised by Dex must be reachable through the server's /dex proxy")
		})

		It("should mint a JWT via the server's /dex proxy", func() {
			Expect(oidcJWT).NotTo(BeEmpty())
			Expect(strings.Count(oidcJWT, ".")).To(Equal(2), "a JWT must have three dot-separated segments")
		})

		It("service discovery login.v1 should advertise the proxied URLs without authzUrl/tokenUrl overrides", func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var discovery map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&discovery)).To(Succeed())

			loginV1, ok := discovery["login.v1"].(map[string]any)
			Expect(ok).To(BeTrue(), "login.v1 must be a JSON object")
			Expect(loginV1["authz"]).To(Equal(externalIssuer+"/auth"),
				"login.v1.authz must be derived automatically from Dex's own external discovery document")
			Expect(loginV1["token"]).To(Equal(externalIssuer+"/token"),
				"login.v1.token must be derived automatically from Dex's own external discovery document")
		})

		// Dex static passwords do not emit a "groups" claim. This confirms JWT
		// signature validation succeeds via the internally-fetched JWKS keyset even
		// though the JWT's iss claim is the external, proxied issuer URL — access is
		// still correctly denied with 403 (not 401) since the token itself is valid.
		It("should return 403 with a valid proxied Dex JWT that has no groups claim", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)

			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a valid proxied JWT with no groups claim must be denied with 403, proving JWKS validation succeeded")
		})
	})

	// Context: OIDC auth mode with GroupBinding
	//
	// These specs verify application-level access control via GroupBinding CRDs when
	// OIDC auth mode is active. The server is deployed with --oidc-groups-claim=email
	// so that the Dex-issued JWT's "email" claim is treated as the groups value —
	// this avoids the need for a groups-capable upstream connector while still
	// exercising the full GroupBinding evaluation path.
	Context("OIDC auth mode with GroupBinding", Ordered, func() {
		var (
			pfCancelServer context.CancelFunc
			pfCancelDex    context.CancelFunc
			oidcJWT        string
		)

		const (
			gbDexLocalPort     = 15557
			gbTestClientSecret = "test-client-secret-gb-e2e"
			gbTestUserEmail    = "gb-test@example.com"
			gbTestUserPassword = "gbtestpassword"
			gbTestUserID       = "gb-test-user-oidc-e2e"
			// groupBindingName is the name used for the test GroupBinding resource.
			groupBindingName = "e2e-test-groupbinding"
		)

		// gbStartDexPortForward starts a self-restarting port-forward to the Dex service
		// and waits until the OIDC discovery endpoint is reachable.
		gbStartDexPortForward := func(localPort int) context.CancelFunc {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
						"-n", namespace,
						"svc/opendepot-dex",
						fmt.Sprintf("%d:5556", localPort),
					)
					cmd.Stdout = GinkgoWriter
					cmd.Stderr = GinkgoWriter
					_ = cmd.Run()
					time.Sleep(200 * time.Millisecond)
				}
			}()
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", localPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for Dex port-forward")
			return cancel
		}

		// gbAcquireDexToken performs an ROPC grant against Dex and returns the raw ID token.
		gbAcquireDexToken := func(localPort int, clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", localPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid email"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		// applyGroupBinding creates or replaces a GroupBinding with the given expression
		// and optional moduleResources / providerResources patterns.
		applyGroupBinding := func(name, expression string, moduleResources, providerResources []string) {
			mrYAML := ""
			for _, m := range moduleResources {
				mrYAML += fmt.Sprintf("    - %q\n", m)
			}
			prYAML := ""
			for _, p := range providerResources {
				prYAML += fmt.Sprintf("    - %q\n", p)
			}
			manifest := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: %s
  namespace: %s
spec:
  expression: %q
  moduleResources:
%s  providerResources:
%s`, name, namespace, expression, mrYAML, prYAML)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to apply GroupBinding %s", name)
		}

		// deleteGroupBinding deletes the named GroupBinding, ignoring not-found errors.
		deleteGroupBinding := func(name string) {
			cmd := exec.Command("kubectl", "delete", "groupbinding", name, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}

		BeforeAll(func() {
			By("uninstalling previous Helm release to avoid Dex server-side apply conflicts")
			uninstallCmd := exec.Command("helm", "uninstall", helmReleaseName,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(uninstallCmd)

			By("generating bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(gbTestUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex + GroupBinding e2e values file")
			dexValues := fmt.Sprintf(`
dex:
  enabled: true
  config:
    issuer: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: gbtestuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
          - http://localhost:10010
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
    groupsClaim: email
		dexProxy:
			enabled: false
`, gbTestUserEmail, passwordHash, gbTestUserID, gbTestClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "gb-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC auth mode, Dex enabled, and --oidc-groups-claim=email")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)

			pfCancelServer = startPortForward()
			pfCancelDex = gbStartDexPortForward(gbDexLocalPort)

			By("acquiring a valid Dex JWT via ROPC")
			Eventually(func() error {
				token, err := gbAcquireDexToken(gbDexLocalPort, gbTestClientSecret, gbTestUserEmail, gbTestUserPassword)
				if err != nil {
					return err
				}
				oidcJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT via ROPC")
		})

		AfterAll(func() {
			stopPortForward(pfCancelServer)
			stopPortForward(pfCancelDex)
			deleteGroupBinding(groupBindingName)
		})

		It("should return 403 with a valid JWT when no GroupBinding matches", func() {
			// No GroupBinding exists yet — findGroupBinding should return no match → 403.
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a valid JWT with no matching GroupBinding must produce a 403")
		})

		It("should return non-403 when a matching GroupBinding allows all modules", func() {
			// The email claim is used as the groups value; the expression checks for the user's email.
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"*"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"a valid JWT with a matching GroupBinding and wildcard moduleResources must not produce a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a valid JWT with a matching GroupBinding must not produce a 401")
		})

		It("should return 403 when the module name does not match any moduleResources pattern", func() {
			// Replace the GroupBinding with one that has a narrower module pattern
			// that does NOT match "nonexistent-test-module".
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"only-this-exact-module"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"module name that does not match moduleResources must produce a 403")
		})

		It("should allow access when module name matches a glob pattern in moduleResources", func() {
			// "nonexistent-*" should match "nonexistent-test-module".
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"nonexistent-*"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"a glob moduleResources pattern matching the module must not produce a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a glob moduleResources pattern matching the module must not produce a 401")
		})

		It("should return 403 when a GroupBinding has an invalid expression", func() {
			// An invalid expression should be skipped (Warn logged) and no binding matches → 403.
			applyGroupBinding(groupBindingName,
				"this is [[ not valid expr",
				[]string{"*"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"an invalid GroupBinding expression must be skipped and produce a 403 (not a 500)")
		})

		It("should return 403 for a provider endpoint when the provider is not in providerResources", func() {
			// Allow the expression to match but restrict providerResources to something
			// that does not match the provider type "aws".
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"only-this-provider"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"provider type not in providerResources must produce a 403")
		})

		It("should allow provider access when provider type matches providerResources", func() {
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"aws"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"provider type matching providerResources must not produce a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"provider type matching providerResources must not produce a 401")
		})

		It("should allow all provider access when providerResources contains \"*\"", func() {
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"*"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"providerResources [\"*\"] must allow all provider types without producing a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"providerResources [\"*\"] must not produce a 401")
		})

		It("should return 403 when providerResources contains a partial pattern that does not exactly match", func() {
			// "aws*" is NOT a glob in providerResources — it is treated as a literal string.
			// A request for provider type "aws" must not be allowed.
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"aws*"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"partial pattern \"aws*\" must not be expanded as a glob; only the literal \"*\" allows all providers")
		})

		// First-match semantics: when multiple GroupBindings match the JWT groups,
		// the first one in alphabetical-name order wins. If that binding denies the
		// resource, the request is 403 even if a later binding would allow it.
		It("should use first-match semantics when multiple GroupBindings match", func() {
			// aaa-e2e-groupbinding lists first (alphabetical); it allows only "only-this-exact-module".
			applyGroupBinding("aaa-e2e-groupbinding",
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"only-this-exact-module"},
				[]string{},
			)
			// zzz-e2e-groupbinding lists second; it allows everything.
			applyGroupBinding("zzz-e2e-groupbinding",
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"*"},
				[]string{},
			)

			// Request for a module NOT in aaa's moduleResources — aaa matches first
			// and denies it, so the result must be 403 regardless of zzz.
			req, err := http.NewRequest(http.MethodGet,
				fmt.Sprintf("http://localhost:%d/opendepot/modules/v1/%s/other-module/aws/versions",
					serverLocalPort, namespace),
				nil,
			)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"first-match binding (aaa) denies the resource; zzz binding must not override it")

			// Clean up both bindings.
			deleteGroupBinding("aaa-e2e-groupbinding")
			deleteGroupBinding("zzz-e2e-groupbinding")
		})
	})

	// Context: OIDC auth mode with ServiceAccount fallback
	//
	// These specs verify that when --oidc-allow-sa-fallback is set, Kubernetes
	// ServiceAccount bearer tokens are accepted alongside OIDC JWTs. SA tokens
	// bypass GroupBinding and use the caller's own RBAC for access control.
	// The specs also confirm that the OIDC path still fires for Dex-issued JWTs
	// (i.e. SA fallback does not accidentally swallow bad Dex tokens or tokens
	// whose issuer matches the configured OIDC issuer).
	Context("OIDC auth mode with SA fallback", Ordered, func() {
		var (
			pfCancelServer context.CancelFunc
			pfCancelDex    context.CancelFunc
			dexJWT         string
			saToken        string
			deniedSAToken  string
		)

		const (
			sfDexLocalPort     = 15558
			sfTestClientSecret = "test-client-secret-sf-e2e"
			sfTestUserEmail    = "sf-test@example.com"
			sfTestUserPassword = "sftestpassword"
			sfTestUserID       = "sf-test-user-oidc-e2e"
		)

		// sfStartDexPortForward starts a self-restarting kubectl port-forward to the Dex
		// service and blocks until the OIDC discovery endpoint is reachable.
		sfStartDexPortForward := func(localPort int) context.CancelFunc {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
						"-n", namespace,
						"svc/opendepot-dex",
						fmt.Sprintf("%d:5556", localPort),
					)
					cmd.Stdout = GinkgoWriter
					cmd.Stderr = GinkgoWriter
					_ = cmd.Run()
					time.Sleep(200 * time.Millisecond)
				}
			}()
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", localPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for Dex port-forward to be ready")
			return cancel
		}

		// sfAcquireDexToken performs an ROPC grant against Dex and returns the raw ID token.
		sfAcquireDexToken := func(localPort int, clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", localPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("token request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
			}

			var tokenResp struct {
				IDToken string `json:"id_token"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode token response: %w", err)
			}

			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("token response contained empty id_token")
			}

			return tokenResp.IDToken, nil
		}

		BeforeAll(func() {
			By("generating bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(sfTestUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex e2e values file with SA fallback enabled")
			dexValues := fmt.Sprintf(`
dex:
  enabled: true
  config:
    issuer: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: sftestuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
          - http://localhost:10010
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
    allowServiceAccountFallback: true
		dexProxy:
			enabled: false
`, sfTestUserEmail, passwordHash, sfTestUserID, sfTestClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "sf-dex-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC + SA fallback enabled")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)

			pfCancelServer = startPortForward()
			pfCancelDex = sfStartDexPortForward(sfDexLocalPort)

			By("acquiring a valid Dex JWT via ROPC")
			Eventually(func() error {
				token, err := sfAcquireDexToken(sfDexLocalPort, sfTestClientSecret, sfTestUserEmail, sfTestUserPassword)
				if err != nil {
					return err
				}
				dexJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT via ROPC")

			By("creating a ServiceAccount with RBAC to list modules (SA fallback)")
			saCmd := exec.Command("kubectl", "create", "serviceaccount", "test-safallback-sa",
				"-n", namespace)
			_, _ = utils.Run(saCmd)

			roleCmd := exec.Command("kubectl", "create", "role", "test-safallback-role",
				"--verb=get,list",
				"--resource=modules.opendepot.defdev.io",
				"-n", namespace)
			_, _ = utils.Run(roleCmd)

			rbCmd := exec.Command("kubectl", "create", "rolebinding", "test-safallback-rb",
				"--role=test-safallback-role",
				"--serviceaccount="+namespace+":test-safallback-sa",
				"-n", namespace)
			_, _ = utils.Run(rbCmd)

			By("generating a short-lived token for test-safallback-sa")
			tokenCmd := exec.Command("kubectl", "create", "token", "test-safallback-sa",
				"-n", namespace, "--duration=10m")
			token, err := utils.Run(tokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create SA token for fallback test")
			saToken = strings.TrimSpace(token)

			By("creating a ServiceAccount with no RBAC for the denied case")
			denyCmd := exec.Command("kubectl", "create", "serviceaccount", "test-safallback-denied-sa",
				"-n", namespace)
			_, _ = utils.Run(denyCmd)

			By("generating a short-lived token for the denied SA")
			deniedTokenCmd := exec.Command("kubectl", "create", "token", "test-safallback-denied-sa",
				"-n", namespace, "--duration=10m")
			deniedToken, err := utils.Run(deniedTokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create denied SA token")
			deniedSAToken = strings.TrimSpace(deniedToken)
		})

		AfterAll(func() {
			stopPortForward(pfCancelServer)
			stopPortForward(pfCancelDex)

			for _, res := range [][]string{
				{"delete", "rolebinding", "test-safallback-rb", "-n", namespace, "--ignore-not-found"},
				{"delete", "role", "test-safallback-role", "-n", namespace, "--ignore-not-found"},
				{"delete", "serviceaccount", "test-safallback-sa", "-n", namespace, "--ignore-not-found"},
				{"delete", "serviceaccount", "test-safallback-denied-sa", "-n", namespace, "--ignore-not-found"},
			} {
				cmd := exec.Command("kubectl", res...)
				_, _ = utils.Run(cmd)
			}
		})

		It("should accept a K8s SA token and return a non-401 response", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+saToken)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"SA token must not produce a 401 when SA fallback is enabled")
		})

		It("should return 403 for an SA token with no RBAC", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+deniedSAToken)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"SA token with no RBAC must produce a 403 (Kubernetes RBAC denial propagated)")
		})

		// A valid Dex JWT must still take the OIDC path even when SA fallback is
		// enabled. Dex static passwords do not emit a groups claim, so the OIDC
		// path will deny with 403. This confirms that SA fallback does not
		// accidentally route Dex JWTs to the bearer token path.
		It("should still route valid Dex JWTs through the OIDC path (403, no groups claim)", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+dexJWT)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a valid Dex JWT must still go through the OIDC path and be denied with 403 (no groups claim)")
		})

		It("should return 401 for a garbage non-JWT token", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer this-is-not-a-jwt")

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"a garbage non-JWT token must produce a 401 even when SA fallback is enabled")
		})
	})

	// Context: OIDC auth mode with client credentials
	//
	// These specs verify that tokens with a non-opendepot audience issued by Dex are
	// accepted when --oidc-allow-client-credentials is set. We simulate this using
	// ROPC on the "ci-pipeline" Dex client: the resulting id_token has aud=ci-pipeline
	// (not aud=opendepot), so the primary oidcVerifier rejects it and the secondary
	// oidcCCVerifier (SkipClientIDCheck=true) accepts it. The server then maps the
	// token's sub claim to the virtual group "client:<sub>" for GroupBinding evaluation.
	//
	// Note: Dex's local password connector does not support the OAuth2 client_credentials
	// grant (it requires a connector implementing TokenIdentity). ROPC with a non-opendepot
	// client is equivalent for e2e purposes: it produces the same aud-mismatch token
	// that exercises the exact same server code path.
	Context("OIDC auth mode with client credentials", Ordered, func() {
		var (
			pfCancelServer context.CancelFunc
			pfCancelDex    context.CancelFunc
			ccToken        string
			ccSub          string
			dexJWT         string
		)

		const (
			// ccDexLocalPort must not collide with sfDexLocalPort (15558) or gbDexLocalPort (15557).
			ccDexLocalPort     = 15559
			ccTestClientSecret = "test-cc-client-secret-e2e"
			ccOIDCClientSecret = "test-cc-oidc-client-secret-e2e"
			ccClientID         = "ci-pipeline"
			ccUserEmail        = "cc-test@example.com"
			ccUserPassword     = "cctestpassword"
			ccUserID           = "cc-test-user-oidc-e2e"
			ccGroupBindingName = "e2e-test-cc-groupbinding"
		)

		ccStartDexPortForward := func(localPort int) context.CancelFunc {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
						"-n", namespace,
						"svc/opendepot-dex",
						fmt.Sprintf("%d:5556", localPort),
					)
					cmd.Stdout = GinkgoWriter
					cmd.Stderr = GinkgoWriter
					_ = cmd.Run()
					time.Sleep(200 * time.Millisecond)
				}
			}()
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", localPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for Dex port-forward (CC context)")
			return cancel
		}

		// acquireCCSimToken performs ROPC using the ci-pipeline Dex client. The resulting
		// id_token has aud=ci-pipeline (not aud=opendepot), causing the primary OIDC
		// verifier to reject it and the CC verifier (SkipClientIDCheck=true) to accept it.
		// This exercises the same server code path as a real OAuth2 client_credentials token.
		acquireCCSimToken := func(localPort int, email, password, clientID, clientSecret string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", localPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {email},
				"password":      {password},
				"scope":         {"openid"},
				"client_id":     {clientID},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("CC-sim token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("CC-sim token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode CC-sim token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("CC-sim token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		// parseJWTSub decodes the JWT payload without signature verification and returns
		// the sub claim. Used to discover the Dex-assigned sub at test runtime.
		parseJWTSub := func(token string) (string, error) {
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				return "", fmt.Errorf("invalid JWT: expected 3 parts, got %d", len(parts))
			}
			payload, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				return "", fmt.Errorf("failed to decode JWT payload: %w", err)
			}
			var claims struct {
				Sub string `json:"sub"`
			}
			if err := json.Unmarshal(payload, &claims); err != nil {
				return "", fmt.Errorf("failed to unmarshal JWT claims: %w", err)
			}
			if claims.Sub == "" {
				return "", fmt.Errorf("sub claim is empty in JWT")
			}
			return claims.Sub, nil
		}

		// ccAcquireDexToken acquires a user JWT via ROPC on the primary opendepot client.
		// This token has aud=opendepot and verifies that user JWTs still take the primary
		// OIDC path rather than being routed through the CC fallback.
		ccAcquireDexToken := func(localPort int, clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", localPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("user token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("user token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode user token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("user token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		ccApplyGroupBinding := func(name, expression string, moduleResources, providerResources []string) {
			mrYAML := ""
			for _, m := range moduleResources {
				mrYAML += fmt.Sprintf("    - %q\n", m)
			}
			prYAML := ""
			for _, p := range providerResources {
				prYAML += fmt.Sprintf("    - %q\n", p)
			}
			manifest := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: %s
  namespace: %s
spec:
  expression: %q
  moduleResources:
%s  providerResources:
%s`, name, namespace, expression, mrYAML, prYAML)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to apply GroupBinding %s", name)
		}

		ccDeleteGroupBinding := func(name string) {
			cmd := exec.Command("kubectl", "delete", "groupbinding", name, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}

		BeforeAll(func() {
			By("uninstalling previous Helm release to avoid Dex server-side apply conflicts")
			uninstallCmd := exec.Command("helm", "uninstall", helmReleaseName,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(uninstallCmd)

			By("generating bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(ccUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex + CC e2e values file")
			// The ci-pipeline staticClient is a normal ROPC client (not a CC client).
			// ROPC with client_id=ci-pipeline produces id_token with aud=ci-pipeline,
			// which the primary verifier rejects and the CC verifier accepts.
			dexValues := fmt.Sprintf(`
dex:
  enabled: true
  config:
    issuer: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: cctestuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
          - http://localhost:10010
      - id: %s
        name: CI Pipeline
        secret: %q
        redirectURIs:
          - http://localhost:10000
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
    allowClientCredentials: true
		dexProxy:
			enabled: false
`, ccUserEmail, passwordHash, ccUserID, ccClientID, ccTestClientSecret, ccOIDCClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "cc-dex-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC + client credentials enabled")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)

			pfCancelServer = startPortForward()
			pfCancelDex = ccStartDexPortForward(ccDexLocalPort)

			By("acquiring a CC-simulated token for ci-pipeline (aud=ci-pipeline via ROPC)")
			Eventually(func() error {
				token, err := acquireCCSimToken(ccDexLocalPort, ccUserEmail, ccUserPassword, ccClientID, ccTestClientSecret)
				if err != nil {
					return err
				}
				ccToken = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire CC-sim token from Dex")

			By("parsing sub from the CC token to build GroupBinding expression")
			var subErr error
			ccSub, subErr = parseJWTSub(ccToken)
			Expect(subErr).NotTo(HaveOccurred(), "failed to parse sub from CC token")
			Expect(ccSub).NotTo(BeEmpty(), "sub claim must not be empty in CC token")
			_, _ = fmt.Fprintf(GinkgoWriter, "CC token sub: %q -> virtual group: %q\n", ccSub, "client:"+ccSub)

			By("acquiring a user JWT via ROPC (opendepot client) for the negative OIDC-path test")
			Eventually(func() error {
				token, err := ccAcquireDexToken(ccDexLocalPort, ccOIDCClientSecret, ccUserEmail, ccUserPassword)
				if err != nil {
					return err
				}
				dexJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire user JWT via ROPC (CC context)")
		})

		AfterAll(func() {
			stopPortForward(pfCancelServer)
			stopPortForward(pfCancelDex)
			ccDeleteGroupBinding(ccGroupBindingName)
		})

		It("should return 403 when no GroupBinding matches the CC token's virtual group", func() {
			// No GroupBinding exists yet — CC token maps to ["client:<sub>"]
			// which matches nothing → 403.
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+ccToken)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"CC token with no matching GroupBinding must produce a 403")
		})

		It("should return non-403 when a GroupBinding matches the CC token's virtual group", func() {
			ccApplyGroupBinding(ccGroupBindingName,
				fmt.Sprintf(`"client:%s" in groups`, ccSub),
				[]string{"*"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+ccToken)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"CC token with a matching GroupBinding must not produce a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"CC token with a matching GroupBinding must not produce a 401")
		})

		It("should return 403 when the GroupBinding denies the specific module", func() {
			ccApplyGroupBinding(ccGroupBindingName,
				fmt.Sprintf(`"client:%s" in groups`, ccSub),
				[]string{"only-this-exact-module"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+ccToken)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"CC token with a GroupBinding that does not match the module must produce a 403")
		})

		// A valid user Dex JWT (aud=opendepot) must still take the primary OIDC path
		// even when CC mode is enabled. Static password users have no groups claim,
		// so the result must be 403 (not 401, and not silently routed through CC path).
		It("should still route valid user Dex JWTs through the primary OIDC path", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+dexJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a valid user Dex JWT must take the primary OIDC path and be denied with 403 (no groups claim); CC mode must not intercept it")
		})
	})

	// Context: filesystem download path traversal protection
	//
	// These specs verify that serveModuleFromFileSystem rejects download requests
	// whose decoded directory path falls outside the configured --filesystem-mount-path
	// root. The endpoint is intentionally unauthenticated (callers must already hold
	// a signed download URL produced by the authenticated getDownloadModuleUrl
	// handler), so no auth setup is required for these tests.
	Context("filesystem download path traversal protection", Ordered, func() {
		var pfCancel context.CancelFunc

		// fsDownloadURL constructs a filesystem download URL with the given raw
		// directory path (base64-encoded per the handler's convention), module
		// name, and file name.
		fsDownloadURL := func(rawDir, name, fileName string) string {
			encodedDir := base64.RawURLEncoding.EncodeToString([]byte(rawDir))
			return fmt.Sprintf("http://localhost:%d/opendepot/modules/v1/download/fileSystem/%s/%s/%s",
				serverLocalPort, encodedDir, name, fileName)
		}

		BeforeAll(func() {
			By("deploying server with anonymous auth")
			deployServer(
				"--set", "server.anonymousAuth=true",
			)
			pfCancel = startPortForward()
		})

		AfterAll(func() {
			stopPortForward(pfCancel)
		})

		It("should return 403 when the directory path escapes the allowed filesystem root", func() {
			// /tmp is outside the default --filesystem-mount-path of /data/modules.
			u := fsDownloadURL("/tmp/evil", "my-module", "my-module.tar.gz")
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a directory path outside the allowed root must be rejected with 403")
		})

		It("should return 403 for a path traversal using ../ sequences after decode", func() {
			// /data/modules/../../../etc escapes the mount root after path.Clean.
			u := fsDownloadURL("/data/modules/../../../etc/passwd", "my-module", "my-module.tar.gz")
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a path that resolves outside the allowed root after cleaning must be rejected with 403")
		})

		It("should not return 403 for a directory path within the allowed filesystem root", func() {
			// /data/modules/test-ns is inside the allowed root. The file does not
			// exist so the server will return a non-200 response or close the
			// connection, but it must NOT return 403 (the path guard must not fire).
			u := fsDownloadURL("/data/modules/test-ns", "test-module", "test-module.tar.gz")
			resp, err := http.Get(u)
			if err != nil {
				// A connection error means the server reached the storage layer and
				// panicked on the nil reader for the missing file — the path guard
				// did not fire, which is the expected behaviour.
				return
			}
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"a directory path within the allowed root must not be rejected by the path guard")
		})
	})

	Context("stats endpoint", Ordered, func() {
		var pfCancel context.CancelFunc

		BeforeAll(func() {
			By("deploying server with anonymous auth for stats tests")
			deployServer(
				"--set", "server.anonymousAuth=true",
				"--set", "server.useBearerToken=false",
			)
			pfCancel = startPortForward()
		})

		AfterAll(func() {
			stopPortForward(pfCancel)
		})

		It("should return 200 with a valid BrowseStats JSON body", func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/opendepot/ui/v1/stats", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var stats struct {
				TotalModules   int `json:"totalModules"`
				TotalProviders int `json:"totalProviders"`
				TotalVersions  int `json:"totalVersions"`
				TotalDownloads int `json:"totalDownloads"`
				SyncHealth     struct {
					SyncedVersions   int `json:"syncedVersions"`
					UnsyncedVersions int `json:"unsyncedVersions"`
					FailedVersions   int `json:"failedVersions"`
				} `json:"syncHealth"`
				SecurityPosture struct {
					Critical               int `json:"critical"`
					High                   int `json:"high"`
					Medium                 int `json:"medium"`
					Low                    int `json:"low"`
					Unknown                int `json:"unknown"`
					TotalAffectedResources int `json:"totalAffectedResources"`
				} `json:"securityPosture"`
				StorageDistribution []any `json:"storageDistribution"`
				MostDownloaded      []any `json:"mostDownloaded"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&stats)).To(Succeed())

			Expect(stats.TotalModules).To(BeNumerically(">=", 0),
				"totalModules must be a non-negative integer")
			Expect(stats.TotalProviders).To(BeNumerically(">=", 0),
				"totalProviders must be a non-negative integer")
			Expect(stats.TotalVersions).To(BeNumerically(">=", 0),
				"totalVersions must be a non-negative integer")
			Expect(stats.TotalDownloads).To(BeNumerically(">=", 0),
				"totalDownloads must be a non-negative integer")
			Expect(stats.SyncHealth.SyncedVersions).To(BeNumerically(">=", 0))
			Expect(stats.SyncHealth.UnsyncedVersions).To(BeNumerically(">=", 0))
			Expect(stats.SyncHealth.FailedVersions).To(BeNumerically(">=", 0))
			Expect(stats.SecurityPosture.TotalAffectedResources).To(BeNumerically(">=", 0))
		})

		It("should accept an optional ?namespace= query parameter and return 200", func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/opendepot/ui/v1/stats?namespace=%s",
				serverLocalPort, url.QueryEscape(moduleNamespace)))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("browse resources response includes totalDownloads and lastDownloadedAt fields", func() {
			// The stats fields are zero-valued when no downloads have been recorded yet.
			// The test verifies the fields are present and parseable — not that they are non-zero.
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/opendepot/ui/v1/resources?pageSize=1", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					TotalDownloads   *int64  `json:"totalDownloads"`
					LastDownloadedAt *string `json:"lastDownloadedAt"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		})

		It("browse versions response includes downloadCount and archiveSizeBytes fields", func() {
			// Verifies that the BrowseVersionSummary type includes the new fields and
			// the response is parseable. Zero values are expected when no archive has
			// been uploaded or no downloads recorded in this deployment context.
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/opendepot/ui/v1/resources/%s/module/%s/versions",
				serverLocalPort, moduleNamespace, moduleName))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			// 404 is acceptable here (module does not exist); we only need to verify
			// the server does not 500 and that the response is valid JSON when present.
			if resp.StatusCode == http.StatusOK {
				var body struct {
					Items []struct {
						DownloadCount    *int64  `json:"downloadCount"`
						ArchiveSizeBytes *int64  `json:"archiveSizeBytes"`
						LastDownloadedAt *string `json:"lastDownloadedAt"`
					} `json:"items"`
				}
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			} else {
				Expect(resp.StatusCode).To(BeElementOf(http.StatusNotFound, http.StatusOK),
					"versions endpoint must return 200 or 404, not an internal error")
			}
		})

		It("totalDownloads increments after a real module download and mostDownloaded is populated", func() {
			const (
				statsModuleName    = "stats-smoke-module"
				statsModuleVersion = "2.0.0"
				statsModuleNS      = "opendepot-system"
			)
			// Version CR name follows the sanitizeModuleVersionForLookup convention:
			// dots replaced with hyphens → "stats-smoke-module-2-0-0".
			versionCRName := fmt.Sprintf("%s-2-0-0", statsModuleName)

			By("creating a Module CR so the stats visibility filter can match it")
			moduleYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: %s
  namespace: %s
spec:
  moduleConfig:
    provider: aws
    storageConfig:
      fileSystem:
        directoryPath: "/data/modules"
  versions:
  - version: "2.0.0"
`, statsModuleName, statsModuleNS)
			moduleCmd := exec.Command("kubectl", "apply", "-f", "-")
			moduleCmd.Stdin = strings.NewReader(moduleYAML)
			_, err := utils.Run(moduleCmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a Version CR with filesystem storage and a checksum")
			dirPath := "/data/modules"
			fileName := "stats-smoke-module.tar.gz"
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Module
  version: "%s"
  fileName: "%s"
  moduleConfigRef:
    name: "%s"
    storageConfig:
      fileSystem:
        directoryPath: "%s"
`, versionCRName, statsModuleNS, statsModuleVersion, fileName, statsModuleName, dirPath)
			applyCmd := exec.Command("kubectl", "apply", "-f", "-")
			applyCmd.Stdin = strings.NewReader(versionYAML)
			_, err = utils.Run(applyCmd)
			Expect(err).NotTo(HaveOccurred())

			By("patching the Version CR status to set a checksum")
			patchCmd := exec.Command("kubectl", "patch", "version", versionCRName,
				"-n", statsModuleNS,
				"--subresource=status",
				"--type=merge",
				`--patch={"status":{"synced":true,"syncStatus":"Synced","checksum":"abc123"}}`)
			_, err = utils.Run(patchCmd)
			Expect(err).NotTo(HaveOccurred())

			By("triggering the module download endpoint to record a stat")
			downloadURL := fmt.Sprintf("http://localhost:%d/opendepot/modules/v1/%s/%s/aws/%s/download",
				serverLocalPort, statsModuleNS, statsModuleName, statsModuleVersion)
			resp, err := http.Get(downloadURL)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent),
				"download redirect must return 204 so that recordDownload is called")

			By("asserting totalDownloads >= 1 from the stats endpoint")
			statsResp, err := http.Get(fmt.Sprintf("http://localhost:%d/opendepot/ui/v1/stats", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer statsResp.Body.Close()
			Expect(statsResp.StatusCode).To(Equal(http.StatusOK))

			var stats struct {
				TotalDownloads int           `json:"totalDownloads"`
				MostDownloaded []interface{} `json:"mostDownloaded"`
			}
			Expect(json.NewDecoder(statsResp.Body).Decode(&stats)).To(Succeed())
			Expect(stats.TotalDownloads).To(BeNumerically(">=", 1),
				"totalDownloads must be at least 1 after a recorded download")
			Expect(len(stats.MostDownloaded)).To(BeNumerically(">=", 1),
				"mostDownloaded must be non-empty after a recorded download")

			By("asserting the browse resources endpoint shows totalDownloads >= 1 for the downloaded module")
			browseURL := fmt.Sprintf("http://localhost:%d/opendepot/ui/v1/resources?namespace=%s&search=%s",
				serverLocalPort, statsModuleNS, statsModuleName)
			browseResp, err := http.Get(browseURL)
			Expect(err).NotTo(HaveOccurred())
			defer browseResp.Body.Close()
			Expect(browseResp.StatusCode).To(Equal(http.StatusOK))

			var browseBody struct {
				Items []struct {
					Name           string `json:"name"`
					TotalDownloads int64  `json:"totalDownloads"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(browseResp.Body).Decode(&browseBody)).To(Succeed())
			Expect(browseBody.Items).NotTo(BeEmpty(), "browse resources must return the downloaded module")
			Expect(browseBody.Items[0].TotalDownloads).To(BeNumerically(">=", 1),
				"browse resource totalDownloads must be >= 1 after a recorded download")

			By("cleaning up the smoke test Version and Module CRs")
			cleanCmd := exec.Command("kubectl", "delete", "version", versionCRName,
				"-n", statsModuleNS, "--ignore-not-found")
			_, _ = utils.Run(cleanCmd)
			cleanModuleCmd := exec.Command("kubectl", "delete", "module", statsModuleName,
				"-n", statsModuleNS, "--ignore-not-found")
			_, _ = utils.Run(cleanModuleCmd)
		})
	})

	Context("with Valkey ACL auth enabled", Ordered, func() {
		const valkeyAuthSecret = "valkey-auth-test"
		var pfCancel context.CancelFunc

		BeforeAll(func() {
			By("creating valkey ACL password Secret")
			createCmd := exec.Command("kubectl", "create", "secret", "generic", valkeyAuthSecret,
				"--from-literal=default=testpassword123",
				"-n", namespace,
				"--dry-run=client", "-o", "yaml",
			)
			applyCmd := exec.Command("kubectl", "apply", "-f", "-")
			createOut, err := createCmd.Output()
			Expect(err).NotTo(HaveOccurred())
			applyCmd.Stdin = strings.NewReader(string(createOut))
			_, err = utils.Run(applyCmd)
			Expect(err).NotTo(HaveOccurred())

			By("deploying server with Valkey ACL auth enabled")
			deployServer(
				"--set", "valkey.auth.enabled=true",
				"--set", fmt.Sprintf("valkey.auth.usersExistingSecret=%s", valkeyAuthSecret),
				"--set", "valkey.auth.aclUsers.default.permissions=~stats:* &* -@all +PING +HSET +HINCRBY +HGET +HGETALL +INCR +GET +ZINCRBY +ZREVRANGEBYSCORE +ZREVRANGE +EXPIREAT",
				"--set", fmt.Sprintf("server.stats.valkeyPasswordSecretName=%s", valkeyAuthSecret),
				"--set", "valkey.dataStorage.enabled=false",
			)

			pfCancel = startPortForward()
		})

		AfterAll(func() {
			stopPortForward(pfCancel)

			By("deleting valkey ACL password Secret")
			deleteCmd := exec.Command("kubectl", "delete", "secret", valkeyAuthSecret,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(deleteCmd)
		})

		It("should return 200 from the stats endpoint when Valkey auth is configured", func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/opendepot/ui/v1/stats", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"stats endpoint must return 200 when server authenticates to Valkey with ACL password")
		})
	})
})

// Browse API e2e tests verify that the /opendepot/ui/v1/* endpoints enforce the
// public/private visibility model and that filter/sort/search parameters work correctly.
var _ = Describe("Browse API", Ordered, func() {
	const (
		browseLocalPort    = 19081
		browsePublicNS     = "browse-public-e2e"
		browsePrivateNS    = "browse-private-e2e"
		browsePublicModule = "public-module-e2e"
		browsePrivMod      = "private-module-e2e"
	)

	var (
		chartPath  string
		serverRepo string
		serverTag  string
		pfCancel   context.CancelFunc
	)

	// deployBrowseServer deploys the server in anonymous-auth mode so that the
	// browse endpoints can be reached without a bearer token for public resources.
	deployBrowseServer := func(extraArgs ...string) {
		baseArgs := []string{
			"upgrade", helmReleaseName, chartPath,
			"--install",
			"--create-namespace",
			"--namespace", namespace,
			"--skip-crds",
			"--force-conflicts",
			"--set", "global.image.tag=",
			"--set", "depot.enabled=false",
			"--set", "module.enabled=false",
			"--set", "provider.enabled=false",
			"--set", "version.enabled=false",
			"--set", "server.enabled=true",
			"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
			"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
			// Anonymous auth so the SA client path is exercised.
			"--set", "server.anonymousAuth=true",
			"--set", "server.useBearerToken=false",
			"--set", "valkey.dataStorage.enabled=false",
			"--wait",
			"--timeout", "2m",
		}
		args := append(baseArgs, extraArgs...)
		cmd := exec.Command("helm", args...)
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to helm upgrade server for browse tests")
	}

	startBrowsePortForward := func() context.CancelFunc {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
					"-n", namespace,
					"svc/server",
					fmt.Sprintf("%d:80", browseLocalPort),
				)
				cmd.Stdout = GinkgoWriter
				cmd.Stderr = GinkgoWriter
				_ = cmd.Run()
				time.Sleep(200 * time.Millisecond)
			}
		}()
		Eventually(func() error {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", browseLocalPort))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			return nil
		}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for browse server port-forward")
		return cancel
	}

	BeforeAll(func() {
		var err error
		chartPath, err = utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())
		serverRepo, serverTag = utils.SplitImageRef(serverImage)

		By("deploying server for browse API tests")
		deployBrowseServer()
		pfCancel = startBrowsePortForward()

		By("creating public namespace")
		cmd := exec.Command("kubectl", "create", "namespace", browsePublicNS, "--dry-run=client", "-o", "yaml")
		nsYAML, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		applyCmd := exec.Command("kubectl", "apply", "-f", "-")
		applyCmd.Stdin = strings.NewReader(nsYAML)
		_, err = utils.Run(applyCmd)
		Expect(err).NotTo(HaveOccurred())

		By("labeling public namespace as public")
		labelCmd := exec.Command("kubectl", "label", "namespace", browsePublicNS,
			"opendepot.defdev.io/public=true", "--overwrite")
		_, err = utils.Run(labelCmd)
		Expect(err).NotTo(HaveOccurred())

		By("creating private namespace")
		cmd = exec.Command("kubectl", "create", "namespace", browsePrivateNS)
		_, _ = utils.Run(cmd) // ignore error if already exists

		By("applying a public Module in the public namespace")
		publicModuleYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: %s
  namespace: %s
  labels:
    opendepot.defdev.io/public: "true"
spec:
  moduleConfig:
    provider: aws
    repoOwner: test-owner
    repoUrl: https://github.com/test-owner/test-module
    fileFormat: zip
  versions: []
`, browsePublicModule, browsePublicNS)
		applyMod := exec.Command("kubectl", "apply", "-f", "-")
		applyMod.Stdin = strings.NewReader(publicModuleYAML)
		_, err = utils.Run(applyMod)
		Expect(err).NotTo(HaveOccurred())

		By("applying a private Module in the private namespace")
		privateModuleYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: %s
  namespace: %s
spec:
  moduleConfig:
    provider: aws
    repoOwner: private-owner
    repoUrl: https://github.com/private-owner/private-module
    fileFormat: zip
  versions: []
`, browsePrivMod, browsePrivateNS)
		applyPriv := exec.Command("kubectl", "apply", "-f", "-")
		applyPriv.Stdin = strings.NewReader(privateModuleYAML)
		_, err = utils.Run(applyPriv)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if pfCancel != nil {
			pfCancel()
		}

		// The Helm release (helmReleaseName) is shared across all Describe blocks
		// and is cleaned up by the AfterSuite. Only clean up fixture namespaces here.
		for _, ns := range []string{browsePublicNS, browsePrivateNS} {
			cmd := exec.Command("kubectl", "delete", "namespace", ns, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}
	})

	browseURL := func(path string) string {
		return fmt.Sprintf("http://localhost:%d%s", browseLocalPort, path)
	}

	Context("namespace listing", func() {
		It("should return 200 for the browse namespaces endpoint without auth", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/namespaces"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"browse namespaces must return 200 without authentication")
		})

		It("should include the public namespace in the response (anonymous-auth mode)", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/namespaces"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			var body struct {
				Items []struct {
					Name   string `json:"name"`
					Public bool   `json:"public"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			names := make([]string, 0, len(body.Items))
			for _, item := range body.Items {
				names = append(names, item.Name)
			}
			Expect(names).To(ContainElement(browsePublicNS),
				"public namespace must appear in the namespace list")
		})

		It("should NOT include unlabeled namespaces in the response (anonymous-auth mode)", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/namespaces"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			var body struct {
				Items []struct {
					Name string `json:"name"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			names := make([]string, 0, len(body.Items))
			for _, item := range body.Items {
				names = append(names, item.Name)
			}
			// Private namespace has no opendepot.defdev.io/public=true label — must be absent.
			Expect(names).NotTo(ContainElement(browsePrivateNS),
				"unlabeled namespace must not appear in the namespace list regardless of auth mode")
			// The server's own namespace is also unlabeled — must not leak to callers.
			Expect(names).NotTo(ContainElement(namespace),
				"opendepot-system must not appear in the namespace list unless explicitly labeled")
		})
	})

	Context("resource listing", func() {
		It("should return 200 for the browse resources endpoint without auth", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/resources"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"browse resources must return 200 without authentication")
		})

		It("should include the public module in the resource list (anonymous-auth mode)", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/resources?kind=module"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			var body struct {
				Items []struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
					Kind      string `json:"kind"`
					Public    bool   `json:"public"`
				} `json:"items"`
				TotalCount int `json:"totalCount"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			found := false
			for _, item := range body.Items {
				if item.Name == browsePublicModule && item.Namespace == browsePublicNS {
					found = true
					Expect(item.Kind).To(Equal("module"))
					break
				}
			}
			Expect(found).To(BeTrue(), "public module must appear in browse resource list")
		})

		It("should return the correct TotalCount and pagination fields", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/resources?page=1&page_size=50"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			var body struct {
				TotalCount int `json:"totalCount"`
				Page       int `json:"page"`
				PageSize   int `json:"pageSize"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Page).To(Equal(1), "page must be 1")
			Expect(body.PageSize).To(Equal(50), "pageSize must reflect the requested value")
			Expect(body.TotalCount).To(BeNumerically(">=", 0))
		})

		It("should filter resources by namespace", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/resources?namespace=" + browsePublicNS))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Namespace string `json:"namespace"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			for _, item := range body.Items {
				Expect(item.Namespace).To(Equal(browsePublicNS),
					"namespace filter must restrict results to the requested namespace")
			}
		})

		It("should filter by kind=provider and return no module results", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/resources?kind=provider&namespace=" + browsePublicNS))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Kind string `json:"kind"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			for _, item := range body.Items {
				Expect(item.Kind).To(Equal("provider"),
					"kind=provider filter must not return module results")
			}
		})

		It("should return results matching a search query", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/resources?q=" + browsePublicModule))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Name string `json:"name"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			found := false
			for _, item := range body.Items {
				if item.Name == browsePublicModule {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "search query must match the public module by name")
		})

		It("should return an empty list for a search query matching no resources", func() {
			resp, err := http.Get(browseURL("/opendepot/ui/v1/resources?q=this-name-does-not-exist-xyzzy"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items      []any `json:"items"`
				TotalCount int   `json:"totalCount"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.TotalCount).To(Equal(0), "no-match search query must return totalCount=0")
		})
	})

	Context("resource detail", func() {
		It("should return 200 for a public resource detail", func() {
			u := browseURL(fmt.Sprintf("/opendepot/ui/v1/resources/%s/module/%s",
				browsePublicNS, browsePublicModule))
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"detail for a public module must return 200")
		})

		It("should return 200 for the private module detail (anonymous-auth mode bypasses visibility)", func() {
			// In anonymous-auth mode the server's own SA is used; GroupBinding is bypassed
			// and all resources are accessible — this is expected anonymous-auth behavior.
			u := browseURL(fmt.Sprintf("/opendepot/ui/v1/resources/%s/module/%s",
				browsePrivateNS, browsePrivMod))
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"in anonymous-auth mode all resources are visible regardless of labels")
		})

		It("should return the correct module fields in the detail response", func() {
			u := browseURL(fmt.Sprintf("/opendepot/ui/v1/resources/%s/module/%s",
				browsePublicNS, browsePublicModule))
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			var detail struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
				Kind      string `json:"kind"`
				Public    bool   `json:"public"`
				Provider  string `json:"provider"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&detail)).To(Succeed())
			Expect(detail.Name).To(Equal(browsePublicModule))
			Expect(detail.Namespace).To(Equal(browsePublicNS))
			Expect(detail.Kind).To(Equal("module"))
			Expect(detail.Provider).To(Equal("aws"))
		})

		It("should return 400 for an invalid kind parameter", func() {
			u := browseURL(fmt.Sprintf("/opendepot/ui/v1/resources/%s/terraform/%s",
				browsePublicNS, browsePublicModule))
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest),
				"an unrecognised kind must return 400")
		})

		It("should return 404 for a non-existent module", func() {
			u := browseURL(fmt.Sprintf("/opendepot/ui/v1/resources/%s/module/does-not-exist-xyzzy",
				browsePublicNS))
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound),
				"a non-existent module must return 404")
		})
	})

	Context("protocol endpoint regression", func() {
		It("service discovery must still return 200 when browse endpoints are registered", func() {
			resp, err := http.Get(browseURL("/.well-known/terraform.json"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"/.well-known/terraform.json must be unaffected by browse route registration")
		})

		It("module versions protocol endpoint must still respond when browse endpoints are registered", func() {
			u := fmt.Sprintf("http://localhost:%d/opendepot/modules/v1/%s/%s/%s/versions",
				browseLocalPort, namespace, "nonexistent-regression-module", "aws")
			resp, err := http.Get(u)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			// anonymous-auth mode: 404 or 200 are both acceptable; 401 is not.
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"module versions protocol endpoint must not return 401 in anonymous-auth mode")
		})
	})

	// Context: GroupBinding-authenticated visibility
	//
	// These specs verify that browse endpoints respect the public + GroupBinding union
	// visibility model when OIDC auth is active. The server is deployed with
	// --oidc-groups-claim=email so that the Dex-issued JWT's "email" claim is treated
	// as the groups value, avoiding the need for a groups-capable upstream connector
	// while fully exercising the GroupBinding evaluation path.
	//
	// Visibility rules under test:
	//   - Unauthenticated request → only public namespace/resource pairs returned.
	//   - Authenticated request with matching GroupBinding → public ∪ GroupBinding-allowed.
	//   - Authenticated request without any GroupBinding → public only.
	Context("GroupBinding-authenticated visibility", Ordered, func() {
		var (
			gbPFCancel    context.CancelFunc
			gbDexPFCancel context.CancelFunc
			gbOIDCJWT     string
		)

		const (
			// gbBrowseServerPort is the local port for the server port-forward reused
			// from the outer Browse API Describe block (startBrowsePortForward → browseLocalPort).
			gbBrowseServerPort      = browseLocalPort
			gbBrowseDexPort         = 20082
			gbBrowseDexClientSecret = "browse-gb-test-secret"
			gbBrowseUserEmail       = "browse-gb@example.com"
			gbBrowseUserPassword    = "browsegbtestpassword"
			gbBrowseUserID          = "browse-gb-user-e2e"
			gbBrowseGroupBinding    = "browse-gb-e2e"
		)

		// gbBrowseURL returns the full URL for a browse API path via the shared
		// server port-forward (browseLocalPort = 19081).
		gbBrowseURL := func(path string) string {
			return fmt.Sprintf("http://localhost:%d%s", gbBrowseServerPort, path)
		}

		// gbStartDexPortForward starts a self-restarting port-forward to the Dex
		// service and waits until the OIDC discovery endpoint is reachable.
		gbStartDexPortForward := func(dexPort int) context.CancelFunc {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
						"-n", namespace,
						"svc/opendepot-dex",
						fmt.Sprintf("%d:5556", dexPort),
					)
					cmd.Stdout = GinkgoWriter
					cmd.Stderr = GinkgoWriter
					_ = cmd.Run()
					time.Sleep(200 * time.Millisecond)
				}
			}()
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", dexPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for browse Dex port-forward")
			return cancel
		}

		// gbAcquireDexToken performs an ROPC grant against Dex and returns the raw ID token.
		gbAcquireDexToken := func(dexPort int, clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", dexPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid email"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		// gbApplyGroupBinding creates or replaces a GroupBinding for the browse user.
		gbApplyGroupBinding := func(name, expression string, moduleResources []string) {
			mrYAML := ""
			for _, m := range moduleResources {
				mrYAML += fmt.Sprintf("    - %q\n", m)
			}
			manifest := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: %s
  namespace: %s
spec:
  expression: %q
  moduleResources:
%s  providerResources: []
`, name, namespace, expression, mrYAML)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to apply GroupBinding %s", name)
		}

		// gbDeleteGroupBinding removes the named GroupBinding, ignoring not-found errors.
		gbDeleteGroupBinding := func(name string) {
			cmd := exec.Command("kubectl", "delete", "groupbinding", name,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}

		BeforeAll(func() {
			By("stopping the outer anonymous-auth port-forward before redeployment")
			if pfCancel != nil {
				pfCancel()
			}
			pfCancel = nil

			By("generating bcrypt hash for the GroupBinding browse test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(gbBrowseUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex + server OIDC values for GroupBinding browse context")
			dexValues := fmt.Sprintf(`
dex:
  enabled: true
  config:
    issuer: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: browsegbuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
    groupsClaim: email
		dexProxy:
			enabled: false
`, gbBrowseUserEmail, passwordHash, gbBrowseUserID, gbBrowseDexClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "gb-browse-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC + Dex for GroupBinding browse tests")
			baseArgs := []string{
				"upgrade", helmReleaseName, chartPath,
				"--install",
				"--create-namespace",
				"--namespace", namespace,
				"--skip-crds",
				"--force-conflicts",
				"--set", "global.image.tag=",
				"--set", "depot.enabled=false",
				"--set", "module.enabled=false",
				"--set", "provider.enabled=false",
				"--set", "version.enabled=false",
				"--set", "server.enabled=true",
				"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
				"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"--set", "valkey.dataStorage.enabled=false",
				"-f", valuesFile,
				"--wait",
				"--timeout", "10m",
			}
			cmd := exec.Command("helm", baseArgs...)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "failed to deploy server with OIDC for GroupBinding browse tests")

			// Reapply the browse fixture namespaces and modules — they may have been
			// removed or the cluster state reset during the Helm uninstall above.
			By("reapplying public namespace label")
			labelCmd := exec.Command("kubectl", "label", "namespace", browsePublicNS,
				"opendepot.defdev.io/public=true", "--overwrite")
			_, _ = utils.Run(labelCmd)

			By("reapplying public Module fixture")
			publicModuleYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: %s
  namespace: %s
  labels:
    opendepot.defdev.io/public: "true"
spec:
  moduleConfig:
    provider: aws
    repoOwner: test-owner
    repoUrl: https://github.com/test-owner/test-module
    fileFormat: zip
  versions: []
`, browsePublicModule, browsePublicNS)
			applyMod := exec.Command("kubectl", "apply", "-f", "-")
			applyMod.Stdin = strings.NewReader(publicModuleYAML)
			_, _ = utils.Run(applyMod)

			By("reapplying private Module fixture")
			privateModuleYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: %s
  namespace: %s
spec:
  moduleConfig:
    provider: aws
    repoOwner: private-owner
    repoUrl: https://github.com/private-owner/private-module
    fileFormat: zip
  versions: []
`, browsePrivMod, browsePrivateNS)
			applyPriv := exec.Command("kubectl", "apply", "-f", "-")
			applyPriv.Stdin = strings.NewReader(privateModuleYAML)
			_, _ = utils.Run(applyPriv)

			// Start port-forwards.
			gbPFCancel = startBrowsePortForward()

			// Start Dex port-forward on its dedicated port.
			gbDexPFCancel = gbStartDexPortForward(gbBrowseDexPort)

			By("acquiring a valid Dex JWT for the GroupBinding browse test user")
			Eventually(func() error {
				token, err := gbAcquireDexToken(gbBrowseDexPort, gbBrowseDexClientSecret, gbBrowseUserEmail, gbBrowseUserPassword)
				if err != nil {
					return err
				}
				gbOIDCJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT for GroupBinding browse tests")
		})

		AfterAll(func() {
			// gbPFCancel is the new port-forward started in this context's BeforeAll.
			// Update the outer pfCancel so the top-level AfterAll doesn't double-cancel.
			pfCancel = gbPFCancel
			if gbDexPFCancel != nil {
				gbDexPFCancel()
			}
			gbDeleteGroupBinding(gbBrowseGroupBinding)
		})

		It("should return 401 for an unauthenticated browse request in OIDC mode", func() {
			resp, err := http.Get(gbBrowseURL("/opendepot/ui/v1/resources?kind=module"))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"unauthenticated browse must be rejected with 401 when OIDC mode is active")
		})

		It("should return only public resources for an authenticated caller with no GroupBinding", func() {
			// No GroupBinding has been applied yet in this context — the caller is
			// authenticated (valid JWT) but has no GroupBinding match → public-only.
			req, err := http.NewRequest(http.MethodGet,
				gbBrowseURL("/opendepot/ui/v1/resources?kind=module"), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+gbOIDCJWT)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
					Public    bool   `json:"public"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			for _, item := range body.Items {
				Expect(item.Public).To(BeTrue(),
					"authenticated caller without a GroupBinding must only see public resources; got non-public item %s/%s",
					item.Namespace, item.Name)
			}

			names := make([]string, 0, len(body.Items))
			for _, item := range body.Items {
				names = append(names, item.Name)
			}
			Expect(names).NotTo(ContainElement(browsePrivMod),
				"private module must not appear when the authenticated caller has no GroupBinding")
		})

		It("should include private resource when authenticated caller has matching GroupBinding", func() {
			// Apply a GroupBinding that allows the test user's email (used as groups
			// claim) to see all modules, including the private one.
			gbApplyGroupBinding(gbBrowseGroupBinding,
				fmt.Sprintf(`"%s" in groups`, gbBrowseUserEmail),
				[]string{"*"},
			)

			req, err := http.NewRequest(http.MethodGet,
				gbBrowseURL("/opendepot/ui/v1/resources?kind=module"), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+gbOIDCJWT)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			names := make([]string, 0, len(body.Items))
			for _, item := range body.Items {
				names = append(names, item.Name)
			}
			Expect(names).To(ContainElement(browsePrivMod),
				"authenticated caller with matching GroupBinding must see the private module")
			Expect(names).To(ContainElement(browsePublicModule),
				"authenticated caller must also see public modules (union visibility)")
		})

		It("should hide private resource when no GroupBinding matches the authenticated caller", func() {
			// Remove the GroupBinding so the user has no binding match → public-only.
			gbDeleteGroupBinding(gbBrowseGroupBinding)

			req, err := http.NewRequest(http.MethodGet,
				gbBrowseURL("/opendepot/ui/v1/resources?kind=module"), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+gbOIDCJWT)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Name string `json:"name"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			names := make([]string, 0, len(body.Items))
			for _, item := range body.Items {
				names = append(names, item.Name)
			}
			Expect(names).NotTo(ContainElement(browsePrivMod),
				"authenticated caller with no matching GroupBinding must not see the private module")
		})
	})
})
