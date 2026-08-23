---
tags:
  - reference
  - api
---

# API Reference

For breaking changes between versions, see [Upgrading](../upgrading.md).

## Service Discovery

```
GET /.well-known/terraform.json
```

**Response:**

```json
{
  "modules.v1": "/opendepot/modules/v1/",
  "providers.v1": "/opendepot/providers/v1/",
  "login.v1": {
    "client": "opentofu-cli",
    "grant_types": ["authz_code", "device_code"],
    "authz": "https://opendepot.example.com/dex/auth",
    "token": "https://opendepot.example.com/dex/token",
    "ports": [10000, 10001, 10002, 10003, 10004, 10005, 10006, 10007, 10008, 10009, 10010]
  }
}
```

The `authz` and `token` URLs above assume the recommended [server-proxied Dex](../configuration/oidc.md#recommended-proxy-dex-through-the-server) setup, so they share the same host as the rest of the registry API. Without the proxy, they point at wherever Dex itself is exposed.

The `login.v1` field is only present when OIDC authentication is enabled. It advertises the OIDC endpoints and ports that the `tofu login` command uses to obtain a JWT.

## OIDC Device Authorization

```
POST {login.v1.authz}
```

Initiates the OAuth 2.0 device authorization grant flow. Used by `tofu login` on headless systems (servers, CI runners) to obtain a device code and user code.

**Query Parameters:**

| Parameter | Description |
|-----------|-------------|
| `client_id` | From `login.v1.client` |
| `scope` | Space-separated scopes (e.g. `openid profile email groups`) |

**Response:**

```json
{
  "device_code": "AQABAGF...",
  "user_code": "WXYZ-ABCD",
  "verification_uri": "https://opendepot.example.com/dex/device",
  "expires_in": 300,
  "interval": 5
}
```

## OIDC Token Exchange

```
POST {login.v1.token}
```

Exchanges an authorization code (authz_code flow) or device code (device_code flow) for a JWT. Used by `tofu login` to obtain the bearer token returned to the client.

**Request (authz_code grant):**

```
POST https://opendepot.example.com/dex/token
grant_type=authorization_code&code=<AUTH_CODE>&client_id=<CLIENT_ID>&redirect_uri=http://localhost:10000
```

**Request (device_code grant):**

```
POST https://opendepot.example.com/dex/token
grant_type=urn:ietf:params:oauth:grant-type:device_code&device_code=<DEVICE_CODE>&client_id=<CLIENT_ID>
```

**Response:**

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "bearer",
  "expires_in": 3600,
  "id_token": "eyJhbGciOiJSUzI1NiIs..."
}
```

The `id_token` (JWT) is returned to `tofu login` and subsequently passed to OpenDepot as a bearer token in the `Authorization: Bearer <id_token>` header.

## Authentication Modes

All endpoints that require authentication accept the `Authorization: Bearer <token>` header. The server operates in one of the following modes, configured at startup:

| Mode | Value | Token accepted | K8s calls use |
|---|---|---|---|
| Anonymous | `--anonymous-auth` | None | Server SA |
| Kubeconfig | (default) | Base64-encoded kubeconfig | Caller's kubeconfig identity |
| Bearer token | `--use-bearer-token` | SA bearer token | Caller's SA RBAC |
| OIDC | `--oidc-issuer-url` + `--oidc-client-id` | Dex JWT | Server SA (GroupBinding controls access) |
| OIDC + SA fallback | above + `--oidc-allow-sa-fallback` | Dex JWT **or** SA token | Dex JWT → Server SA; SA token → Caller's SA RBAC |

## List Module Versions

```
GET /opendepot/modules/v1/{namespace}/{name}/{system}/versions
```

Returns all available versions of a module. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Module resource |
| `name` | Module name |
| `system` | Provider (e.g., `aws`, `azurerm`) |

## Download Module

```
GET /opendepot/modules/v1/{namespace}/{name}/{system}/{version}/download
```

Returns `204 No Content` with an `X-Terraform-Get` header pointing to the storage-specific download URL. Requires authentication.

## Storage Download Endpoints (Modules)

These endpoints are called by OpenTofu/Terraform after receiving the `X-Terraform-Get` redirect. They validate the SHA256 checksum and stream the module archive.

```
GET /opendepot/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}?fileChecksum={checksum}
GET /opendepot/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}?fileChecksum={checksum}
GET /opendepot/modules/v1/download/gcs/{bucket}/{name}/{fileName}?fileChecksum={checksum}
GET /opendepot/modules/v1/download/fileSystem/{directory}/{name}/{fileName}?fileChecksum={checksum}
```

## List Provider Versions

```
GET /opendepot/providers/v1/{namespace}/{type}/versions
```

Returns all available versions of a provider and the platforms each version supports. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `type` | Provider name (e.g., `aws`, `azurerm`) |

**Response:**

```json
{
  "versions": [
    {
      "version": "5.80.0",
      "protocols": ["6.0"],
      "platforms": [
        { "os": "linux", "arch": "amd64" },
        { "os": "linux", "arch": "arm64" }
      ]
    }
  ]
}
```

## Provider Package Metadata

```
GET /opendepot/providers/v1/{namespace}/{type}/{version}/download/{os}/{arch}
```

Returns the download URL, SHA256 checksum, and GPG signing key for a specific provider binary. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `type` | Provider name |
| `version` | Provider version |
| `os` | Operating system (e.g., `linux`, `darwin`) |
| `arch` | CPU architecture (e.g., `amd64`, `arm64`) |

**Response:**

```json
{
  "protocols": ["6.0"],
  "os": "linux",
  "arch": "amd64",
  "filename": "terraform-provider-aws_5.80.0_linux_amd64.zip",
  "download_url": "https://.../opendepot/providers/v1/download/opendepot-system/aws/5.80.0",
  "shasum": "<hex-sha256>",
  "shasums_url": "https://.../opendepot/providers/v1/opendepot-system/aws/5.80.0/SHA256SUMS/linux/amd64",
  "shasums_signature_url": "https://.../opendepot/providers/v1/opendepot-system/aws/5.80.0/SHA256SUMS.sig/linux/amd64",
  "signing_keys": {
    "gpg_public_keys": [
      {
        "key_id": "<KEY_ID>",
        "ascii_armor": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n..."
      }
    ]
  }
}
```

## Provider Binary Download

```
GET /opendepot/providers/v1/download/{namespace}/{type}/{version}
```

Streams the provider binary archive (`.zip`) directly from storage. Does **not** require client authentication — the server uses its own ServiceAccount per the Terraform Provider Registry Protocol, which both OpenTofu and Terraform implement.

## Provider SHA256SUMS

```
GET /opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS/{os}/{arch}
```

Returns the `SHA256SUMS` text file for the specified provider version and platform. Does **not** require client authentication.

## Provider SHA256SUMS Signature

```
GET /opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS.sig/{os}/{arch}
```

Returns the detached GPG signature over the `SHA256SUMS` file, signed with the key configured in `server.gpg.secretName`. Does **not** require client authentication.

## Provider Network Mirror Protocol

OpenDepot implements the [Provider Network Mirror Protocol](https://opentofu.org/docs/internals/provider-network-mirror-protocol/) (shared by both OpenTofu and Terraform) so configurations can reference providers by their canonical upstream identity (e.g., `registry.opentofu.org/hashicorp/aws` or `registry.terraform.io/hashicorp/aws`) while installing from OpenDepot. The mirror URL is namespace-scoped:

```
https://<host>/opendepot/providers/mirror/v1/<namespace>/
```

Clients configure this URL in their `.tofurc` via the `provider_installation.network_mirror` block. See [Consuming Providers](../guides/providers.md) for full usage examples.

### Mirror: List Provider Versions

```
GET /opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/index.json
```

Returns all available versions of a provider from the specified origin registry. Requires authentication when anonymous mode is disabled.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `hostname` | Origin registry hostname (`registry.opentofu.org` or `registry.terraform.io`) |
| `providerNamespace` | Provider namespace at the origin (e.g., `hashicorp`, `datadog`) |
| `type` | Provider name (e.g., `aws`, `azurerm`) |

**Response:**

```json
{
  "versions": {
    "5.80.0": {},
    "5.81.0": {}
  }
}
```

The version keys are normalized semver strings without a leading `v`. The values are empty objects per the Network Mirror Protocol specification.

### Mirror: Provider Version Metadata

```
GET /opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/{version}.json
```

Returns archive URLs and checksums for all OS/architecture combinations of a specific provider version. Requires authentication when anonymous mode is disabled.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `hostname` | Origin registry hostname (`registry.opentofu.org` or `registry.terraform.io`) |

**Response:**

```json
{
  "archives": {
    "linux_amd64": {
      "url": "5.80.0/linux/amd64/terraform-provider-aws_5.80.0_linux_amd64.zip",
      "hashes": ["zh:abc123..."]
    },
    "linux_arm64": {
      "url": "5.80.0/linux/arm64/terraform-provider-aws_5.80.0_linux_arm64.zip",
      "hashes": ["zh:def456..."]
    }
  }
}
```

The `url` field is a relative path from the mirror base URL. The `hashes` array uses the `zh:` prefix for packed ZIP checksums as required by the Network Mirror Protocol.

### Mirror: Provider Archive Download

```
GET /opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/{version}/{os}/{arch}/{filename}
```

Streams the provider binary archive (`.zip`) directly from storage. Does **not** require client authentication when using anonymous mirror access — the server uses its own ServiceAccount. Archive URLs are discovered via the version metadata endpoint above.

## Browse API

The browse endpoints power the [Registry Explorer UI](../guides/registry-explorer.md) and can also be called directly. All endpoints are accessible without authentication; providing an `Authorization: Bearer <token>` header extends visibility per the [browse visibility rules](../guides/registry-explorer.md#browse-visibility-rules).

### List Namespaces

```
GET /opendepot/ui/v1/namespaces
```

Returns only namespaces that carry the `opendepot.defdev.io/public=true` label. The label selector is sent directly to the Kubernetes API, so system namespaces (`kube-system`, `default`, `kube-public`) are never included in the response regardless of auth mode. The `public` field is always `true` because unlabeled namespaces are excluded before the response is built.

An admin must label a namespace before it appears in the sidebar:

```bash
kubectl label namespace <ns> opendepot.defdev.io/public=true
```

**Response:**

```json
{
  "items": [
    { "name": "opendepot-system", "public": true }
  ]
}
```

### List Resources

```
GET /opendepot/ui/v1/resources
```

Returns a paginated, filtered list of visible `Module` and `Provider` resources.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string (repeatable) | Filter by one or more namespaces |
| `kind` | string | `module` or `provider` |
| `q` | string | Search string matched against resource name |
| `synced` | bool | Filter to synced (`true`) or unsynced (`false`) resources |
| `os` | string | Filter providers by operating system |
| `arch` | string | Filter providers by CPU architecture |
| `severity` | string | Filter to resources with findings at or above this level (`CRITICAL`, `HIGH`, `MEDIUM`, `LOW`) |
| `public_only` | bool | When `true`, return only publicly-labelled resources |
| `sort_by` | string | Sort field |
| `sort_dir` | string | `asc` or `desc` |
| `page` | int | Page number (1-based) |
| `page_size` | int | Results per page |

**Response:**

```json
{
  "items": [
    {
      "kind": "module",
      "namespace": "opendepot-system",
      "name": "terraform-aws-vpc",
      "latestVersion": "3.19.0",
      "synced": true,
      "provider": "aws",
      "repoUrl": "https://github.com/terraform-aws-modules/terraform-aws-vpc",
      "scanCounts": { "critical": 0, "high": 1, "medium": 2, "low": 0, "unknown": 0 },
      "public": true,
      "hasUnsyncedVersions": true,
      "totalDownloads": 4821,
      "lastDownloadedAt": "2026-05-25T14:32:00Z"
    }
  ],
  "totalCount": 1,
  "page": 1,
  "pageSize": 20
}
```

`hasUnsyncedVersions` is present and `true` when at least one `Version` CR under the resource has `status.synced: false` or a `status.syncStatus` containing `"failed"` or `"error"` (case-insensitive). The field is omitted from the response when all versions are healthy.

`scanCounts` reflects vulnerability findings from the **latest version only**. The field is omitted when no version has been scanned.

### Resource Detail

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}
```

