---
tags:
  - guides
  - ui
  - registry-explorer
  - browse
---

# Registry Explorer UI

The Registry Explorer is a browsable, searchable frontend for OpenDepot. Enable it by setting `ui.enabled: true` in the Helm chart.

!!! note
    The UI is disabled by default. Existing server-only deployments are unaffected when `ui.enabled` remains `false`.

## Routing Architecture

When `ui.enabled: true`, OpenDepot deploys a Next.js frontend alongside the server. NGINX runs inside the UI pod and splits traffic between the registry protocol server and the browser UI:

| Path prefix | Destination |
|-------------|-------------|
| `/opendepot/*` | Server — registry protocol and browse API |
| `/.well-known/*` | Server — service discovery |
| `/` (all other paths) | Next.js UI at port 3000 |

When `ui.ingress.enabled: true`, the Kubernetes Ingress applies the same split-path rules at the load balancer. The `/opendepot` and `/.well-known` paths route directly to the `server` Service; all other paths go to the `ui` Service. NGINX re-proxies browser-originated same-origin API calls from the UI back to the server.

When `server.ingress.istio.enabled: true` and `ui.enabled: true`, the Istio `VirtualService` template automatically adds the same split HTTP match rules — `/opendepot` and `/.well-known` route to the `server` Service; the catch-all default routes to the `ui` Service. No additional Istio configuration is required.

!!! warning
    When `ui.enabled: true`, the `server-ingress.yaml` template is automatically disabled. If you were previously routing directly through the server Ingress, migrate to `ui.ingress` before enabling the UI.

## Local Quick Start (Kind)

