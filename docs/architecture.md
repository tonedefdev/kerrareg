# Architecture

Kerrareg consists of four Kubernetes controllers and a server, all deployed via the Helm chart.

## Event Flow

1. **Depot controller** watches `Depot` resources, queries the GitHub Releases API for modules matching version constraints, queries the HashiCorp Releases API for providers matching version constraints, and creates or updates `Module` and `Provider` resources
2. **Module controller** watches `Module` resources, creates a `Version` resource for each version listed in `spec.versions`, generates unique filenames, and tracks the latest version
3. **Provider controller** watches `Provider` resources, creates a `Version` resource for each version and OS/architecture combination in `spec.versions`, and tracks the latest version
4. **Version controller** watches `Version` resources, fetches module source from GitHub or provider binaries from the HashiCorp Releases API, computes SHA256 checksums, generates GPG signatures (for providers), and uploads archives to the configured storage backend
5. **Server** handles OpenTofu/Terraform requests, queries Kubernetes for `Module`, `Provider`, and `Version` resources, and redirects downloads to the storage backend

## Services

### Version Controller (Core)

The most critical component. It performs the actual work of fetching module and provider artifacts and uploading them to storage.

**Reconciliation loop (modules):**

1. Fetches the module source from GitHub at the specified version/tag
2. Packages the source into a distribution archive (`.tar.gz` or `.zip`)
3. Generates a UUID7 filename for the archive (via `spec.fileName`, set by the Module controller on creation)
4. Computes a base64-encoded SHA256 checksum
5. Uploads the archive to the configured storage backend
6. Updates the `Version` resource status with the checksum and sync state

**Reconciliation loop (providers):**

1. Queries the HashiCorp Releases API for the provider binary matching the target OS/architecture
2. Downloads the provider archive (`.zip`)
3. Generates a UUID7 filename and persists it to `spec.fileName` on the `Version` resource — subsequent reconciliations reuse the same filename, preventing duplicate uploads
4. Computes a SHA256 checksum and generates a detached GPG signature over the `SHA256SUMS` file
5. Uploads the archive to the configured storage backend
6. Updates the `Version` resource status with the sync state

**Unpredictable filenames:** Both module and provider archives are stored with UUID7-generated filenames (e.g., `019726b3-1a2b-7c3d-8e4f-5a6b7c8d9e0f.zip`) instead of the original source filename. This prevents enumeration of storage objects by unauthenticated clients — the download URL cannot be guessed without first authenticating to the registry API and retrieving the `Version` resource.

**Immutability:** When `immutable: true` is set in the module config, the Version controller enforces that the stored checksum always matches the archive checksum. This prevents any modification or replacement of a published version.

### Module Controller

Orchestrates version lifecycle management. For each version in `Module.spec.versions`, the Module controller:

- Creates a corresponding `Version` resource with the module configuration
- Generates a UUID7 filename with the appropriate extension (`.zip` or `.tar.gz`)
- Tracks the latest version using semantic version sorting
- Garbage-collects orphaned `Version` resources when versions are removed
- Enforces `versionHistoryLimit` when configured

### Provider Controller

Orchestrates provider version lifecycle management. For each version in `Provider.spec.versions`, the Provider controller creates a `Version` resource for every OS/architecture combination defined in `spec.providerConfig.operatingSystems` and `spec.providerConfig.architectures`. For example, a single `Provider` with one version, two operating systems (`linux`, `darwin`), and two architectures (`amd64`, `arm64`) will produce four `Version` resources.

The Provider controller:

- Creates `Version` resources for each version × OS × architecture combination
- Tracks the latest version using semantic version sorting
- Garbage-collects orphaned `Version` resources when versions are removed
- Enforces `versionHistoryLimit` when configured
- Labels each `Version` with `kerrareg.io/provider=<name>` for easy filtering

**Example Provider resource:**

```yaml
apiVersion: kerrareg.io/v1alpha1
kind: Provider
metadata:
  name: aws
  namespace: kerrareg-system
spec:
  providerConfig:
    name: aws
    operatingSystems:
      - linux
      - darwin
    architectures:
      - amd64
      - arm64
    storageConfig:
      s3:
        bucket: kerrareg-providers
        region: us-west-2
  versions:
    - version: "5.80.0"
    - version: "5.81.0"
```

This produces eight `Version` resources (`5.80.0-linux-amd64`, `5.80.0-linux-arm64`, `5.80.0-darwin-amd64`, `5.80.0-darwin-arm64`, and the same four for `5.81.0`). The Version controller then fetches each binary from the HashiCorp Releases API and stores it in S3 under a UUID7 filename.

### Depot Controller

Automates module and provider discovery. The Depot controller:

- Queries the **GitHub Releases API** for each entry in `spec.moduleConfigs`, resolves version constraints, and creates or updates `Module` resources
- Queries the **HashiCorp Releases API** for each entry in `spec.providerConfigs`, resolves version constraints, and creates or updates `Provider` resources
- Supports configurable polling intervals (`pollingIntervalMinutes`)
- Inherits `global` config (storage, GitHub auth, file format) to each module unless overridden
- Updates `status.modules` and `status.providers` with the names of all managed resources
- Serves as a **migration bridge** — import modules and providers in bulk, then delete the Depot once you transition to CI/CD-driven publishing

### Server

Implements both the Module Registry Protocol and the Provider Registry Protocol as an HTTP API. The server authenticates requests using either Kubernetes bearer tokens or base64-encoded kubeconfigs, then queries the Kubernetes API for module, provider, and version data.

Provider artifact endpoints (binary download, `SHA256SUMS`, `SHA256SUMS.sig`) are served using the server's own ServiceAccount per the [Terraform Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol) — OpenTofu fetches these URLs without forwarding client credentials, so authentication is provided at the metadata tier rather than the artifact tier.

!!! warning
    To prevent unauthenticated users from easily enumerating provider artifacts, provider files are stored with UUID7-based filenames.

## Project Structure

```
kerrareg/
├── api/v1alpha1/              # CRD type definitions
│   ├── types.go               # Depot, Module, Version, StorageConfig schemas
│   └── groupversion_info.go   # API group registration
├── chart/kerrareg/            # Helm chart
│   ├── Chart.yaml
│   ├── values.yaml
│   ├── crds/                  # CRD manifests
│   └── templates/             # Deployment, RBAC, Service templates
├── pkg/
│   ├── github/                # GitHub API client (App auth, archive fetching)
│   └── storage/               # Storage backend implementations
├── services/
│   ├── server/                # Registry Protocol API (HTTP server)
│   ├── version/               # Version controller (core — fetch & store)
│   ├── module/                # Module controller (version lifecycle)
│   ├── provider/              # Provider controller (version lifecycle for providers)
│   └── depot/                 # Depot controller (GitHub + HashiCorp discovery)
├── Makefile                   # Build, load, deploy targets
└── go.work                    # Go workspace (multi-module)
```
