# Kerrareg Helm Chart

A comprehensive Helm chart for deploying Kerrareg, a self-hosted Terraform Module Registry, on Kubernetes.

## Overview

This Helm chart deploys all four core Kerrareg services:

- **Version Controller** (Core) - Handles fetching modules from GitHub and storing them in configured backends
- **Module Controller** - Orchestrates module version creation and management
- **Depot Controller** (Optional) - Pulls modules based on version constraints from external sources
- **Server** - Implements the Terraform Module Registry Protocol API

## Quick Start

### Prerequisites

- Kubernetes 1.11+
- Helm 3.0+
- Kerrareg Custom Resource Definitions (CRDs) installed

### Installing CRDs

Before deploying the chart, install the Custom Resource Definitions:

```bash
cd services/version && make install
cd ../module && make install
cd ../depot && make install
```

### Basic Installation

```bash
# Install with default values (Depot disabled)
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace

# Install with Depot enabled
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set depot.enabled=true
```

## Configuration

### Global Settings

| Parameter | Default | Description |
|-----------|---------|-------------|
| `global.namespace` | `kerrareg` | Kubernetes namespace |
| `global.imagePullPolicy` | `IfNotPresent` | Image pull policy |

### Version Controller

The core service that handles module fetching and storage.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `version.enabled` | `true` | Enable Version controller |
| `version.replicaCount` | `1` | Number of replicas |
| `version.image.repository` | `kerrareg/version` | Container image repository |
| `version.image.tag` | `latest` | Container image tag |
| `version.resources` | See values.yaml | Resource requests/limits |

### Module Controller

Orchestrates module version lifecycle.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `module.enabled` | `true` | Enable Module controller |
| `module.replicaCount` | `1` | Number of replicas |
| `module.image.repository` | `kerrareg/module` | Container image repository |
| `module.image.tag` | `latest` | Container image tag |
| `module.resources` | See values.yaml | Resource requests/limits |

### Depot Controller (Optional)

Pulls modules based on version constraints. Disabled by default.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `depot.enabled` | `false` | Enable Depot controller |
| `depot.replicaCount` | `1` | Number of replicas |
| `depot.image.repository` | `kerrareg/depot` | Container image repository |
| `depot.image.tag` | `latest` | Container image tag |
| `depot.resources` | See values.yaml | Resource requests/limits |

### Server

The Registry Protocol API endpoint.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `server.enabled` | `true` | Enable Server |
| `server.replicaCount` | `3` | Number of replicas |
| `server.image.repository` | `kerrareg/server` | Container image repository |
| `server.image.tag` | `latest` | Container image tag |
| `server.service.type` | `LoadBalancer` | Kubernetes service type |
| `server.service.port` | `443` | Service port |
| `server.service.targetPort` | `8443` | Container port |
| `server.tls.enabled` | `false` | Enable TLS (requires kerrareg-tls secret) |
| `server.ingress.enabled` | `false` | Enable Ingress |
| `server.podDisruptionBudget.enabled` | `false` | Enable PDB |
| `server.podDisruptionBudget.minAvailable` | `2` | Minimum available pods |

### RBAC and Service Accounts

| Parameter | Default | Description |
|-----------|---------|-------------|
| `serviceAccount.create` | `true` | Create service accounts |
| `rbac.create` | `true` | Create RBAC roles and bindings |
| `crd.install` | `true` | Install CRDs |

## Usage Examples

### Deploy with Custom Image Tags

```bash
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set version.image.tag=v0.1.0 \
  --set module.image.tag=v0.1.0 \
  --set server.image.tag=v0.1.0
```

### Deploy with Ingress

```bash
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set server.ingress.enabled=true \
  --set server.ingress.hosts[0].host=kerrareg.example.com \
  --set server.ingress.hosts[0].paths[0].path=/ \
  --set server.ingress.hosts[0].paths[0].pathType=Prefix
```

### Deploy with Depot Enabled

```bash
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set depot.enabled=true
```

### Deploy with Custom Values File

```bash
# Create custom values file
cat > custom-values.yaml <<EOF
depot:
  enabled: true

server:
  replicaCount: 5
  service:
    type: ClusterIP
  ingress:
    enabled: true
    hosts:
      - host: kerrareg.example.com
        paths:
          - path: /
            pathType: Prefix

version:
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
EOF

# Install with custom values
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  -f custom-values.yaml
```

## Namespace Handling

The chart automatically creates the namespace specified in `global.namespace` if it doesn't exist. To use an existing namespace:

```bash
helm install kerrareg ./chart/kerrareg \
  -n my-existing-namespace \
  --set global.namespace=my-existing-namespace
```

## TLS Configuration

To enable TLS for the server:

1. Create a secret with your TLS certificate and key:

```bash
kubectl create secret tls kerrareg-tls \
  --cert=path/to/cert.crt \
  --key=path/to/key.key \
  -n kerrareg
```

2. Enable TLS in the values:

```bash
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set server.tls.enabled=true
```

## Upgrading

To upgrade an existing Kerrareg deployment:

```bash
helm upgrade kerrareg ./chart/kerrareg \
  -n kerrareg \
  --values custom-values.yaml
```

## Uninstalling

To uninstall Kerrareg:

```bash
helm uninstall kerrareg -n kerrareg
```

Note: This does not remove CRDs or persistent data. To clean up CRDs:

```bash
cd services/version && make uninstall
cd ../module && make uninstall
cd ../depot && make uninstall
```

## Architecture

The chart deploys services in the following dependency order:

1. **Version Controller** (Core) - Must run first as it's the primary worker
2. **Module Controller** - Depends on Version controller
3. **Server** - Exposes the registry API (can run independently)
4. **Depot Controller** (Optional) - Coordinates modules (can be disabled for push-based workflows)

## Notes

- The chart does not include CRD definitions. Install CRDs separately using `make install` in each service directory.
- The Server service uses a LoadBalancer by default. Modify `server.service.type` to use ClusterIP with Ingress or NodePort.
- For production deployments, set appropriate resource limits and enable pod disruption budgets.
- Ensure Kubernetes RBAC is enabled for proper controller function.

## Support

For issues or questions about this chart, refer to the main Kerrareg documentation in the project README.
