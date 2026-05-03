# OpenDepot Helm Chart

A Helm chart for deploying OpenDepot, a self-hosted OpenTofu/Terraform Module and Provider Registry, on Kubernetes.

## Overview

This chart deploys five OpenDepot services:

| Service | Default | Description |
|---------|---------|-------------|
| **Version Controller** | Enabled | Fetches module source from GitHub and stores it in the configured backend |
| **Module Controller** | Enabled | Orchestrates `Version` resource creation and lifecycle |
| **Depot Controller** | Enabled | Pulls modules from external sources based on version constraints |
| **Provider Controller** | Disabled | Mirrors provider binaries from the HashiCorp Releases API |
| **Server** | Enabled | Implements the Terraform Module Registry Protocol API |

All images are pulled from `ghcr.io/tonedefdev/opendepot/` and default to the tag set in `global.image.tag`.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.0+
- OpenDepot CRDs installed (see below)

## Installing CRDs

CRDs are bundled in the `crds/` directory of this chart and are applied automatically on `helm install`. To install them manually:

```bash
kubectl apply -f chart/opendepot/crds/
```

To remove CRDs (this deletes all OpenDepot custom resources):

```bash
kubectl delete -f chart/opendepot/crds/
```

## Quick Start

```bash
# Minimal install — uses opendepot-system namespace by default
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace

# Override the global image tag
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set global.image.tag=1.0.0
```

## Configuration Reference

### Global

| Parameter | Default | Description |
|-----------|---------|-------------|
| `global.namespace` | `opendepot-system` | Kubernetes namespace for all resources |
| `global.imagePullPolicy` | `IfNotPresent` | Image pull policy applied to all containers |
| `global.image.tag` | `1.0.0-rc1` | Default image tag used by all services unless overridden per-service |

### Version Controller

| Parameter | Default | Description |
|-----------|---------|-------------|
| `version.enabled` | `true` | Deploy the Version controller |
| `version.replicaCount` | `1` | Number of replicas |
| `version.image.repository` | `ghcr.io/tonedefdev/opendepot/version-controller` | Image repository |
| `version.image.tag` | `""` | Per-service tag override (falls back to `global.image.tag`) |
| `version.resources` | `100m / 128Mi` req, `512Mi` limit | Resource requests and limits |
| `version.nodeSelector` | `{}` | Node selector |
| `version.tolerations` | `[]` | Tolerations |
| `version.affinity` | `{}` | Affinity rules |

### Module Controller

| Parameter | Default | Description |
|-----------|---------|-------------|
| `module.enabled` | `true` | Deploy the Module controller |
| `module.replicaCount` | `1` | Number of replicas |
| `module.image.repository` | `ghcr.io/tonedefdev/opendepot/module-controller` | Image repository |
| `module.image.tag` | `""` | Per-service tag override |
| `module.resources` | `100m / 128Mi` req, `512Mi` limit | Resource requests and limits |
| `module.nodeSelector` | `{}` | Node selector |
| `module.tolerations` | `[]` | Tolerations |
| `module.affinity` | `{}` | Affinity rules |

### Depot Controller

| Parameter | Default | Description |
|-----------|---------|-------------|
| `depot.enabled` | `true` | Deploy the Depot controller |
| `depot.replicaCount` | `1` | Number of replicas |
| `depot.image.repository` | `ghcr.io/tonedefdev/opendepot/depot-controller` | Image repository |
| `depot.image.tag` | `""` | Per-service tag override |
| `depot.resources` | `100m / 128Mi` req, `512Mi` limit | Resource requests and limits |
| `depot.nodeSelector` | `{}` | Node selector |
| `depot.tolerations` | `[]` | Tolerations |
| `depot.affinity` | `{}` | Affinity rules |

### Provider Controller

The Provider controller is disabled by default. Enable it to mirror provider binaries from the HashiCorp Releases API into your registry.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `provider.enabled` | `false` | Deploy the Provider controller |
| `provider.replicaCount` | `1` | Number of replicas |
| `provider.image.repository` | `ghcr.io/tonedefdev/opendepot/provider-controller` | Image repository |
| `provider.image.tag` | `""` | Per-service tag override |
| `provider.resources` | `100m / 128Mi` req, `512Mi` limit | Resource requests and limits |
| `provider.nodeSelector` | `{}` | Node selector |
| `provider.tolerations` | `[]` | Tolerations |
| `provider.affinity` | `{}` | Affinity rules |

