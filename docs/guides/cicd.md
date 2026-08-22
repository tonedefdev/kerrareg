---
tags:
  - cicd
  - push-based
  - guides
---

# CI/CD Pipelines

## Registry Reads with Dex Client Credentials

If your organization uses OIDC (Dex) for human users and you want CI/CD pipelines to authenticate **without** needing `kubectl` or a ServiceAccount, use the Dex client credentials grant instead. This is the recommended approach because it preserves separation of duties: OIDC stays read-only for registry access, while Kubernetes RBAC governs create, update, and delete permissions.

The examples below assume the recommended [server-proxied Dex](../configuration/oidc.md#recommended-proxy-dex-through-the-server) setup (`server.oidc.dexProxy.enabled: true`), so the Dex token endpoint shares the same host as the registry API. If Dex is exposed separately instead, substitute that host (e.g. `https://dex.defdev.io/dex/token`).

Enable client credentials support in your Helm values and register a dedicated Dex static client for the pipeline:

```yaml
dex:
  config:
    oauth2:
      grantTypes:
        - authorization_code
        - client_credentials
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - https://opendepot.defdev.io/...
      - id: ci-pipeline
        name: CI Pipeline
        secretEnv: OPENDEPOT_CC_CLIENT_SECRET
        grantTypes:
          - client_credentials
server:
  oidc:
    enabled: true
    allowClientCredentials: true
```

Create a `GroupBinding` to authorize the pipeline client:

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: ci-pipeline-binding
  namespace: opendepot-system
spec:
  expression: '"client:ci-pipeline" in groups'
  moduleResources:
    - "*"
  providerResources:
    - "*"
```

### GitHub Actions Example

```yaml
jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Get Dex CC token for OpenDepot registry
        id: opendepot-token
        env:
          CC_CLIENT_SECRET: ${{ secrets.OPENDEPOT_CC_CLIENT_SECRET }}
        run: |
          TOKEN=$(curl -sf -X POST https://opendepot.defdev.io/dex/token \
            -d grant_type=client_credentials \
            -d client_id=ci-pipeline \
            -d "client_secret=${CC_CLIENT_SECRET}" \
            -d scope=openid \
            | jq -r '.access_token')
          echo "token=$TOKEN" >> "$GITHUB_OUTPUT"

      - name: Write .tofurc
        run: |
          cat > ~/.tofurc <<EOF
          credentials "opendepot.defdev.io" {
            token = "${{ steps.opendepot-token.outputs.token }}"
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
          EOF

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1

      - run: tofu init
```

This configures OpenTofu to install providers from OpenDepot via the Network Mirror Protocol while preserving canonical `registry.opentofu.org` provider identity in your configuration and lockfile. The CC token is short-lived (TTL controlled by Dex) and scoped to read-only operations via the `GroupBinding`. No `kubectl` access or cluster kubeconfig is required — only the Dex token endpoint must be reachable from the runner.

To use Terraform instead, rename the file to `.terraformrc` and replace `setup-opentofu` with `hashicorp/setup-terraform`. If your `Provider` resources use `upstreamRegistry: registry.terraform.io`, update the `include` and `exclude` patterns to `registry.terraform.io/*/*`.

For full configuration details see [Client Credentials (Machine-to-Machine)](../configuration/oidc.md#client-credentials-machine-to-machine). For a side-by-side comparison of all supported authentication methods and their access-control mechanisms, see the [Authentication Comparison](../authentication.md#authentication-comparison) table.

## Registry Reads: SA Fallback with OIDC

!!! warning "Last resort — exhaust other options first"
    SA fallback **bypasses GroupBinding entirely**. When a ServiceAccount token is used, the SA's Kubernetes RBAC governs access instead of the centralized GroupBinding model your OIDC setup provides. This means:

    - Human users and pipeline tokens follow different access control paths, which increases audit surface.
    - A leaked pipeline token grants direct cluster API access, not just registry access.
    - SA-token access is invisible to GroupBinding audit logs.

    Before enabling SA fallback, consider whether one of these fits your use case instead:

    - **Publishing modules only?** → Use the [GitOps approach](gitops.md). No pipeline credentials needed at all — Argo CD handles cluster auth, developers push to Git.
    - **Reading the registry from pipelines without cluster access?** → Use [Dex Client Credentials](#registry-reads-with-dex-client-credentials). Pipelines get a Dex-issued token scoped to GroupBinding, with no Kubernetes API exposure.

    SA fallback is appropriate when your pipeline must interact with the Kubernetes API directly for reasons beyond registry access and you have already ruled out the above options.

When your organization uses OIDC for human users, CI/CD pipelines still need to run `tofu init` and download providers from the registry. By default OIDC and bearer-token modes are mutually exclusive, which would require pipelines to use a separate credential mechanism. The ServiceAccount fallback removes this constraint.

### Enable SA Fallback in Your Helm Values

```yaml
server:
  oidc:
    enabled: true
    allowServiceAccountFallback: true
```

This lets K8s SA tokens authenticate alongside OIDC JWTs. SA tokens bypass GroupBinding and rely on Kubernetes RBAC directly.

### Create an SA and Bind Registry-Reader RBAC

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ci-registry-reader
  namespace: my-ci-namespace
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: opendepot-registry-reader
  namespace: opendepot-system
rules:
- apiGroups: ["opendepot.defdev.io"]
  resources: ["modules", "versions", "providers"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: opendepot-registry-reader-binding
  namespace: opendepot-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: opendepot-registry-reader
subjects:
- kind: ServiceAccount
  name: ci-registry-reader
  namespace: my-ci-namespace
```

### GitHub Actions Example

```yaml
jobs:
  plan:
    runs-on: ubuntu-latest
    permissions:
      id-token: write  # required for OIDC to your cloud provider
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::<ACCOUNT>:role/my-role
          aws-region: us-west-2

      - name: Configure kubeconfig for EKS
        run: aws eks update-kubeconfig --name my-cluster --region us-west-2

      - name: Get SA token for OpenDepot registry
        id: opendepot-token
        run: |
          TOKEN=$(kubectl create token ci-registry-reader \
            -n my-ci-namespace \
            --duration=15m)
          echo "token=$TOKEN" >> "$GITHUB_OUTPUT"

      - name: Write .tofurc
        run: |
          cat > ~/.tofurc <<EOF
          credentials "opendepot.defdev.io" {
            token = "${{ steps.opendepot-token.outputs.token }}"
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
          EOF

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1

      - run: tofu init
```

This configures OpenTofu to install providers from OpenDepot via the Network Mirror Protocol while preserving canonical `registry.opentofu.org` provider identity. The SA token is short-lived (15 minutes) and scoped to read-only registry operations via the RBAC defined above. No Dex client credentials are needed.

To use Terraform instead, rename the file to `.terraformrc` and replace `setup-opentofu` with `hashicorp/setup-terraform`. If your `Provider` resources use `upstreamRegistry: registry.terraform.io`, update the `include` and `exclude` patterns to `registry.terraform.io/*/*`.

This approach uses `kubectl create token` to authenticate as the dedicated `ci-registry-reader` SA, keeping the pipeline's registry access strictly bounded to the RBAC above — regardless of how broad the runner's cloud IAM role is. If your runner's cloud IAM role already has appropriate K8s RBAC configured, you can simplify by using the provider token directly instead of creating an SA token (see [Managed Cluster Tokens](../authentication.md#method-2-managed-cluster-tokens)).

## Push-Based Workflows

For private modules you control, bypass the Depot entirely and create `Module` resources directly from your CI/CD pipeline:

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
    immutable: true
    storageConfig:
      s3:
        bucket: opendepot-modules
        region: us-west-2
    githubClientConfig:
      useAuthenticatedClient: true
  versions:
    - version: "21.10.1"
    - version: "21.11.0"
    - version: "21.12.0"
```

**GitHub Actions Example:**

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
          role-to-assume: arn:aws:iam::<AWS_ACCOUNT_ID>:role/opendepot-github-actions-role
          aws-region: us-west-2

      - name: Setup kubeconfig
        run: aws eks update-kubeconfig --name my-cluster --region us-west-2

      - name: Publish module version
        run: |
          kubectl apply -f - <<EOF
          apiVersion: opendepot.defdev.io/v1alpha1
          kind: Module
          metadata:
            name: my-module
            namespace: opendepot-system
          spec:
            moduleConfig:
              name: my-module
              provider: aws
              repoOwner: my-org
              repoUrl: https://github.com/my-org/terraform-aws-my-module
              fileFormat: zip
              storageConfig:
                s3:
                  bucket: opendepot-modules
                  region: us-west-2
            versions:
              - version: ${{ github.event.release.tag_name }}
          EOF
```

The Module controller creates the `Version` resource, and the Version controller fetches the archive from GitHub and uploads it to storage — no manual archive upload needed.

To publish subsequent versions of an existing module from a pipeline, see [Adding versions to an existing module](operations.md#adding-versions-to-an-existing-module).

## Related Admin Operations

For day-2 operations such as force re-sync, inline `Version` configs, provider lifecycle actions, vulnerability scanning runbooks, and pre-signed URL tuning, see [Registry Operations](operations.md).

For canonical configuration reference:

- [Vulnerability Scanning](../configuration/scanning.md)
- [Storage Backends](../storage.md#pre-signed-url-redirects)
