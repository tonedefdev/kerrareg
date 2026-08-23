---
tags:
  - operations
  - admin
  - guides
---

# Registry Operations

Operational runbooks for managing module and provider lifecycles after initial setup.

## Module Operations

### Adding versions to an existing module

To publish a new version of a module that already exists in OpenDepot, append the version to the `spec.versions` list. Existing versions are preserved - the Module controller only creates `Version` resources for entries it has not seen before.

Using `kubectl patch` (quick):

```bash
kubectl patch module terraform-aws-eks -n opendepot-system \
  --type json -p '[{"op":"add","path":"/spec/versions/-","value":{"version":"21.13.0"}}]'
```

Using `kubectl apply` (declarative):

Include all existing versions alongside the new one. The Module controller is idempotent and does not re-create versions that already exist.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: terraform-aws-eks
  namespace: opendepot-system
spec:
  moduleConfig:
    name: terraform-aws-eks
    provider: aws
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-eks
    fileFormat: zip
    storageConfig:
      s3:
        bucket: opendepot-modules
        region: us-west-2
  versions:
    - version: "21.10.1"
    - version: "21.11.0"
    - version: "21.12.0"
    - version: "21.13.0"
```

GitHub Actions Example (Append on Release):

```yaml
- name: Add version to existing module
  run: |
    VERSION=${{ github.event.release.tag_name }}
    kubectl patch module my-module -n opendepot-system \
      --type json \
      -p "[{\"op\":\"add\",\"path\":\"/spec/versions/-\",\"value\":{\"version\":\"${VERSION}\"}}]"
```

Removing a version: remove the entry from `spec.versions` and re-apply. The Module controller garbage-collects orphaned `Version` resources. If `versionHistoryLimit` is set, older versions are automatically pruned when the limit is exceeded.

### Force re-sync

If a Module or Version fails to sync (for example, due to a transient network error), you can force a re-sync by setting `forceSync: true` on the resource.

```bash
# Force a Module to re-sync all its versions
kubectl patch module terraform-aws-eks -n opendepot-system \
  --type merge -p '{"spec":{"forceSync":true}}'

# Force a single Version to re-sync
kubectl patch version.opendepot.defdev.io terraform-aws-eks-21.18.0 -n opendepot-system \
  --type merge -p '{"spec":{"forceSync":true}}'
```

The controller resets `forceSync` to `false` after reconciliation completes.

### Module READMEs

For `Module`-type `Version` resources, the Version controller automatically resolves the module's README and makes it available to the Registry Explorer. Resolution is best-effort: it tries the GitHub API first (`GetReadme`, using the same authenticated/unauthenticated client selection as the archive download), and falls back to extracting a `README.md` from the already-downloaded module archive if the GitHub lookup fails or is unauthenticated.

The README is base64-encoded and stored in a ConfigMap named `<version-name>-readme`, owned by the `Version` resource. Because it is owned via `ownerReferences`, the ConfigMap is garbage collected automatically when the `Version` CR is deleted — no manual cleanup is required. The reference is recorded on the `Version` resource as `status.readmeConfigMapRef`.

The controller only (re)fetches the README on the version's first sync (`status.readmeConfigMapRef` unset) or when `forceSync: true` is set, to avoid unnecessary GitHub API calls on every reconcile. A README that cannot be resolved by either method is not an error — the `Version` still syncs normally and the Registry Explorer simply omits the README card.

This behavior does not apply to `Provider`-type Version resources.

### Inline module configuration (Version CR)

`moduleConfigRef.name` is optional on a `Version` CR. When `name` is omitted, or when no `Module` CR with that name exists in the namespace, the Version controller treats all fields on `moduleConfigRef` as fully inline. A UUID-based filename is generated automatically so the archive has a stable storage key, and the download proceeds using the GitHub and storage config set directly on the `Version` CR.

This is useful when running the Version controller standalone (without the Module controller), or for one-off version testing where creating a full `Module` CR is unnecessary.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: terraform-aws-s3-bucket-4-3-0
  namespace: opendepot-system
spec:
  type: Module
  version: "4.3.0"
  moduleConfigRef:
    repoOwner: terraform-aws-modules
    repoUrl: "https://github.com/terraform-aws-modules/terraform-aws-s3-bucket"
    githubClientConfig:
      useAuthenticatedClient: false
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
```

!!! note
    When `name` is omitted the `Version` CR is completely self-contained. No `Module` CR needs to exist in the namespace.

## Provider Operations

### Adding a new provider version

To publish a new version, append it to `spec.versions`:

