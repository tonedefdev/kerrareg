---
tags:
  - guides
  - gitops
  - cicd
  - depot
---

# Guides

End-to-end workflows for the most common OpenDepot use cases.

## For Users

Guides for teams consuming modules and providers from OpenDepot.

<div class="grid cards" markdown>

- :material-package: &nbsp;[__Consuming Modules__](modules.md)

    ---

    Reference synced modules from your OpenTofu or Terraform configurations using the registry source format.

- :material-puzzle: &nbsp;[__Consuming Providers__](providers.md)

    ---

    Use OpenDepot as a private provider mirror, including GPG-verified downloads for air-gapped environments.

</div>

## For Admins

Guides for platform and infrastructure teams operating OpenDepot.

<div class="grid cards" markdown>

- :material-source-branch: &nbsp;[__GitOps with Argo CD__](gitops.md)

    ---

    Manage `Module` manifests in Git and let Argo CD sync them to the cluster. Every published version maps to an approved, merged pull request.

- :material-download-circle: &nbsp;[__Depot (Pull-Based)__](depot.md)

    ---

    Automatically discover and sync module versions from GitHub and provider versions from the OpenTofu or Terraform Registry without writing any `kubectl apply` commands or configuring direct pipeline authentication.

- :material-upload-circle: &nbsp;[__CI/CD Pipelines__](cicd.md)

    ---

    Pipeline-focused workflows for CI/CD registry reads (OIDC or token-based auth) and push-based publishing of `Module` resources.

- :material-wrench-cog: &nbsp;[__Registry Operations__](operations.md)

    ---

    Day-2 admin runbooks for force re-sync, module/provider lifecycle operations, scanning checks, and pre-signed URL tuning.

- :material-transfer: &nbsp;[__Migrating to OpenDepot__](migration.md)

    ---

    Move existing modules and providers from the public registry or another self-hosted registry to OpenDepot.

- :material-shield-lock: &nbsp;[__GroupBinding Access Control__](groupbinding.md)

    ---

    Restrict which modules and providers each OIDC group may access using `GroupBinding` resources and expr-lang expressions.

- :material-web: &nbsp;[__Registry Explorer UI__](registry-explorer.md)

    ---

    Enable the browsable registry frontend, configure public visibility labels, and set up OIDC login and `GroupBinding`-based access for the UI.

</div>
