---
tags:
  - upgrading
  - releases
  - breaking-changes
---

# Upgrading

Breaking changes and upgrade steps for each OpenDepot release. Check this page before running `helm upgrade` on an existing installation.

!!! tip
    Helm does not update CRDs during `helm upgrade`. Always apply the latest CRDs before upgrading:
    ```bash
    helm show crds opendepot/opendepot | kubectl apply --server-side -f -
    ```

## v0.10.0

v0.10.0 enables Valkey ACL authentication and the bundled Dex reverse proxy by default. All existing installations must create a Valkey password Secret. Existing OIDC installations must also review their Dex configuration.

### Breaking Changes

- **Valkey authentication is required outside development mode.** The chart rejects `valkey.auth.enabled: false` unless `global.developmentMode: true`.
- **A pre-existing Secret is required.** The default configuration expects an `opendepot-valkey-auth` Secret with a `default` key in the OpenDepot namespace. The chart does not create this Secret.
- **The Valkey and server Secret names must match.** If you use a custom Secret name, set both `valkey.auth.usersExistingSecret` and `server.stats.valkeyPasswordSecretName` to that name.

### OIDC Default Change

`server.oidc.dexProxy.enabled` now defaults to `true`. This hardens bundled Dex deployments by proxying `/dex/*` through the existing server or UI ingress, so Dex no longer requires a separate public ingress or hostname.

This change does not affect installations with OIDC disabled. Existing OIDC installations require action in either of these cases:

- **Bundled Dex with an internal or auto-derived issuer:** Set `server.oidc.issuerUrl` and `dex.config.issuer` to the same external, path-based URL served by the existing ingress, such as `https://opendepot.example.com/dex`.
- **External or separately exposed Dex:** Set `server.oidc.dexProxy.enabled: false`. The bundled proxy cannot target an externally managed issuer.

### Provider Upstream Registry Selection

v0.10.0 adds `spec.providerConfig.upstreamRegistry` to `Provider` resources and `spec.providerConfigs[].upstreamRegistry` to `Depot` resources. The field selects the canonical registry used for provider discovery, downloads, Network Mirror identity, and Registry Explorer snippets.

Supported values are:

- `registry.opentofu.org` (default)
- `registry.terraform.io`

Existing resources require no changes. When the field is omitted, OpenDepot continues to use `registry.opentofu.org`, matching the behavior of earlier releases.

Set the field explicitly to mirror a provider from the Terraform Registry:

=== "Provider"

      ```yaml
      apiVersion: opendepot.defdev.io/v1alpha1
      kind: Provider
      metadata:
         name: aws
         namespace: opendepot-system
      spec:
         providerConfig:
            name: aws
            namespace: hashicorp
            upstreamRegistry: registry.terraform.io
      ```

=== "Depot"

      ```yaml
      apiVersion: opendepot.defdev.io/v1alpha1
      kind: Depot
      metadata:
         name: upstream-providers
         namespace: opendepot-system
      spec:
         providerConfigs:
            - name: aws
               namespace: hashicorp
               upstreamRegistry: registry.terraform.io
      ```