The fastest way to try the UI is with a local [Kind](https://kind.sigs.k8s.io/) cluster using the anonymous-auth mode — no OIDC configuration required.

**Prerequisites**: Docker, Kind, kubectl, Helm, and `make` installed.

```bash
# Build all container images, deploy the UI in anonymous-auth mode,
# and start the port-forward in one step.
make ui-setup
```

Once complete, open **http://opendepot.localtest.me:8080** in your browser.
`opendepot.localtest.me` resolves to `127.0.0.1` via public DNS — no `/etc/hosts` editing needed.

In anonymous-auth mode every visitor sees all resources. This is fine for local exploration but should never be used in production.

### Testing with OIDC login

To test the full OIDC login flow locally (user accounts, GroupBinding visibility rules), run:

```bash
# Build all images, deploy Dex + server OIDC + UI OIDC + scanning,
# and start the port-forward.
make ui-setup-oidc PASS=yourpassword
```

This single target runs the full end-to-end setup: it builds and loads all service images (including the scanning-enabled version-controller variant), auto-creates a throwaway GPG signing secret for the provider shasums endpoint (`make ui-gpg-secret`), deploys the complete Helm release with Dex, OIDC (using [`server.oidc.dexProxy.enabled`](../configuration/oidc.md#recommended-proxy-dex-through-the-server) so Dex is reverse-proxied through the server/UI rather than needing its own port-forward), provider controller, and Trivy scanning, starts the single UI port-forward, and writes `~/.tofurc` (`make ui-tofurc`) so `tofu login opendepot.localtest.me:8080` works immediately. No `mkcert` or TLS certificate is required for local testing.

This registers two Dex static clients:

| Client | Purpose | Redirect URI |
|--------|---------|--------------|
| `opendepot-ui` | Browser PKCE login | `http://opendepot.localtest.me:8080/auth/callback` |
| `opendepot` | `tofu login` CLI flow | `http://localhost:1000{0-10}/login` |

The default test user is `dev@example.com` / `PASS` and belongs to the group `local-test-group`. Override defaults via Makefile variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `OIDC_EMAIL` | `dev@example.com` | Test user email |
| `OIDC_USER` | `devuser` | Test user display name |
| `OIDC_GROUP` | `local-test-group` | Group name for GroupBinding tests |
| `UI_PORT` | `8080` | Host port for the UI port-forward |

Stop the port-forward when done:

```bash
make ui-stop
```

#### `make ui-tofurc` — configure `tofu login`

`make ui-tofurc` writes `~/.tofurc` with a `host` block that maps `opendepot.localtest.me:8080` to the HTTP endpoints exposed by the single UI port-forward. This is called automatically by `make ui-setup-oidc`, but you can run it manually at any time to regenerate the file — for example, if `~/.tofurc` was overwritten or if you changed `UI_PORT`.

The generated block looks like this:

```hcl
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
```

The `authz`/`token` URLs point at Dex through the single UI port-forward ([`server.oidc.dexProxy.enabled`](../configuration/oidc.md#recommended-proxy-dex-through-the-server)) rather than a separate Dex port-forward. Because the port-forward exposes the registry at `opendepot.localtest.me:8080` (hostname with port), OpenTofu requires both the `modules.v1`/`providers.v1` service overrides **and** the `login.v1` block in the same `host` key — a bare `host "opendepot.localtest.me"` block without the port is insufficient for authentication.

#### `make ui-gpg-secret` — provider GPG signing key

`make ui-gpg-secret` generates a throwaway RSA 2048 GPG keypair and creates the `opendepot-provider-gpg` Kubernetes Secret in `opendepot-system`. The secret is referenced by `server.gpg.secretName` in the Helm values and enables the provider shasums endpoint to sign `SHA256SUMS` files. Without this secret, `tofu init` for a provider returns `501` with `signing metadata not configured`.

This target is called automatically by `make ui-deploy` (and therefore by `make ui-setup-oidc`). It is idempotent — if the secret already exists, it prints a message and exits without overwriting it.

!!! note "Why `~/.tofurc` is still needed"
    This local registry is served over plain HTTP (no mkcert TLS), but OpenTofu's service discovery protocol only works over HTTPS. The `host` block in `~/.tofurc` bypasses discovery entirely by defining `modules.v1`/`providers.v1`/`login.v1` explicitly, which is why `login.v1` must be spelled out even though [`server.oidc.dexProxy.enabled`](../configuration/oidc.md#recommended-proxy-dex-through-the-server) already makes Dex reachable through the single UI port-forward.

## Enabling the UI

### 1. Create the session secret

The UI uses encrypted server-side session cookies. Provide a secret of at least 32 characters before deploying:

```bash
kubectl create secret generic ui-session-secret \
  --from-literal=sessionPassword=$(openssl rand -base64 32) \
  -n opendepot-system
```

### 2. (Optional) Register an OIDC client

When `ui.oidc.enabled: true`, the UI uses the OIDC authorization code flow to authenticate users. The groups claim from the resulting token is matched against [`GroupBinding`](groupbinding.md) resources to determine which resources the user can browse beyond the publicly-labelled set. Skip this step if you only need anonymous browsing.

Add a `staticClient` to your Dex configuration. The UI requires the `authorization_code` grant type and a registered callback URI:

```yaml
dex:
  config:
    staticClients:
      - id: opendepot-ui
        name: OpenDepot UI
        secretEnv: OPENDEPOT_UI_CLIENT_SECRET  # (1)!
        redirectURIs:
          - https://opendepot.example.com/auth/callback
        responseTypes:
          - code
        grantTypes:
          - authorization_code
```

1. Reference the client secret from an environment variable. Expose it via `dex.envFrom` pointing to a Kubernetes Secret — never set it as a plain string literal.

Create the client secret:

```bash
kubectl create secret generic ui-oidc-secret \
  --from-literal=clientSecret=<your-client-secret> \
  -n opendepot-system
```

### 3. Set Helm values

At minimum:

```yaml
ui:
  enabled: true
  sessionPasswordSecretName: ui-session-secret
  ingress:
    enabled: true
    className: nginx
    annotations:
      nginx.ingress.kubernetes.io/ssl-redirect: "true"
    hosts:
      - host: opendepot.example.com
        paths:
          - path: /
            pathType: Prefix
    tls:
      - secretName: opendepot-tls
        hosts:
          - opendepot.example.com
```

To enable OIDC login (Step 2 above), add:

```yaml
ui:
  oidc:
    enabled: true
    issuerUrl: https://dex.example.com/dex
    clientId: opendepot-ui
    clientSecretName: ui-oidc-secret
    scopes: "openid profile email groups"
    callbackPath: /auth/callback
```

The `groups` scope (or whichever scope emits the groups claim in your IdP) must be included for `GroupBinding` evaluation to function.

!!! danger "Developer token input — never enable in production"
    `ui.auth.devTokenInput.enabled: true` renders a token input field in the Sidebar that accepts any arbitrary bearer token, useful for testing the UI against a live cluster without configuring a full OIDC provider. The default is `false`. Do not change it outside of non-production environments.

See [Helm Chart — UI Configuration](../helm-chart.md#ui-configuration) for the full `ui.*` Helm values reference, including all `ui.ingress.*` and `ui.oidc.*` fields.

### 4. Apply the chart

```bash
helm upgrade opendepot opendepot/opendepot \
  --namespace opendepot-system \
  -f values.yaml
```

## Public Visibility Labels

By default, the browse endpoints return only resources that have been explicitly marked as public. Two labels control visibility:

| Label | Applied to | Effect |
|-------|-----------|--------|
| `opendepot.defdev.io/public: "true"` | Kubernetes Namespace | Marks the namespace as publicly discoverable |
| `opendepot.defdev.io/public: "true"` | `Module` or `Provider` resource | Marks the individual resource as publicly viewable |

**Both** labels must be present for unauthenticated callers to see a resource. Labelling only the namespace or only the resource is not sufficient.

### Label a namespace

```bash
kubectl label namespace opendepot-system opendepot.defdev.io/public=true
```

### Label a Module resource

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: terraform-aws-vpc
  namespace: opendepot-system
  labels:
    opendepot.defdev.io/public: "true"
spec:
  # ...
```

### Label a Provider resource

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: aws
  namespace: opendepot-system
  labels:
    opendepot.defdev.io/public: "true"
spec:
  # ...
```

## Browse Visibility Rules

The browse endpoints apply the following visibility logic based on the caller's authentication state:

| Caller | Resources visible |
|--------|------------------|
| Unauthenticated — OIDC mode (`server.oidc.enabled=true`) + `server.anonymousAuth=false` | **401 Unauthorized** |
| Invalid or expired token — same OIDC mode conditions | **401 Unauthorized** |
| Unauthenticated — non-OIDC mode or `server.anonymousAuth=true` | Public namespace + public resource only |
| OIDC-authenticated, no matching `GroupBinding` | Public namespace + public resource only |
| OIDC-authenticated, matching `GroupBinding` | Public resources ∪ resources allowed by the `GroupBinding` |
| `anonymousAuth: true` mode | All resources (public labels are ignored) |
| Non-OIDC bearer token (SA or kubeconfig) | Public resources only — `GroupBinding` does not apply |

!!! warning "Unauthenticated access is blocked in OIDC mode"
    When `server.oidc.enabled=true` and `server.anonymousAuth=false`, all nine browse endpoints (`/namespaces`, `/resources`, `/resources/{namespace}/{kind}/{name}`, `/resources/{namespace}/{kind}/{name}/versions`, `/resources/{namespace}/{kind}/{name}/scan-findings`, `/depots`, `/depots/graph`, and `/stats`) require a valid `Authorization: Bearer <token>` header. Requests without a valid token receive `401 Unauthorized`. The Registry Explorer, Depots graph, and Stats pages automatically redirect to `/auth/login` when the API returns 401.

!!! note "Namespace list is always label-filtered"
    The `GET /opendepot/ui/v1/namespaces` endpoint enforces the `opendepot.defdev.io/public=true` label filter at the Kubernetes API level regardless of auth mode. The rows above describe module and provider resource visibility only. `anonymousAuth: true` mode and `GroupBinding` bypass have no effect on namespace listing — namespaces without the label, including `kube-system`, `default`, and `kube-public`, are never returned.

## GroupBinding for Browse Access

`GroupBinding` resources grant OIDC-authenticated users access to resources beyond the public set. The same `GroupBinding` CRD used for registry protocol access also controls browse visibility.

A `GroupBinding` is evaluated using an [expr-lang](https://expr-lang.org/) expression against the OIDC groups claim. The first matching binding (alphabetically by name) defines the resources the user may see.

### Example

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: platform-team-binding
  namespace: opendepot-system
spec:
  expression: '"platform-team" in groups'
  moduleResources:
    - "terraform-aws-*"   # all modules matching this glob
    - "terraform-gcp-vpc"
  providerResources:
    - "aws"
    - "google"
```

To grant a group access to all modules and providers:

```yaml
spec:
  expression: '"my-org-users" in groups'
  moduleResources:
    - "*"
  providerResources:
    - "*"
```

See [GroupBinding Access Control](groupbinding.md) for full expression syntax, client credentials support, and first-match semantics.

## HCL Usage Snippets

Every module and provider detail page (`/<namespace>/<kind>/<name>`) shows a **Usage** card with ready-to-paste HCL blocks and copy-to-clipboard buttons.

For a **provider**, the card shows CLI configuration snippets for both OpenTofu (`.tofurc`) and Terraform (`.terraformrc`) with the `provider_installation.network_mirror` block:

```hcl
provider_installation {
  network_mirror {
    url     = "<mirrorUrl>"
    include = ["<canonicalSource>"]
  }

  direct {
    exclude = ["<canonicalSource>"]
  }
}
```

The `<canonicalSource>` placeholder is populated with the canonical upstream identity (`registry.opentofu.org/*/*` or `registry.terraform.io/*/*`) based on the Provider resource's `spec.providerConfig.upstreamRegistry` field.

The card also shows a **Provider requirement** block with the canonical source identity:

```hcl
terraform {
  required_providers {
    <name> = {
      source  = "<providerNamespace>/<name>"
      version = "<latestVersion>"
    }
  }
}
```

The canonical source (e.g., `hashicorp/aws`) is preserved in your configuration and lockfile, while the mirror URL tells the CLI to install from OpenDepot. See [Consuming Providers](providers.md) for full details on the Network Mirror workflow.

For a **module**, the card shows a single `module` block:

```hcl
module "<name>" {
  source  = "<registryHost>/<namespace>/<name>/<provider>"
  version = "<latestVersion>"
}
```

The registry host is derived automatically from `NEXT_PUBLIC_BASE_URL` (set by the Helm chart via `ui.baseUrl`). If the latest version is not yet synced, the `version` attribute is omitted from the snippet. No new Helm chart values or environment variables are required.

## Module READMEs

When a module's README could be resolved for its latest version, the resource detail page renders a **README** card above the Overview section, formatted as markdown with GitHub-flavored table, task list, and strikethrough support. The card is omitted entirely when no README is available for the resource.

The markdown is rendered client-side with `react-markdown` and `remark-gfm` — raw HTML embedded in a README is never executed, only standard markdown syntax is rendered.

See [Module READMEs](operations.md#module-readmes) for how OpenDepot resolves and stores README content.

## Version Sync Warnings

Resource cards in the browse grid and the resource detail page header show an amber warning icon when any `Version` CR under a `Module` or `Provider` has a sync problem, even if the parent resource itself is marked as synced. Hovering the icon displays the tooltip **"Some versions are out of sync"**.

A version is considered out of sync when either of the following is true:

- `status.synced: false`
- `status.syncStatus` contains `"failed"` or `"error"` (case-insensitive)

This indicator is independent of the per-resource sync status chip. A resource can show a green sync status alongside the version warning — indicating the resource is healthy but one or more of its version artifacts failed to download or process.

The server computes this flag for grid cards and exposes it as `hasUnsyncedVersions` in the [List Resources](#list-resources) response. The detail page derives the same flag client-side from the version list already present in the [Resource Detail](#resource-detail) response.

## Versions Table

The versions table on module and provider detail pages supports server-side filtering and pagination via the [List Resource Versions](#list-resource-versions) endpoint.

### Filter bar

| Control | Applicable to | Description |
|---------|--------------|-------------|
| Version search | All | Case-insensitive substring match on the version string |
| Sync status | All | Filter to **All**, **Synced**, or **Failed** versions |
| OS | Providers only | Filter by operating system; options populated from the versions in the current resource |
| Arch | Providers only | Filter by CPU architecture; options populated from the versions in the current resource |

A **Clear** button appears when any filter is active and resets all filters at once.

### Pagination

A **Rows per page** selector (10 / 20 / 50 / 100) and page navigation controls appear at the bottom of the table. Changing any filter resets the table to page 1.

While the table is loading, skeleton rows are displayed. When no versions match the active filters, the table shows **"No versions match the current filters."**

## Scan Findings

The **Scan Findings** section on module and provider detail pages lists vulnerability findings for a selected version. Source findings (IaC misconfigurations and dependency vulnerabilities) are shown for both modules and providers. Binary findings are shown per platform (`os/arch`) for providers.

When scan results are available for more than one version, a **version selector dropdown** appears in the scan findings header. Selecting a different version re-fetches findings for that version from the server. The dropdown lists all versions with recorded scans, sorted newest-first.

A **↻ refresh button** appears next to the section heading. Clicking it re-fetches findings for the currently selected version via the [Resource Scan Findings](../reference/api.md#resource-scan-findings) endpoint without a full page reload. The button spins briefly while the request is in flight and reverts to idle when the updated findings are displayed. If the request fails, the previously loaded findings are preserved.

!!! note
    Scan counts shown on resource cards (browse grid, Depots graph) are derived from the latest version only, regardless of the version selected in the drill-down panel.

## Depots Page

The **Depots** page (`/depots`) renders an interactive relationship graph of all visible `Depot` resources and the `Module` and `Provider` resources each depot manages. It is accessible from the **Depots** entry in the sidebar navigation.

```mermaid
graph LR
  D[Depot] -->|manages| M1[Module A]
  D -->|manages| M2[Module B]
  D -->|manages| P[Provider]
```

Each node in the graph is clickable and opens a detail panel showing:

| Node type | Details shown |
|-----------|---------------|
| Depot | Storage backend, polling interval, managed resource counts |
| Module | Latest version, sync status, scan severity counts, link to detail page |
| Provider | Sync status, provider namespace |

### Namespace filtering

A namespace selector at the top of the page narrows the graph to a single Kubernetes namespace. This is useful in large deployments with many depots spread across multiple namespaces.

### Visibility

The Depots page applies the same visibility rules as the rest of the Registry Explorer. Unauthenticated visitors see only depots and resources in namespaces labelled `opendepot.defdev.io/public=true`. OIDC-authenticated users with a matching `GroupBinding` additionally see the resources allowed by that binding. In anonymous-auth mode all depot resources are visible. Regardless of auth mode, the namespace selector on this page only lists namespaces carrying the `opendepot.defdev.io/public=true` label.

The graph is powered by the [`GET /opendepot/ui/v1/depots/graph`](../reference/api.md#depot-relationship-graph) browse endpoint.

## Stats Dashboard

The **Stats** page (`/stats`) provides an at-a-glance overview of registry health and usage. It is accessible from the **Stats** entry in the sidebar (bar chart icon).

The dashboard shows:

| Section | Description |
|---------|-------------|
| Summary cards | Total modules, providers, versions, storage used, and cumulative downloads |
| Sync health | Progress bar showing the ratio of synced, unsynced, and failed versions |
| Security posture | Severity chips (CRITICAL / HIGH / MEDIUM / LOW / UNKNOWN) with finding counts |
| Storage distribution | Per-backend version counts |
| Most downloaded | Table of the top 10 most-downloaded resources, with version, namespace, download count, and last-downloaded timestamp |

Summary counts (modules, providers, versions, sync health, security posture, storage distribution) are computed on-demand from Kubernetes CRDs and are always current. Download counts and the most-downloaded table are sourced from the bundled Valkey stats store and are always active.

!!! note "Visibility"
    The stats page applies the same visibility rules as the rest of the Registry Explorer. Unauthenticated visitors see statistics derived only from publicly-labelled resources. OIDC-authenticated users with a matching `GroupBinding` additionally see statistics for the resources allowed by that binding.

## Download Tracking

Download events are recorded automatically in the bundled Valkey instance that is deployed alongside the server. Before installation, create the required ACL Secret:

```bash
kubectl create secret generic opendepot-valkey-auth \
  --from-literal=default="$(openssl rand -base64 32)" \
  --namespace opendepot-system
```

By default, Valkey persists data to a PVC so stats survive pod restarts. For local development or Kind clusters without a StorageClass, disable persistence in your Helm values:

```yaml
valkey:
  dataStorage:
    enabled: false  # ephemeral storage; stats lost on restart
```

For production, leave `valkey.dataStorage.enabled: true` (the default). See the [Valkey Stats Store](../helm-chart.md#valkey-stats-store) Helm values reference for the full set of options.

Existing server-only deployments require no changes until you set `ui.enabled: true`. When you are ready to enable the UI:

1. Create the session secret (see [Enabling the UI](#1-create-the-session-secret)).
2. Set `ui.enabled: true` and `ui.sessionPasswordSecretName`.
3. Move your Ingress configuration from `server.ingress` to `ui.ingress` — the server Ingress is suppressed automatically when the UI is enabled.
4. Optionally configure `ui.oidc.*` for authenticated browse.
5. Label namespaces and resources with `opendepot.defdev.io/public=true` for anonymous browse access.

## Browse API

The browse endpoints are used by the Registry Explorer UI and can also be called directly (for integrations or automation). When `server.oidc.enabled=true` and `server.anonymousAuth=false`, all endpoints require a valid `Authorization: Bearer <token>` header — requests without one return `401 Unauthorized`. In non-OIDC mode or when `server.anonymousAuth=true`, endpoints are accessible without authentication; an optional `Authorization: Bearer <token>` header extends visibility per the [rules above](#browse-visibility-rules).

### List Namespaces

```
GET /opendepot/ui/v1/namespaces
```

Returns only namespaces that carry the `opendepot.defdev.io/public=true` label. The label selector is applied at the Kubernetes API level, so system namespaces (`kube-system`, `default`, `kube-public`) are never returned regardless of the caller's auth state. The `public` field is always `true` in the response because unlabeled namespaces are excluded before the response is built.

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
| `kind` | string | Filter by resource kind: `module` or `provider` |
| `q` | string | Search string matched against resource name |
| `synced` | bool | Filter to only synced (`true`) or unsynced (`false`) resources |
| `os` | string | Filter providers by operating system (e.g., `linux`) |
| `arch` | string | Filter providers by CPU architecture (e.g., `amd64`) |
| `severity` | string | Filter to resources with scan findings at or above this severity (`CRITICAL`, `HIGH`, `MEDIUM`, `LOW`) |
| `public_only` | bool | When `true`, return only resources with the public label |
| `sort_by` | string | Sort field (e.g., `name`, `latestVersion`) |
| `sort_dir` | string | Sort direction: `asc` or `desc` |
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
      "totalDownloads": 4821,
      "lastDownloadedAt": "2026-05-25T14:32:00Z"
    }
  ],
  "totalCount": 1,
  "page": 1,
  "pageSize": 20
}
```

`scanCounts` reflects vulnerability findings from the **latest version only**. The field is omitted when no version has been scanned.

### Resource Detail

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}
```

Returns full detail for a single resource, including all versions and scan findings. The `sourceScanFindings` and `binaryScanFindings` fields contain findings from the **latest version only**.

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
  "readmeContent": "# terraform-aws-vpc\n\nTerraform module..."
}
```

The `sourceScanFindings` and `binaryScanFindings` fields in this response are scoped to the latest version. Use the [Resource Scan Findings](#resource-scan-findings) endpoint to fetch findings for a specific version on demand without reloading the full detail page.

`readmeContent` is only present for `module` resources, and only when a README could be resolved for the latest version — see [Module READMEs](operations.md#module-readmes). The Registry Explorer renders it as formatted markdown on the resource detail page.

### List Resource Versions

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions
```

Returns a paginated, filtered list of versions for a single resource. Authentication follows the same rules as the other browse endpoints.

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

### Resource Scan Findings

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings
```

Returns scan findings for a single resource. Defaults to the most recently scanned version; pass `?version=<semver>` to select a specific source scan version, or `?binaryVersion=<semver>` (providers only) to select a specific binary scan version. See the [API Reference](../reference/api.md#resource-scan-findings) for the full response schema.
