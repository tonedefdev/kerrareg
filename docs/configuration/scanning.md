---
tags:
  - configuration
  - scanning
  - trivy
  - security
  - providers
  - modules
---

# Vulnerability Scanning

OpenDepot integrates [Trivy](https://trivy.dev/) to scan both provider artifacts and module archives. Scanning is split into two tiers:

- **Module IaC scanning** — enabled by `scanning.enabled: true`. Detects HCL misconfigurations in module archives using Trivy's bundled config rules. Requires no external infrastructure. Setting this flag automatically switches the version-controller to the `-scanning` image variant, which bundles the Trivy binary.
- **Provider scanning** — enabled by `scanning.providerScanning: true` (requires `scanning.enabled: true`). Scans provider binaries and source dependencies against the Trivy vulnerability database. Requires a shared PersistentVolumeClaim and a CronJob to keep the database current.

## Provider Scanning

For each provider version, the Version controller performs two scans:

- **Binary scan** — runs `trivy rootfs` against the compiled provider binary extracted from the HashiCorp release archive. Results are stored per `Version` resource in `Version.status.binaryScan` because each OS/architecture binary may embed different Go standard library versions or runtime dependencies. The binary is written with `0500` (execute) permissions before scanning; Trivy requires the execute bit to be set for gobinary detection — binaries without execute permission are silently skipped.
- **Source scan** — fetches `go.mod` from the provider's GitHub repository and runs `trivy fs` to find vulnerable source dependencies. Results are stored on each `Version` resource in `Version.status.sourceScan`, deduplicated across OS/architecture variants of the same provider version since all variants share the same source code. When a provider repository has no `go.mod`, the scan completes with `findings: []` (an empty slice, not absent) — this is a tombstone indicating the version was scanned and nothing was found, as opposed to not yet scanned.

!!! note
    `status.binaryScan` will be empty for any `Version` that was synced before scanning was enabled. The controller does not automatically re-scan on restart to avoid re-downloading potentially hundreds of large provider binaries. To trigger a one-time re-download and re-scan, set `forceSync: true` on the `Version` resource:

    ```bash
    kubectl patch version aws-5-80-0-linux-amd64 -n opendepot-system \
      --type merge -p '{"spec":{"forceSync":true}}'
    ```

    The controller resets `forceSync` to `false` after reconciliation completes. See [Force re-sync a specific provider version](../guides/providers.md#consuming-providers) for the full `Version` name pattern.

See [Vulnerability Scanning Runbooks](../guides/operations.md#vulnerability-scanning-runbooks) for examples of reading scan results.

## Module IaC Scanning

For each module version, the Version controller runs an IaC scan on the extracted module archive:

- **IaC scan** — extracts the module archive (`.zip` or `.tar.gz`) to a temporary directory and runs `trivy fs` targeting Trivy's config-class checks. This detects HCL misconfigurations — for example, S3 buckets with public ACLs, security groups open to `0.0.0.0/0`, or unencrypted storage resources.

Findings are stored in `Version.status.sourceScan` and use the same `SecurityFinding` struct as provider scans. The `vulnerabilityID` field contains a Trivy rule ID (e.g. `AVD-AWS-0057`) rather than a CVE identifier.

See [Vulnerability Scanning Runbooks](../guides/operations.md#vulnerability-scanning-runbooks) for examples of reading scan results.

## Scanning Image Variant

When `scanning.enabled: true`, the Helm chart automatically selects the version-controller image tagged with the `-scanning` suffix (e.g. `0.2.7-scanning`). This variant is built from the same Dockerfile as the standard image using the `INCLUDE_TRIVY=true` build argument, which copies the Trivy binary into the final image. The standard image (`0.2.7`) does not include Trivy, keeping it lean for installations that do not require any scanning.

## Prerequisites

Provider scanning requires a shared PersistentVolumeClaim for the Trivy vulnerability database and a CronJob that keeps it up to date. Both are created automatically when `scanning.providerScanning` is set to `true`.

The PVC must use a `StorageClass` that supports `ReadWriteMany` access so that the `trivy-db-updater` CronJob and the version-controller pod can mount it simultaneously. For single-node environments such as Kind, `ReadWriteOnce` with the default storage class is sufficient.

!!! note
    The Trivy DB (PVC + CronJob) is only required for provider binary and source scans. Module IaC scanning uses config rules bundled in the Trivy binary and works without any DB infrastructure — just set `scanning.enabled: true`.

## Enabling Module IaC Scanning

To enable lightweight module scanning with no additional infrastructure:

```yaml
scanning:
  enabled: true
  blockOnCritical: false
  blockOnHigh: false
```

Apply via Helm upgrade:

```bash
helm upgrade opendepot opendepot/opendepot \
  --namespace opendepot-system \
  --set scanning.enabled=true
```

## Enabling Provider Scanning

To enable full provider binary and source scanning alongside module IaC scanning:

```yaml
scanning:
  enabled: true
  providerScanning: true
  blockOnCritical: false
  blockOnHigh: false
  cache:
    storageClassName: "efs-sc"   # must support ReadWriteMany in multi-node clusters
    accessMode: ReadWriteMany
    size: 1Gi
```

Apply via Helm upgrade:

```bash
helm upgrade opendepot opendepot/opendepot \
  --namespace opendepot-system \
  --set scanning.enabled=true \
  --set scanning.providerScanning=true \
  --set scanning.cache.storageClassName=efs-sc
```

## Policy Enforcement

When `blockOnCritical` or `blockOnHigh` is set to `true`, the Version controller will stop reconciliation for any provider or module version that has findings at or above the configured severity threshold. The `Version` resource will remain in an unsynced state with a descriptive `syncStatus` message until the vulnerability is resolved or the policy flag is relaxed.

```yaml
scanning:
  enabled: true
  blockOnCritical: true
  blockOnHigh: false
```

!!! warning
    Policy enforcement halts reconciliation for the affected `Version` resource only. Other versions that do not have findings at the blocked severity level will continue to reconcile normally.

## Offline Mode

By default, provider scanning runs in offline mode (`scanning.offline: true`). Trivy reads the vulnerability database from the shared PVC populated by the `trivy-db-updater` CronJob and makes no outbound network calls during a scan. This is the recommended configuration for production clusters.

Set `scanning.offline: false` only if you want Trivy to download the database directly at scan time instead of relying on the CronJob. This requires the version-controller pod to have egress access to `ghcr.io`.

!!! note
    `scanning.offline` only applies to provider scanning. Module IaC scanning uses bundled config rules and makes no network calls regardless of this setting.

## Trivy DB Updater CronJob

The `trivy-db-updater` CronJob runs `trivy image --download-db-only` on a schedule to refresh the local vulnerability database cache. It is only created when `scanning.providerScanning: true`. By default it runs daily at 02:00 UTC.

```yaml
scanning:
  dbUpdater:
    schedule: "0 2 * * *"
    image:
      repository: aquasec/trivy
      tag: "0.70.0"
```

## Provider Source Repository Resolution

When performing a source scan, the Version controller resolves the provider's GitHub repository using the following chain:

1. **Explicit override** — if `spec.providerConfig.sourceRepository` is set, it is used directly and no lookup is performed.
2. **OpenTofu registry lookup** — for providers with `upstreamRegistry: registry.opentofu.org`, queries `api.opentofu.org` for the registered VCS URL.
3. **Heuristic fallback** — for Terraform Registry providers, or when the OpenTofu lookup fails, constructs `https://github.com/{namespace}/terraform-provider-{name}` from the configured namespace and name.

If all three steps fail to produce a usable URL, the Version controller logs a warning and skips the source scan. The binary scan still runs.

The resolved URL is written to `Provider.status.resolvedSourceRepository` after the first successful scan. Once set, subsequent reconciliations do not overwrite it. To inspect the effective repository URL in use:

```bash
kubectl get provider <name> -n <namespace> \
  -o jsonpath='{.status.resolvedSourceRepository}'
```

**`namespace` field**

The `namespace` field on `ProviderConfig` controls which organisation is used in the OpenTofu lookup and the heuristic fallback. It defaults to `hashicorp`.

Set `namespace` when using a provider that is not published under the `hashicorp` organization:

```yaml
spec:
  providerConfig:
    name: github
    namespace: integrations
```

**`sourceRepository` override**

Use `sourceRepository` to pin a specific GitHub URL when registry metadata is unavailable or the heuristic does not match the provider repository:

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    sourceRepository: "https://github.com/my-org/terraform-provider-myprovider"
```

## Authenticated Source Scanning

By default, the Version controller uses an **unauthenticated** GitHub client when fetching a provider's `go.mod` for source scanning. This is sufficient for public provider repositories.

For **private source repositories** or to avoid API rate limits in high-volume environments, set `githubClientConfig.useAuthenticatedClient: true` on the `ProviderConfig`:

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    githubClientConfig:
      useAuthenticatedClient: true
```

!!! note
    If the `opendepot-github-application-secret` Secret is missing from the `Version` resource's namespace or the authenticated client cannot be initialised, the controller falls back to an unauthenticated client automatically. Source scanning continues; no manual intervention is required.

See [GitHub Authentication](github-auth.md) for instructions on creating the `opendepot-github-application-secret` Secret.

See [Helm Chart — Scanning Values](../helm-chart.md#scanning-values) for the full `scanning.*` Helm values reference.
