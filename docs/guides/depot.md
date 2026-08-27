---
tags:
  - depot
  - pull-based
  - guides
---

# Pull-Based Workflow: Using the Depot

The Depot is a pull-based controller that discovers, downloads, and continuously tracks modules and providers from external sources. It handles the full lifecycle — from initial import through ongoing version tracking — so your registry stays current without manual intervention.

The Depot is well-suited to three scenarios:

- **Upstream provider mirroring** — sync major cloud providers (AWS, Azure, Google Cloud, and others) from the OpenTofu or Terraform Registry into your own storage backend, run Trivy scans automatically, and pick up new releases on a schedule
- **Public module tracking** — follow upstream open-source modules by pointing to their GitHub repositories, pin or float versions with constraints, and run Trivy IaC scans on every synced archive
- **Private module import** — pull from private GitHub repositories using GitHub App authentication; also the foundation for one-time registry migration (see [Migrating to OpenDepot](migration.md))

---

## Syncing upstream providers

When you self-host a registry, you take ownership of provider distribution. The Depot mirrors providers from the upstream registries so your teams never pull directly from an external source — and every version that enters your registry is scanned by Trivy before it becomes available.

Providers mirrored by OpenDepot retain their **canonical identity** (e.g., `registry.opentofu.org/hashicorp/aws` or `registry.terraform.io/hashicorp/aws`). Your configurations reference the canonical source, and you configure OpenDepot as the installation source via the Network Mirror Protocol. The upstream registry is controlled per `Provider` resource via `spec.providerConfig.upstreamRegistry` (defaults to `registry.opentofu.org`). See [Consuming Providers](../guides/providers.md) for full CLI configuration examples.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: upstream-providers
  namespace: opendepot-system
spec:
  global:
    storageConfig:
      s3:
        bucket: opendepot-providers
        region: us-east-1
  providerConfigs:
    - name: aws
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 5.80.0"
    - name: azurerm
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 4.0.0"
    - name: google
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 6.0.0"
  pollingIntervalMinutes: 1440
```

This Depot queries the upstream provider registry API for each provider (defaults to `registry.opentofu.org` when `upstreamRegistry` is omitted), filters releases to those matching the version constraint, and creates `Provider` and `Version` resources for each matching OS/architecture combination. The Version controller downloads each binary, uploads it to S3, and — when [scanning is enabled](../configuration/scanning.md) — runs both a binary scan (`trivy rootfs`) and a source scan (`trivy fs` against the provider's `go.mod`) before marking the version as synced.

To mirror from `registry.terraform.io` instead, set the `upstreamRegistry` field explicitly:

```yaml
providerConfigs:
  - name: aws
    upstreamRegistry: registry.terraform.io
    operatingSystems:
      - linux
    architectures:
      - amd64
      - arm64
    versionConstraints: ">= 5.80.0"
```

Setting `pollingIntervalMinutes: 1440` re-checks for new releases once per day. When a new upstream version matches your constraint, the Depot creates the corresponding resources automatically and the scanning and storage pipeline runs without any manual steps.

!!! tip
    Use a version constraint like `">= 5.80.0"` to track all current and future releases above a known-good baseline. Tighten it to `"~> 5.80"` if you want to stay on the 5.x line and exclude major version bumps until you are ready to adopt them.

!!! note
    Provider names default to the `hashicorp` namespace. To mirror a provider from a different registry namespace (e.g., `DataDog/datadog` or `integrations/github`), set the `namespace` field explicitly:
    ```yaml
    providerConfigs:
      - name: datadog
        namespace: DataDog
        versionConstraints: ">= 3.50.0"
    ```

---

## Tracking public modules

For open-source modules maintained by third parties, the Depot lets you define exactly which versions reach your registry. You point it at the upstream GitHub repository, set a version constraint, and let the Depot handle discovery, download, and IaC scanning.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: public-modules
  namespace: opendepot-system
spec:
  global:
    moduleConfig:
      fileFormat: zip
      immutable: true
    storageConfig:
      s3:
        bucket: opendepot-modules
        region: us-east-1
  moduleConfigs:
    - name: terraform-aws-vpc
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 5.0.0"
    - name: terraform-aws-eks
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 20.0.0, != 21.13.0"
    - name: terraform-google-kubernetes-engine
      provider: google
      repoOwner: terraform-google-modules
      versionConstraints: ">= 33.0.0"
  pollingIntervalMinutes: 360
```

The Depot queries each repository for GitHub releases matching the version constraint, creates `Module` and `Version` resources, downloads the release archives, and uploads them to S3. When [module IaC scanning is enabled](../configuration/scanning.md#module-iac-scanning), the Version controller extracts each archive and runs `trivy fs` with config-class checks — catching HCL misconfigurations like open security groups, public S3 ACLs, or unencrypted storage before the module version becomes consumable.

!!! note
    Module IaC scanning does not require the Trivy vulnerability database PVC or CronJob. Config rules are bundled in the Trivy binary, so you can enable module scanning with a minimal scanning configuration:
    ```yaml
    scanning:
      enabled: true
    ```
    See [Vulnerability Scanning](../configuration/scanning.md) for the full configuration reference.

Setting `pollingIntervalMinutes: 360` re-checks for new upstream releases every six hours. New versions that satisfy the constraint are picked up automatically — no Depot changes required.

---

## Combined example

The examples above focus on a single resource type for clarity. In practice, a single Depot can manage both modules and providers together:

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: my-team-depot
  namespace: opendepot-system
spec:
  global:
    githubClientConfig:
      useAuthenticatedClient: true
    moduleConfig:
      fileFormat: zip
      immutable: true
    storageConfig:
      s3:
        bucket: opendepot-modules
        region: us-west-2
  moduleConfigs:
    - name: terraform-aws-eks
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 21.10.1, != 21.13.0"
    - name: terraform-azurerm-aks
      provider: azurerm
      repoOwner: azure
      versionConstraints: ">= 10.0.0"
  providerConfigs:
    - name: aws
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 5.80.0"
      storageConfig:
        s3:
          bucket: opendepot-modules
          region: us-west-2
  pollingIntervalMinutes: 60
```

This Depot will:

1. Query the `terraform-aws-modules/terraform-aws-eks` and `azure/terraform-azurerm-aks` GitHub repositories for releases
2. Filter releases matching the version constraints and create `Module` resources
3. Query the configured upstream registry for the `aws` provider and create a `Provider` resource for matching versions
4. The Module and Provider controllers create `Version` resources for each discovered version and OS/architecture
5. The Version controller fetches archives from GitHub (modules) or the configured upstream provider registry and uploads them to the S3 bucket
6. Re-check for new releases every 60 minutes

---

## Options

**Polling interval:** Set `pollingIntervalMinutes` to have the Depot periodically re-query GitHub and the configured upstream provider registries for new releases. If omitted, the Depot reconciles once and does not poll.

**Per-module storage override:** Any module can override the global storage config:

```yaml
moduleConfigs:
  - name: terraform-aws-eks
    provider: aws
    repoOwner: terraform-aws-modules
    versionConstraints: ">= 21.10.1"
    storageConfig:
      azureStorage:
        accountName: opendepotmodules
        accountUrl: https://opendepotmodules.blob.core.windows.net
        subscriptionID: 00000000-0000-0000-0000-000000000000
        resourceGroup: opendepot-rg
```

**Version history limit:** Set `versionHistoryLimit` on any module or provider config to cap how many versions the Depot retains in the registry. When the limit is reached, older versions are removed as newer ones are added.
