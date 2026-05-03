---
tags:
  - authentication
  - kubernetes
  - security
search:
  boost: 2
---

# Authenticating with OpenDepot

OpenDepot supports two authentication methods. Both leverage Kubernetes credentials — either a short-lived bearer token or a base64-encoded kubeconfig.

### Method 1: Environment Variables (Recommended)

Use an environment variable to pass a Kubernetes access token. OpenTofu (all versions) and Terraform (v1.2+) support this method.

The variable name is derived from the registry hostname: replace dots with underscores and convert to uppercase.

`opendepot.defdev.io` → `TF_TOKEN_OPENDEPOT_DEFDEV_IO`

=== "Amazon EKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(aws eks get-token \
      --cluster-name my-cluster \
      --region us-west-2 \
      --output json | jq -r '.status.token')

    tofu init
    tofu plan
    ```

=== "Google GKE"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(gcloud auth print-access-token)

    tofu init
    tofu plan
    ```

=== "Azure AKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(az account get-access-token \
      --resource 6dae42f8-4368-4678-94ff-3960e28e3630 \
      --query accessToken -o tsv)

    tofu init
    tofu plan
    ```

=== "Generic OIDC"

    ```bash
    # Any cluster using kubelogin or exec credentials
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(kubectl get secret \
      -n opendepot-system my-sa-token \
      -o jsonpath='{.data.token}' | base64 -d)

    tofu init
    tofu plan
    ```

Tokens are short-lived and automatically rotate, making this the most secure option.

### Method 2: Base64-Encoded Kubeconfig

For development or environments where environment variables are not practical, encode your kubeconfig and store it in a credentials file.

!!! note
    This method requires `server.useBearerToken: false` in your Helm values.

**1. Encode your kubeconfig:**

```bash
kubectl config view --raw | base64 | tr -d '\n' > /tmp/kubeconfig.b64
```

**2. Create `~/.terraform.d/credentials.tfrc.json`:**

```json
{
  "credentials": {
    "opendepot.defdev.io": {
      "token": "<contents-of-kubeconfig.b64>"
    }
  }
}
```

```bash
chmod 600 ~/.terraform.d/credentials.tfrc.json
```

### Authentication Comparison

| Feature | Environment Variable | Kubeconfig File |
|---------|---------------------|-----------------|
| Token Lifetime | Short-lived (auto-rotating) | Long-lived (manual rotation) |
| Security | Highest | Good |
| Setup | Low | Low |
| Best For | Production, CI/CD | Development |
| OpenTofu Support | All versions | All versions |
| Terraform Support | v1.2+ | All versions |

### CI/CD Example

```yaml
name: Apply Infrastructure

on:
  push:
    branches: [main]

jobs:
  apply:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::ACCOUNT_ID:role/github-actions-role
          aws-region: us-west-2

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1

      - name: Set registry token
        run: |
          TOKEN=$(aws eks get-token --cluster-name my-cluster --region us-west-2 --output json | jq -r '.status.token')
          echo "TF_TOKEN_OPENDEPOT_DEFDEV_IO=$TOKEN" >> $GITHUB_ENV

      - run: tofu init
      - run: tofu plan
```