### Server

| Parameter | Default | Description |
|-----------|---------|-------------|
| `server.enabled` | `true` | Deploy the Server |
| `server.replicaCount` | `1` | Number of replicas |
| `server.anonymousAuth` | `false` | Allow unauthenticated requests (`--anonymous-auth` flag) |
| `server.useBearerToken` | `true` | Require bearer token authentication (`--use-bearer-token` flag) |
| `server.image.repository` | `ghcr.io/tonedefdev/opendepot/server` | Image repository |
| `server.image.tag` | `""` | Per-service tag override |
| `server.service.type` | `LoadBalancer` | Kubernetes service type |
| `server.service.port` | `80` | Exposed service port |
| `server.service.targetPort` | `8080` | Container port |
| `server.resources` | `100m / 128Mi` req, `512Mi` limit | Resource requests and limits |
| `server.nodeSelector` | `{}` | Node selector |
| `server.tolerations` | `[]` | Tolerations |
| `server.affinity` | `{}` | Affinity rules |

#### Server — TLS

| Parameter | Default | Description |
|-----------|---------|-------------|
| `server.tls.enabled` | `false` | Mount TLS certificate and pass paths to the server binary |
| `server.tls.certPath` | `/etc/tls/tls.crt` | Path inside the container for the TLS certificate |
| `server.tls.keyPath` | `/etc/tls/tls.key` | Path inside the container for the TLS private key |

When TLS is enabled the chart mounts a `Secret` named `opendepot-tls` as a volume at the directory containing `certPath`. Create the secret before installing:

```bash
kubectl create secret tls opendepot-tls \
  --cert=path/to/cert.crt \
  --key=path/to/key.key \
  -n opendepot-system
```

#### Server — Ingress

| Parameter | Default | Description |
|-----------|---------|-------------|
| `server.ingress.enabled` | `false` | Create a standard Kubernetes `Ingress` resource |
| `server.ingress.className` | `""` | `ingressClassName` field |
| `server.ingress.annotations` | `{}` | Annotations added to the Ingress |
| `server.ingress.hosts` | `[{host: opendepot.defdev.io, paths: [{path: /, pathType: Prefix}]}]` | Host/path routing rules |
| `server.ingress.tls` | `[]` | TLS blocks for the Ingress |

#### Server — Istio

| Parameter | Default | Description |
|-----------|---------|-------------|
| `server.ingress.istio.enabled` | `false` | Create an Istio `VirtualService` instead of (or alongside) an Ingress |
| `server.ingress.istio.hosts` | `[opendepot.defdev.io]` | Hosts for the VirtualService |
| `server.ingress.istio.gateway` | `istio-ingress/istio-ingress-gateway` | Gateway reference (`namespace/name`) |

#### Server — GPG (Provider Signing)

To enable GPG signing of provider binaries served by the registry, create a `Secret` containing the required environment variables and reference it here:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `server.gpg.secretName` | `""` | Name of a `Secret` whose keys are injected as environment variables |

The secret must contain the following keys:

| Key | Description |
|-----|-------------|
| `OPENDEPOT_PROVIDER_GPG_KEY_ID` | GPG key ID |
| `OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR` | ASCII-armored public key |
| `OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64` | Base64-encoded private key |
| `OPENDEPOT_PROVIDER_GPG_SOURCE_URL` | (Optional) Key source URL |

#### Server — Pod Disruption Budget

| Parameter | Default | Description |
|-----------|---------|-------------|
| `server.podDisruptionBudget.enabled` | `false` | Create a `PodDisruptionBudget` for the Server |
| `server.podDisruptionBudget.minAvailable` | `2` | Minimum number of available pods |

### RBAC

| Parameter | Default | Description |
|-----------|---------|-------------|
| `rbac.create` | `true` | Create `ClusterRole`/`ClusterRoleBinding` (or `Role`/`RoleBinding`) for each service |
| `rbac.scopeToNamespace` | `false` | Use namespace-scoped `Role`/`RoleBinding` instead of cluster-scoped. Also sets `WATCH_NAMESPACE` on controller deployments |

