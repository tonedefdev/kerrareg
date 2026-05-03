---
tags:
  - modules
  - consuming
  - guides
---

# Consuming Modules

Once modules are synced, reference them in your OpenTofu or Terraform configuration:

```hcl
module "eks" {
  source  = "opendepot.defdev.io/opendepot-system/terraform-aws-eks/aws"
  version = "~> 21.0"
}

module "aks" {
  source  = "opendepot.defdev.io/opendepot-system/terraform-azurerm-aks/azurerm"
  version = ">= 10.0.0"
}
```

The source format is `<registry-host>/<namespace>/<name>/<provider>`, where `<namespace>` is the Kubernetes namespace where the `Module` resource lives.

## Inline Module Configuration

`moduleConfigRef.name` is optional on a `Version` CR. When `name` is omitted, or when no `Module` CR with that name exists in the namespace, the Version controller treats all fields on `moduleConfigRef` as fully inline. A UUID-based filename is generated automatically so the archive has a stable storage key, and the download proceeds using the GitHub and storage config set directly on the `Version` CR.

This is useful when running the version controller standalone (without the module controller), or for one-off version testing where creating a full `Module` CR is unnecessary.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: terraform-aws-s3-bucket-4.3.0
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

## Vulnerability Scanning

When [scanning is enabled](../configuration/scanning.md), the Version controller runs a Trivy IaC scan on the extracted module archive and stores findings on the `Version` resource.

Read IaC scan results from `Version.status.sourceScan`:

```bash
kubectl get version terraform-aws-key-pair-2.0.0 -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

```json
{
  "scannedAt": "2026-05-03T02:11:00Z",
  "findings": [
    {
      "vulnerabilityID": "AVD-AWS-0057",
      "pkgName": "aws_key_pair",
      "installedVersion": "",
      "severity": "LOW",
      "title": "Key pair does not use a modern key algorithm"
    }
  ]
}
```

Module IaC findings detect HCL misconfigurations (e.g. insecure resource defaults, overly permissive policies). The `vulnerabilityID` field contains a Trivy rule ID such as `AVD-AWS-0057` rather than a CVE identifier. If no misconfigurations are found, `findings` will be an empty array.

See [Vulnerability Scanning](../configuration/scanning.md) for configuration details and policy enforcement options.