Returns full detail for a single resource including all versions and scan findings. The `sourceScanFindings` and `binaryScanFindings` fields contain findings from the **latest version only**.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the resource |
| `kind` | `module` or `provider` |
| `name` | Resource name |

**Response:**

```json
{
  "kind": "module",
  "namespace": "opendepot-system",
  "name": "terraform-aws-vpc",
  "latestVersion": "3.19.0",
  "synced": true,
  "public": true,
  "versions": [
    { "version": "3.19.0", "synced": true },
    { "version": "3.18.0", "synced": true }
  ],
  "sourceScanFindings": [
    {
      "vulnerabilityID": "CVE-2024-12345",
      "pkgName": "some-dep",
      "installedVersion": "1.0.0",
      "fixedVersion": "1.0.1",
      "severity": "HIGH",
      "title": "Example vulnerability"
    }
  ],
  "binaryScanFindings": {
    "linux/amd64": []
  },
  "readmeContent": "# terraform-aws-vpc\n\nTerraform module..."
}
```

`readmeContent` is the decoded (plain markdown) README for the module's latest version. It is only present for `module` resources, and only when a README could be resolved — see [`VersionStatus.readmeConfigMapRef`](#versionstatus-fields). The field is omitted for providers and for modules with no resolvable README.

### List Resource Versions

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions
```

Returns a paginated, filtered list of versions for a single resource. Used by the Registry Explorer detail page to populate the versions table. Authentication follows the same rules as the other browse endpoints.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the resource |
| `kind` | `module` or `provider` |
| `name` | Resource name |

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | `1` | Page number (1-based) |
| `page_size` | int | `20` | Items per page (max `100`) |
| `q` | string | — | Case-insensitive substring filter on the version string |
| `synced` | string | — | `true` = healthy versions only, `false` = failed or error versions only; omit for all |
| `os` | string | — | Exact OS filter (case-insensitive); providers only |
| `arch` | string | — | Exact architecture filter (case-insensitive); providers only |

**Response:**

```json
{
  "items": [
    {
      "version": "3.19.0",
      "synced": true,
      "scanCounts": { "critical": 0, "high": 1, "medium": 2, "low": 0, "unknown": 0 },
      "downloadCount": 1243,
      "lastDownloadedAt": "2026-05-25T14:32:00Z",
      "archiveSizeBytes": 2097152
    }
  ],
  "totalCount": 42,
  "page": 1,
  "pageSize": 20,
  "availableOS": ["darwin", "linux", "windows"],
  "availableArch": ["amd64", "arm64"]
}
```

`availableOS` and `availableArch` are populated from the full (pre-filter) version set so filter dropdowns remain populated while a filter is active. Both fields are omitted for modules; they are only present for providers. Versions are sorted newest-first.

`downloadCount` and `lastDownloadedAt` are omitted when no downloads have been recorded. `archiveSizeBytes` is omitted when `VersionStatus.archiveSizeBytes` has not been set by the version controller.

### Resource Scan Findings

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings
```

