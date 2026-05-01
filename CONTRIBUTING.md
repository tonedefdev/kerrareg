# Contributing to Kerrareg

Thank you for your interest in contributing to Kerrareg! This guide covers everything you need to run the end-to-end test suite locally.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Repository Layout](#repository-layout)
- [Local Cluster Setup](#local-cluster-setup)
- [Running the E2E Tests](#running-the-e2e-tests)
  - [Module Controller](#module-controller)
  - [Provider Controller](#provider-controller)
  - [Depot Controller](#depot-controller)
  - [Version Controller](#version-controller)
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
| kubectl | v1.27+ | Cluster interaction |
| [Helm](https://helm.sh/) | v3.14+ | Chart installation |
| [OpenTofu](https://opentofu.org/) (`tofu`) | v1.6+ | Required for `tofu init` tests in the module and provider suites |
| gpg | 2.x | Required for provider GPG signing tests |

Verify everything is on your `PATH` before running tests:

```bash
go version && docker version --format '{{.Server.Version}}' && kind version && kubectl version --client --short && helm version --short && tofu version && gpg --version | head -1
```

---

## Repository Layout

```
kerrareg/
├── api/v1alpha1/        # CRD types; run `make generate manifests` here to regenerate
├── chart/kerrareg/      # Helm chart deployed by every e2e suite
│   └── crds/            # CRD YAML files applied before each test run
├── services/
│   ├── depot/           # Depot controller — watches Depot CRs, creates Module/Provider CRs
│   │   └── test/e2e/
│   ├── module/          # Module controller — downloads and stores module artifacts
│   │   └── test/e2e/
│   ├── provider/        # Provider controller — downloads, signs, and stores provider artifacts
│   │   └── test/e2e/
│   ├── server/          # Registry API server
│   └── version/         # Version controller — computes checksums and tracks artifact state
└── pkg/                 # Shared packages (storage backends, GitHub client)
```

---

## Local Cluster Setup

Each e2e suite is fully self-contained — it builds images, loads them into a Kind cluster, applies CRDs, and deploys the Helm chart. All you need is a running Kind cluster named `kind`:

```bash
kind create cluster --name kind
```

> **Note:** If you already have a `kind` cluster from a previous run it can be reused. The suites use `helm upgrade --install` so they are safe to run repeatedly.

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
IMG=module-controller:e2e-test go test ./test/e2e/ -v -count=1 -timeout 20m
```

The suite uses `kerrareg.localtest.me` as the registry hostname — this is a public DNS name that resolves to `127.0.0.1` and satisfies OpenTofu's requirement for a hostname that contains at least one dot.

### Provider Controller

The provider suite builds the provider controller, version controller, and server images. It exercises:

- Provider CR reconciliation and Version CR creation
- Artifact download from the HashiCorp Releases API
- GPG signing of `SHA256SUMS` and generation of `SHA256SUMS.sig`
- Provider Registry Protocol API endpoints (`providers.v1`)
- `tofu init` against the local registry
- Kubernetes RBAC enforcement (anonymous auth on/off, bearer-token auth)

> **Note:** Provider binaries can be several hundred MB. The artifact download step has a 5-minute timeout. Ensure you have sufficient disk space and a stable internet connection.

The suite generates a temporary GPG key pair automatically — no manual key setup is required.

```bash
cd services/provider
IMG=provider-controller:e2e-test go test ./test/e2e/ -v -count=1 -timeout 20m
```

### Depot Controller

The depot suite builds the depot controller image only. It exercises:

- Depot CR reconciliation creating Module and Provider CRs from `moduleConfigs` and `providerConfigs`
- Version constraint filtering (uses `= X.Y.Z` exact-match constraints)
- `status.modules` and `status.providers` population on the Depot CR
- Re-reconciliation of existing CRs when the Depot is patched

```bash
cd services/depot
IMG=depot-controller:e2e-test go test ./test/e2e/ -v -count=1 -timeout 20m
```

The depot suite calls the [HashiCorp Releases API](https://api.releases.hashicorp.com) for provider discovery. The API enforces a maximum page size of `20` and uses an ISO 8601 timestamp as the pagination cursor.

### Version Controller

The version suite builds the version controller, module controller, and server images. It exercises:

- Version controller pod health (`app=version-controller`)
- Version CR creation by the module controller (a Module CR is applied; the module controller creates a `{module-name}-{version}` Version CR)
- Version CR reconciliation by the version controller (`status.synced=true`)

```bash
cd services/version
go test ./test/e2e/ -v -count=1 -timeout 20m
```

> **Note:** Unlike the other suites, no `IMG=` prefix is needed. The `BeforeSuite` builds all three images internally using the default tag `version-controller:e2e-test`. The suite relies on the module controller to create the Version CR — standalone Version CRs are not tested directly because the version controller requires a `moduleConfigRef.name` pointing to an existing Module CR.

---

## Regenerating CRDs

When you change types in `api/v1alpha1/types.go` you must regenerate both the deep-copy code and the CRD YAML files before running any e2e suite:

```bash
cd api/v1alpha1
make generate manifests
```

This writes updated CRDs to `chart/kerrareg/crds/`. The e2e suites apply that directory with `kubectl apply --server-side --force-conflicts` in their `BeforeSuite`, so a fresh `make generate manifests` is all that is needed — no manual `kubectl apply` is required before running tests.

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

---

## Adding a Storage Backend

Kerrareg's storage layer is abstracted behind the `Storage` interface defined in [`pkg/storage/storage.go`](pkg/storage/storage.go). Adding support for a new storage system (e.g. Oracle Object Storage, MinIO, an SFTP server) requires only implementing that interface and wiring the new type into the controllers.

### The interface

```go
type Storage interface {
    DeleteObject(ctx context.Context, soi *types.StorageObjectInput) error
    GetObject(ctx context.Context, soi *types.StorageObjectInput) (io.Reader, error)
    GetObjectChecksum(ctx context.Context, soi *types.StorageObjectInput) error
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

       "github.com/tonedefdev/kerrareg/pkg/storage/types"
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

5. **Update the Helm chart** (`chart/kerrareg/values.yaml` and the relevant deployment template) to surface any new configuration your backend requires (endpoint, bucket name, credentials reference, etc.).

### `RemoveTrailingSlash` helper

The package exposes `storage.RemoveTrailingSlash(s *string)` which strips a trailing `/` or `\` from a path string. Use it when constructing `soi.FilePath` to avoid double-slash object keys, consistent with how the existing backends behave.

---

## Test Architecture

Each controller's e2e suite follows the same pattern:

1. **BeforeSuite** — builds Docker images, loads them into Kind via `kind load docker-image`, applies CRDs from `chart/kerrareg/crds/`, then runs `helm upgrade --install` with `--set` overrides to deploy the local images.
2. **Ordered `Describe` block** — a `BeforeAll` creates the test CRs; `AfterAll` deletes them. Tests within the block run sequentially and build on each other's state (e.g. later tests assume a synced artifact from an earlier test).
3. **AfterSuite** — reverts the Helm release back to production image references so the cluster is left in a clean state.

The Helm release name is `kerrareg` and the namespace is `kerrareg-system` for all suites. Because suites share the same cluster and Helm release, **do not run multiple suites concurrently** — run them one at a time.
