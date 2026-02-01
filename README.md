# Kerrareg: A Terraform Module Registry Implementation

Kerrareg is a self-hosted Terraform Module Registry that implements the official [Terraform Module Registry Protocol](https://developer.hashicorp.com/terraform/internals/module-registry-protocol). It enables organizations to create private module registries without relying on the public Terraform Registry, giving you complete control over module distribution, versioning, and storage.

## Table of Contents

- [What is a Terraform Module Registry?](#what-is-a-terraform-module-registry)
- [Architecture Overview](#architecture-overview)
- [Services](#services)
- [Getting Started](#getting-started)
- [Prerequisites](#prerequisites)
- [Configuration](#configuration)
- [Deployment](#deployment)

## What is a Terraform Module Registry?

A Terraform Module Registry is a service that implements a standardized protocol for discovering and downloading Terraform modules. When you reference a module in your Terraform configuration like:

```hcl
module "example" {
  source = "registry.example.com/myorg/vpc/aws"
  version = "~> 1.2.0"
}
```

Terraform CLI uses the Module Registry Protocol to:

1. **Discover available versions** - Query the registry for all versions of the module matching your version constraints
2. **Resolve version constraints** - Match your semantic version constraints (`~> 1.2.0`, `>= 1.0.0, < 2.0.0`, etc.) against available versions
3. **Download the module** - Retrieve the source code for the selected module version

Kerrareg implements all required endpoints of this protocol, allowing Terraform CLI and other IaC tools (like OpenTofu) to treat your private registry exactly like the public Terraform Registry.

## Architecture Overview

Kerrareg consists of multiple services that work together in a Kubernetes environment:

```
┌─────────────────────────────────────────────────────────┐
│                  Terraform CLI / OpenTofu               │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│            Server (Registry Protocol API)               │
│  • Service Discovery                                    │
│  • List Available Versions                              │
│  • Download Module Endpoint                             │
│  • File Serving (S3, Azure Blob, Filesystem)            │
└────────────────────┬────────────────────────────────────┘
                     │
    ┌────────────────┼────────────────┐
    ▼                ▼                ▼
┌────────┐      ┌────────┐      ┌──────────────┐
│ Depot  │      │ Module │      │ Version      │
│        │      │        │      │ (Core)       │
└────┬───┘      └────────┘      └────┬─────────┘
     │                               │
     └───────────────┬───────────────┘
                     ▼
        ┌────────────┬────────────┐
        ▼            ▼            ▼
   ┌─────────┐ ┌─────────┐ ┌──────────┐
   │ GitHub  │ │ Storage │ │ Metadata │
   │ (fetch) │ │Backends │ │ (S3/Az)  │
   └─────────┘ └─────────┘ └──────────┘
```

## Services

### Version (`services/version`) - Core Service

**The Version service is the most critical component of Kerrareg.** It handles the actual module source code retrieval and storage, implementing the core workflow that enables the entire registry to function.

**Key Responsibilities:**
- **GitHub Source Retrieval**: Fetches module source code from GitHub repositories at specific versions (tags/releases)
- **Storage Interface Implementation**: Implements the unified storage interface to support multiple backends
  - **Amazon S3**: Stores module archives in S3 buckets
  - **Local Filesystem**: Stores modules locally (testing/development only)
  - **Azure Blob Storage**: Stores modules in Azure Storage Accounts
- **Module Preparation**: Transforms raw module source from GitHub into distribution-ready archives
- **Checksum Generation**: Computes and validates SHA256 checksums for module versions
- **Version Metadata**: Generates and stores version metadata required by the registry protocol

**How it works:**
The Version controller watches for `Version` resources created by the Module controller and reconciles them by:
1. Fetching the module source from GitHub at the specified version/tag
2. Preparing the source into a distribution archive (tar.gz or zip)
3. Computing the SHA256 checksum of the archive
4. Uploading the archive to the configured storage backend
5. Updating the Version resource status with checksum and sync information

**Storage Recommendation:**
- Use **S3** or **Azure Blob Storage** for production environments
- Use **Local Filesystem** storage only for testing and development

### Server (`services/server`)

The **Server** implements the complete Terraform Module Registry Protocol. It exposes the following endpoints:

#### Service Discovery
- **Endpoint**: `/.well-known/terraform.json`
- **Response**: Returns the base URL for the module registry protocol endpoints
- **Purpose**: Allows Terraform CLI to discover where the registry API is located

#### List Available Versions
- **Endpoint**: `GET /kerrareg/modules/v1/{namespace}/{name}/{system}/versions`
- **Response**: Returns all available versions of a module that match the module address
- **Purpose**: Enables Terraform's version constraint resolution

#### Download Module
- **Endpoint**: `GET /kerrareg/modules/v1/{namespace}/{name}/{system}/{version}/download`
- **Response**: Returns a 204 No Content status with an `X-Terraform-Get` header containing the download location
- **Purpose**: Provides Terraform with a download URL for the selected module version

#### File Serving Endpoints
The server also provides direct file serving capabilities for modules stored in various backends:

- **S3**: `/kerrareg/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}`
- **Azure Blob Storage**: `/kerrareg/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}`
- **Filesystem**: `/kerrareg/modules/v1/download/fileSystem/{directory}/{name}/{fileName}`

These endpoints authenticate with Kubernetes to retrieve metadata about the module version, validate checksums, and serve the module archive directly to Terraform CLI.

### Module (`services/module`)

The **Module** is a Kubernetes controller that orchestrates the creation and management of module versions. It serves as the bridge between the Depot (which identifies which versions to create) and the Version controller (which performs the actual work).

**Key Responsibilities:**
- **Version Creation**: Creates `Version` resources for each module version that needs to be available
- **Latest Version Tracking**: Automatically determines and tracks the latest available version of a module
- **Lifecycle Management**: Manages the full lifecycle of module versions from creation through cleanup
- **File Metadata**: Generates random filenames for versions and stores metadata about files

**How it works:**
1. The Module controller watches for changes to `Module` resources
2. For each requested module version in the `Module.spec.versions` list:
   - Creates a corresponding `Version` resource
   - Adds module configuration references
   - Triggers the Version controller to perform the actual work
3. The Module controller also:
   - Tracks the latest available version
   - Generates random filenames for each version to ensure uniqueness
   - Cleans up versions no longer needed
   - Manages sync status and orchestrates the reconciliation process

### Depot (`services/depot`)

The **Depot** is a Kubernetes controller that acts as a module curator and version manager. It automatically manages module versions based on version constraints, similar to how a package manager like `npm` handles dependency resolution.

**Key Responsibilities:**
- **Version Constraint Resolution**: Monitors a list of `ModuleConfig` resources and automatically resolves version constraints (e.g., `>= 1.0.0, < 2.0.0`)
- **Module Creation**: Creates `Module` resources for each `ModuleConfig`
- **Source Discovery**: Queries GitHub repositories for available releases/tags matching your constraints
- **Coordination**: Orchestrates the creation of `Module` and `Version` resources based on version constraints

**How it works:**
1. You define a `Depot` resource with a list of `ModuleConfig` entries, each specifying:
   - Module name and provider
   - GitHub repository location
   - Version constraints (e.g., `~> 1.2.0` for bugfix updates only)
   - Storage configuration
   - Authentication settings

2. The Depot controller watches for changes and automatically:
   - Queries the GitHub repository for releases/tags
   - Resolves version constraints using semantic versioning
   - Creates or updates `Module` resources with the matching versions
   - Triggers the Module controller to create specific `Version` resources

#### When to Use the Depot

The Depot supports two distinct workflow models:

**Pull-Based Workflow (Depot-Driven)**

Use the Depot for **public modules you don't control** or modules that are maintained externally. The Depot automatically pulls new versions from GitHub based on your version constraints, making it ideal for:

- Consuming HashiCorp modules or community-maintained modules
- Maintaining a curated set of external modules from multiple organizations
- Automatic version updates without manual intervention
- Initial migration of existing modules into Kerrareg

**Push-Based Workflow (CI-Driven)**

For **private modules you own and control**, use a **CI/CD pipeline** (e.g., GitHub Actions) instead of the Depot:

1. In your module's GitHub repository, create a GitHub Actions workflow that:
   - Triggers on new releases or version tags
   - Builds and packages the module
   - Generates a Kubernetes client authenticated to access Kerrareg
   - Directly create or update the `Module` version references. This in turn creates any new `Version` resources in the cluster.
   - The `Version` controller then pushes the module archive to your configured storage backend

2. With this approach:
   - No Depot is required (pull-based coordination is bypassed)
   - You have full control over when versions are published
   - Version constraints are determined by your CI logic
   - Faster feedback loop from development to registry
   - Better integration with your release process

**Migration Path**

The Depot is also valuable for **migrating existing modules to Kerrareg**:

1. Create a Depot with `ModuleConfig` entries for all your current modules
2. Specify version constraints to import existing versions (e.g., `"*"` to import all)
3. The Depot automatically pulls and imports all modules into Kerrareg
4. Verify everything is working correctly
5. Once migration is complete:
   - Delete the Depot resources
   - Transition module owners to the push-based CI workflow
   - Modules continue to be served by Kerrareg without the Depot

## Storage Backends

The Version service supports multiple storage backends for module archives through the Storage interface:

### Amazon S3 (Recommended for Production)
Store modules in AWS S3 buckets with configurable regions and key prefixes. Provides high availability, durability, and integrates well with most cloud environments.

### Azure Blob Storage (Recommended for Production)
Store modules in Azure Storage Accounts with support for subscriptions and resource groups. Ideal for Azure-centric environments.

### Local Filesystem (Testing Only)
Store modules on a local filesystem path. Use this backend only for development and testing environments. **Not recommended for production use.**

## Getting Started

### Prerequisites

- Go version v1.24.0+
- Docker version 17.03+
- kubectl version v1.11.3+
- Access to a Kubernetes v1.11.3+ cluster
- GitHub repository containing your modules (for pulling releases)
- One of the supported storage backends (S3, Azure Blob Storage, or local filesystem)

### Installation

#### 1. Build and Push Container Images

Build the server, depot, and module services as Docker images:

```sh
# Build server image
cd services/server
docker build -t <registry>/kerrareg-server:latest .

# Build depot image
cd ../depot
make docker-build docker-push IMG=<registry>/depot:latest

# Build module image
cd ../module
make docker-build docker-push IMG=<registry>/module:latest
```

#### 2. Install CRDs

Install the Kerrareg Custom Resource Definitions:

```sh
cd services/version
make install

cd ../module
make install

cd ../depot
make install
```

#### 3. Deploy Controllers

Deploy the Version, Module, and Depot controllers (deploy in this order):

```sh
# Deploy Version controller (core service - deploy first)
cd services/version
make deploy IMG=<registry>/version:latest

# Deploy Module controller
cd ../module
make deploy IMG=<registry>/module:latest

# Deploy Depot controller
cd ../depot
make deploy IMG=<registry>/depot:latest
```

#### 4. Deploy the Server

Deploy the Server as a service (typically as a Deployment with a LoadBalancer or Ingress):

```sh
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kerrareg-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: kerrareg-server
  template:
    metadata:
      labels:
        app: kerrareg-server
    spec:
      containers:
      - name: server
        image: <registry>/kerrareg-server:latest
        ports:
        - containerPort: 8443
        volumeMounts:
        - name: tls
          mountPath: /etc/tls
      volumes:
      - name: tls
        secret:
          secretName: kerrareg-tls
---
apiVersion: v1
kind: Service
metadata:
  name: kerrareg-server
spec:
  type: LoadBalancer
  selector:
    app: kerrareg-server
  ports:
  - protocol: TCP
    port: 443
    targetPort: 8443
EOF
```

### Configuration

#### Creating a Depot

Define a `Depot` resource to specify which modules your registry should manage:

```yaml
apiVersion: kerrareg.io/v1alpha1
kind: Depot
metadata:
  name: my-modules
spec:
  global:
    storageConfig:
      s3:
        bucket: my-module-bucket
        region: us-west-2
    githubClientConfig:
      useAuthenticatedClient: true  # Requires kerrareg-github-application-secret
  
  moduleConfigs:
    - name: vpc
      provider: aws
      repoOwner: myorg
      repoUrl: https://github.com/myorg/terraform-aws-vpc
      versionConstraints: ">= 1.0.0, < 2.0.0"
      fileFormat: tar
    
    - name: ecs-cluster
      provider: aws
      repoOwner: myorg
      repoUrl: https://github.com/myorg/terraform-aws-ecs
      versionConstraints: "~> 2.1.0"
      fileFormat: zip
```

#### Defining Module Versions

Create a `Module` resource that specifies which versions of a module should be available:

```yaml
apiVersion: kerrareg.io/v1alpha1
kind: Module
metadata:
  name: vpc-aws
spec:
  moduleConfig:
    name: vpc
    provider: aws
    repoOwner: myorg
    repoUrl: https://github.com/myorg/terraform-aws-vpc
    storageConfig:
      s3:
        bucket: my-module-bucket
        region: us-west-2
  
  versions:
    - version: 1.0.0
    - version: 1.1.0
    - version: 1.2.0
```

#### GitHub Authentication

For higher rate limits and private repositories, set up GitHub App authentication:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kerrareg-github-application-secret
type: Opaque
data:
  githubAppID: <base64-encoded-app-id>
  githubInstallID: <base64-encoded-install-id>
  githubPrivateKey: <base64-encoded-private-key>
```

## Kubernetes RBAC Setup

Since Kerrareg is Kubernetes-native, managing access requires setting up appropriate Kubernetes ServiceAccounts and RBAC permissions. This is especially important for CI/CD pipelines that need to create and manage module resources.

### Understanding Kerrareg Resources

Kerrareg defines the following Custom Resources in the `kerrareg.io` API group (v1alpha1):

- **Depot** - Curates modules from external sources based on version constraints
- **Module** - Represents a Terraform module with version information
- **Version** - Represents a specific version of a module with metadata and checksums

These resources support the following operations:
- `create` - Create new resources
- `update` / `patch` - Modify existing resources
- `delete` - Remove resources
- `get` / `list` / `watch` - Read resources

### ServiceAccount for CI/CD Pipelines

For CI/CD pipelines (e.g., GitHub Actions) that need to push module versions to Kerrareg, create a dedicated ServiceAccount with minimal required permissions:

**1. Create a ServiceAccount and associated RBAC resources:**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kerrareg-ci-publisher
  namespace: kerrareg  # or your chosen namespace

---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kerrareg-module-publisher
  namespace: kerrareg
rules:
# Permissions for managing Module resources
- apiGroups: ["kerrareg.io"]
  resources: ["modules"]
  verbs: ["create", "update", "patch", "get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kerrareg-ci-publisher-binding
  namespace: kerrareg
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kerrareg-module-publisher
subjects:
- kind: ServiceAccount
  name: kerrareg-ci-publisher
  namespace: kerrareg
```

**2. Generate a kubeconfig for the ServiceAccount:**

```bash
# Get the ServiceAccount token
TOKEN=$(kubectl get secret -n kerrareg \
  $(kubectl get secret -n kerrareg | grep kerrareg-ci-publisher-token | awk '{print $1}') \
  -o jsonpath='{.data.token}' | base64 --decode)

# Get the Kubernetes API server URL
API_SERVER=$(kubectl cluster-info | grep 'Kubernetes master' | awk '/https/ {print $NF}' | sed 's/$//' )

# Create a kubeconfig
cat > ci-kubeconfig.yaml <<EOF
apiVersion: v1
clusters:
- cluster:
    server: $API_SERVER
  name: kerrareg-cluster
contexts:
- context:
    cluster: kerrareg-cluster
    user: ci-publisher
  name: ci-context
current-context: ci-context
kind: Config
preferences: {}
users:
- name: ci-publisher
  user:
    token: $TOKEN
EOF
```

**3. Use in GitHub Actions:**

```yaml
name: Publish Module Version

on:
  release:
    types: [published]

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build module archive
        run: |
          mkdir -p dist
          # Your build process here (e.g., compress module files)
          tar czf dist/module-${{ github.ref_name }}.tar.gz .

      - name: Setup kubeconfig
        run: |
          mkdir -p ~/.kube
          echo "${{ secrets.KERRAREG_KUBECONFIG }}" | base64 -d > ~/.kube/config
          chmod 600 ~/.kube/config

      - name: Create or patch Module resource
        run: |
          kubectl apply -f - <<EOF
          apiVersion: kerrareg.io/v1alpha1
          kind: Module
          metadata:
            name: mymodule-aws
            namespace: kerrareg
          spec:
            moduleConfig:
              name: mymodule
              provider: aws
              repoOwner: myorg
              repoUrl: https://github.com/myorg/terraform-aws-mymodule
              storageConfig:
                s3:
                  bucket: my-module-bucket
                  region: us-west-2
            versions:
              - version: ${{ github.ref_name }}
          EOF

      - name: Upload to S3
        run: |
          aws s3 cp dist/module-${{ github.ref_name }}.tar.gz \
            s3://my-module-bucket/${{ github.ref_name }}/module.tar.gz
```

> **Note:** The `kubectl apply` command will create the Module if it doesn't exist, or update it if it does. The Module controller will automatically create Version resources for each version listed in `spec.versions`. The Version controller then handles fetching from GitHub, computing checksums, and uploading to storage.

### ServiceAccount for Depot Controllers

If using the Depot controller, it runs as a built-in ServiceAccount created during deployment. Ensure the Depot has permissions to:
- Read external GitHub repositories (requires GitHub App authentication via Secrets)
- Create Module and Version resources
- Access configured storage backends

### ServiceAccount for Module and Version Controllers

The Module and Version controllers run as built-in ServiceAccounts created during deployment. They require permissions to:
- Manage Module and Version resources (create, update, patch, get, list, watch, delete)
- Read configuration from ConfigMaps and Secrets
- Manage their own resources within the Kerrareg API group

## Usage

Once deployed and configured, Terraform can reference your modules:

```hcl
module "vpc" {
  source = "kerrareg.example.com/myorg/vpc/aws"
  version = "~> 1.2.0"
  
  name = "production-vpc"
  cidr = "10.0.0.0/16"
}
```

Terraform will automatically:
1. Discover your registry using service discovery
2. Query available versions matching `~> 1.2.0`
3. Select the latest matching version
4. Download and extract the module

## Authenticating with Kerrareg

Kerrareg supports two authentication methods for OpenTofu and Terraform to access your private module registry.

### Method 1: Environment Variables with Kubernetes Access Tokens (Recommended)

Use environment variables to pass access tokens to OpenTofu. This method is also supported in Terraform versions > 1.2 and is simpler than credential helpers while maintaining security through token expiration. All versions of OpenTofu support this method.

**How it works:**
1. Fetch a fresh access token from your Kubernetes cluster
2. Set the token as an environment variable in the format `TF_TOKEN_<KERRAREG_HOSTNAME>`
3. OpenTofu automatically uses the token from the environment variable
4. Tokens expire after a short period, providing automatic credential rotation

**Setup Steps:**

**1. Fetch an access token for the Kubernetes cluster and set the OpenTofu environment variable**:

```bash
# One-liner to set token and run Terraform
export TF_TOKEN_KERRAREG_EXAMPLE_COM=$(aws eks get-token --cluster-name your-eks-cluster-name --region us-west-2 --output json | jq -r '.status.token')
```

> **Note:** This example uses Amazon EKS (Elastic Kubernetes Service). You can adapt the token retrieval command based on your Kubernetes distribution (e.g., `gke-gcloud-auth-plugin` for GKE, or your cluster's native authentication method).

> **Note on Environment Variable Format:** The hostname must be converted to a valid environment variable name:
> - Replace dots (`.`) with underscores (`_`)
> - Convert to uppercase
> - Example: `kerrareg.example.com` → `TF_TOKEN_KERRAREG_EXAMPLE_COM`
> 
> See [OpenTofu Environment Variable Credentials](https://opentofu.org/docs/cli/config/config-file/#environment-variable-credentials) for more details.

**2. Run OpenTofu:**

```bash
# Now run your Terraform commands
tofu init
tofu plan
tofu apply
```

**3. Use in your Terraform configuration:**

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

module "vpc" {
  source = "kerrareg.example.com/myorg/vpc/aws"
  version = "~> 1.2.0"
}
```

### Method 2: Base64-Encoded Kubeconfig in Credentials File

Use a credentials file with a base64-encoded kubeconfig for direct authentication. This can be useful for local development workstations and developing Kerrareg itself.

**Setup Steps:**

**1. Get your kubeconfig and encode it:**

```bash
# Get your current kubeconfig
kubectl config view --raw > /tmp/kubeconfig.yaml

# Base64 encode it
cat /tmp/kubeconfig.yaml | base64 -w 0 > /tmp/kubeconfig.b64

# Display the encoded value
cat /tmp/kubeconfig.b64
```

**2. Create or update `~/.terraform.d/credentials.tfrc.json`:**

```json
{
  "credentials": {
    "kerrareg.example.com": {
      "token": "<BASE64_ENCODED_KUBECONFIG>"
    }
  }
}
```

Replace `<BASE64_ENCODED_KUBECONFIG>` with the output from the previous step.

**3. Set proper permissions:**

```bash
chmod 600 ~/.terraform.d/credentials.tfrc.json
```

**4. Use in your Terraform configuration:**

```hcl
module "vpc" {
  source = "kerrareg.example.com/myorg/vpc/aws"
  version = "~> 1.2.0"
}
```

### Authentication Method Comparison

| Feature | Environment Variables | Kubeconfig File |
|---------|----------------------|-----------------|
| Token Refresh | Automatic (short-lived) | Manual |
| Security | Highest (tokens fetched on-demand) | Good (static credentials) |
| Setup Complexity | Low | Low |
| Best For | Production environments, CI/CD | Development, legacy systems |
| Credential Rotation | Automatic | Requires manual update |
| Long-lived Credentials | No | Yes |
| Terraform Version | > 1.2 | All versions |

### Using Credentials in CI/CD Environments

**GitHub Actions with Environment Variables (Recommended):**

```yaml
name: Apply Infrastructure

on: [push]

jobs:
  apply:
    runs-on: ubuntu-latest
    permissions:
      id-token: write  # Required for OIDC token federation
    steps:
      - uses: actions/checkout@v3

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v2
        with:
          role-to-assume: arn:aws:iam::ACCOUNT_ID:role/github-actions-role
          aws-region: us-west-2

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1

      - name: Fetch Kubernetes token
        run: |
          TOKEN=$(aws eks get-token --cluster-name my-cluster --region us-west-2 --output json | jq -r '.status.token')
          echo "TF_TOKEN_KERRAREG_EXAMPLE_COM=$TOKEN" >> $GITHUB_ENV

      - name: Tofu Init
        run: tofu init

      - name: Tofu Plan
        run: tofu plan
```

## Architecture Details

### Resource Relationships

```
Depot
  └─ ModuleConfig (list)
       └─ Module (created by Depot controller)
            └─ Version (list, created by Module controller)
                 └─ Version status (checksum, sync status, GitHub fetch, storage upload)
```

### Kubernetes API Resources

Kerrareg defines the following Kubernetes Custom Resources:

- **Depot**: Represents a collection of modules with shared configuration
- **Module**: Represents a specific module (namespace/name/system combination)
- **Version**: Represents a specific version of a module or provider

### Event Flow

1. **User creates a Depot** → Depot controller processes ModuleConfigs
2. **Depot controller creates Modules** → Triggered for each ModuleConfig, resolves version constraints
3. **Module controller creates Versions** → One for each requested version, generates metadata
4. **Version controller syncs** (Core work):
   - Fetches module source from GitHub at specified version
   - Prepares module into distribution archive
   - Computes SHA256 checksum
   - Uploads to storage backend (S3, Azure, or filesystem)
   - Updates Version resource with metadata
5. **Server queries Kubernetes API** → Retrieves Version data for registry protocol endpoints
6. **Terraform queries Server** → Gets versions or download URL from server

## Version Constraints

Kerrareg supports all Terraform/OpenTofu version constraint syntax:

- Exact version: `1.2.0`
- Greater than/less than: `>= 1.0.0, < 2.0.0`
- Pessimistic constraint: `~> 1.2.0` (allows bugfix updates only)
- Wildcard: `1.*`
- Multiple constraints: `>= 1.0.0, < 2.0.0, != 1.5.0`

## Project Structure

```
kerrareg/
├── api/v1alpha1/          # Kubernetes CRD definitions
│   ├── types.go           # Resource schemas (Depot, Module, Version, etc.)
│   └── groupversion_info.go
├── pkg/
│   ├── storage/           # Storage backend implementations (used by Version service)
│   │   ├── s3.go
│   │   ├── azure.go
│   │   ├── filesystem.go
│   │   └── storage.go
│   └── github/            # GitHub API interactions
├── services/
│   ├── server/            # Registry Protocol API
│   │   └── main.go
│   ├── version/           # Version controller (CORE) - Fetches from GitHub, stores in backends
│   │   ├── cmd/
│   │   └── config/
│   ├── module/            # Module controller - Orchestrates version creation
│   │   ├── cmd/
│   │   └── config/
│   └── depot/             # Depot controller - Resolves version constraints
│       ├── cmd/
│       └── config/
└── README.md              # This file
```

## Support and Development

For issues, feature requests, or contributions, please refer to the project repository.

## License

See LICENSE file for details.