Returns scan findings for a single resource. The optional `?version=` query parameter selects which scanned version's findings to return. When omitted, findings from the most recently scanned version are returned. Both leading `v` and surrounding whitespace are stripped from the version string, so `v1.2.3`, `1.2.3`, and ` 1.2.3 ` are treated identically. Authentication follows the same rules as the other browse endpoints.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the resource |
| `kind` | `module` or `provider` |
| `name` | Resource name |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `version` | string | Selects the source scan entry for this exact version. Applies to both modules and providers. Leading `v` and surrounding whitespace are stripped automatically. Omit to return findings from the latest scanned version. |
| `binaryVersion` | string | Providers only. Selects the binary scan entries for this exact version. When omitted, binary findings from the latest scanned version per platform are returned. Leading `v` is stripped automatically. |

**Response (`BrowseScanFindings`):**

```json
{
  "sourceScanFindings": [
    {
      "vulnerabilityID": "CVE-2024-12345",
      "pkgName": "some-dep",
      "installedVersion": "1.0.0",
      "fixedVersion": "1.0.1",
      "severity": "HIGH",
      "title": "Example vulnerability"
    }
  ],
  "binaryScanFindings": {
    "linux/amd64": [],
    "darwin/arm64": []
  },
  "selectedVersion": "3.2.3",
  "scannedVersions": ["3.2.3", "3.2.0"],
  "binaryVersions": ["3.2.3", "3.2.0"]
}
```

