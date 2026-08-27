---
tags:
  - quickstart
  - kind
  - tilt
  - local-development
search:
  boost: 2
---

# Local Quickstart with Tilt

The fastest way to run OpenDepot locally is the repository's [Tilt](https://tilt.dev/) environment. It creates a persistent Kind cluster and local image registry, builds the complete stack, configures local OIDC, and exposes the Registry Explorer at `https://opendepot.localtest.me:8443/`.

The environment includes:

- All OpenDepot controllers and the server
- The Registry Explorer UI and NGINX proxy
- Dex with a local test user
- Valkey download statistics
- Filesystem storage backed by the Kind node
- Module and provider scanning with Trivy
- Provider GPG signing
- A trusted HTTPS proxy for Provider Network Mirror testing

`opendepot.localtest.me` resolves to `127.0.0.1` through public DNS. You do not need to edit `/etc/hosts` or install an ingress controller.

## Prerequisites

Clone the OpenDepot repository, then install the following tools:

| Tool | Purpose |
|------|---------|
| [Docker](https://docs.docker.com/get-docker/) | Runs the local cluster and image registry |
| [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | Provides the Kubernetes cluster |
| [ctlptl](https://github.com/tilt-dev/ctlptl#installation) | Creates and reuses the cluster and registry |
| [Tilt](https://docs.tilt.dev/install.html) 0.33.20 or later | Builds, deploys, and live-updates OpenDepot |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Manages sample resources |
| [Helm 3](https://helm.sh/docs/intro/install/) | Renders the OpenDepot chart |
| [Go](https://go.dev/doc/install/) | Runs the local provider mirror proxy and resolves the host platform |
| [GnuPG](https://gnupg.org/download/) and OpenSSL | Generate local signing and session secrets |

The bootstrap script also needs either `htpasswd` or Python 3 with the `bcrypt` package to hash the local Dex password.

Install [OpenTofu](https://opentofu.org/docs/intro/install/) or [Terraform](https://developer.hashicorp.com/terraform/install/) if you want to test registry consumption from a CLI. Provider Network Mirror testing also requires [mkcert](https://github.com/FiloSottile/mkcert).

## Step 1: Start OpenDepot

From the repository root, choose a password for the local Dex user and run the Tilt launcher:

```bash
export OPENDEPOT_DEV_PASSWORD='choose-a-local-password'
tilt/scripts/up.sh
```

The launcher:

1. Verifies the required tools and Docker daemon.
2. Creates or reuses the `kind-opendepot` cluster and the `opendepot-registry` local registry.
3. Selects the `kind-opendepot` Kubernetes context.
4. Creates the local UI, OIDC, and GPG secrets.
5. Generates the ignored `tilt/.generated/values.yaml` file with the hashed Dex password.
6. Starts Tilt and deploys the complete OpenDepot stack.

The initial build downloads the development toolchain and Trivy components. Subsequent starts reuse the cluster, registry, and build cache.

Wait until the `ui` resource is ready in the Tilt dashboard:

- OpenDepot: [https://opendepot.localtest.me:8443/](https://opendepot.localtest.me:8443/)
- Tilt dashboard: [http://localhost:10350](http://localhost:10350)

## Step 2: Sign In

Open the Registry Explorer and sign in with the local Dex user:

| Field | Value |
|-------|-------|
| Email | `dev@example.com` |
| Password | The value of `OPENDEPOT_DEV_PASSWORD` |

The user belongs to `local-test-group`. The sample resource control creates a `GroupBinding` that grants this group access to the sample module.

## Step 3: Create the Sample Module

Trigger the sample resource control from the Tilt dashboard, or run:

```bash
tilt trigger seed-sample-resources
```

This creates:

- A `terraform-aws-key-pair` Module at version `v2.0.3`
- A `local-test-access` GroupBinding for `local-test-group`

Watch the generated Version resource until `SYNCED` is `true`:

```bash
kubectl get versions -n opendepot-system -w
```

Refresh the Registry Explorer to browse the module, version metadata, and scan results.

## Step 4: Use the Module with OpenTofu

The local endpoint uses HTTP. Configure OpenTofu with an explicit `host` block that advertises the registry and Dex endpoints:

```bash
mkdir -p /tmp/opendepot-module-test
cd /tmp/opendepot-module-test

cat > main.tf <<'EOF'
module "key_pair" {
  source  = "opendepot.localtest.me:8443/opendepot-system/terraform-aws-key-pair/aws"
  version = "2.0.3"
}
EOF

cat > .tofurc <<'EOF'
host "opendepot.localtest.me:8443" {
  services = {
    "modules.v1"   = "https://opendepot.localtest.me:8443//opendepot/modules/v1/"
    "providers.v1" = "https://opendepot.localtest.me:8443//opendepot/providers/v1/"
    "login.v1" = {
      client      = "opendepot"
      grant_types = ["authz_code"]
      authz       = "https://opendepot.localtest.me:8443//dex/auth"
      token       = "https://opendepot.localtest.me:8443//dex/token"
      scopes      = ["openid", "email", "profile", "groups", "offline_access"]
      ports       = [10000, 10010]
    }
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu login opendepot.localtest.me:8443
TF_CLI_CONFIG_FILE=.tofurc tofu init
```

Complete the browser login with `dev@example.com` and your local password. OpenTofu stores the token in its credentials file, then downloads the module through OpenDepot.

!!! note
    Keep the port in both the module source and `host` block. OpenTofu treats `opendepot.localtest.me` and `opendepot.localtest.me:8443` as different registry hosts.

## Step 5: Test a Depot

A `Depot` discovers module and provider versions from their upstream sources. Apply a small pull-based example:

```bash
cat <<'EOF' | kubectl apply -f -
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

The Depot controller queries GitHub for modules and the configured upstream registry for providers. Provider discovery defaults to `registry.opentofu.org`; set `upstreamRegistry: registry.terraform.io` on a provider config to use the Terraform Registry instead.

Open the **Depots** page in the Registry Explorer to inspect the resources and relationships created by the controller.

## Step 6: Test a Provider Network Mirror

The Provider Network Mirror Protocol requires HTTPS. For the simplest protocol test, enable anonymous metadata access in the generated local values before starting Tilt:

```bash
export OPENDEPOT_DEV_PASSWORD='choose-a-local-password'
export OPENDEPOT_DEV_ANONYMOUS_AUTH=true
tilt/scripts/up.sh
```

If Tilt is already running, set `OPENDEPOT_DEV_ANONYMOUS_AUTH=true`, rerun `tilt/scripts/bootstrap.sh`, and wait for Tilt to apply the updated values.

Install the local CA once, then start the HTTPS proxy from the Tilt dashboard or command line:

```bash
mkcert -install
tilt trigger provider-mirror-tls
```

The mirror is available at:

```text
https://opendepot.localtest.me:8443/opendepot/providers/mirror/v1/<kubernetes-namespace>/
```

Create a small OpenTofu-origin provider for your local platform:

```bash
PROVIDER_OS=$(go env GOOS)
PROVIDER_ARCH=$(go env GOARCH)

cat <<EOF | kubectl apply -f -
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: null
  namespace: opendepot-system
spec:
  providerConfig:
    name: null
    upstreamRegistry: registry.opentofu.org
    operatingSystems: [$PROVIDER_OS]
    architectures: [$PROVIDER_ARCH]
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: "3.2.3"
EOF

kubectl get versions -n opendepot-system -w
```

After the platform-specific Version reports `SYNCED=true`, configure OpenTofu to use only the OpenDepot mirror:

```bash
mkdir -p /tmp/opendepot-provider-test
cd /tmp/opendepot-provider-test

cat > main.tf <<'EOF'
terraform {
  required_providers {
    null = {
      source  = "registry.opentofu.org/hashicorp/null"
      version = "3.2.3"
    }
  }
}
EOF

cat > .tofurc <<'EOF'
provider_installation {
  network_mirror {
    url     = "https://opendepot.localtest.me:8443/opendepot/providers/mirror/v1/opendepot-system/"
    include = ["registry.opentofu.org/hashicorp/null"]
  }
  direct {
    exclude = ["registry.opentofu.org/hashicorp/null"]
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu init
```

The generated `.terraform.lock.hcl` retains the canonical `registry.opentofu.org/hashicorp/null` identity even though OpenDepot supplied the archive.

To test a Terraform-origin provider, set `upstreamRegistry: registry.terraform.io`, use a `registry.terraform.io/<namespace>/<type>` source, and match that hostname in the `include` and `exclude` patterns. See [Consuming Providers](../guides/providers.md) for complete examples for both CLIs.

Unset anonymous access after the protocol test:

```bash
unset OPENDEPOT_DEV_ANONYMOUS_AUTH
tilt/scripts/bootstrap.sh
```

## Step 7: Inspect Scan Results

Tilt enables module and provider scanning by default and seeds the Trivy vulnerability database after the Version controller becomes ready.

Inspect the sample module's IaC scan:

```bash
kubectl get version terraform-aws-key-pair-2-0-3 \
  -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

For a provider, inspect the platform-specific binary and source scans:

```bash
kubectl get version null-3-2-3-${PROVIDER_OS}-${PROVIDER_ARCH} \
  -n opendepot-system \
  -o jsonpath='{.status.binaryScan}' | jq .

kubectl get version null-3-2-3-${PROVIDER_OS}-${PROVIDER_ARCH} \
  -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

Trigger an immediate database refresh when needed:

```bash
tilt trigger refresh-trivy-db
```

## Development Controls

The Tilt dashboard exposes these controls, which are also available from the command line:

| Control | Purpose |
|---------|---------|
| `tilt trigger seed-sample-resources` | Create the sample Module and GroupBinding |
| `tilt trigger clear-sample-resources` | Remove the sample resources |
| `tilt trigger refresh-trivy-db` | Refresh the Trivy vulnerability database |
| `tilt trigger provider-mirror-tls` | Start the trusted provider mirror HTTPS proxy |

Go source changes are synced into the corresponding running container, rebuilt, and restarted without replacing the pod. UI changes under `services/ui/src/` and `services/ui/public/` use Next.js hot module replacement.

## Cleanup

Stop the foreground Tilt process with ++ctrl+c++. Remove the deployed resources while preserving the reusable cluster and image registry:

```bash
tilt down
```

Destroy and recreate the development cluster and registry, including all local OpenDepot data:

```bash
tilt/scripts/reset-cluster.sh
```

After a reset, start the environment again with `tilt/scripts/up.sh`.

See [Contributing](../contributing.md) for live-update details and the end-to-end test workflow.