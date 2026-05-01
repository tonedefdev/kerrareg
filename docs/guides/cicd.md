# Push-Based Workflow: CI/CD Pipeline

For private modules you control, bypass the Depot entirely and create `Module` resources directly from your CI/CD pipeline:

```yaml
apiVersion: kerrareg.io/v1alpha1
kind: Module
metadata:
  name: terraform-aws-eks
  namespace: kerrareg-system
spec:
  moduleConfig:
    name: terraform-aws-eks
    provider: aws
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-eks
    fileFormat: zip
    immutable: true
    storageConfig:
      s3:
        bucket: kerrareg-modules
        region: us-west-2
    githubClientConfig:
      useAuthenticatedClient: true
  versions:
    - version: "21.10.1"
    - version: "21.11.0"
    - version: "21.12.0"
```

**GitHub Actions example:**

```yaml
name: Publish Module Version

on:
  release:
    types: [published]

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::<AWS_ACCOUNT_ID>:role/kerrareg-github-actions-role
          aws-region: us-west-2

      - name: Setup kubeconfig
        run: aws eks update-kubeconfig --name my-cluster --region us-west-2

      - name: Publish module version
        run: |
          kubectl apply -f - <<EOF
          apiVersion: kerrareg.io/v1alpha1
          kind: Module
          metadata:
            name: my-module
            namespace: kerrareg-system
          spec:
            moduleConfig:
              name: my-module
              provider: aws
              repoOwner: my-org
              repoUrl: https://github.com/my-org/terraform-aws-my-module
              fileFormat: zip
              storageConfig:
                s3:
                  bucket: kerrareg-modules
                  region: us-west-2
            versions:
              - version: ${{ github.event.release.tag_name }}
          EOF
```

The Module controller creates the `Version` resource, and the Version controller fetches the archive from GitHub and uploads it to storage — no manual archive upload needed.

## Adding Versions to an Existing Module

To publish a new version of a module that already exists in Kerrareg, append the version to the `spec.versions` list. Existing versions are preserved — the Module controller only creates `Version` resources for entries it hasn't seen before.

**Using `kubectl patch` (quick):**

```bash
kubectl patch module terraform-aws-eks -n kerrareg-system \
  --type json -p '[{"op":"add","path":"/spec/versions/-","value":{"version":"21.13.0"}}]'
```

**Using `kubectl apply` (declarative):**

Include all existing versions alongside the new one. The Module controller is idempotent — it won't re-create versions that already exist.

```yaml
apiVersion: kerrareg.io/v1alpha1
kind: Module
metadata:
  name: terraform-aws-eks
  namespace: kerrareg-system
spec:
  moduleConfig:
    name: terraform-aws-eks
    provider: aws
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-eks
    fileFormat: zip
    storageConfig:
      s3:
        bucket: kerrareg-modules
        region: us-west-2
  versions:
    - version: "21.10.1"
    - version: "21.11.0"
    - version: "21.12.0"
    - version: "21.13.0"   # new version
```

**GitHub Actions example (append on release):**

```yaml
- name: Add version to existing module
  run: |
    VERSION=${{ github.event.release.tag_name }}
    kubectl patch module my-module -n kerrareg-system \
      --type json \
      -p "[{\"op\":\"add\",\"path\":\"/spec/versions/-\",\"value\":{\"version\":\"${VERSION}\"}}]"
```

**Removing a version:** Remove the entry from `spec.versions` and re-apply. The Module controller garbage-collects orphaned `Version` resources. If `versionHistoryLimit` is set, older versions are automatically pruned when the limit is exceeded.

## Force Re-Sync

If a Module or Version fails to sync (e.g., due to a transient network error), you can force a re-sync by setting `forceSync: true` on the resource:

```bash
# Force a Module to re-sync all its versions
kubectl patch module terraform-aws-eks -n kerrareg-system \
  --type merge -p '{"spec":{"forceSync":true}}'

# Force a single Version to re-sync
kubectl patch version.kerrareg.io terraform-aws-eks-21.18.0 -n kerrareg-system \
  --type merge -p '{"spec":{"forceSync":true}}'
```

The controller resets `forceSync` to `false` after reconciliation completes.
