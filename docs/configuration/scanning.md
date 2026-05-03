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

OpenDepot integrates [Trivy](https://trivy.dev/) to scan both provider artifacts and module archives. When enabled, the Version controller runs scans automatically during reconciliation and stores findings directly on the Kubernetes resources.

## Provider Scanning

For each provider version, the Version controller performs two scans:

- **Binary scan** — runs `trivy rootfs` against the compiled provider binary extracted from the HashiCorp release archive. Results are stored per `Version` resource in `Version.status.binaryScan` because each OS/architecture binary may embed different Go standard library versions or runtime dependencies.
- **Source scan** — fetches `go.mod` from the provider's GitHub repository and runs `trivy fs` to find vulnerable source dependencies. Results are stored on the `Provider` resource in `Provider.status.sourceScan` and deduplicated across OS/architecture variants since all variants share the same source code.

See [Consuming Providers](../guides/providers.md#vulnerability-scanning) for examples of reading scan results.

## Module IaC Scanning

For each module version, the Version controller runs an IaC scan on the extracted module archive:

- **IaC scan** — extracts the module archive (`.zip` or `.tar.gz`) to a temporary directory and runs `trivy fs` targeting Trivy's config-class checks. This detects HCL misconfigurations — for example, S3 buckets with public ACLs, security groups open to `0.0.0.0/0`, or unencrypted storage resources.

Findings are stored in `Version.status.sourceScan` and use the same `SecurityFinding` struct as provider scans. The `vulnerabilityID` field contains a Trivy rule ID (e.g. `AVD-AWS-0057`) rather than a CVE identifier.

!!! note
    Module IaC scanning does **not** require the Trivy vulnerability database (the CronJob or PVC). Config-class rules are bundled in the Trivy binary itself. You can enable module scanning without setting up the DB infrastructure.

See [Consuming Modules](../guides/modules.md#vulnerability-scanning) for examples of reading scan results.

## Prerequisites

Scanning requires a shared PersistentVolumeClaim for the Trivy vulnerability database and a CronJob that keeps it up to date. Both are created automatically when `scanning.enabled` is set to `true`.

The PVC must use a `StorageClass` that supports `ReadWriteMany` access so that the `trivy-db-updater` CronJob and the version-controller pod can mount it simultaneously. For single-node environments such as Kind, `ReadWriteOnce` with the default storage class is sufficient.

!!! note
    The Trivy DB (PVC + CronJob) is only required for **provider** binary and source scans. Module IaC scanning uses config rules bundled in the Trivy binary and works without a populated DB cache.

## Enabling Scanning

```yaml
scanning:
  enabled: true
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

By default, scanning runs in offline mode (`scanning.offline: true`). Trivy reads the vulnerability database from the shared PVC populated by the `trivy-db-updater` CronJob and makes no outbound network calls during a scan. This is the recommended configuration for production clusters.

Set `scanning.offline: false` only if you want Trivy to download the database directly at scan time instead of relying on the CronJob. This requires the version-controller pod to have egress access to `ghcr.io`.

## Trivy DB Updater CronJob

The `trivy-db-updater` CronJob runs `trivy image --download-db-only` on a schedule to refresh the local vulnerability database cache. By default it runs daily at 02:00 UTC.

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
2. **OpenTofu registry lookup** — queries `api.opentofu.org/registry/docs/providers/{namespace}/{name}` to retrieve the provider's registered VCS URL. This works for any provider published in the OpenTofu registry, regardless of the owning organization.
3. **Heuristic fallback** — constructs `https://github.com/{namespace}/terraform-provider-{name}` from the configured namespace and provider name.

If all three steps fail to produce a usable URL, the Version controller logs a warning and skips the source scan. The binary scan still runs.

**`namespace` field**

The `namespace` field on `ProviderConfig` controls which organisation is used in the registry lookup (step 2) and the heuristic fallback (step 3). It defaults to `hashicorp`, so existing `Provider` resources continue to work without any changes.

Set `namespace` when using a provider that is not published under the `hashicorp` organization:

```yaml
spec:
  providerConfig:
    name: github
    namespace: integrations
```

**`sourceRepository` override**

Use `sourceRepository` to pin a specific GitHub URL when the OpenTofu registry returns the wrong repository or is unreachable:

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

## Helm Values Reference

| Value | Default | Description |
|---|---|---|
| `scanning.enabled` | `false` | Enable Trivy vulnerability scanning for provider artifacts and module IaC |
| `scanning.cacheMountPath` | `/var/cache/trivy` | Mount path inside the version-controller container for the Trivy DB cache |
| `scanning.offline` | `true` | Pass `--offline-scan` to Trivy. Prevents network calls during scans |
| `scanning.blockOnCritical` | `false` | Halt provider reconciliation when CRITICAL findings are present |
| `scanning.blockOnHigh` | `false` | Halt provider reconciliation when HIGH findings are present |
| `scanning.scanModules` | `false` | Enable Trivy IaC scanning for module version archives (requires `scanning.enabled=true`) |
| `scanning.cache.storageClassName` | `""` | StorageClass for the Trivy cache PVC (must support ReadWriteMany for multi-node). Omitted from the PVC manifest when blank, allowing the cluster default to apply stably across upgrades |
| `scanning.cache.accessMode` | `ReadWriteMany` | Access mode for the Trivy cache PVC |
| `scanning.cache.size` | `1Gi` | Size of the Trivy DB cache PVC |
| `scanning.dbUpdater.schedule` | `"0 2 * * *"` | Cron schedule for the Trivy DB update job |
| `scanning.dbUpdater.image.repository` | `aquasec/trivy` | Trivy image repository for the db-updater CronJob |
| `scanning.dbUpdater.image.tag` | `"0.70.0"` | Trivy image tag for the db-updater CronJob |
