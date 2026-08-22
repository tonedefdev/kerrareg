---
tags:
  - quickstart
  - kind
  - local-development
search:
  boost: 2
---

# Local Quickstart (kind)

The fastest way to try OpenDepot is with a local [kind](https://kind.sigs.k8s.io/) cluster using the filesystem storage backend and `hostPath`. This avoids any cloud provider setup — no S3 bucket, no Azure Storage Account, no credentials, no ingress controller, and no TLS certificates. You'll have a fully functional registry in minutes using `kubectl port-forward` and the public `*.localtest.me` DNS service (all `*.localtest.me` hostnames resolve to `127.0.0.1`).

!!! note
    OpenTofu and Terraform require module registry hostnames to contain at least one dot. `localhost` alone is not valid. `opendepot.localtest.me` resolves to `127.0.0.1` via public DNS, making it a convenient dotted hostname for local testing without editing `/etc/hosts` or installing any ingress controller.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm 3](https://helm.sh/docs/intro/install/)
- [OpenTofu](https://opentofu.org/docs/intro/install/) or [Terraform](https://developer.hashicorp.com/terraform/install)

## Step 1: Create the Cluster

```bash
kind create cluster --name opendepot
```

## Step 2: Deploy with Helm

```bash
helm repo add opendepot https://tonedefdev.github.io/opendepot
helm repo update
helm install opendepot opendepot/opendepot \
  -n opendepot-system --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/data/modules \
  --set server.anonymousAuth=true \
  --wait
```

Verify all pods are running:

```bash
kubectl get pods -n opendepot-system
```

!!! note
    **Apple Silicon users:** If building from source, the default `PLATFORM` is `linux/arm64`. For Intel Macs or Linux, run `make deploy PLATFORM=linux/amd64`.

## Step 3: Port-Forward the Server

In a separate terminal, forward the OpenDepot server to a local port:

```bash
kubectl port-forward svc/server 8080:80 -n opendepot-system
```

The server is now reachable at `http://opendepot.localtest.me:8080` — no ingress controller or TLS certificate required. OpenTofu will resolve `opendepot.localtest.me` to `127.0.0.1` via public DNS and connect through the port-forward.

Verify service discovery is working:

```bash
curl http://opendepot.localtest.me:8080/.well-known/terraform.json
```

Expected output:

```json
{"modules.v1":"/opendepot/modules/v1/"}
```

!!! note
    When OIDC authentication is enabled (see [OIDC Authentication](../configuration/oidc.md)), the response also includes a `login.v1` object with `authz`, `token`, and `grant_types` fields. The quickstart uses `anonymousAuth: true`, so `login.v1` is absent here.

## Step 4: Create a Test Module

Apply a `Module` resource that pulls a small public module from GitHub:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: terraform-aws-s3-bucket
  namespace: opendepot-system
spec:
  moduleConfig:
    provider: aws
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-s3-bucket
    fileFormat: zip
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: "4.3.0"
EOF
```

!!! note
    The Module CR name (`terraform-aws-s3-bucket`) must match the GitHub repository name, because the module controller uses it as the repository name when fetching archives if `spec.moduleConfig.name` is omitted.

Watch the Version resource sync:

```bash
kubectl get versions -n opendepot-system -w
```

Once `SYNCED` shows `true`, the module archive has been fetched from GitHub and stored in the local filesystem.

## Step 5: Use the Registry with OpenTofu

Create a working directory with a Terraform/OpenTofu config and a `.tofurc` (or `.terraformrc`) that points OpenTofu at your local registry:

```bash
mkdir /tmp/opendepot-test && cd /tmp/opendepot-test

cat > main.tf <<'EOF'
module "s3_bucket" {
  source  = "opendepot.localtest.me/opendepot-system/terraform-aws-s3-bucket/aws"
  version = "4.3.0"
}
EOF

cat > .tofurc <<'EOF'
host "opendepot.localtest.me" {
  services = {
    "modules.v1" = "http://opendepot.localtest.me:8080/opendepot/modules/v1/"
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu init
```

The `.tofurc` `host` block overrides the default HTTPS protocol discovery for this hostname, allowing plain HTTP over the port-forward. The host block key is the bare hostname without a port; the port belongs only in the `services` URL value. You should see OpenTofu download the module from your local OpenDepot instance:

```
Initializing modules...
Downloading opendepot.localtest.me/opendepot-system/terraform-aws-s3-bucket/aws 4.3.0 for s3_bucket...
- s3_bucket in .terraform/modules/s3_bucket

OpenTofu has been successfully initialized!
```

## Step 6: (Optional) Test with Authentication

OpenDepot supports three authentication modes: Kubernetes bearer tokens, base64-encoded kubeconfigs, and OIDC JWTs via Dex. See [Authenticating with OpenDepot](../authentication.md) for a full comparison.

To test Kubernetes-native bearer-token auth, redeploy with `anonymousAuth` disabled:

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  --set server.anonymousAuth=false \
  --set server.useBearerToken=true \
  --wait
```

Create a ServiceAccount and bind it to a read-only role:

```bash
kubectl create serviceaccount test-user -n opendepot-system

kubectl create role opendepot-reader -n opendepot-system \
  --resource=modules.opendepot.defdev.io,versions.opendepot.defdev.io,providers.opendepot.defdev.io \
  --verb=get,list,watch

kubectl create rolebinding test-user-reader -n opendepot-system \
  --role=opendepot-reader \
  --serviceaccount=opendepot-system:test-user
```

Generate a short-lived token and pass it via the `TF_TOKEN_*` environment variable. OpenTofu maps the variable name back to a hostname by replacing each `_` with `.` (lowercased), so `TF_TOKEN_OPENDEPOT_LOCALTEST_ME` supplies credentials for `opendepot.localtest.me`. The existing `.tofurc` from Step 5 already has the required `host` block — no changes to it are needed:

```bash
TOKEN=$(kubectl create token test-user -n opendepot-system --duration=1h)

TF_TOKEN_OPENDEPOT_LOCALTEST_ME="$TOKEN" TF_CLI_CONFIG_FILE=.tofurc tofu init
```

OpenTofu sends the bearer token to OpenDepot, which forwards it to the Kubernetes API for authentication and RBAC authorization. This is the same flow used in production — no separate user database or API keys required.

## Step 7: (Optional) Test with a Depot

To test automatic version discovery from GitHub:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: test-depot
  namespace: opendepot-system
spec:
  global:
    moduleConfig:
      fileFormat: zip
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  moduleConfigs:
    - name: terraform-aws-s3-bucket
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 4.3.0, <= 4.4.0"
  providerConfigs:
    - name: random
      operatingSystems:
        - linux
      architectures:
        - amd64
      versionConstraints: "= 3.6.0"
      storageConfig:
        fileSystem:
          directoryPath: /data/modules
EOF
```

The Depot controller queries GitHub releases for modules and the HashiCorp Releases API for providers, creates `Module` and `Provider` resources for matching versions, and the pipeline syncs them to local storage automatically.

## Step 8: (Optional) Test with a Provider

Providers are synced from the [HashiCorp Releases API](https://releases.hashicorp.com) and served via the [Terraform Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol). Provider binaries can be large (the `aws` provider for a single OS/arch is ~700 MB), so this step is optional.

**Step 8a: Generate a GPG key for provider signing**

OpenTofu verifies a GPG signature over the `SHA256SUMS` file when installing a provider. Generate a dedicated key and store it as a Kubernetes Secret:

```bash
# Generate a key (no passphrase, batch mode)
gpg --batch --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Name-Real: OpenDepot Local
Name-Email: opendepot@local.test
Expire-Date: 0
%no-protection
EOF

KEY_ID=$(gpg --list-keys --with-colons opendepot@local.test | awk -F: '/^pub/{print $5}' | tail -1)
ASCII_ARMOR=$(gpg --armor --export "$KEY_ID")
PRIVATE_B64=$(gpg --armor --export-secret-keys "$KEY_ID" | base64 | tr -d '\n')

kubectl create secret generic opendepot-provider-gpg \
  --namespace opendepot-system \
  --from-literal=OPENDEPOT_PROVIDER_GPG_KEY_ID="$KEY_ID" \
  --from-literal=OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR="$ASCII_ARMOR" \
  --from-literal=OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64="$PRIVATE_B64"
```

**Step 8b: Redeploy OpenDepot with the provider controller and GPG secret**

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  --set provider.enabled=true \
  --set server.gpg.secretName=opendepot-provider-gpg \
  --wait
```

**Step 8c: Create a Provider resource**

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: aws
  namespace: opendepot-system
spec:
  providerConfig:
    name: aws
    operatingSystems:
      - linux
    architectures:
      - amd64
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: "5.80.0"
EOF
```

Watch the Version resource sync (this downloads ~700 MB from HashiCorp):

```bash
kubectl get versions -n opendepot-system -w
```

Once `SYNCED` shows `true`, the provider binary is stored in the local filesystem.

**Step 8d: Use the provider registry with OpenTofu**

```bash
mkdir /tmp/opendepot-provider-test && cd /tmp/opendepot-provider-test

cat > main.tf <<'EOF'
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.80.0"
    }
  }
}
EOF

cat > .tofurc <<'EOF'
provider_installation {
  network_mirror {
    url     = "http://opendepot.localtest.me:8080/opendepot/providers/mirror/v1/opendepot-system/"
    include = ["registry.opentofu.org/*/*"]
  }

  direct {
    exclude = ["registry.opentofu.org/*/*"]
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu init
```

The `provider_installation` block tells OpenTofu to install providers from OpenDepot while preserving the canonical `hashicorp/aws` identity in your configuration and lockfile. The `direct.exclude` entry ensures installations always come from OpenDepot rather than falling back to `registry.opentofu.org`. OpenTofu will resolve `opendepot.localtest.me` to `127.0.0.1` and install the provider from your local OpenDepot instance:

```
Initializing provider plugins...
- Finding hashicorp/aws versions matching "5.80.0"...
- Installing opendepot.localtest.me:8080/opendepot-system/aws v5.80.0...
- Installed opendepot.localtest.me:8080/opendepot-system/aws v5.80.0

OpenTofu has been successfully initialized!
```

**Step 8d (authenticated): Using the provider registry with bearer token auth**

If you enabled authentication in Step 6, providers need a `credentials` block in `.tofurc` because the provider source address includes the port (`opendepot.localtest.me:8080`) and the `TF_TOKEN_*` env var format does not support ports. Generate a token and write a `.tofurc` that covers the provider credentials and both `host` blocks:

```bash
TOKEN=$(kubectl create token test-user -n opendepot-system --duration=1h)

cat > /tmp/opendepot-provider-test/.tofurc <<EOF
credentials "opendepot.localtest.me:8080" {
  token = "${TOKEN}"
}

host "opendepot.localtest.me" {
  services = {
    "modules.v1" = "http://opendepot.localtest.me:8080/opendepot/modules/v1/"
  }
}

host "opendepot.localtest.me:8080" {
  services = {
    "providers.v1" = "http://opendepot.localtest.me:8080/opendepot/providers/v1/"
  }
}
EOF

TF_TOKEN_OPENDEPOT_LOCALTEST_ME="$TOKEN" TF_CLI_CONFIG_FILE=/tmp/opendepot-provider-test/.tofurc tofu init
```

!!! warning
    Using `token` inside a `host` block is silently ignored by OpenTofu — provider credentials must be in a separate `credentials` block. The `credentials` block key must exactly match the hostname as it appears in the `source` address, including the port. A mismatch means OpenTofu sends no token and the server returns 401.

## Step 9: (Optional) Test Trivy Scanning

This step shows Trivy scanning in action against the module from Step 4 and, if you completed Step 8, the provider as well.

**Step 9a: Enable scanning**

Setting `scanning.enabled=true` activates module IaC scanning with no additional infrastructure — the version-controller automatically uses the `-scanning` image variant. To also enable provider binary and source scanning (which needs the Trivy DB PVC and CronJob), set `scanning.providerScanning=true`. Kind uses a single-node cluster, so `ReadWriteOnce` access mode and the default storage class are sufficient. Set `offline=false` so Trivy downloads the vulnerability database directly rather than waiting for the CronJob to complete on a fresh cluster:

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  --set scanning.enabled=true \
  --set scanning.providerScanning=true \
  --set scanning.offline=false \
  --set scanning.cache.accessMode=ReadWriteOnce \
  --wait
```

!!! note
    `scanning.offline=false` is a convenience for local development. In production, leave `offline=true` (the default) and rely on the `trivy-db-updater` CronJob to keep the database current. `scanning.offline` only applies to provider scanning — module IaC scanning uses bundled config rules and makes no network calls.

**Step 9b: Trigger a module scan**

Enabling the Trivy scanner forces the Version controller to restart to apply the correct configuration.

Wait for the Version resource to reconcile, then inspect the IaC findings:

```bash
kubectl get versions -n opendepot-system -w
# wait for SYNCED=true, then Ctrl-C

kubectl get version terraform-aws-s3-bucket-4.3.0 \
  -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

You should see something like:

```json
{
  "scannedAt": "2026-05-03T02:11:00Z",
  "findings": [
    {
      "vulnerabilityID": "AWS-0086",
      "pkgName": "aws_s3_bucket",
      "installedVersion": "",
      "severity": "HIGH",
      "title": "S3 Bucket does not have logging enabled"
    },
    {
      "vulnerabilityID": "AWS-0088",
      "pkgName": "aws_s3_bucket",
      "installedVersion": "",
      "severity": "MEDIUM",
      "title": "S3 Bucket does not have versioning enabled"
    }
  ]
}
```

Module IaC findings contain Trivy rule IDs such as `AWS-0086` rather than CVE identifiers. An empty `findings` array means no misconfigurations were detected.

**Step 9c: (Requires Step 8) Inspect provider scan results**

If you completed Step 8, the provider binary and source scans run automatically once the controller has restarted.

Check the binary scan on the Version resource (per OS/arch):

```bash
kubectl get version aws-5-80-0-linux-amd64 \
  -n opendepot-system \
  -o jsonpath='{.status.binaryScan}' | jq .
```

```json
{
  "scannedAt": "2026-05-03T02:12:00Z",
  "findings": [
    {
      "vulnerabilityID": "CVE-2024-24790",
      "pkgName": "stdlib",
      "installedVersion": "1.22.3",
      "fixedVersion": "1.22.4",
      "severity": "CRITICAL",
      "title": "net/netip: Unexpected behavior from Is methods for IPv4-mapped IPv6 addresses"
    }
  ]
}
```

Check the source scan on the provider Version resource:

```bash
kubectl get version aws-5-80-0-linux-amd64 \
  -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

```json
{
  "scannedAt": "2026-05-03T02:12:05Z",
  "findings": []
}
```

Provider binary findings contain CVE identifiers and package version details. The source scan covers `go.mod` dependencies — an empty `findings` array means no vulnerable dependencies were detected.

!!! note
    If `status.binaryScan` is empty after the controller restarts, the version was already cached from a previous run and the fast-path skipped re-downloading it. Use `forceSync: true` to trigger a one-time re-download and re-scan:

    ```bash
    kubectl patch version aws-5-80-0-linux-amd64 -n opendepot-system \
      --type merge -p '{"spec":{"forceSync":true}}'
    ```

## Cleanup

Stop the port-forward and delete the Kind cluster:

```bash
kubectl port-forward svc/server 8080:80 -n opendepot-system
kind delete cluster --name opendepot
```

## Local OIDC Testing with `make` Targets

If you want to test the full OIDC login flow (`tofu login`) against a local Kind cluster without any cloud infrastructure, the repository includes a set of `make` targets that automate the setup.

!!! note "Prerequisites"
    [mkcert](https://github.com/FiloSottile/mkcert) (`brew install mkcert`) and either `htpasswd` (from `brew install httpd`) or the Python `bcrypt` package are required in addition to the standard quickstart prerequisites.

These `make` targets use `opendepot.localtest.me` as the default test hostname, which resolves to `127.0.0.1` via public DNS — no `/etc/hosts` editing required.

**Full setup from a freshly created Kind cluster:**

```bash
# Install mkcert CA (one-time)
mkcert -install

# Create the Kind cluster
kind create cluster --name opendepot

# Build + load images, generate TLS cert, deploy with Dex, and start the port-forward
make oidc-setup PASS=mysecretpassword
```

**Login and verify the auth flow:**

```bash
# Open tofu login in the browser — authenticate with the static test user
make oidc-login

# Username: dev@example.com
# Password: Set during `make oidc-setup PASS=<PASSWORD>`

# Change to test/local directory in the project repo and run `tofu init`
cd test/local && tofu init
```

Since no `GroupBinding` resources have been applied, a 403 Forbidden response is expected:

```txt
Initializing the backend...
Initializing modules...
╷
│ Error: Error accessing remote module registry
│ 
│   on main.tf line 1:
│    1: module "key_pair" {
│ 
│ Failed to retrieve available versions for module "key_pair" (main.tf:1) from opendepot.localtest.me:8080: error looking up module versions: 403 Forbidden.
```

```bash
# Now create a test Module and GroupBinding for the static user's group
make oidc-test-resources

# Re-run `tofu init`
tofu init
```

Now that we have added the `GroupBinding` that allows `dev@example.com` access to this module we are successfully able to access the registry to download it:

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

For a complete reference of all available targets and how the `dexProxy` single port-forward pattern works here, see [Local OIDC E2E Testing](../contributing.md#local-oidc-e2e-testing).

## Try the Registry Explorer UI

The Registry Explorer is a browseable frontend that visualizes your modules, providers, and depots. The fastest way to launch it locally is:

```bash
make ui-setup
```

This builds all container images, deploys the UI in anonymous-auth mode (no OIDC configuration required), and starts a port-forward. Open **http://opendepot.localtest.me:8080** in your browser.

The **Depots** page (`/depots`) shows an interactive relationship graph of all `Depot` resources and their managed modules and providers.

To test the full OIDC login flow with user accounts and `GroupBinding` visibility rules:

```bash
make ui-setup-oidc PASS=yourpassword
```

This deploys the UI with OIDC login, enables the provider controller and Trivy scanning, auto-creates the GPG signing secret required for provider shasums, and writes `~/.tofurc` so `tofu login opendepot.localtest.me:8080` works immediately after setup. No `mkcert` or TLS certificate is required.

See the [Registry Explorer UI guide](../guides/registry-explorer.md) for the complete setup, public visibility labels, and `GroupBinding` configuration.


