---
tags:
  - providers
  - consuming
  - guides
---

# Consuming Providers

OpenDepot implements the **OpenTofu Provider Network Mirror Protocol** so your OpenTofu configurations can reference providers by their canonical upstream identity (e.g., `hashicorp/aws` or `registry.opentofu.org/hashicorp/aws`) while installing provider binaries from OpenDepot. The lockfile preserves the canonical identity, so your IaC stays portable across teams and environments.

## Default workflow: Network Mirror (OpenTofu only)

!!! info "OpenTofu-only; registry.opentofu.org providers only"
    The initial release supports OpenTofu and mirrors only providers originating from `registry.opentofu.org`. Direct `terraform` CLI support and multi-origin support may be added in future releases.

Once providers are synced, declare them in your OpenTofu configuration using their **canonical source identity**:

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.80"
    }
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 4.0.0"
    }
  }
}
```

The `source` field uses the format `<provider-namespace>/<provider-type>` (or the fully-qualified form `registry.opentofu.org/<provider-namespace>/<provider-type>`). This matches the upstream `registry.opentofu.org` identity, not your OpenDepot hostname.

**CLI configuration: namespace-scoped mirror URL**

Because the canonical source does not reference OpenDepot directly, you configure OpenDepot as the **installation source** in your `.tofurc` file using a `provider_installation` block:

=== "Anonymous access"

    ```hcl
    provider_installation {
      network_mirror {
        url     = "https://opendepot.defdev.io/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.opentofu.org/*/*"]
      }

      direct {
        exclude = ["registry.opentofu.org/*/*"]
      }
    }
    ```

    The `direct.exclude` entry prevents OpenTofu from falling back to `registry.opentofu.org` if a mirrored provider is missing a version, ensuring all installations come from OpenDepot.

=== "Authenticated access (OIDC)"

    ```hcl
    credentials "opendepot.defdev.io" {
      token = "<oidc-access-token>"
    }

    provider_installation {
      network_mirror {
        url     = "https://opendepot.defdev.io/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.opentofu.org/*/*"]
      }

      direct {
        exclude = ["registry.opentofu.org/*/*"]
      }
    }
    ```

    Obtain a token:
    ```bash
    tofu login opendepot.defdev.io
    ```
    Or retrieve it directly from your OIDC provider and place it in `.tofurc`.

=== "Non-default port"

    When your OpenDepot instance runs on a non-default port, include the port in both the `credentials` hostname and the `network_mirror.url`:

    ```hcl
    credentials "opendepot.localtest.me:8080" {
      token = "<oidc-access-token>"
    }

    provider_installation {
      network_mirror {
        url     = "https://opendepot.localtest.me:8080/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.opentofu.org/*/*"]
      }

      direct {
        exclude = ["registry.opentofu.org/*/*"]
      }
    }
    ```

**Key details**

- **Hostname versus namespace**: Credentials are matched by the OpenDepot hostname (and port, if non-default). The mirror URL is namespace-scoped — `/opendepot/providers/mirror/v1/<kubernetes-namespace>/` — and OpenTofu authenticates metadata requests using the credentials associated with the hostname.
- **Canonical identity in lockfile**: The `.terraform.lock.hcl` records the canonical `registry.opentofu.org/hashicorp/aws` identity, not an OpenDepot-specific hostname. This preserves portability — teams can switch between OpenDepot instances or upstream without rewriting source declarations.
- **Mirror-only installation**: The `direct.exclude` entry is critical. Without it, OpenTofu may fall back to `registry.opentofu.org` if a mirrored provider lacks a version you request, bypassing OpenDepot's scanning and access controls.

!!! note
    Provider archive downloads (the binary, `SHA256SUMS`, and `SHA256SUMS.sig`) do not require client authentication when using anonymous mirror access. OpenTofu fetches these URLs after receiving the archive metadata from the metadata endpoint. When authentication is enabled, the metadata endpoint is protected, but archive URLs themselves are either unauthenticated or presigned depending on your storage backend configuration. See [Storage Backends](../storage.md#pre-signed-url-redirects) for presigned URL behavior.

## Advanced: Direct OpenDepot provider identity

For private or internal providers not mirrored from `registry.opentofu.org`, OpenDepot still supports the **Provider Registry Protocol** with direct OpenDepot identity. In this mode, the `source` field references your OpenDepot hostname and Kubernetes namespace directly:

```hcl
terraform {
  required_providers {
    custom_provider = {
      source  = "opendepot.defdev.io/my-team/custom-provider"
      version = "~> 1.0"
    }
  }
}
```

The source format is `<registry-host>/<namespace>/<name>`, where `<namespace>` is the Kubernetes namespace where the `Provider` resource lives and `<name>` matches `spec.providerConfig.name` (or the `Provider` resource name if `name` is omitted).

Configure the `.tofurc` with a `host` block pointing to the `providers.v1` service:

```hcl
credentials "opendepot.defdev.io" {
  token = "<oidc-access-token>"
}

host "opendepot.defdev.io" {
  services = {
    "providers.v1" = "https://opendepot.defdev.io/opendepot/providers/v1/"
  }
}
```

This approach is recommended **only** when the provider is not sourced from `registry.opentofu.org` or when you need OpenTofu/Terraform dual-compatibility in a transitional state. For canonical `registry.opentofu.org` providers, use the Network Mirror workflow above.

## Next Steps for Admins

For provider publishing and lifecycle operations (adding versions, force re-sync, source repository overrides), use [Registry Operations](operations.md).

For scan configuration and policy controls, see [Vulnerability Scanning](../configuration/scanning.md).

For pre-signed redirects and storage backends, see [Storage Backends](../storage.md#pre-signed-url-redirects).
