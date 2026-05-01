# GitOps with Argo CD

For teams that manage infrastructure declaratively through Git, Kerrareg fits naturally into a GitOps workflow with [Argo CD](https://argo-cd.readthedocs.io/). Instead of running `kubectl apply` from a CI pipeline, you check your `Module` manifests into a Git repository and let Argo CD sync them to the cluster.

**How it works:**

1. A developer opens a PR against their OpenTofu module repository with the code changes
2. The same PR includes an update to the Kerrareg `Module` manifest, adding the new version to `spec.versions`
3. The team reviews both the module code and the registry manifest in a single PR
4. On approval and merge, Argo CD detects the change and syncs the `Module` resource to the cluster
5. Kerrareg takes over — the Module controller creates a `Version` resource, and the Version controller fetches the archive from GitHub and uploads it to storage

This gives you a complete audit trail: every module version published to your registry maps to an approved, merged pull request.

**Example repository structure:**

```
kerrareg-manifests/
├── modules/
│   ├── terraform-aws-eks.yaml
│   ├── terraform-aws-vpc.yaml
│   └── terraform-azurerm-aks.yaml
└── kustomization.yaml
```

**Module manifest (`modules/terraform-aws-eks.yaml`):**

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
    - version: "21.13.0"   # added in PR #42
```

**Argo CD Application:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kerrareg-modules
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/my-org/kerrareg-manifests
    targetRevision: main
    path: modules
  destination:
    server: https://kubernetes.default.svc
    namespace: kerrareg-system
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
```

!!! tip
    Set `prune: false` so that Argo CD does not delete `Module` resources removed from Git — this prevents accidental module deletion. Use `selfHeal: true` so that any manual drift on the cluster is corrected back to the Git-declared state.

**Why this works well with Kerrareg:**

- **Single PR, full visibility** — module code and registry manifest are reviewed together
- **No cluster credentials in CI** — Argo CD handles authentication to the cluster; developers only push to Git
- **Immutable audit trail** — Git history records exactly who added each version and when
- **Declarative all the way down** — Git declares the desired state, Argo CD syncs it, and Kerrareg reconciles it to storage
