---
tags:
  - contributing
---

# Contributing

Thank you for your interest in contributing to OpenDepot! Please read our [CONTRIBUTING.md](https://github.com/tonedefdev/opendepot/blob/main/CONTRIBUTING.md) on GitHub for guidelines on opening issues, submitting pull requests, and the development workflow.

Pull requests also run the e2e workflow, which builds the service images and scans them with Trivy before the controller tests execute. A PR fails if the scanner reports critical or high severity vulnerabilities in a built image.

## Security Scanning

The security review runs two Trivy scans against every pull request:

- `trivy image` — scans each built service image for OS package and Go module CVEs.
- `trivy config` — scans the Helm chart templates for Kubernetes misconfigurations.

OpenDepot bundles Trivy v0.74.0 in the `-scanning` image variant. If an upstream Trivy release has a HIGH CVE with no available fix, track it in a GitHub issue before adding a suppression. Add any suppression to both the repository root and `chart/opendepot/.trivyignore`, and include the tracking issue URL as a comment.

!!! note
    `.trivyignore` suppressions are scoped to the repository and apply only to findings in the embedded Trivy binary. CVEs in the controller's own dependencies must be resolved, not suppressed.

## Local OIDC E2E Testing

The repository ships a set of `make` targets for end-to-end OIDC testing against a local Kind cluster. They wire together Kind, Helm, Dex v2.45.0, filesystem storage, mkcert TLS, and a static test user so that you can run `tofu login` without any cloud infrastructure.

!!! note
    The `make` targets use `opendepot.localtest.me` as the default registry hostname. This hostname resolves to `127.0.0.1` via public DNS — no `/etc/hosts` editing required. You can override it with `OIDC_REGISTRY_HOST=<hostname>` if you need a custom local name, in which case `make oidc-hosts` will add the entry to `/etc/hosts`.

| Target | Purpose |
|--------|---------|
| `make oidc-hosts` | Adds `$(OIDC_REGISTRY_HOST) → 127.0.0.1` to `/etc/hosts` (requires `sudo`). Only needed when using a custom `OIDC_REGISTRY_HOST` that does not resolve via public DNS |
| `make oidc-tls` | Generates a locally-trusted mkcert TLS cert for `opendepot.localtest.me`, `localhost`, and `127.0.0.1` and creates the `opendepot-tls` Kubernetes Secret |
| `make oidc-deploy PASS=<password>` | Deploys OpenDepot + Dex v2.45.0 with filesystem storage and a static OIDC test user |
| `make oidc-forward` | Port-forwards the server (`localhost:8080`) and Dex (`localhost:5556`) |
| `make oidc-login` | Runs `tofu login opendepot.localtest.me:8080` |
| `make oidc-test-resources` | Creates a sample Module and a GroupBinding for the test user's group |
| `make oidc-verify-module` | Runs an authenticated request against the test module to verify the full auth flow |
| `make oidc-test-clean` | Removes the test Module and GroupBinding |
| `make oidc-stop` | Stops all port-forwards |
| `make oidc-setup PASS=<password>` | Runs all setup steps (`deploy`, `oidc-tls`, `oidc-deploy`, `oidc-forward`) in sequence |

**Prerequisites:** [kind](https://kind.sigs.k8s.io/), [kubectl](https://kubernetes.io/docs/tasks/tools/), [Helm 3](https://helm.sh/docs/intro/install/), [mkcert](https://github.com/FiloSottile/mkcert), [OpenTofu](https://opentofu.org/docs/intro/install/), and either `htpasswd` (from `httpd`) or the Python `bcrypt` package for hashing the test user password.

**Full setup from a freshly created Kind cluster:**

```bash
# One-time: install mkcert CA into the system trust store
mkcert -install

# Create a kind cluster (if you don't have one already)
kind create cluster --name opendepot

# Build and load images, generate TLS cert, deploy, and port-forward
make oidc-setup PASS=mysecretpassword
```

**Login and test:**

```bash
# Opens tofu login - authenticate in the browser
make oidc-login 

# Username: dev@example.com
# Password: Set during `make oidc-setup PASS=<PASSWORD>`

# Now create a test Module and GroupBinding for the static user's group
make oidc-test-resources

# Change to test/local in the OpenDepot project repo and run `tofu init`
cd test/local && tofu init
```

If successful you should see:

```txt
Initializing the backend...
Initializing modules...
Downloading opendepot.localtest.me:8080/opendepot-system/terraform-aws-key-pair/aws 2.0.3 for key_pair...
- key_pair in .terraform/modules/key_pair

Initializing provider plugins...

OpenTofu has been successfully initialized!

You may now begin working with OpenTofu. Try running "tofu plan" to see
any changes that are required for your infrastructure. All OpenTofu commands
should now work.

If you ever set or change modules or backend configuration for OpenTofu,
rerun this command to reinitialize your working directory. If you forget, other
commands will detect it and remind you to do so if necessary.
```


The `oidc-deploy` target configures `server.oidc.dexProxy.enabled: true`, so Dex is reverse-proxied through the server itself — the [recommended](configuration/oidc.md#recommended-proxy-dex-through-the-server) setup. `make oidc-forward` port-forwards only the server (`localhost:8080`); Dex is reachable at `/dex/*` through that same port-forward, so there's no need to also forward Dex directly.

The `ui-setup-oidc` target configures the same `dexProxy` setup for the CLI login path, and additionally routes the UI's own browser login through the UI's nginx, which has a `/dex` location proxying to the server for environments without a real Ingress controller (local Kind has none). `make ui-forward` port-forwards only the UI (`localhost:8080`); both `tofu login` and the UI's own OIDC login work through that single port-forward.

`ui-setup-oidc` still writes `~/.tofurc` (via `make ui-tofurc`), since this local registry is plain HTTP and OpenTofu's service discovery only works over HTTPS — a CLI config `host` block is required regardless of `dexProxy`. The `login.v1` URLs in that block now point at Dex through the single UI port-forward instead of a separate Dex port-forward.

The older split-network pattern — where the server reaches Dex via the in-cluster service URL while `authzUrl`/`tokenUrl` overrides redirect the browser through a separately port-forwarded Dex — is still available and described in [Split-Network OIDC](configuration/oidc.md#split-network-oidc-authzurl-tokenurl), but is no longer used by the local Kind Make targets since `dexProxy` covers the common case with fewer port-forwards.
