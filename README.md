# Kerrareg

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/tonedefdev/kerrareg/blob/main/LICENSE)
[![Helm](https://img.shields.io/badge/Helm_Chart-0.1.0-0F1689?logo=helm&logoColor=white)](https://github.com/tonedefdev/kerrareg/tree/main/chart/kerrareg)

<p align="center">
  <img src="img/kerrareg.png" width="400" />
</p>

A Kubernetes-native, self-hosted module registry that implements the [Module Registry Protocol](https://opentofu.org/docs/internals/module-registry-protocol/). Kerrareg gives organizations complete control over module distribution, versioning, and storage — without relying on the public registry.

Compatible with **OpenTofu** (all versions) and **Terraform** (v1.2+).

## Table of Contents

- [Why Kerrareg?](#why-kerrareg)
- [How It Works](#how-it-works)
- [Architecture](#architecture)
- [Services](#services)
- [Storage Backends](#storage-backends)
- [Getting Started](#getting-started)
- [Local Testing with kind](#local-testing-with-kind)
- [Configuration](#configuration)
- [Usage](#usage)
- [Migrating to Kerrareg](#migrating-to-kerrareg)
- [Authenticating with Kerrareg](#authenticating-with-kerrareg)
- [Kubernetes RBAC](#kubernetes-rbac)
- [API Reference](#api-reference)
- [Version Constraints](#version-constraints)
- [Project Structure](#project-structure)
- [License](#license)

## Why Kerrareg?

There are several open-source Terraform/OpenTofu module registries — [Terrareg](https://github.com/MatthewJohn/terrareg), [Tapir](https://github.com/PacoVK/tapir), [Citizen](https://github.com/outsideris/citizen), and others. They're good projects, but they all share a common challenge: **authentication and authorization are bolted on**. Most require you to stand up a separate database, configure API keys or OAuth flows, and manage user accounts outside of your infrastructure platform.

Kerrareg takes a fundamentally different approach. Instead of reinventing auth, it delegates entirely to Kubernetes — the platform you're already running.

### Security First

| Capability | Kerrareg | Traditional Registries |
|-----------|----------|------------------------|
| **Authentication** | Kubernetes bearer tokens or kubeconfig — no proprietary tokens, no user database | API keys, OAuth, or basic auth requiring a separate identity store |
| **Authorization** | Kubernetes RBAC — namespace-scoped roles control who can read, publish, or admin modules | Custom permission models, often coarse-grained or application-level only |
| **Token Lifecycle** | Short-lived, auto-rotating tokens via `aws eks get-token`, `gcloud auth`, or `az account get-access-token` | Long-lived API keys that must be manually rotated |
| **Audit Trail** | Kubernetes audit logs capture every API call with user identity, verb, and resource | Varies — often requires additional logging configuration |
| **Zero Additional Infrastructure** | No database, no Redis, no external IdP integration | Typically requires PostgreSQL, MySQL, or SQLite plus session management |

Because Kerrareg uses Kubernetes ServiceAccounts and RBAC natively, your existing identity federation (IRSA on EKS, Workload Identity on GKE/AKS, or any OIDC provider) works out of the box. There's nothing extra to configure — if a user or CI pipeline can authenticate to your cluster, they can authenticate to Kerrareg.

### Desired State Reconciliation

Traditional registries are imperative: you push a module version via an API call, and the registry stores it. If something goes wrong — a failed upload, a corrupted archive, a storage outage — you have to detect and remediate it yourself.

Kerrareg is **declarative**. You describe the modules and versions you want, and Kubernetes controllers continuously reconcile toward that desired state:

- **Self-healing:** If a Version resource fails to sync, the controller retries with exponential backoff. Transient GitHub or storage errors resolve automatically.
- **Idempotent:** Applying the same Module manifest twice is a no-op. Controllers only act on drift.
- **Garbage collection:** Remove a version from `spec.versions` and the controller cleans up the Version resource and its storage artifact.
- **Immutability enforcement:** When `immutable: true` is set, the controller validates checksums on every reconciliation — not just at upload time.

This is the same operational model that makes Kubernetes itself reliable, applied to your module registry.

### How Kerrareg Compares

| Feature | Kerrareg | Terrareg | Tapir |
|---------|----------|----------|-------|
| Auth mechanism | Kubernetes RBAC + bearer tokens | API keys + SAML/OpenID Connect | API keys |
| Database required | No (Kubernetes API is the datastore) | Yes (PostgreSQL/MySQL/SQLite) | Yes (MongoDB/PostgreSQL) |
| Deployment model | Helm chart, runs on any Kubernetes cluster | Docker Compose or standalone | Docker Compose or standalone |
| Self-healing | Yes (controller reconciliation loop) | No | No |
| Multi-cloud storage | S3, Azure Blob, GCS, Filesystem | S3, Filesystem | S3, GCS, Filesystem |
| Version discovery | Automatic via Depot (GitHub Releases API) | Manual upload or API push | Manual upload or API push |
| Immutability enforcement | Checksum validated every reconciliation | At upload time only | At upload time only |
| Air-gapped support | Yes (filesystem backend + PVC) | Yes (filesystem) | Limited |

> **In short:** If you're already running Kubernetes, Kerrareg gives you a module registry where security, auth, and operations come free — no extra infrastructure, no extra accounts, no extra attack surface.

## How It Works

When you reference a module in your OpenTofu configuration:

```hcl
module "eks" {
  source  = "kerrareg.defdev.io/kerrareg-system/terraform-aws-eks/aws"
  version = "~> 21.0"
}
```

OpenTofu uses the Module Registry Protocol to:

1. **Discover** the registry API via `/.well-known/terraform.json`
2. **List versions** matching your constraint (`~> 21.0`)
3. **Download** the module archive from the configured storage backend

Kerrareg implements all required protocol endpoints, making it a drop-in replacement for any public or private module registry.

## Architecture

Kerrareg consists of four services running in Kubernetes:

```
┌───────────────────────────────────────────────────────────┐
│                   OpenTofu / Terraform CLI                 │
└────────────────────────┬──────────────────────────────────┘
                         │
                         ▼
┌───────────────────────────────────────────────────────────┐
│               Server (Registry Protocol API)              │
│  • Service Discovery    • List Versions                   │
│  • Download Redirect    • File Serving (S3/Azure/FS)      │
└────────────────────────┬──────────────────────────────────┘
                         │ reads Version + Module resources
       ┌─────────────────┼─────────────────┐
       ▼                 ▼                 ▼
  ┌─────────┐      ┌──────────┐      ┌──────────┐
  │  Depot   │─────▶│  Module  │─────▶│ Version  │
  │controller│      │controller│      │controller│
  └─────────┘      └──────────┘      └────┬─────┘
  discovers          creates               │ fetches & stores
  versions from      Version               ▼
  GitHub             resources        ┌──────────┐
                                      │ Storage  │
                                      │ Backend  │
                                      └──────────┘
                                      S3 │ Azure │ GCS │ FS
```

### Event Flow

1. **Depot controller** watches `Depot` resources, queries GitHub for releases matching version constraints, and creates or updates `Module` resources
2. **Module controller** watches `Module` resources, creates a `Version` resource for each version listed in `spec.versions`, generates unique filenames, and tracks the latest version
3. **Version controller** watches `Version` resources, fetches module source from GitHub, computes SHA256 checksums, and uploads archives to the configured storage backend
4. **Server** handles OpenTofu/Terraform requests, queries Kubernetes for `Module` and `Version` resources, and redirects downloads to the storage backend

## Services

### Version Controller (Core)

The most critical component. It performs the actual work of fetching module source code from GitHub and uploading it to storage.

**Reconciliation loop:**

1. Fetches the module source from GitHub at the specified version/tag
2. Packages the source into a distribution archive (`.tar.gz` or `.zip`)
3. Computes a base64-encoded SHA256 checksum
4. Uploads the archive to the configured storage backend
5. Updates the `Version` resource status with the checksum and sync state

**Immutability:** When `immutable: true` is set in the module config, the Version controller enforces that the stored checksum always matches the archive checksum. This prevents any modification or replacement of a published version.

### Module Controller

Orchestrates version lifecycle management. For each version in `Module.spec.versions`, the Module controller:

- Creates a corresponding `Version` resource with the module configuration
- Generates a UUID7 filename with the appropriate extension (`.zip` or `.tar.gz`)
- Tracks the latest version using semantic version sorting
- Garbage-collects orphaned `Version` resources when versions are removed
- Enforces `versionHistoryLimit` when configured

### Depot Controller

Automates module discovery from GitHub. The Depot controller:

- Queries the GitHub Releases API for each module in `spec.moduleConfigs`
- Resolves version constraints against available releases
- Creates or updates `Module` resources with discovered versions
- Supports configurable polling intervals (`pollingIntervalMinutes`)
- Inherits `global` config (storage, GitHub auth, file format) to each module unless overridden
- Serves as a **migration bridge** — import modules from public registries, then delete the Depot once you transition to CI/CD-driven publishing

### Server

Implements the Module Registry Protocol as an HTTP API. The server authenticates requests using either Kubernetes bearer tokens or base64-encoded kubeconfigs, then queries the Kubernetes API for module and version data.

## Storage Backends

Kerrareg supports four storage backends. Each is configured via the `storageConfig` field on `Depot.spec.global.storageConfig`, `ModuleConfig.storageConfig`, or directly on a `Module.spec.moduleConfig.storageConfig`.

### Amazon S3

**Recommended for production.** Stores module archives in S3 buckets with SHA256 checksum validation.

**CRD Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bucket` | string | Yes | S3 bucket name |
| `region` | string | Yes | AWS region (e.g., `us-west-2`) |
| `key` | string | No | Bucket key prefix (auto-generated by the Module controller) |

**Authentication:** Uses the [AWS SDK v2 default credentials chain](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html). In Kubernetes, this typically means:

- **EKS with IRSA** (recommended): Annotate the Version controller's ServiceAccount with an IAM role ARN
- **Environment variables**: Set `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optionally `AWS_SESSION_TOKEN`
- **EC2 instance profile**: Automatically used when running on EC2/EKS nodes

**Required IAM Permissions:**

```json
{
  "Effect": "Allow",
  "Action": [
    "s3:GetObject",
    "s3:PutObject",
    "s3:DeleteObject",
    "s3:GetObjectAttributes"
  ],
  "Resource": "arn:aws:s3:::your-bucket-name/*"
}
```

**Example Configuration:**

```yaml
storageConfig:
  s3:
    bucket: kerrareg-modules
    region: us-west-2
```

### Azure Blob Storage

**Recommended for production.** Stores module archives in Azure Blob Storage containers with checksum metadata.

**CRD Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `accountName` | string | Yes | Azure Storage Account name |
| `accountUrl` | string | Yes | Storage Account URL (e.g., `https://myaccount.blob.core.windows.net`) |
| `subscriptionID` | string | Yes | Azure subscription ID |
| `resourceGroup` | string | Yes | Resource Group containing the Storage Account |

**Authentication:** Uses [Azure DefaultAzureCredential](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication). In Kubernetes, this typically means:

- **AKS with Workload Identity** (recommended): Configure federated identity credentials on the Version controller's ServiceAccount
- **Managed Identity**: Assign a managed identity to the AKS node pool or pod
- **Environment variables**: Set `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_CLIENT_SECRET`

**Required Azure RBAC Roles:**

- `Storage Blob Data Contributor` on the Storage Account (for read, write, and delete)
- `Reader` on the Storage Account resource (for container metadata operations)

**Example Configuration:**

```yaml
storageConfig:
  azureStorage:
    accountName: kerraregmodules
    accountUrl: https://kerraregmodules.blob.core.windows.net
    subscriptionID: 00000000-0000-0000-0000-000000000000
    resourceGroup: kerrareg-rg
```

### Google Cloud Storage

**CRD Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bucket` | string | Yes | GCS bucket name |

**Authentication:** Uses [Application Default Credentials (ADC)](https://cloud.google.com/docs/authentication/application-default-credentials). In Kubernetes, this typically means:

- **GKE with Workload Identity** (recommended): Bind a Google service account to the Version controller's Kubernetes ServiceAccount
- **Service account key**: Mount a JSON key file and set `GOOGLE_APPLICATION_CREDENTIALS`

**Required GCS Permissions:**

- `storage.objects.create`
- `storage.objects.get`
- `storage.objects.delete`
- `storage.objects.getMetadata` (or the `Storage Object Admin` role)

**Example Configuration:**

```yaml
storageConfig:
  gcs:
    bucket: kerrareg-modules
```

### Local Filesystem

Stores module archives on a shared volume mounted to both the Version controller and the Server pods. Suitable for **development, testing, and air-gapped environments** when paired with a `PersistentVolumeClaim`.

**CRD Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `directoryPath` | string | No | Directory path where modules are stored (must match the container mount path) |

**How it works:** The Helm chart creates a shared volume (either a `PersistentVolumeClaim` or a `hostPath`) and mounts it to both the Version controller and the Server. The Version controller writes module archives to the volume, and the Server reads and serves them.

**Helm Storage Configuration:**

| Value | Default | Description |
|-------|---------|-------------|
| `storage.filesystem.enabled` | `false` | Enable shared volume for filesystem storage |
| `storage.filesystem.mountPath` | `/data/modules` | Mount path inside containers |
| `storage.filesystem.hostPath` | `""` | Use a hostPath volume (for local dev with kind) |
| `storage.filesystem.storageClassName` | `""` | StorageClass for the PVC (must support `ReadWriteMany`) |
| `storage.filesystem.size` | `10Gi` | PVC size |

> **Important:** Set `directoryPath` in your CRD to match the `storage.filesystem.mountPath` Helm value (default `/data/modules`).

**Local Development with kind (hostPath):**

```bash
helm upgrade --install kerrareg chart/kerrareg \
  -n kerrareg-system \
  --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/tmp/kerrareg-modules
```

When using `hostPath`, the chart adds an `initContainer` that runs as root to set ownership of the volume to uid `65532` (the non-root user the containers run as).

**Production with PVC (ReadWriteMany):**

```bash
helm upgrade --install kerrareg chart/kerrareg \
  -n kerrareg-system \
  --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.storageClassName=efs-sc \
  --set storage.filesystem.size=50Gi
```

The PVC requires a StorageClass that supports `ReadWriteMany` (e.g., AWS EFS, Azure Files, NFS).

**Example CRD Configuration:**

```yaml
storageConfig:
  fileSystem:
    directoryPath: /data/modules
```

### Storage Backend Comparison

| Feature | Amazon S3 | Azure Blob | Google Cloud Storage | Filesystem |
|---------|-----------|------------|---------------------|------------|
| Production Ready | Yes | Yes | Yes | With PVC |
| Checksum Validation | SHA256 (native) | SHA256 (metadata) | SHA256 (metadata) | SHA256 (computed) |
| Authentication | AWS SDK v2 defaults | DefaultAzureCredential | ADC | None |
| Server Download Route | Yes | Yes | Yes | Yes |
| Shared Volume Required | No | No | No | Yes (PVC or hostPath) |

## Getting Started

### Prerequisites

- Kubernetes v1.16+
- Helm 3.0+
- `kubectl` configured to access your cluster
- A supported storage backend (S3 bucket, Azure Storage Account, or local filesystem)
- *(Optional)* A GitHub App for authenticated API access

### Install CRDs

CRDs must be installed before deploying the Helm chart:

```bash
kubectl apply -f chart/kerrareg/crds/
```

### Install with Helm

```bash
helm upgrade --install kerrareg chart/kerrareg \
  -n kerrareg-system \
  --create-namespace
```

To customize values:

```bash
helm upgrade --install kerrareg chart/kerrareg \
  -n kerrareg-system \
  --create-namespace \
  --set global.image.tag=v0.1.0 \
  --set server.service.type=ClusterIP \
  --set depot.enabled=false
```

Or use a values file:

```bash
helm upgrade --install kerrareg chart/kerrareg \
  -n kerrareg-system \
  --create-namespace \
  -f my-values.yaml
```

### Helm Chart Values

#### Global

| Value | Default | Description |
|-------|---------|-------------|
| `global.namespace` | `kerrareg-system` | Namespace for all resources |
| `global.imagePullPolicy` | `IfNotPresent` | Image pull policy |
| `global.image.tag` | `dev` | Image tag for all services |

#### Server

| Value | Default | Description |
|-------|---------|-------------|
| `server.enabled` | `true` | Deploy the server |
| `server.replicaCount` | `1` | Number of replicas |
| `server.anonymousAuth` | `false` | Use the server's service account for unauthenticated module access (see note below) |
| `server.useBearerToken` | `true` | Use bearer token auth instead of kubeconfig |
| `server.image.repository` | `ghcr.io/tonedefdev/kerrareg/server` | Server image |
| `server.service.type` | `LoadBalancer` | Service type |
| `server.service.port` | `80` | Service port |
| `server.service.targetPort` | `8080` | Container port |
| `server.tls.enabled` | `false` | Enable TLS on the server |
| `server.tls.certPath` | `/etc/tls/tls.crt` | Path to TLS certificate |
| `server.tls.keyPath` | `/etc/tls/tls.key` | Path to TLS key |
| `server.ingress.enabled` | `false` | Enable Kubernetes Ingress |
| `server.ingress.istio.enabled` | `true` | Enable Istio VirtualService |
| `server.ingress.istio.hosts` | `[kerrareg.defdev.io]` | Istio VirtualService hosts |
| `server.resources.requests.cpu` | `100m` | CPU request |
| `server.resources.requests.memory` | `128Mi` | Memory request |
| `server.resources.limits.cpu` | `500m` | CPU limit |
| `server.resources.limits.memory` | `512Mi` | Memory limit |
| `server.nodeSelector` | `{}` | Node selector |
| `server.tolerations` | `[]` | Tolerations |
| `server.affinity` | `{}` | Affinity rules |
| `server.podDisruptionBudget.enabled` | `false` | Enable PDB |
| `server.podDisruptionBudget.minAvailable` | `2` | Minimum available pods |
| `server.ingress.enabled` | `false` | Enable Kubernetes Ingress |
| `server.ingress.hosts` | see values.yaml | Standard Ingress host/path rules |
| `server.ingress.tls` | `[]` | Standard Ingress TLS configuration |

#### Controllers

These values apply to `version`, `module`, and `depot` independently:

| Value | Default | Description |
|-------|---------|-------------|
| `<service>.enabled` | `true` | Deploy the controller |
| `<service>.replicaCount` | `1` | Number of replicas |
| `<service>.image.repository` | `ghcr.io/tonedefdev/kerrareg/<service>-controller` | Image repository |
| `<service>.resources.requests.cpu` | `100m` | CPU request |
| `<service>.resources.requests.memory` | `128Mi` | Memory request |
| `<service>.resources.limits.cpu` | `500m` | CPU limit |
| `<service>.resources.limits.memory` | `512Mi` | Memory limit |
| `<service>.nodeSelector` | `{}` | Node selector |
| `<service>.tolerations` | `[]` | Tolerations |
| `<service>.affinity` | `{}` | Affinity rules |

#### Service Account & RBAC

| Value | Default | Description |
|-------|---------|-------------|
| `serviceAccount.create` | `true` | Create service accounts |
| `serviceAccount.annotations` | `{}` | Annotations (use for IRSA/Workload Identity) |
| `rbac.create` | `true` | Create RBAC roles and bindings |

#### Storage

| Value | Default | Description |
|-------|---------|-------------|
| `storage.filesystem.enabled` | `false` | Enable shared volume for filesystem storage |
| `storage.filesystem.mountPath` | `/data/modules` | Mount path inside containers |
| `storage.filesystem.hostPath` | `""` | Use a hostPath volume (for local dev with kind) |
| `storage.filesystem.storageClassName` | `""` | StorageClass for PVC (requires `ReadWriteMany`) |
| `storage.filesystem.size` | `10Gi` | PVC storage size |

### Build from Source (Alternative)

If you prefer to build container images yourself:

```bash
# Build all services for linux/arm64
make build

# Load into a kind cluster
make load

# Or build and load in one step
make deploy
```

**Additional Makefile targets:**

| Target | Description |
|--------|-------------|
| `make build` | Build all container images |
| `make load` | Load all images into the kind cluster |
| `make deploy` | Build and load all images |
| `make service NAME=server` | Build and load a single service |
| `make restart` | Restart all deployments in `kerrareg-system` |
| `make redeploy` | Build, load, and restart all services |
| `make kind-restart` | Full cluster recreation with Istio, TLS, gateway, and Helm deploy |

**Configurable variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `PLATFORM` | `linux/arm64` | Target platform for container builds |
| `KIND_CLUSTER` | `kind` | Name of the kind cluster |
| `TAG` | `dev` | Image tag for all services |
| `REGISTRY` | `ghcr.io/tonedefdev/kerrareg` | Container registry prefix |

## Local Testing with kind

The fastest way to try Kerrareg is with a local [kind](https://kind.sigs.k8s.io/) cluster using the filesystem storage backend and `hostPath`. This avoids any cloud provider setup — no S3 bucket, no Azure Storage Account, no credentials. You'll have a fully functional registry in minutes.

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm 3](https://helm.sh/docs/intro/install/)
- [cloud-provider-kind](https://github.com/kubernetes-sigs/cloud-provider-kind) (for LoadBalancer support)
- [OpenTofu](https://opentofu.org/docs/intro/install/) or [Terraform](https://developer.hashicorp.com/terraform/install)

### Step 1: Create the Cluster

```bash
kind create cluster --name kerrareg
```

### Step 2: Install Istio (Ingress)

Kerrareg works with any Kubernetes ingress controller — NGINX, Traefik, Contour, HAProxy, or the built-in Gateway API. This guide uses [Istio](https://istio.io/) because it provides automatic mTLS between services, fine-grained traffic policies, and TLS termination at the gateway — giving you defense-in-depth even for local testing. If you prefer a different ingress controller, set `server.ingress.istio.enabled: false` in your Helm values and configure a standard [Kubernetes Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) instead.

Add the Istio Helm repo and install:

```bash
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update

helm install istio-base istio/base -n istio-system --create-namespace --wait
helm install istiod istio/istiod -n istio-system --wait

kubectl create namespace istio-ingress
helm install istio-ingress istio/gateway -n istio-ingress --wait
```

### Step 3: Configure TLS and the Gateway

OpenTofu and Terraform **require** HTTPS when communicating with module registries — there is no way to bypass this. You must generate a TLS certificate for your registry hostname.

For local testing, create a self-signed certificate with `openssl`:

```bash
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout tls.key -out tls.crt \
  -days 365 \
  -subj "/CN=kerrareg.defdev.io" \
  -addext "subjectAltName=DNS:kerrareg.defdev.io"
```

Create the Kubernetes Secret in the `istio-ingress` namespace (where the gateway reads it):

```bash
kubectl create secret tls istio-ingress-gateway-certs \
  -n istio-ingress \
  --cert=tls.crt \
  --key=tls.key
```

Then trust the certificate on your machine so OpenTofu/Terraform accepts it:

**macOS:**

```bash
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain tls.crt
```

**Linux:**

```bash
sudo cp tls.crt /usr/local/share/ca-certificates/kerrareg.crt
sudo update-ca-certificates
```

Apply the Istio Gateway resource:

```bash
kubectl apply -f chart/kerrareg/gateway.yaml
```

Add a `/etc/hosts` entry so the hostname resolves locally (you'll update the IP in Step 6):

```bash
echo "127.0.0.1 kerrareg.defdev.io" | sudo tee -a /etc/hosts
```

### Step 4: Build and Load Images

Build all container images and load them into the kind cluster:

```bash
make deploy
```

> **Apple Silicon users:** The default `PLATFORM` is `linux/arm64`. For Intel Macs or Linux, run `make deploy PLATFORM=linux/amd64`.

### Step 5: Install with Helm

Deploy Kerrareg with filesystem storage using `hostPath`, anonymous auth enabled (no credentials needed for testing), and the Istio ingress route:

```bash
helm upgrade --install kerrareg chart/kerrareg \
  -n kerrareg-system --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/tmp/kerrareg-modules \
  --set server.anonymousAuth=true \
  --set server.useBearerToken=false \
  --wait
```

Verify all pods are running:

```bash
kubectl get pods -n kerrareg-system
```

### Step 6: Expose the Gateway

kind doesn't natively support `LoadBalancer` services. Use [cloud-provider-kind](https://github.com/kubernetes-sigs/cloud-provider-kind) to assign an external IP:

```bash
sudo cloud-provider-kind &
```

Wait a few seconds, then find the external IP:

```bash
kubectl get svc -n istio-ingress
```

Update your `/etc/hosts` entry if the IP isn't `127.0.0.1`:

```bash
# Replace <EXTERNAL-IP> with the actual IP from the command above
sudo sed -i '' "s/.*kerrareg.defdev.io/<EXTERNAL-IP> kerrareg.defdev.io/" /etc/hosts
```

### Step 7: Apply CRDs and Create a Test Module

Install the CRDs and create a `Module` resource that pulls a public module from GitHub using filesystem storage:

```bash
kubectl apply -f chart/kerrareg/crds/
```

```yaml
cat <<EOF | kubectl apply -f -
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
    fileFormat: tar
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: "21.10.1"
EOF
```

Watch the Version resource sync:

```bash
kubectl get versions.kerrareg.io -n kerrareg-system -w
```

Once the status shows `Synced`, the module archive has been fetched from GitHub and stored on the local filesystem.

### Step 8: Use the Registry with OpenTofu

Create a test configuration:

```hcl
# main.tf
module "eks" {
  source  = "kerrareg.defdev.io/kerrareg-system/terraform-aws-eks/aws"
  version = "21.10.1"
}
```

Since `anonymousAuth` is enabled, no credentials are needed. If you trusted the self-signed certificate in Step 3, just run:

```bash
tofu init -backend=false
```

You should see OpenTofu download the module from your local Kerrareg instance.

### Step 9: (Optional) Test with Authentication

To test Kerrareg's Kubernetes-native auth, redeploy with `anonymousAuth` disabled and bearer token auth enabled:

```bash
helm upgrade kerrareg chart/kerrareg \
  -n kerrareg-system \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/tmp/kerrareg-modules \
  --set server.anonymousAuth=false \
  --set server.useBearerToken=true \
  --wait
```

Create a ServiceAccount and bind it to a read-only role:

```bash
kubectl create serviceaccount test-user -n kerrareg-system

kubectl create role kerrareg-reader -n kerrareg-system \
  --resource=modules.kerrareg.io,versions.kerrareg.io \
  --verb=get,list,watch

kubectl create rolebinding test-user-reader -n kerrareg-system \
  --role=kerrareg-reader \
  --serviceaccount=kerrareg-system:test-user
```

Generate a short-lived token and set it as the registry credential:

```bash
export TF_TOKEN_KERRAREG_DEFDEV_IO=$(kubectl create token test-user -n kerrareg-system)
tofu init -backend=false
```

OpenTofu sends the bearer token to Kerrareg, which forwards it to the Kubernetes API for authentication and RBAC authorization. This is the same flow used in production — no separate user database or API keys required.

### Step 10: (Optional) Test with a Depot

To test automatic version discovery from GitHub:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: kerrareg.io/v1alpha1
kind: Depot
metadata:
  name: test-depot
  namespace: kerrareg-system
spec:
  global:
    moduleConfig:
      fileFormat: tar
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  moduleConfigs:
    - name: terraform-aws-eks
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 21.10.0, < 21.12.0"
EOF
```

The Depot controller queries GitHub releases, creates `Module` resources for matching versions, and the pipeline syncs them to local storage automatically.

### Cleanup

```bash
kind delete cluster --name kerrareg
sudo sed -i '' '/kerrareg.defdev.io/d' /etc/hosts
```

## Configuration

### GitHub Authentication

For private repositories and to avoid GitHub API rate limits, create a GitHub App and store its credentials as a Kubernetes Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kerrareg-github-application-secret
  namespace: kerrareg-system
type: Opaque
data:
  githubAppID: <base64-encoded-app-id>
  githubInstallID: <base64-encoded-install-id>
  githubPrivateKey: <base64-encoded-private-key>
```

> **Important:** The private key must be base64-encoded **before** being added to the Secret's `data` field (i.e., it is double base64-encoded: once for the PEM content, once by Kubernetes). The controller decodes both layers automatically.

Then enable authenticated access in your module config:

```yaml
githubClientConfig:
  useAuthenticatedClient: true
```

### TLS Configuration

#### Direct TLS on the Server

Set `server.tls.enabled: true` in your Helm values and provide a TLS Secret named `kerrareg-tls`:

```yaml
server:
  tls:
    enabled: true
    certPath: /etc/tls/tls.crt
    keyPath: /etc/tls/tls.key
```

> **Note:** When TLS is enabled, the server listens on port `443` instead of `8080`. Ensure your Service `targetPort` and any probes are updated accordingly.

> **Note on `anonymousAuth`:** When enabled, the server uses its own ServiceAccount to query the Kubernetes API for Module and Version resources. No client credentials are required. The server's ClusterRole only permits reading `modules` and `versions`, so anonymous users cannot create or modify resources.

#### TLS via Istio Ingress Gateway

For TLS termination at the Istio ingress gateway, enable the Istio VirtualService and create a Gateway resource. The chart's VirtualService references the gateway `istio-ingress/istio-ingress-gateway` by default. See [chart/kerrareg/gateway.yaml](chart/kerrareg/gateway.yaml) for an example, and store your TLS certificate as a Secret in the `istio-ingress` namespace:

```yaml
server:
  ingress:
    istio:
      enabled: true
      hosts:
        - kerrareg.defdev.io
```

## Usage

### Pull-Based Workflow: Using the Depot

Use the Depot for public or externally maintained modules. The Depot automatically discovers versions from GitHub and manages the full lifecycle.

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
  pollingIntervalMinutes: 60
```

This Depot will:

1. Query the `terraform-aws-modules/terraform-aws-eks` and `azure/terraform-azurerm-aks` GitHub repositories for releases
2. Filter releases matching the version constraints
3. Create `Module` resources for each module
4. The Module controller creates `Version` resources for each discovered version
5. The Version controller fetches archives from GitHub and uploads them to the S3 bucket
6. Re-check GitHub for new releases every 60 minutes

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

### Push-Based Workflow: CI/CD Pipeline

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

      - name: Setup kubeconfig
        run: |
          mkdir -p ~/.kube
          echo "${{ secrets.KERRAREG_KUBECONFIG }}" | base64 -d > ~/.kube/config
          chmod 600 ~/.kube/config

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

### Adding Versions to an Existing Module

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

### Force Re-Sync

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

### Migrating to Kerrareg

> **Key feature:** The Depot is designed as a migration tool, not just an ongoing automation. If you're moving from a public registry, a private registry, or any GitHub-hosted module source to Kerrareg, the Depot handles the heavy lifting — discovering versions, downloading archives, and populating your storage backend. Once everything is synced, simply delete the Depot and switch to the [push-based CI/CD workflow](#push-based-workflow-cicd-pipeline). Deleting a Depot **does not** delete the Modules it created, so your registry stays fully intact.

Use the Depot to bulk-import existing modules into Kerrareg:

1. Create a `Depot` with broad version constraints (e.g., `">= 0.0.0"`) to pull in the full release history
2. Wait for all versions to sync (check `Module` and `Version` status resources)
3. Update your OpenTofu/Terraform configurations to source modules from Kerrareg
4. Delete the Depot — all `Module` and `Version` resources remain untouched
5. Going forward, publish new versions via your CI/CD pipeline using the [push-based workflow](#push-based-workflow-cicd-pipeline)

This pattern lets you adopt Kerrareg incrementally without disrupting existing workflows. The Depot bridges the gap between your current registry and a fully self-hosted solution.

### Consuming Modules

Once modules are synced, reference them in your OpenTofu or Terraform configuration:

```hcl
module "eks" {
  source  = "kerrareg.defdev.io/kerrareg-system/terraform-aws-eks/aws"
  version = "~> 21.0"
}

module "aks" {
  source  = "kerrareg.defdev.io/kerrareg-system/terraform-azurerm-aks/azurerm"
  version = ">= 10.0.0"
}
```

The source format is `<registry-host>/<namespace>/<name>/<provider>`, where `<namespace>` is the Kubernetes namespace where the `Module` resource lives.

## Authenticating with Kerrareg

Kerrareg supports two authentication methods. Both leverage Kubernetes credentials — either a short-lived bearer token or a base64-encoded kubeconfig.

### Method 1: Environment Variables (Recommended)

Use an environment variable to pass a Kubernetes access token. OpenTofu (all versions) and Terraform (v1.2+) support this method.

The variable name is derived from the registry hostname: replace dots with underscores and convert to uppercase.

`kerrareg.defdev.io` → `TF_TOKEN_KERRAREG_DEFDEV_IO`

**Amazon EKS:**

```bash
export TF_TOKEN_KERRAREG_DEFDEV_IO=$(aws eks get-token \
  --cluster-name my-cluster \
  --region us-west-2 \
  --output json | jq -r '.status.token')

tofu init
tofu plan
```

**Google GKE:**

```bash
export TF_TOKEN_KERRAREG_DEFDEV_IO=$(gcloud auth print-access-token)
```

**Azure AKS:**

```bash
export TF_TOKEN_KERRAREG_DEFDEV_IO=$(az account get-access-token \
  --resource 6dae42f8-4368-4678-94ff-3960e28e3630 \
  --query accessToken -o tsv)
```

Tokens are short-lived and automatically rotate, making this the most secure option.

### Method 2: Base64-Encoded Kubeconfig

For development or environments where environment variables are not practical, encode your kubeconfig and store it in a credentials file.

> **Note:** This method requires `server.useBearerToken: false` in your Helm values.

**1. Encode your kubeconfig:**

```bash
kubectl config view --raw | base64 | tr -d '\n' > /tmp/kubeconfig.b64
```

**2. Create `~/.terraform.d/credentials.tfrc.json`:**

```json
{
  "credentials": {
    "kerrareg.defdev.io": {
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
          echo "TF_TOKEN_KERRAREG_DEFDEV_IO=$TOKEN" >> $GITHUB_ENV

      - run: tofu init
      - run: tofu plan
```

## Kubernetes RBAC

The Helm chart creates ServiceAccounts and RBAC resources for each controller automatically when `rbac.create: true` (the default).

### Controller Permissions

| Controller | Resource | Verbs |
|-----------|----------|-------|
| Depot | `depots` | create, delete, get, list, patch, update, watch |
| Depot | `depots/finalizers` | update |
| Depot | `depots/status` | get, patch, update |
| Depot | `modules` | create, get, list, patch, update, watch |
| Depot | `secrets` | get, list, watch |
| Module | `modules` | create, delete, get, list, patch, update, watch |
| Module | `modules/finalizers` | update |
| Module | `modules/status` | get, patch, update |
| Module | `versions` | create, get, list, patch, update, watch |
| Version | `modules` | get, list, watch |
| Version | `modules/status` | get, patch, update |
| Version | `versions` | create, delete, get, list, patch, update, watch |
| Version | `versions/finalizers` | update |
| Version | `versions/status` | get, patch, update |
| Version | `secrets` | get, list, watch |
| Server | `versions` | get, list, watch |
| Server | `modules` | get, list |

### CI/CD ServiceAccount

For CI/CD pipelines that need to create or update `Module` resources:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kerrareg-ci-publisher
  namespace: kerrareg-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kerrareg-module-publisher
  namespace: kerrareg-system
rules:
  - apiGroups: ["kerrareg.io"]
    resources: ["modules"]
    verbs: ["create", "update", "patch", "get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kerrareg-ci-publisher-binding
  namespace: kerrareg-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kerrareg-module-publisher
subjects:
  - kind: ServiceAccount
    name: kerrareg-ci-publisher
    namespace: kerrareg-system
```

## API Reference

### Service Discovery

```
GET /.well-known/terraform.json
```

**Response:**

```json
{
  "modules.v1": "/kerrareg/modules/v1/"
}
```

### List Module Versions

```
GET /kerrareg/modules/v1/{namespace}/{name}/{system}/versions
```

Returns all available versions of a module. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Module resource |
| `name` | Module name |
| `system` | Provider (e.g., `aws`, `azurerm`) |

### Download Module

```
GET /kerrareg/modules/v1/{namespace}/{name}/{system}/{version}/download
```

Returns `204 No Content` with an `X-Terraform-Get` header pointing to the storage-specific download URL. Requires authentication.

### Storage Download Endpoints

These endpoints are called by OpenTofu/Terraform after receiving the `X-Terraform-Get` redirect. They validate the SHA256 checksum and stream the module archive.

```
GET /kerrareg/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}?fileChecksum={checksum}
GET /kerrareg/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}?fileChecksum={checksum}
GET /kerrareg/modules/v1/download/gcs/{bucket}/{name}/{fileName}?fileChecksum={checksum}
GET /kerrareg/modules/v1/download/fileSystem/{directory}/{name}/{fileName}?fileChecksum={checksum}
```

## Version Constraints

Kerrareg supports all standard OpenTofu/Terraform version constraint syntax:

| Syntax | Example | Meaning |
|--------|---------|---------|
| Exact | `1.2.0` | Only version 1.2.0 |
| Comparison | `>= 1.0.0, < 2.0.0` | Any 1.x version |
| Pessimistic | `~> 1.2.0` | >= 1.2.0, < 1.3.0 (bugfixes only) |
| Pessimistic (minor) | `~> 1.2` | >= 1.2.0, < 2.0.0 |
| Exclusion | `>= 1.0.0, != 1.5.0` | Any 1.x except 1.5.0 |

## Project Structure

```
kerrareg/
├── api/v1alpha1/              # CRD type definitions
│   ├── types.go               # Depot, Module, Version, StorageConfig schemas
│   └── groupversion_info.go   # API group registration
├── chart/kerrareg/            # Helm chart
│   ├── Chart.yaml
│   ├── values.yaml
│   ├── crds/                  # CRD manifests
│   └── templates/             # Deployment, RBAC, Service templates
├── pkg/
│   ├── github/                # GitHub API client (App auth, archive fetching)
│   │   └── github.go
│   └── storage/               # Storage backend implementations
│       ├── storage.go         # Storage interface definition
│       ├── aws.go             # Amazon S3
│       ├── azure.go           # Azure Blob Storage
│       ├── gcp.go             # Google Cloud Storage
│       ├── filesystem.go      # Local filesystem
│       └── types/             # StorageObjectInput, StorageMethod
├── services/
│   ├── server/                # Registry Protocol API (HTTP server)
│   ├── version/               # Version controller (core — fetch & store)
│   ├── module/                # Module controller (version lifecycle)
│   └── depot/                 # Depot controller (GitHub discovery)
├── Makefile                   # Build, load, deploy targets
└── go.work                    # Go workspace (multi-module)
```

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
