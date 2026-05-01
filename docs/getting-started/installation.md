# Installation


## Prerequisites

- Kubernetes v1.16+
- Helm 3.0+
- `kubectl` configured to access your cluster
- A supported storage backend (S3 bucket, Azure Storage Account, or local filesystem)
- *(Optional)* A GitHub App for authenticated API access

## Install CRDs

CRDs must be installed before deploying the Helm chart:

```bash
kubectl apply -f chart/kerrareg/crds/
```

## Install with Helm

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

## Helm Chart Values

## Global

| Value | Default | Description |
|-------|---------|-------------|
| `global.namespace` | `kerrareg-system` | Namespace for all resources |
| `global.imagePullPolicy` | `IfNotPresent` | Image pull policy |
| `global.image.tag` | `dev` | Image tag for all services |

## Server

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

## Controllers

These values apply to `version`, `module`, `depot`, and `provider` independently:

| Value | Default | Description |
|-------|---------|-------------|
| `<service>.enabled` | `true` (`provider`: `false`) | Deploy the controller |
| `<service>.replicaCount` | `1` | Number of replicas |
| `<service>.image.repository` | `ghcr.io/tonedefdev/kerrareg/<service>-controller` | Image repository |
| `<service>.image.tag` | `""` | Overrides `global.image.tag` when set |
| `<service>.resources.requests.cpu` | `100m` | CPU request |
| `<service>.resources.requests.memory` | `128Mi` | Memory request |
| `<service>.resources.limits.cpu` | `500m` | CPU limit |
| `<service>.resources.limits.memory` | `512Mi` | Memory limit |
| `<service>.nodeSelector` | `{}` | Node selector |
| `<service>.tolerations` | `[]` | Tolerations |
| `<service>.affinity` | `{}` | Affinity rules |

!!! note
    The provider controller is disabled by default (`provider.enabled: false`). Enable it explicitly when you are ready to sync provider binaries — provider archives can be several hundred megabytes each.

## GPG Signing (Providers)

The server signs `SHA256SUMS` files for provider packages using a GPG key you supply. OpenTofu verifies this signature as part of the [Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol). You must create a Kubernetes Secret with the following keys and reference it via `server.gpg.secretName`:

| Secret Key | Description |
|-----------|-------------|
| `KERRAREG_PROVIDER_GPG_KEY_ID` | Short or long hex key ID of the signing key |
| `KERRAREG_PROVIDER_GPG_ASCII_ARMOR` | ASCII-armored public key block (included in the API response so OpenTofu can verify) |
| `KERRAREG_PROVIDER_GPG_PRIVATE_KEY_BASE64` | Base64-encoded ASCII-armored private key (used by the server to sign `SHA256SUMS`) |

| Value | Default | Description |
|-------|---------|-------------|
| `server.gpg.secretName` | `""` | Name of the Kubernetes Secret containing GPG signing credentials |

See [GPG Signing for Providers](../configuration/gpg.md) in the Configuration section for full setup instructions.

## Service Account & RBAC

| Value | Default | Description |
|-------|---------|-------------|
| `serviceAccount.create` | `true` | Create service accounts |
| `serviceAccount.annotations` | `{}` | Annotations (use for IRSA/Workload Identity) |
| `rbac.create` | `true` | Create RBAC roles and bindings |
| `rbac.scopeToNamespace` | `false` | Use namespace-scoped Role/RoleBinding instead of ClusterRole/ClusterRoleBinding |

## Storage

| Value | Default | Description |
|-------|---------|-------------|
| `storage.filesystem.enabled` | `false` | Enable shared volume for filesystem storage |
| `storage.filesystem.mountPath` | `/data/modules` | Mount path inside containers |
| `storage.filesystem.hostPath` | `""` | Use a hostPath volume (for local dev with kind) |
| `storage.filesystem.storageClassName` | `""` | StorageClass for PVC (requires `ReadWriteMany`) |
| `storage.filesystem.size` | `10Gi` | PVC storage size |

## Build from Source (Alternative)

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
| `make kind-restart` | Full cluster recreation with Istio, TLS, gateway, and Helm deploy (for production-like local setup) |

**Configurable variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `PLATFORM` | `linux/arm64` | Target platform for container builds |
| `KIND_CLUSTER` | `kind` | Name of the kind cluster |
| `TAG` | `dev` | Image tag for all services |
| `REGISTRY` | `ghcr.io/tonedefdev/kerrareg` | Container registry prefix |
