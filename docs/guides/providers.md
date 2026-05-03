---
tags:
  - providers
  - consuming
  - guides
---

# Consuming Providers

Once providers are synced, declare them as required providers in your OpenTofu or Terraform configuration using the `<registry-host>/<namespace>/<name>` source format:

```hcl
terraform {
  required_providers {
    aws = {
      source  = "opendepot.defdev.io/opendepot-system/aws"
      version = "~> 5.80"
    }
    azurerm = {
      source  = "opendepot.defdev.io/opendepot-system/azurerm"
      version = ">= 4.0.0"
    }
  }
}
```

The source format is `<registry-host>/<namespace>/<name>`, where `<namespace>` is the Kubernetes namespace where the `Provider` resource lives and `<name>` matches `spec.providerConfig.name` (or the `Provider` resource name if `name` is omitted).

**Pointing OpenTofu at the provider registry**

Because OpenDepot serves providers at a custom host, you need a `host` block in your `.tofurc` or `.terraformrc` to tell OpenTofu where the `providers.v1` API lives:

```
host "opendepot.defdev.io" {
  services = {
    "providers.v1" = "https://opendepot.defdev.io/opendepot/providers/v1/"
  }
}
```

With authentication (recommended for production):

```
host "opendepot.defdev.io" {
  services = {
    "providers.v1" = "https://opendepot.defdev.io/opendepot/providers/v1/"
  }
  token = "<kubernetes-bearer-token>"
}
```

Or using the environment variable approach:

```bash
export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(aws eks get-token \
  --cluster-name my-cluster \
  --region us-west-2 \
  --output json | jq -r '.status.token')

tofu init
```

!!! note
    Provider artifact downloads (the binary, `SHA256SUMS`, and `SHA256SUMS.sig`) do not require client authentication. OpenTofu fetches these URLs after receiving the download metadata from the auth-protected `download` endpoint, and the Terraform Provider Registry Protocol does not forward credentials to artifact URLs. The server uses its own ServiceAccount for these requests. Security is enforced at the metadata tier where the download URL is issued.

**Adding a new provider version**

To publish a new version, append it to `spec.versions`:

```bash
kubectl patch provider aws -n opendepot-system \
  --type json -p '[{"op":"add","path":"/spec/versions/-","value":{"version":"5.81.0"}}]'
```

The Provider controller creates new `Version` resources for every OS/architecture combination, and the Version controller fetches and stores the binaries automatically.

**Force re-sync**

```bash
kubectl patch provider aws -n opendepot-system \
  --type merge -p '{"spec":{"forceSync":true}}'
```

## Vulnerability Scanning

When [scanning is enabled](../configuration/scanning.md), the Version controller runs Trivy against each provider artifact and stores findings on the Kubernetes resources.

**Binary scan results** are stored per `Version` resource in `status.binaryScan`:

```bash
kubectl get version aws-5.81.0-linux-amd64 -n opendepot-system -o jsonpath='{.status.binaryScan}' | jq .
```

```json
{
  "scannedAt": "2026-05-03T02:10:00Z",
  "findings": [
    {
      "vulnerabilityID": "CVE-2024-12345",
      "pkgName": "golang.org/x/net",
      "installedVersion": "0.20.0",
      "fixedVersion": "0.23.0",
      "severity": "HIGH",
      "title": "HTTP/2 CONTINUATION flood vulnerability"
    }
  ]
}
```

**Source scan results** (go.mod dependencies) are stored on the `Provider` resource in `status.sourceScan` and are deduplicated across OS/architecture variants:

```bash
kubectl get provider aws -n opendepot-system -o jsonpath='{.status.sourceScan}' | jq .
```

```json
{
  "scannedAt": "2026-05-03T02:10:05Z",
  "version": "5.81.0",
  "findings": []
}
```

Each `SecurityFinding` contains the following fields:

| Field | Description |
|---|---|
| `vulnerabilityID` | CVE or GHSA identifier |
| `pkgName` | Package containing the vulnerability |
| `installedVersion` | Version of the package currently in use |
| `fixedVersion` | Minimum version that resolves the vulnerability, if known |
| `severity` | `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `UNKNOWN` |
| `title` | Short description of the vulnerability |

**Provider source repository**

OpenDepot automatically resolves the provider's GitHub repository for source scanning using the OpenTofu registry (`api.opentofu.org`). This works for any provider published in the registry regardless of its owning organisation.

For providers not published under the `hashicorp` organisation, set the `namespace` field to match the registry namespace:

```yaml
spec:
  providerConfig:
    name: github
    namespace: integrations
```

Use `sourceRepository` to pin a specific URL when the registry lookup returns the wrong repository or is unreachable:

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    sourceRepository: "https://github.com/my-org/terraform-provider-myprovider"
```

**Authenticated source scanning**

For private provider source repositories or high-volume environments where unauthenticated GitHub requests hit API rate limits, set `githubClientConfig.useAuthenticatedClient: true` on the `ProviderConfig`. The Version controller will use the same `opendepot-github-application-secret` Secret that modules use, so no additional Secret is required if GitHub App auth is already configured in the namespace.

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    githubClientConfig:
      useAuthenticatedClient: true
```

See [GitHub Authentication](../configuration/github-auth.md) and [Authenticated Source Scanning](../configuration/scanning.md#authenticated-source-scanning) for setup details.

See [Vulnerability Scanning](../configuration/scanning.md) for full configuration details including policy enforcement and Helm values.
