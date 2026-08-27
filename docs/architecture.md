---
tags:
  - architecture
  - internals
  - kubernetes
---

# Architecture

OpenDepot consists of four Kubernetes controllers, a server, a bundled Valkey stats store, and an optional UI frontend, all deployed via the Helm chart.

## Event Flow

1. **Depot controller** watches `Depot` resources, queries the GitHub Releases API for modules matching version constraints, queries the configured upstream provider registry (`registry.opentofu.org` or `registry.terraform.io`) for providers matching version constraints, and creates or updates `Module` and `Provider` resources
2. **Module controller** watches `Module` resources, creates a `Version` resource for each version listed in `spec.versions`, generates unique filenames, and tracks the latest version
3. **Provider controller** watches `Provider` resources, creates a `Version` resource for each version and OS/architecture combination in `spec.versions`, and tracks the latest version
4. **Version controller** watches `Version` resources, fetches module source from GitHub or provider binaries via the configured upstream registry download API, computes SHA256 checksums, generates GPG signatures (for providers), and uploads archives to the configured storage backend
5. **Server** handles OpenTofu/Terraform read requests, queries Kubernetes for `Module`, `Provider`, `Version`, and (when OIDC is enabled) `GroupBinding` resources, serves or redirects artifact downloads, and records download events in the bundled Valkey stats store
6. **Registry Explorer UI** (optional, `ui.enabled: true`) — a Next.js frontend with an NGINX sidecar that provides a browsable registry explorer. NGINX splits traffic between the UI and the server using path-based routing

## Services

### Version Controller (Core)

The most critical component. It performs the actual work of fetching module and provider artifacts and uploading them to storage.

**Reconciliation loop (modules):**

1. Fetches the module source from GitHub at the specified version/tag
2. Packages the source into a distribution archive (`.tar.gz` or `.zip`)
3. Generates a UUID7 filename for the archive (via `spec.fileName`, set by the Module controller on creation)
4. Computes a base64-encoded SHA256 checksum
5. Uploads the archive to the configured storage backend
6. When scanning is enabled, extracts the archive to a temporary directory and runs an IaC scan (`trivy fs`) for HCL misconfigurations, storing findings in `Version.status.sourceScan`
7. If `blockOnCritical` or `blockOnHigh` is configured, halts reconciliation for any version with findings at or above the threshold
8. Updates the `Version` resource status with the checksum and sync state

**Reconciliation loop (providers):**

1. Queries the configured upstream provider registry API (either `registry.opentofu.org` or `registry.terraform.io`, based on `spec.providerConfig.upstreamRegistry`) for the provider binary matching the target OS/architecture
2. Downloads the provider archive (`.zip`)
3. Generates a UUID7 filename and persists it to `spec.fileName` on the `Version` resource — subsequent reconciliations reuse the same filename, preventing duplicate uploads
4. Computes a SHA256 checksum and generates a detached GPG signature over the `SHA256SUMS` file
5. Uploads the archive to the configured storage backend
6. When scanning is enabled, runs a binary scan (`trivy rootfs`) against the extracted provider binary and stores findings in `Version.status.binaryScan`; resolves the provider's source repository (explicit override → upstream registry lookup → heuristic fallback), writes the resolved URL to `Provider.status.resolvedSourceRepository`, and runs a source scan (`trivy fs`) storing results in `Version.status.sourceScan` (deduplicated across OS/architecture variants of the same version)
7. If `blockOnCritical` or `blockOnHigh` is configured, halts reconciliation for any version with findings at or above the threshold
8. Updates the `Version` resource status with the sync state

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
- Labels each `Version` with `opendepot.defdev.io/provider=<name>` for easy filtering

**Example Provider resource:**

```yaml
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
      - darwin
    architectures:
      - amd64
      - arm64
    storageConfig:
      s3:
        bucket: opendepot-providers
        region: us-west-2
  versions:
    - version: "5.80.0"
    - version: "5.81.0"
```

This produces eight `Version` resources with normalized names (`aws-5-80-0-linux-amd64`, `aws-5-80-0-linux-arm64`, `aws-5-80-0-darwin-amd64`, `aws-5-80-0-darwin-arm64`, and the same four for `5.81.0`). Version resource names are lowercased and replace `.`, `_`, and `/` with `-`. The Version controller then resolves each binary through the configured upstream provider registry and stores it in S3 under a UUID7 filename.

### Depot Controller

Automates module and provider discovery. The Depot controller:

- Queries the **GitHub Releases API** for each entry in `spec.moduleConfigs`, resolves version constraints, and creates or updates `Module` resources
- Queries the configured **upstream provider registry** for each entry in `spec.providerConfigs`, resolves version constraints, and creates or updates `Provider` resources. `spec.providerConfig.upstreamRegistry` defaults to `registry.opentofu.org` and also supports `registry.terraform.io`.
- Supports configurable polling intervals (`pollingIntervalMinutes`)
- Inherits `global` config (storage, GitHub auth, file format) to each module unless overridden
- Updates `status.modules` and `status.providers` with the names of all managed resources
- Serves as a **migration bridge** — import modules and providers in bulk, then delete the Depot once you transition to CI/CD-driven publishing