`sourceScanFindings` contains IaC (module) or `go.mod` (provider) vulnerability findings. `binaryScanFindings` is a map of `os/arch` → findings; it is only populated for providers. Both fields are omitted when empty.

`selectedVersion` is the version whose source scan findings are included in this response. `scannedVersions` is the full list of versions with accumulated source scan results, sorted descending by semver — used by the UI to populate the source scan version selector dropdown. `binaryVersions` is the equivalent list for binary scan results and is only present for providers. All three fields are omitted when no scan results exist for the resource.

This endpoint is used by the [Registry Explorer UI](../guides/registry-explorer.md#scan-findings) refresh button to re-fetch findings without a full page reload.

### List Depots

```
GET /opendepot/ui/v1/depots
```

Returns a flat list of all visible `Depot` resources with their storage backend, polling interval, and managed resource counts.

**Visibility:** public depots are always included. Non-public depots are included when the server is in anonymous-auth mode or when the caller has a matching `GroupBinding`. An optional `?namespace=` query parameter filters results to a single namespace.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Filter by namespace |

**Response:**

```json
{
  "items": [
    {
      "namespace": "opendepot-system",
      "name": "platform-depot",
      "modules": ["terraform-aws-vpc", "terraform-aws-s3-bucket"],
      "providers": ["aws"],
      "pollingIntervalMinutes": 30,
      "storageBackend": "s3"
    }
  ]
}
```

### Depot Relationship Graph

```
GET /opendepot/ui/v1/depots/graph
```

Returns a graph of all visible `Depot`, `Module`, and `Provider` resources with directed edges connecting each depot to its managed modules and providers. Used by the [Depots page](#depots-page) in the Registry Explorer UI to render the interactive relationship diagram.

**Visibility:** same rules as [List Depots](#list-depots).

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Filter the graph to a single namespace |

**Response:**

```json
{
  "depots": [
    {
      "id": "opendepot-system/platform-depot",
      "namespace": "opendepot-system",
      "name": "platform-depot",
      "storageBackend": "s3",
      "pollingIntervalMinutes": 30,
      "managedModuleNames": ["terraform-aws-vpc"],
      "managedProviderNames": ["aws"]
    }
  ],
  "modules": [
    {
      "id": "opendepot-system/module/terraform-aws-vpc",
      "namespace": "opendepot-system",
      "name": "terraform-aws-vpc",
      "provider": "aws",
      "synced": true,
      "latestVersion": "3.19.0",
      "depotID": "opendepot-system/platform-depot",
      "scanCounts": { "critical": 0, "high": 1, "medium": 2, "low": 0, "unknown": 0 }
    }
  ],
  "providers": [
    {
      "id": "opendepot-system/provider/aws",
      "namespace": "opendepot-system",
      "name": "aws",
      "synced": true
    }
  ],
  "edges": [
    { "id": "e-depot-mod-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/module/terraform-aws-vpc" },
    { "id": "e-depot-prov-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/provider/aws" }
  ],
  "summary": {
    "totalDepots": 1,
    "totalModules": 1,
    "totalProviders": 1
  },
  "generatedAt": "2026-05-25T12:00:00Z"
}
```

### Registry Stats

```
GET /opendepot/ui/v1/stats
```

Returns aggregate registry statistics as JSON. All counts are scoped to the resources visible to the caller (same visibility rules as the other browse endpoints).

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Scope statistics to a single namespace. Omit for all accessible namespaces. |

**Response:**

```json
{
  "totalModules": 12,
  "totalProviders": 3,
  "totalVersions": 87,
  "totalStorageBytes": 5368709120,
  "totalDownloads": 24601,
  "syncHealth": {
    "syncedVersions": 82,
    "unsyncedVersions": 3,
    "failedVersions": 2
  },
  "securityPosture": {
    "critical": 0,
    "high": 4,
    "medium": 11,
    "low": 7,
    "unknown": 1,
    "totalAffectedResources": 6
  },
  "storageDistribution": [
    { "backend": "s3", "count": 10 },
    { "backend": "filesystem", "count": 5 }
  ],
  "mostDownloaded": [
    {
      "namespace": "opendepot-system",
      "kind": "module",
      "name": "terraform-aws-vpc",
      "version": "3.19.0",
      "downloadCount": 4821,
      "lastDownloadedAt": "2026-05-25T14:32:00Z"
    }
  ]
}
```

`totalStorageBytes` is the sum of `VersionStatus.archiveSizeBytes` across all visible versions; it is `0` when no archive sizes have been recorded. `totalDownloads` and `mostDownloaded` are sourced from the bundled Valkey stats store; both are `0` / empty until at least one download has been recorded. Download counts are cross-referenced against the caller's visibility set — private resource names do not appear in `mostDownloaded` for unauthenticated callers.

## Kubernetes Resource Types

### SecurityFinding

Represents a single vulnerability finding from a Trivy scan.

| Field | Type | Description |
|---|---|---|
| `vulnerabilityID` | `string` | CVE or GHSA identifier for the vulnerability |
| `pkgName` | `string` | Name of the package containing the vulnerability |
| `installedVersion` | `string` | Version of the package currently in use |
| `fixedVersion` | `string` | Minimum version that resolves the vulnerability, if known |
| `severity` | `string` | `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `UNKNOWN` |
| `title` | `string` | Short description of the vulnerability |

### BinaryScan

Holds Trivy binary scan (`trivy rootfs`) results for a specific provider artifact. Stored in `Version.status.binaryScan`. Each OS/architecture binary is scanned independently because Go stdlib versions and runtime dependencies may differ between compiled artifacts.

!!! note
    Trivy requires the execute bit to be set on the binary for gobinary detection. Provider binaries are written with `0500` permissions before scanning; binaries without execute permission are silently skipped by Trivy and `findings` will be empty.

| Field | Type | Description |
|---|---|---|
| `scannedAt` | `string` | RFC3339 timestamp at which the binary scan completed |
| `findings` | `[]SecurityFinding` | Vulnerabilities found in the compiled provider binary |

### SourceScan

Holds Trivy source scan results. Used for both provider `go.mod` dependency scans and module IaC (HCL filesystem) scans. Stored in `Version.status.sourceScan` for every `Version` resource when scanning is enabled.

| Field | Type | Description |
|---|---|---|
| `scannedAt` | `string` | RFC3339 timestamp at which the scan completed |
| `findings` | `[]SecurityFinding` | Findings produced by the scan. For provider `Version` resources these are `go.mod` dependency vulnerabilities (CVE identifiers). For module `Version` resources these are HCL misconfigurations (`vulnerabilityID` contains a Trivy rule ID such as `aws-0057`). |

### ProviderConfig fields

| Field | Type | Description |
|---|---|---|
| `namespace` | `string` | The organisation namespace in the upstream registry (e.g. `hashicorp`, `integrations`, `DataDog`). Defaults to `hashicorp`. Used for binary download and source repository lookup. Existing `Provider` resources without this field continue to work unchanged. |
| `upstreamRegistry` | `string` | The canonical registry used to discover and download this provider. Controls upstream version discovery, archive acquisition, and the canonical provider identity exposed via the Network Mirror Protocol and UI snippets. Allowed values: `registry.opentofu.org`, `registry.terraform.io`. Defaults to `registry.opentofu.org` when omitted. Providers from different origins with the same namespace/type should be placed in separate Kubernetes namespaces to avoid Version resource-name collisions. See [Consuming Providers](../guides/providers.md) for examples. |
| `sourceRepository` | `string` | Full GitHub URL of the provider's source repository (e.g. `https://github.com/hashicorp/terraform-provider-aws`). When omitted for an OpenTofu Registry provider, OpenDepot queries `api.opentofu.org` and falls back to `https://github.com/{namespace}/terraform-provider-{name}` if the lookup fails. Terraform Registry providers use the fallback directly because that registry does not expose equivalent source metadata. Set this field to override an incorrect or unavailable result. |

### ReadmeConfigMapRef

References the ConfigMap and data key holding a module `Version`'s base64 encoded README content. Stored in `Version.status.readmeConfigMapRef`. The ConfigMap is owned by the `Version` resource (via `ownerReferences`), so it is garbage collected automatically when the `Version` is deleted.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of the ConfigMap holding the README content. |
| `key` | `string` | Key within the ConfigMap's `data` holding the base64 encoded README content. |

### VersionStatus fields

| Field | Type | Description |
|---|---|---|
| `binaryScan` | `BinaryScan` | Binary vulnerability scan result for this specific provider artifact. Populated only for provider `Version` resources when scanning is enabled. |
| `sourceScan` | `SourceScan` | Scan result for this version. Contains IaC misconfigurations for module `Version` resources and `go.mod` dependency vulnerabilities for provider `Version` resources. Populated when scanning is enabled. |
| `archiveSizeBytes` | `int64` | Compressed archive size in bytes stored in the backend. Set automatically by the version controller after a successful upload. Omitted until the archive has been synced. |
| `readmeConfigMapRef` | `ReadmeConfigMapRef` | Reference to the ConfigMap holding this version's base64 encoded README content. Only populated for module `Version` resources when a README could be resolved from GitHub or the module archive. See [Module READMEs](../guides/operations.md#module-readmes). |

### ProviderStatus fields

| Field | Type | Description |
|---|---|---|
| `resolvedSourceRepository` | `string` | The VCS source URL resolved by the Version controller. Set automatically on first scan; `spec.providerConfig.sourceRepository` takes precedence if set. See [Provider Source Repository Resolution](../configuration/scanning.md#provider-source-repository-resolution). |

### GroupBinding

`GroupBinding` is a namespaced resource that grants a group of OIDC users access to specific modules and providers. The server evaluates all GroupBindings in alphabetical order by name and applies the first one whose `expression` matches the user's groups claim. If an expression fails to compile or evaluate, the request is denied with `403 Forbidden`. Requires OIDC authentication to be enabled.

See the [GroupBinding guide](../guides/groupbinding.md) for usage examples.

**GroupBindingSpec fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `expression` | `string` | Yes | An [expr-lang](https://expr-lang.org/) boolean expression evaluated against the user's groups. The evaluation environment exposes `groups []string`. Must return `true` or `false`. Example: `'"platform-team" in groups'` |
| `moduleResources` | `[]string` | No | Glob patterns (`path.Match` semantics) for module names the group may access. Empty list denies access to all modules. Example: `["aws-*", "gcp-networking"]` |
| `providerResources` | `[]string` | No | Exact provider type names the group may access, or `["*"]` to allow all providers. Empty list denies access to all providers. Example: `["aws", "google"]` |

**Example manifest:**

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: platform-team-binding
  namespace: opendepot-system
spec:
  expression: '"platform-team" in groups'
  moduleResources:
    - "aws-*"
    - "gcp-networking"
  providerResources:
    - "aws"
    - "google"
```

### BrowseStats

Returned by `GET /opendepot/ui/v1/stats`.

| Field | Type | Description |
|---|---|---|
| `totalModules` | `int` | Number of visible `Module` resources |
| `totalProviders` | `int` | Number of visible `Provider` resources |
| `totalVersions` | `int` | Total number of `Version` resources across all visible modules and providers |
| `totalStorageBytes` | `int64` | Sum of `VersionStatus.archiveSizeBytes` across all visible versions; `0` when no archive sizes have been recorded |
| `totalDownloads` | `int64` | Cumulative download events recorded in Valkey; `0` until at least one download has been recorded |
| `syncHealth` | `SyncHealthStats` | Breakdown of version sync states |
| `securityPosture` | `SecurityPostureStats` | Aggregate finding counts across all visible resources |
| `storageDistribution` | `[]StorageBackendStat` | Per-backend version counts |
| `mostDownloaded` | `[]PopularResource` | Top 10 most-downloaded resources, filtered to the caller's visibility set |

### SyncHealthStats

| Field | Type | Description |
|---|---|---|
| `syncedVersions` | `int` | Versions with `status.synced: true` |
| `unsyncedVersions` | `int` | Versions not yet synced |
| `failedVersions` | `int` | Versions where sync has failed or errored |

### SecurityPostureStats

| Field | Type | Description |
|---|---|---|
| `critical` | `int` | Findings at CRITICAL severity |
| `high` | `int` | Findings at HIGH severity |
| `medium` | `int` | Findings at MEDIUM severity |
| `low` | `int` | Findings at LOW severity |
| `unknown` | `int` | Findings at UNKNOWN severity |
| `totalAffectedResources` | `int` | Number of distinct resources with at least one finding |

### StorageBackendStat

| Field | Type | Description |
|---|---|---|
| `backend` | `string` | Storage backend identifier (e.g. `s3`, `azure`, `gcs`, `filesystem`) |
| `count` | `int` | Number of versions stored on this backend |

### PopularResource

| Field | Type | Description |
|---|---|---|
| `namespace` | `string` | Kubernetes namespace of the resource |
| `kind` | `string` | `module` or `provider` |
| `name` | `string` | Resource name |
| `version` | `string` | Most-downloaded version |
| `downloadCount` | `int64` | Total download events for this resource/version |
| `lastDownloadedAt` | `string` | RFC3339 timestamp of the most recent download; omitted when no downloads recorded |

### PresignConfig fields

Controls pre-signed URL generation for provider downloads. Set on `StorageConfig.presign`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `bool` | `false` | When `true`, download requests are redirected to the storage backend via a pre-signed URL instead of proxied through the server. |
| `ttl` | `duration` | `15m` | How long the pre-signed URL remains valid (e.g. `"15m"`, `"1h"`). |
| `fallbackToProxy` | `bool` | `true` | When `true`, if pre-sign generation fails the server falls back to proxying the download. Set to `false` to make pre-signing strictly required. |