The provider retains its canonical source address, such as `registry.terraform.io/hashicorp/aws`. Configure the same hostname in the `network_mirror.include` and `direct.exclude` patterns in `.tofurc` or `.terraformrc`. See [Consuming Providers](guides/providers.md#default-workflow-network-mirror) for complete examples.

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Create the Valkey ACL Secret before running `helm upgrade`:
   ```bash
   kubectl create secret generic opendepot-valkey-auth \
     --namespace opendepot-system \
     --from-literal=default='<strong-random-password>'
   ```
3. If you use a custom Secret name, add matching references to your values file:
   ```yaml
   valkey:
     auth:
       usersExistingSecret: my-valkey-auth

   server:
     stats:
       valkeyPasswordSecretName: my-valkey-auth
   ```
4. If OIDC is enabled, choose the appropriate Dex configuration:

=== "Bundled Dex"

       Set the same external, path-based issuer URL for the server and Dex:

       ```yaml
       dex:
         enabled: true
         config:
           issuer: https://opendepot.example.com/dex

       server:
         oidc:
           enabled: true
           issuerUrl: https://opendepot.example.com/dex
       ```

       Expose `/dex` through the existing server or UI ingress. A separate Dex ingress is no longer required.

=== "External or separately exposed Dex"

      Disable the bundled proxy explicitly:

      ```yaml
      server:
      oidc:
         dexProxy:
            enabled: false
      ```

5. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```
6. Confirm that Valkey and the server are ready:
   ```bash
   kubectl rollout status deployment/valkey -n opendepot-system
   kubectl rollout status deployment/server -n opendepot-system
   ```

!!! warning
    Do not disable Valkey authentication in production. `global.developmentMode: true` permits unauthenticated Valkey only for local development environments.

## v0.9.0

v0.9.0 adds an opt-in reverse proxy so Dex never needs its own public ingress or hostname. See [Proxying Dex Through the Server](configuration/oidc.md#recommended-proxy-dex-through-the-server).

Set `server.oidc.dexProxy.enabled: true` to have the server reverse-proxy `/dex/*` requests to the bundled Dex service. This is fully backward compatible — the flag defaults to `false`, and existing `dex.enabled: true` deployments with a separately exposed Dex continue to work unchanged.

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```
3. (Optional) To adopt the recommended proxy mode, set `dex.config.issuer` and `server.oidc.issuerUrl` to the same external, path-based URL and enable `server.oidc.dexProxy.enabled: true`. See [Proxying Dex Through the Server](configuration/oidc.md#recommended-proxy-dex-through-the-server) for the full walkthrough.

No action is required to keep existing behavior — `dexProxy.enabled` defaults to `false`.

## v0.8.0

v0.8.0 adds automatic README resolution for modules. See [Module READMEs](guides/operations.md#module-readmes) and the [Registry Explorer README rendering](guides/registry-explorer.md#module-readmes).

### New RBAC Permissions

The version-controller ServiceAccount now requires `configmaps` (`create`, `get`, `list`, `patch`, `update`, `watch`) to store resolved READMEs, and the server ServiceAccount now requires `configmaps` (`get`, `list`, `watch`) to serve them through the browse API. Both rules are added automatically by the Helm chart — no values changes are required. See [Kubernetes RBAC](rbac.md#controller-permissions).

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```

No manual action is required for existing `Module` and `Version` resources — the version controller resolves and stores READMEs automatically on each Version's next reconcile, or immediately when `forceSync: true` is set.

### Dependency Updates

The UI's `vitest` dependency was bumped to `^3.2.6`, with `vite` and `undici` pinned via `resolutions`, resolving HIGH/CRITICAL npm advisories. This affects the UI's build and test tooling only — no runtime or Helm values changes are required.

## v0.6.0

v0.6.0 replaces the SQLite download-stats backend with a bundled Valkey instance.

### Breaking Changes

- **`--stats-db-path` is removed.** The server flag no longer exists. Any custom Helm values overrides that reference `server.stats.*` must be removed — the chart will reject unknown values.
- **`server.stats` values block is removed.** Remove `server.stats.emptyDir`, `server.stats.persistence.*`, or any `server.stats` key from your `values.yaml` before upgrading.
- **Stats history is not migrated.** Valkey starts with a clean slate — download counts accumulated in the previous SQLite database are not carried over. Historic data can be discarded or archived manually before upgrading.

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Remove any `server.stats` keys from your `values.yaml`.
3. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```

Valkey is deployed automatically as part of the chart. Download tracking resumes immediately after the server pod becomes ready. For production clusters, `valkey.dataStorage.enabled: true` (the default) ensures stats survive pod restarts — no additional configuration is required.

## v0.5.0

### Breaking Changes

| Change | Affected field | Action required |
|--------|---------------|----------------|
| `Provider.status.sourceScans` removed; per-version provider source scans moved to `Version.status.sourceScan` | `ProviderStatus`, `VersionStatus` | Update any automation or scripts that read `.status.sourceScans` on a `Provider` resource. Provider source scan results are now stored on each `Version` resource in `status.sourceScan`, alongside module IaC scan results. |
| `Provider.status.resolvedSourceRepository` added (read-only, string) | `ProviderStatus` | No action required. The field is populated automatically by the Version controller after the first scan. |

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Update any scripts or automation reading `Provider.status.sourceScans` to read `Version.status.sourceScan` instead.
3. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```
