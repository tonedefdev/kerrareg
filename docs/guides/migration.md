---
tags:
  - migration
  - guides
---

# Migrating to OpenDepot

!!! tip
    Migration is one of several Depot use cases. The Depot also supports ongoing upstream provider mirroring and public module tracking with automatic Trivy scanning — see [Pull-Based Workflow: Using the Depot](../guides/depot.md) for the full picture. For a one-time migration, follow the steps below: once everything is synced, delete the Depot and switch to the [GitOps workflow](../guides/gitops.md). Deleting a Depot **does not** delete the Modules or Providers it created, so your registry stays fully intact.

**Migrating modules** — Use the Depot to bulk-import existing modules into OpenDepot:

1. Create a `Depot` with broad version constraints (e.g., `">= 0.0.0"`) to pull in the full release history
2. Wait for all versions to sync (check `Module` and `Version` status resources)
3. Update your OpenTofu/Terraform configurations to source modules from OpenDepot
4. Delete the Depot — all `Module` and `Version` resources remain untouched
5. Going forward, publish new versions via the GitOps workflow

**Migrating providers** — Use `spec.providerConfigs` in your Depot to mirror providers from upstream registries into your own storage backend:

1. Create a `Depot` with `providerConfigs` listing each provider, your target OS/architecture matrix, and a version constraint (the upstream registry defaults to `registry.opentofu.org`; set `upstreamRegistry: registry.terraform.io` explicitly if needed)
2. Wait for all `Provider` and `Version` resources to sync
3. Update your configurations to reference providers by their canonical identity (e.g., `hashicorp/aws`) and configure OpenDepot as the installation source via `provider_installation.network_mirror` in your `.tofurc` or `.terraformrc` (see [Consuming Providers](../guides/providers.md) for full CLI configuration examples)
4. Delete the Depot — all `Provider` and `Version` resources remain untouched

This pattern lets you adopt OpenDepot incrementally without disrupting existing workflows. The Depot bridges the gap between the public registries and a fully self-hosted solution.

For version-specific breaking changes and upgrade steps, see [Upgrading](../upgrading.md).