```bash
kubectl patch provider aws -n opendepot-system \
  --type json -p '[{"op":"add","path":"/spec/versions/-","value":{"version":"5.81.0"}}]'
```

The Provider controller creates new `Version` resources for every OS/architecture combination, and the Version controller fetches and stores the binaries automatically.

### Force re-sync

```bash
kubectl patch provider aws -n opendepot-system \
  --type merge -p '{"spec":{"forceSync":true}}'
```

### Force re-sync a specific provider version

`Version` resources also support `forceSync`. Use it to force a re-download and re-scan of a specific OS/architecture binary - for example, when `status.binaryScan` is empty because scanning was enabled after the version was first synced.

```bash
kubectl patch version <version-name> -n <namespace> \
  --type merge -p '{"spec":{"forceSync":true}}'
```

The `<version-name>` follows the pattern `<provider>-<version-dashes>-<os>-<arch>` - for example, `aws-5-80-0-linux-amd64`.

The controller bypasses the provider fast-path, re-downloads the artifact, runs the binary scan, and resets `forceSync` to `false` once reconciliation completes.

!!! note
    The Version controller does **not** automatically re-scan provider binaries on restart. Provider binaries are about 700 MB each; re-downloading every cached binary on startup can exhaust memory and I/O in clusters with many provider versions. Use `forceSync: true` on a specific `Version` resource to trigger a targeted one-time re-download and re-scan.

### Provider source repository

For providers from `registry.opentofu.org`, OpenDepot automatically resolves the provider's GitHub repository for source scanning through `api.opentofu.org`. For providers from `registry.terraform.io`, it constructs `https://github.com/{namespace}/terraform-provider-{name}` because the Terraform Registry API does not expose equivalent source metadata.

For providers not published under the `hashicorp` organization, set the `namespace` field to match the registry namespace:

```yaml
spec:
  providerConfig:
    name: github
    namespace: integrations
```

Use `sourceRepository` to pin a specific URL when registry metadata is unavailable, the heuristic does not match the provider repository, or the lookup returns the wrong repository:

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    sourceRepository: "https://github.com/my-org/terraform-provider-myprovider"
```

### Authenticated source scanning

For private provider source repositories or high-volume environments where unauthenticated GitHub requests hit API rate limits, set `githubClientConfig.useAuthenticatedClient: true` on the `ProviderConfig`. The Version controller uses the same `opendepot-github-application-secret` Secret that modules use, so no additional Secret is required if GitHub App auth is already configured in the namespace.

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    githubClientConfig:
      useAuthenticatedClient: true
```

See [GitHub Authentication](../configuration/github-auth.md) for setup details.

## Vulnerability Scanning Runbooks

When [scanning is enabled](../configuration/scanning.md), the Version controller runs Trivy and stores findings on Kubernetes resources.

Module IaC scan results (`Version.status.sourceScan`):

```bash
kubectl get version terraform-aws-key-pair-2-0-0 -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

Provider binary scan results (`Version.status.binaryScan`):

```bash
kubectl get version aws-5-81-0-linux-amd64 -n opendepot-system -o jsonpath='{.status.binaryScan}' | jq .
```

Provider source scan results (`Version.status.sourceScan`):

```bash
kubectl get version aws-5-81-0-linux-amd64 -n opendepot-system -o jsonpath='{.status.sourceScan}' | jq .
```

For complete scanning configuration, thresholds, and policy behavior, see [Vulnerability Scanning](../configuration/scanning.md).

## Pre-signed URL Redirect Operations

By default, provider binary downloads are proxied through the OpenDepot server. Enabling pre-signed URLs causes the server to redirect OpenTofu directly to the storage backend with a time-limited signed URL, reducing server-side bandwidth costs for large provider binaries.

Add a `presign` block to the `storageConfig` on provider `Version` resources (or on `Depot.spec.global.storageConfig` to apply globally):

```yaml
spec:
  providerConfig:
    name: aws
  storageConfig:
    s3:
      bucket: opendepot-providers
      region: us-east-1
    presign:
      enabled: true
      ttl: "15m"
      fallbackToProxy: true
```

When `presign.enabled` is `true`, the `/opendepot/providers/v1/download/{namespace}/{type}/{version}` endpoint responds with `307 Temporary Redirect` to the signed URL. If pre-signing fails and `fallbackToProxy` is `true` (the default), the server automatically falls back to streaming the binary itself. Set `fallbackToProxy: false` to make pre-signing strictly required; any failure returns `502 Bad Gateway`.

Pre-signed URLs are supported on S3, GCS, and Azure Blob Storage. See [Pre-signed URL Redirects](../storage.md#pre-signed-url-redirects) for per-backend IAM requirements and field reference.
