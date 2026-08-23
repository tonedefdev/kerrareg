# Contributing to OpenDepot

Thank you for your interest in contributing to OpenDepot! This guide covers the Tilt development environment and the local end-to-end test suites.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Repository Layout](#repository-layout)
- [Tilt Development Environment](#tilt-development-environment)
  - [Start the Environment](#start-the-environment)
  - [Live Updates](#live-updates)
  - [Development Controls](#development-controls)
  - [Provider Network Mirror](#provider-network-mirror)
  - [Stop or Reset the Environment](#stop-or-reset-the-environment)
- [E2E Cluster Setup](#e2e-cluster-setup)
- [Running the E2E Tests](#running-the-e2e-tests)
  - [Module Controller](#module-controller)
  - [Provider Controller](#provider-controller)
  - [Depot Controller](#depot-controller)
  - [Version Controller](#version-controller)
- [Manual Tests](#manual-tests)
  - [OIDC Login Flow (`tofu login`)](#oidc-login-flow-tofu-login)
- [Regenerating CRDs](#regenerating-crds)
- [Building Images Manually](#building-images-manually)
- [Adding a Storage Backend](#adding-a-storage-backend)
- [Test Architecture](#test-architecture)

---

## Prerequisites

| Tool | Minimum version | Notes |
|------|----------------|-------|
| Go | 1.25 | All service modules target `go 1.25.5` |
| Docker | 17.03+ | Used to build controller images |
| [kind](https://kind.sigs.k8s.io/) | v0.23+ | Local Kubernetes cluster |
| [ctlptl](https://github.com/tilt-dev/ctlptl) | v0.9+ | Manages the persistent development cluster and registry |
| [Tilt](https://docs.tilt.dev/install.html) | v0.33.20+ | Builds, deploys, and live-updates the development stack |
| kubectl | v1.27+ | Cluster interaction |
| [Helm](https://helm.sh/) | v3.14+ | Chart installation |
| [OpenTofu](https://opentofu.org/) (`tofu`) | v1.6+ | Required for `tofu init` tests in the module and provider suites |
| [Terraform](https://developer.hashicorp.com/terraform/install) (`terraform`) | v1.10+ | Required for Terraform-origin provider mirror tests |
| [mkcert](https://github.com/FiloSottile/mkcert) | v1.4+ | Generates locally trusted TLS certificates for provider mirror tests |
| gpg | 2.x | Required for provider GPG signing tests |
| OpenSSL | 1.1+ | Generates local development secrets |

Verify everything is on your `PATH` before starting development or running tests:

```bash
go version && docker version --format '{{.Server.Version}}' && kind version && ctlptl version && tilt version && kubectl version --client && helm version --short && tofu version && terraform version && mkcert --version && gpg --version | head -1 && openssl version
```

The Tilt bootstrap script also needs either `htpasswd` or Python 3 with the `bcrypt` package to hash the local Dex password.

---

## Repository Layout

```
opendepot/
├── api/v1alpha1/        # CRD types; run `make generate manifests` here to regenerate
├── chart/opendepot/      # Helm chart deployed by every e2e suite
│   └── crds/            # CRD YAML files applied before each test run
├── services/
│   ├── depot/           # Depot controller — watches Depot CRs, creates Module/Provider CRs
│   │   └── test/e2e/
│   ├── module/          # Module controller — downloads and stores module artifacts
│   │   └── test/e2e/
│   ├── provider/        # Provider controller — downloads, signs, and stores provider artifacts
│   │   └── test/e2e/
│   ├── server/          # Registry API server
│   ├── ui/              # Next.js UI and its development container
│   └── version/         # Version controller — computes checksums and tracks artifact state
├── tilt/                # Development images, values, cluster config, and helper scripts
├── Tiltfile             # Tilt resources and live-update definitions
└── pkg/                 # Shared packages (storage backends, GitHub client)
```

---

## Tilt Development Environment

The root `Tiltfile` runs the complete OpenDepot stack in Kubernetes, including the Go services, Next.js UI, NGINX, Dex, Valkey, scanning, and provider support. Application traffic enters through a single port-forward to the in-cluster UI service.

Tilt uses a persistent Kind cluster named `kind-opendepot` and a local registry named `opendepot-registry` on `localhost:5005`. The launcher creates or reuses both through ctlptl.

### Start the Environment

Make sure Docker is running, then choose a password for the local Dex user and start Tilt from the repository root:

```bash
export OPENDEPOT_DEV_PASSWORD='choose-a-local-password'
tilt/scripts/up.sh
```

The launcher performs the following setup before starting Tilt:

- Verifies the required development tools and Docker daemon.
- Creates or reuses the `kind-opendepot` cluster and local registry.
- Selects and verifies the `kind-opendepot` Kubernetes context.
- Creates the `opendepot-system` namespace and local UI, OIDC, and GPG secrets, including one random OIDC client credential shared by the UI and Dex.
- Generates the ignored `tilt/.generated/values.yaml` file containing the hashed Dex password.

The scripts set the Docker API compatibility version required by ctlptl automatically. You do not need to export `DOCKER_API_VERSION` yourself.

The initial build is large because the Go development images include the compiler and the version controller includes Trivy and OpenTofu. Subsequent source updates use Tilt's build cache and live-update paths.

When the `ui` resource is ready, open:

- OpenDepot: [http://opendepot.localtest.me:8080](http://opendepot.localtest.me:8080)
- Tilt dashboard: [http://localhost:10350](http://localhost:10350)

Log in to OpenDepot with:

| Field | Value |
|-------|-------|
| Email | `dev@example.com` |
| Password | The value of `OPENDEPOT_DEV_PASSWORD` |

The default local user belongs to `local-test-group`. The sample resource control creates a `GroupBinding` that grants this group access to the sample module.

### Live Updates

Go source changes are synced into the relevant running controller container. Tilt rebuilds that service's binary in the container and restarts the process without replacing the pod. Changes to shared packages trigger every service that imports that package.

UI changes under `services/ui/src/` and `services/ui/public/` are synced into the in-cluster Next.js development container and handled by Next.js HMR. Changes to dependency manifests, Dockerfiles, the entrypoint, Helm values, or the `Tiltfile` trigger the corresponding image rebuild or deployment update.

Production Dockerfiles and chart security defaults remain hardened. The writable filesystems and larger UI memory limit used by live development are enabled only when `tilt/values.yaml` sets `global.developmentMode: true`.

### Development Controls

The Tilt dashboard exposes manual controls alongside the application resources. They can also be triggered from the command line:

```bash
# Create a sample Module and local-test GroupBinding
tilt trigger seed-sample-resources

# Remove the sample resources
tilt trigger clear-sample-resources

# Refresh the Trivy vulnerability database immediately
tilt trigger refresh-trivy-db

# Start the trusted HTTPS proxy used for manual provider mirror tests
tilt trigger provider-mirror-tls

# Remove unused OpenDepot images created by Tilt
tilt trigger cleanup-images
```

The Trivy database is seeded automatically after the version controller becomes ready.
The image cleanup control removes stale OpenDepot Tilt images from host Docker and the dedicated Kind node. Images used by current Kubernetes pods or Docker containers, images from other projects, volumes, the Kind cluster, and the local registry are preserved.

### Provider Network Mirror

The Provider Network Mirror Protocol requires HTTPS. Provider e2e tests place a temporary `mkcert` TLS reverse proxy in front of the server port-forward and use dynamically assigned ports with `/opendepot/providers/mirror/v1/opendepot-system/` for OpenTofu and `/opendepot/providers/mirror/v1/opendepot-terraform-e2e/` for Terraform. The Tilt `provider-mirror-tls` control provides the same topology at a stable URL:

```text
https://opendepot.localtest.me:8443/opendepot/providers/mirror/v1/<kubernetes-namespace>/
```

For a protocol-only test, enable anonymous metadata access when starting Tilt. This is opt-in and affects only the ignored generated values file:

```bash
export OPENDEPOT_DEV_PASSWORD='choose-a-local-password'
export OPENDEPOT_DEV_ANONYMOUS_AUTH=true
tilt/scripts/up.sh
```

After the `ui`, Provider controller, and Version controller are ready, start the TLS proxy from the Tilt dashboard or another terminal:

```bash
mkcert -install
tilt trigger provider-mirror-tls
```

Create small provider fixtures for both canonical origins. They use different provider types so their Version resource names do not collide in the shared Kubernetes namespace:

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
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: random
  namespace: opendepot-system
spec:
  providerConfig:
    name: random
    upstreamRegistry: registry.terraform.io
    operatingSystems: [$PROVIDER_OS]
    architectures: [$PROVIDER_ARCH]
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: "3.7.2"
EOF

kubectl get versions -n opendepot-system -w
```

Wait until both platform-specific Version resources report `SYNCED=true`.

Test the default OpenTofu origin with direct fallback disabled:

```bash
mkdir -p /tmp/opendepot-tofu-mirror
cat > /tmp/opendepot-tofu-mirror/main.tf <<'EOF'
terraform {
  required_providers {
    null = {
      source  = "registry.opentofu.org/hashicorp/null"
      version = "3.2.3"
    }
  }
}
EOF
cat > /tmp/opendepot-tofu-mirror/.tofurc <<'EOF'
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
(cd /tmp/opendepot-tofu-mirror && TF_CLI_CONFIG_FILE=.tofurc tofu init)
```

Test the Terraform origin the same way:

```bash
mkdir -p /tmp/opendepot-terraform-mirror
cat > /tmp/opendepot-terraform-mirror/main.tf <<'EOF'
terraform {
  required_providers {
    random = {
      source  = "registry.terraform.io/hashicorp/random"
      version = "3.7.2"
    }
  }
}
EOF
cat > /tmp/opendepot-terraform-mirror/.terraformrc <<'EOF'
provider_installation {
  network_mirror {
    url     = "https://opendepot.localtest.me:8443/opendepot/providers/mirror/v1/opendepot-system/"
    include = ["registry.terraform.io/hashicorp/random"]
  }
  direct {
    exclude = ["registry.terraform.io/hashicorp/random"]
  }
}
EOF
(cd /tmp/opendepot-terraform-mirror && TF_CLI_CONFIG_FILE=.terraformrc terraform init)
```

Each generated `.terraform.lock.hcl` must retain the canonical source hostname. Because direct installation is excluded, a successful initialization proves the provider came through OpenDepot. Unset `OPENDEPOT_DEV_ANONYMOUS_AUTH` and rerun `tilt/scripts/bootstrap.sh` after protocol testing to restore authenticated local behavior.

### Stop or Reset the Environment

Stop the foreground Tilt process with `Ctrl-C`. To remove the deployed Kubernetes resources while preserving the reusable cluster and registry, run:

```bash
tilt down
```

To destroy and recreate the development cluster and registry, including all local OpenDepot data, run:

```bash
tilt/scripts/reset-cluster.sh
```

After a reset, start the environment again with `tilt/scripts/up.sh`. The launcher regenerates the namespace, secrets, and local Dex configuration.

Tilt image tags also accumulate in the local registry. To clear the registry and recreate the Kind image store, stop Tilt, remove its deployed resources, and run the full image cleanup:

```bash
tilt down
tilt/scripts/cleanup-images.sh --all
```

Full cleanup refuses to run while Tilt or OpenDepot workloads are active. It recreates `kind-opendepot` and `opendepot-registry`, so the next `tilt/scripts/up.sh` run rebuilds and pulls the required images. Preview either mode without changing Docker resources by adding `--dry-run`.

---

## E2E Cluster Setup

The e2e suites use a separate Kind cluster named `kind`; they do not run against the persistent `kind-opendepot` Tilt cluster. Each suite builds images, loads them into the test cluster, applies CRDs, and deploys the Helm chart. Create the test cluster before running a suite:

```bash
kind create cluster --name kind
```

> [!TIP] 
> If you already have a `kind` cluster from a previous run it can be reused. The suites use `helm upgrade --install` so they are safe to run repeatedly.

### Chart Dependencies

OpenDepot uses Helm subcharts for Dex and Valkey. The tarballs are committed to `chart/opendepot/charts/`, so no internet access is required during e2e test runs. If you add or update a subchart dependency, regenerate the lock file and tarballs with:

```bash
make chart-deps
```

`make ui-setup` and `make ui-setup-oidc` call `chart-deps` automatically, so you only need to run it manually after cloning or after editing `chart/opendepot/Chart.yaml`.

---

## Running the E2E Tests

Every suite accepts an `IMG` environment variable that controls the controller image tag that gets built and loaded into Kind. If omitted it defaults to `<controller>:e2e-test`.

### Module Controller

The module suite builds the module controller, version controller, and server images. It exercises:

- Module CR reconciliation and Version CR creation
- Artifact download and checksum verification (`status.synced=true`)
- Module Registry Protocol API endpoints (`modules.v1`)
- `tofu init` against the local registry
- Kubernetes RBAC enforcement (anonymous auth on/off, bearer-token auth)

```bash
cd services/module
make test-e2e
```

The suite uses `opendepot.localtest.me` as the registry hostname — this is a public DNS name that resolves to `127.0.0.1` and satisfies OpenTofu's requirement for a hostname that contains at least one dot.

### Provider Controller

The provider suite builds the provider controller, version controller, and server images. It exercises:

- Provider CR reconciliation and Version CR creation
- Artifact download from the configured OpenTofu or Terraform provider registry
- GPG signing of `SHA256SUMS` and generation of `SHA256SUMS.sig`
- Provider Registry Protocol API endpoints (`providers.v1`)
- `tofu init` and `terraform init` through the Provider Network Mirror Protocol
- Kubernetes RBAC enforcement (anonymous auth on/off, bearer-token auth)

> [!IMPORTANT]
> Provider binaries can be several hundred MB. The artifact download step has a 5-minute timeout. Ensure you have sufficient disk space and a stable internet connection.

The suite generates a temporary GPG key pair automatically — no manual key setup is required.

```bash
cd services/provider
make test-e2e
```

### Depot Controller

The depot suite builds the depot controller image only. It exercises:

- Depot CR reconciliation creating Module and Provider CRs from `moduleConfigs` and `providerConfigs`
- Version constraint filtering (uses `= X.Y.Z` exact-match constraints)
- `status.modules` and `status.providers` population on the Depot CR
- Re-reconciliation of existing CRs when the Depot is patched

```bash
cd services/depot
make test-e2e
```

The depot suite calls the [OpenTofu Registry API](https://registry.opentofu.org) for provider discovery. The API enforces a maximum page size of `20` and uses an ISO 8601 timestamp as the pagination cursor.

### Version Controller

The version suite builds the version controller, module controller, and server images. It exercises:

- Version controller pod health (`app=version-controller`)
- Version CR creation by the module controller (a Module CR is applied; the module controller creates a `{module-name}-{version}` Version CR)
- Version CR reconciliation by the version controller (`status.synced=true`)

```bash
cd services/version
make test-e2e
```

> [!IMPORTANT]
> The version suite builds all three images internally using the default tag `version-controller:e2e-test`. The suite relies on the module controller to create the Version CR — standalone Version CRs are not tested directly because the version controller requires a `moduleConfigRef.name` pointing to an existing Module CR.

---

## Regenerating CRDs

When you change types in `api/v1alpha1/types.go` you must regenerate both the deep-copy code and the CRD YAML files before running any e2e suite:

```bash
cd api/v1alpha1
make generate manifests
```

This writes updated CRDs to `chart/opendepot/crds/`. The e2e suites apply that directory with `kubectl apply --server-side --force-conflicts` in their `BeforeSuite`, so a fresh `make generate manifests` is all that is needed — no manual `kubectl apply` is required before running tests.

> [!WARNING]
> Files under `chart/opendepot/crds/` are auto-generated by `controller-gen`. Do **not** hand-edit them. Always update the Go type definitions in `api/v1alpha1/` and run `make manifests` to regenerate.

---

## Building Images Manually

If you want to iterate quickly on a single service without running the full test suite, you can build and load images with the top-level `Makefile`:

```bash
# Build all images (linux/arm64 by default)
make build

# Load all images into the kind cluster
make load

# Or build+load a single service
make service NAME=depot-controller
```

To build for a different platform (e.g. x86-64):

```bash
PLATFORM=linux/amd64 make build
```

All services that import shared packages (`pkg/` or other services' Go modules) must be built from the **repository root** as the Docker build context — the Dockerfiles use `COPY` directives that reference paths relative to the root. The `make` targets handle this automatically.

> [!NOTE]
> Each service Dockerfile runs `RUN go work edit -dropuse=./test/integration` before `go mod download`. This drops the `test/integration` module from the Go workspace inside the build context, preventing an ambiguous gRPC import error that arises when `test/integration` (which pulls `terratest`) is present in `go.work`. Do not remove this step from a Dockerfile.

---

## Adding a Storage Backend

OpenDepot's storage layer is abstracted behind the `Storage` interface defined in [`pkg/storage/storage.go`](pkg/storage/storage.go). Adding support for a new storage system (e.g. Oracle Object Storage, MinIO, an SFTP server) requires only implementing that interface and wiring the new type into the controllers.

### The interface

```go
type Storage interface {
    DeleteObject(ctx context.Context, soi *types.StorageObjectInput) error
    GetObject(ctx context.Context, soi *types.StorageObjectInput) (io.Reader, error)
    GetObjectChecksum(ctx context.Context, soi *types.StorageObjectInput) error
    PresignObject(ctx context.Context, soi *types.StorageObjectInput) error
    PutObject(ctx context.Context, soi *types.StorageObjectInput) error
}
```

All methods receive a `*types.StorageObjectInput` which carries everything a backend needs:

| Field | Type | Purpose |
|-------|------|---------|
| `FilePath` | `*string` | Destination path / object key / blob name |
| `FileBytes` | `[]byte` | Raw artifact bytes (populated before `PutObject`) |
| `FileExists` | `bool` | **Set this to `true`** inside `GetObjectChecksum` when the object is found |
| `ObjectChecksum` | `*string` | **Set this** to the base64-encoded SHA-256 digest inside `GetObjectChecksum` |
| `ArchiveChecksum` | `*string` | Expected checksum from the source (GitHub archive, etc.) used for verification |
| `Version` | `*v1alpha1.Version` | The Version CR being reconciled |

### Implementation steps

1. **Create a new file** in `pkg/storage/`, e.g. `minio.go`:

   ```go
   package storage

   import (
       "context"
       "io"

       "github.com/tonedefdev/opendepot/pkg/storage/types"
   )

   type MinIO struct {
       // exported fields populated from the CRD spec or a Secret
       Endpoint  string
       Bucket    string
       AccessKey string
       SecretKey string
   }

   func (s *MinIO) DeleteObject(ctx context.Context, soi *types.StorageObjectInput) error { ... }
   func (s *MinIO) GetObject(ctx context.Context, soi *types.StorageObjectInput) (io.Reader, error) { ... }
   func (s *MinIO) GetObjectChecksum(ctx context.Context, soi *types.StorageObjectInput) error { ... }
   func (s *MinIO) PresignObject(ctx context.Context, soi *types.StorageObjectInput) error
   { ... }
   func (s *MinIO) PutObject(ctx context.Context, soi *types.StorageObjectInput) error { ... }
   ```

   Refer to the existing [`filesystem.go`](pkg/storage/filesystem.go), [`aws.go`](pkg/storage/aws.go), or [`azure.go`](pkg/storage/azure.go) implementations as concrete examples.

2. **Add a `StorageMethod` constant** (if needed) in `pkg/storage/types/types.go` and regenerate the stringer:

   ```bash
   cd pkg/storage/types
   go generate ./...
   ```

3. **Extend the CRD** to expose the new backend's configuration. Storage method selection lives in `api/v1alpha1/types.go`. Add a new `storageMethod` enum value and any associated spec fields, then regenerate CRDs:

   ```bash
   cd api/v1alpha1
   make generate manifests
   ```

4. **Wire the backend into each controller** that handles storage. Each relevant controller constructs a `storage.Storage` value based on the `storageMethod` field on the reconciled CR — add a case for the new method that instantiates your new type.

5. **Update the Helm chart** (`chart/opendepot/values.yaml` and the relevant deployment template) to surface any new configuration your backend requires (endpoint, bucket name, credentials reference, etc.).

### `RemoveTrailingSlash` helper

The package exposes `storage.RemoveTrailingSlash(s *string)` which strips a trailing `/` or `\` from a path string. Use it when constructing `soi.FilePath` to avoid double-slash object keys, consistent with how the existing backends behave.

---

## Test Architecture

Each controller's e2e suite follows the same pattern:

1. **BeforeSuite** — builds Docker images, loads them into Kind via `kind load docker-image`, applies CRDs from `chart/opendepot/crds/`, then runs `helm upgrade --install` with `--set` overrides to deploy the local images.
2. **Ordered `Describe` block** — a `BeforeAll` creates the test CRs; `AfterAll` deletes them. Tests within the block run sequentially and build on each other's state (e.g. later tests assume a synced artifact from an earlier test).
3. **AfterSuite** — reverts the Helm release back to production image references so the cluster is left in a clean state.

The Helm release name is `opendepot` and the namespace is `opendepot-system` for all suites. Because suites share the same cluster and Helm release, **do not run multiple suites concurrently** — run them one at a time.

---

## Manual Tests

### OIDC Login Flow (`tofu login`)

The automated e2e suite validates service discovery, OIDC JWT acceptance, and `tofu init` with stored credentials. It does not open a browser and complete the interactive authorization code flow. Use the Tilt environment to verify that flow manually.

#### Why this can't be automated

`tofu login` opens a browser, requires a user to authenticate, and redirects to a temporary callback listener on `localhost`. Tilt provides the required browser-reachable topology: the UI port-forward exposes OpenDepot at `opendepot.localtest.me:8080`, and the server proxies `/dex/*` to the in-cluster Dex service.

#### Prerequisites

- The [Tilt development environment](#tilt-development-environment) prerequisites
- OpenTofu v1.6 or later on your `PATH`
- A browser available on the development machine

#### Step 1: Start Tilt

Start the complete authenticated environment from the repository root:

```bash
export OPENDEPOT_DEV_PASSWORD='choose-a-local-password'
tilt/scripts/up.sh
```

Wait for the `ui` resource to become ready in the [Tilt dashboard](http://localhost:10350). OpenDepot is available at [http://opendepot.localtest.me:8080](http://opendepot.localtest.me:8080).

#### Step 2: Seed the test resources

Create the maintained sample Module and matching GroupBinding:

```bash
tilt trigger seed-sample-resources
kubectl get versions -n opendepot-system -w
```

Wait until `terraform-aws-key-pair-2-0-3` reports `SYNCED=true`, then stop the watch.

#### Step 3: Configure local service discovery

OpenTofu requires an explicit host override because the local registry uses HTTP. Create a temporary CLI configuration:

```bash
cat > /tmp/opendepot-manual-test.tfrc <<'EOF'
host "opendepot.localtest.me:8080" {
  services = {
    "modules.v1"   = "http://opendepot.localtest.me:8080/opendepot/modules/v1/"
    "providers.v1" = "http://opendepot.localtest.me:8080/opendepot/providers/v1/"
    "login.v1" = {
      client      = "opendepot"
      grant_types = ["authz_code"]
      authz       = "http://opendepot.localtest.me:8080/dex/auth"
      token       = "http://opendepot.localtest.me:8080/dex/token"
      scopes      = ["openid", "email", "profile", "groups", "offline_access"]
      ports       = [10000, 10010]
    }
  }
}
EOF
```

Verify that the proxied Dex endpoints are advertised:

```bash
curl -s http://opendepot.localtest.me:8080/.well-known/terraform.json | jq '."login.v1"'
```

The `authz` and `token` values must use `http://opendepot.localtest.me:8080/dex`. An in-cluster `.svc.cluster.local` URL indicates that Tilt's generated values were not applied.

#### Step 4: Run `tofu login`

```bash
TF_CLI_CONFIG_FILE=/tmp/opendepot-manual-test.tfrc \
  tofu login opendepot.localtest.me:8080
```

Expected flow:

1. OpenTofu opens the proxied Dex sign-in page in the browser.
2. Sign in as `dev@example.com` with the value of `OPENDEPOT_DEV_PASSWORD`.
3. Dex redirects to OpenTofu's temporary `localhost` callback.
4. OpenTofu exchanges the authorization code and prints `Successfully retrieved token.`
5. OpenTofu stores the token in `~/.terraform.d/credentials.tfrc.json`.

Verify the stored entry:

```bash
jq '.credentials["opendepot.localtest.me:8080"]' \
  ~/.terraform.d/credentials.tfrc.json
```

#### Step 5: Verify authenticated module access

Use the maintained local fixture, which references the seeded module:

```bash
rm -rf test/local/.terraform
TF_CLI_CONFIG_FILE=/tmp/opendepot-manual-test.tfrc \
  tofu -chdir=test/local init
```

**Pass:** OpenTofu downloads `terraform-aws-key-pair` version `2.0.3` from OpenDepot and completes module initialization. A `401` means the stored token was not sent or was rejected. A `403` means the token was accepted but the sample GroupBinding is missing or does not match `local-test-group`.

#### Cleanup

```bash
tilt trigger clear-sample-resources
rm -f /tmp/opendepot-manual-test.tfrc
tilt down
```

`tilt down` removes the deployed resources but preserves the reusable `kind-opendepot` cluster and local registry. Use `tilt/scripts/reset-cluster.sh` to remove all local OpenDepot data and recreate the development cluster.