### Server

Implements both the Module Registry Protocol and the Provider Registry Protocol as an HTTP API. The server is read-only by design: it serves registry metadata and artifacts, but does not create, update, or delete OpenDepot CRs. The server supports three authentication modes:

- **OIDC** — JWTs issued by the bundled [Dex](https://dexidp.io/) identity broker (or any compatible OIDC provider). The server fetches JWKS from the issuer at startup and validates tokens locally on every request — no round-trip to Dex per call. Fine-grained access control is applied via `GroupBinding` resources evaluated against the groups claim in the JWT (first matching binding in alphabetical order).
- **Bearer token** — Kubernetes ServiceAccount tokens or kubeconfig credentials forwarded directly to the Kubernetes API.
- **Anonymous** — No authentication required. Intended for local development only.

When OIDC is enabled, the service discovery endpoint (`/.well-known/terraform.json`) advertises a `login.v1` block, enabling `tofu login` to drive the authorization code or device code flow through Dex. Dex federates upstream IdPs (GitHub, Entra ID, Okta, LDAP, and more) so users authenticate with their existing organizational identity. By default, `server.oidc.dexProxy.enabled: true` reverse-proxies `/dex/*` through the server, so the bundled Dex service does not require its own public ingress or hostname. See [Proxying Dex Through the Server](configuration/oidc.md#recommended-proxy-dex-through-the-server).

The server also accepts [client credentials](configuration/oidc.md#client-credentials-machine-to-machine) tokens from Dex machine clients when `allowClientCredentials` is enabled. The token's `sub` claim is mapped to a virtual group (`"client:<sub>"`) and evaluated against GroupBinding resources, giving machine identities the same scoped access model as human users.

After authenticating, the server queries the Kubernetes API for `Module`, `Provider`, and `Version` resources to serve registry protocol responses.

Provider artifact endpoints (binary download, `SHA256SUMS`, `SHA256SUMS.sig`) are served using the server's own ServiceAccount per the [Terraform Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol), which both OpenTofu and Terraform implement. Clients fetch these URLs without forwarding registry credentials, so authentication is provided at the metadata tier rather than the artifact tier. When pre-signing is enabled on provider storage config, the server can return a `307 Temporary Redirect` to a backend-native signed URL; otherwise it proxies the artifact response directly.

!!! warning
    To prevent unauthenticated users from easily enumerating provider and module artifacts, files are stored with UUID7-based filenames.

### Valkey Stats Store

A [Valkey](https://valkey.io/) (Redis-compatible) instance deployed automatically alongside the server via the official `valkey-io/valkey-helm` subchart. The server records download events in Valkey using a scoped key namespace (`stats:*`) and reads aggregate counts for the Registry Explorer Stats page.

Valkey runs as a StatefulSet with a PVC for persistence by default (`valkey.dataStorage.enabled: true`). Disable persistence for local development or ephemeral environments where no StorageClass is available.

Optional ACL password authentication can be enabled via `valkey.auth.enabled: true`. When enabled, the server reads the password from the `OPENDEPOT_VALKEY_PASSWORD` environment variable, injected via a Kubernetes `secretKeyRef`. See [Valkey Stats Store](helm-chart.md#valkey-stats-store) for the full configuration reference.

### Registry Explorer UI

An optional Next.js frontend deployed when `ui.enabled: true`. The UI pod runs two processes:

- **Next.js** (port 3000) — serves the browser application
- **NGINX** (port 80) — acts as a reverse proxy in front of both Next.js and the server

NGINX applies split-path routing: requests to `/opendepot/*` and `/.well-known/*` are proxied to the server Service; all other requests are forwarded to Next.js on `localhost:3000`. This means the browser never needs to know the server's address — all API calls are same-origin.

!!! note "Why NGINX instead of Next.js rewrites"
    Next.js can proxy routes itself (via `rewrites()` or middleware), but NGINX is kept as a dedicated layer for a few reasons that don't map cleanly onto Next.js's request pipeline:

    - **Process isolation** — the Next.js server binds to `127.0.0.1:3000` only (see `entrypoint.sh`), so NGINX is the sole process reachable from outside the pod. A bug in the Next.js app can't be reached directly over the network.
    - **Streaming large artifacts** — module tarballs and provider binaries proxied through `/opendepot/*` are passed through by NGINX without being buffered into the Node/V8 process, which matters as file sizes and concurrency grow.
    - **WebSocket upgrades and custom header proxying** — the catch-all location handles `Upgrade`/`Connection` headers and forwards `Authorization`/`X-Request-ID`, which Next.js's `rewrites()` config doesn't support; doing this in Next.js itself would require a custom server, which forfeits some of the benefits of `output: "standalone"`.

The server exposes browse API endpoints (`/opendepot/ui/v1/*`) specifically for the UI. These endpoints apply visibility filtering based on `opendepot.defdev.io/public` labels and, for authenticated callers, `GroupBinding` evaluation. See [Registry Explorer UI](guides/registry-explorer.md) for full details.
