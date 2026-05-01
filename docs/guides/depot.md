# Pull-Based Workflow: Using the Depot

Use the Depot for public, private, or externally maintained modules. The Depot automatically discovers versions from GitHub and manages the full lifecycle. 

!!! tip
    You can setup GitHub authentication via a GitHub Application to access private repos.

```yaml
apiVersion: kerrareg.io/v1alpha1
kind: Depot
metadata:
  name: my-team-depot
  namespace: kerrareg-system
spec:
  global:
    githubClientConfig:
      useAuthenticatedClient: true
    moduleConfig:
      fileFormat: zip
      immutable: true
    storageConfig:
      s3:
        bucket: kerrareg-modules
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
          bucket: kerrareg-modules
          region: us-west-2
  pollingIntervalMinutes: 60
```

This Depot will:

1. Query the `terraform-aws-modules/terraform-aws-eks` and `azure/terraform-azurerm-aks` GitHub repositories for releases
2. Filter releases matching the version constraints and create `Module` resources
3. Query the HashiCorp Releases API for the `aws` provider and create a `Provider` resource for matching versions
4. The Module and Provider controllers create `Version` resources for each discovered version and OS/architecture
5. The Version controller fetches archives from GitHub (modules) or HashiCorp (providers) and uploads them to the S3 bucket
6. Re-check for new releases every 60 minutes

**Polling interval:** Set `pollingIntervalMinutes` to have the Depot periodically re-query GitHub for new releases. This is especially useful for public modules where upstream maintainers publish new versions frequently. If omitted, the Depot reconciles once and does not poll.

**Per-module storage override:** Any module can override the global storage config:

```yaml
moduleConfigs:
  - name: terraform-aws-eks
    provider: aws
    repoOwner: terraform-aws-modules
    versionConstraints: ">= 21.10.1"
    storageConfig:
      azureStorage:
        accountName: kerraregmodules
        accountUrl: https://kerraregmodules.blob.core.windows.net
        subscriptionID: 00000000-0000-0000-0000-000000000000
        resourceGroup: kerrareg-rg
```