### Service Accounts

| Parameter | Default | Description |
|-----------|---------|-------------|
| `serviceAccount.create` | `true` | Create a dedicated `ServiceAccount` for each service |
| `serviceAccount.annotations` | `{}` | Annotations added to every created service account |

### Storage (Filesystem Backend)

The filesystem storage backend uses a shared `PersistentVolumeClaim` (or a `hostPath` volume for local development) mounted by both the Version controller and the Server.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `storage.filesystem.enabled` | `false` | Enable the shared filesystem volume |
| `storage.filesystem.mountPath` | `/data/modules` | Mount path inside containers |
| `storage.filesystem.hostPath` | `""` | Use a `hostPath` volume at this path (useful with kind). When set, the PVC is not created |
| `storage.filesystem.storageClassName` | `""` | `StorageClass` for the PVC. Must support `ReadWriteMany` |
| `storage.filesystem.size` | `10Gi` | PVC storage request |

When `hostPath` is set, an `initContainer` (`busybox:1.37`) runs as root to `chown` the mount point to UID `65532` before the main containers start.

## Usage Examples

### Pin All Services to a Specific Release

```bash
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set global.image.tag=1.0.0
```

### Enable the Provider Controller

```bash
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set provider.enabled=true \
  --set server.gpg.secretName=opendepot-gpg
```

### Ingress with TLS

```bash
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set server.ingress.enabled=true \
  --set server.ingress.className=nginx \
  --set "server.ingress.hosts[0].host=opendepot.example.com" \
  --set "server.ingress.hosts[0].paths[0].path=/" \
  --set "server.ingress.hosts[0].paths[0].pathType=Prefix" \
  --set "server.ingress.tls[0].secretName=opendepot-tls" \
  --set "server.ingress.tls[0].hosts[0]=opendepot.example.com"
```

### Istio VirtualService

```bash
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set server.ingress.istio.enabled=true \
  --set "server.ingress.istio.hosts[0]=opendepot.example.com" \
  --set server.ingress.istio.gateway="istio-system/my-gateway"
```

### Filesystem Storage with kind (Local Development)

```bash
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/mnt/opendepot-modules
```

### Filesystem Storage with a PVC (Production)

```bash
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.storageClassName=efs-sc \
  --set storage.filesystem.size=50Gi
```

### Namespace-Scoped RBAC

```bash
helm install opendepot ./chart/opendepot \
  -n opendepot-system --create-namespace \
  --set rbac.scopeToNamespace=true
```

### Full Custom Values File

```yaml
global:
  namespace: opendepot-system
  image:
    tag: 1.0.0

provider:
  enabled: true

server:
  replicaCount: 3
  anonymousAuth: false
  useBearerToken: true
  service:
    type: ClusterIP
  ingress:
    enabled: true
    className: nginx
    hosts:
      - host: opendepot.example.com
        paths:
          - path: /
            pathType: Prefix
    tls:
      - secretName: opendepot-tls
        hosts:
          - opendepot.example.com
  gpg:
    secretName: opendepot-gpg
  podDisruptionBudget:
    enabled: true
    minAvailable: 2

storage:
  filesystem:
    enabled: true
    storageClassName: efs-sc
    size: 50Gi

rbac:
  scopeToNamespace: false
```

## Upgrading

```bash
helm upgrade opendepot ./chart/opendepot \
  -n opendepot-system \
  -f custom-values.yaml
```

## Uninstalling

```bash
helm uninstall opendepot -n opendepot-system
```

CRDs and PersistentVolumeClaims are not removed automatically. To clean up CRDs:

```bash
kubectl delete -f chart/opendepot/crds/
```

## Security Notes

- All controller containers run as UID `65532` with `runAsNonRoot: true` and `allowPrivilegeEscalation: false`.
- The Server container sets `readOnlyRootFilesystem: true` unless filesystem storage is enabled.
- When `server.anonymousAuth` is `false` and `server.useBearerToken` is `true` (the defaults), the server requires a valid bearer token on every request.
- GPG private key material should always be stored in a Kubernetes `Secret`, never in `values.yaml`.
