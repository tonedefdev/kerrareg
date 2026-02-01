# Kerrareg Helm Charts

This directory contains the Helm chart for deploying Kerrareg.

## Contents

- `kerrareg/` - The main Helm chart for Kerrareg

## Quick Start

### 1. Install CRDs

First, install the Custom Resource Definitions (CRDs):

```bash
# From the root of the repo
cd services/version && make install
cd ../module && make install
cd ../depot && make install
```

### 2. Deploy with Helm

```bash
# Basic deployment (Depot disabled)
helm install kerrareg ./kerrareg \
  -n kerrareg --create-namespace

# With Depot enabled
helm install kerrareg ./kerrareg \
  -n kerrareg --create-namespace \
  --set depot.enabled=true

# With custom values
helm install kerrareg ./kerrareg \
  -n kerrareg --create-namespace \
  -f kerrareg/examples/production-with-depot.yaml
```

## Chart Organization

The chart is organized by service with clear separation of concerns:

```
templates/
├── namespace.yaml                    # Kubernetes namespace
├── crds-info.yaml                    # CRD installation info
│
├── version-serviceaccount.yaml       # Version controller SA
├── version-rbac.yaml                 # Version controller RBAC
├── version-deployment.yaml           # Version controller Deployment
│
├── module-serviceaccount.yaml        # Module controller SA
├── module-rbac.yaml                  # Module controller RBAC
├── module-deployment.yaml            # Module controller Deployment
│
├── depot-serviceaccount.yaml         # Depot controller SA (optional)
├── depot-rbac.yaml                   # Depot controller RBAC (optional)
├── depot-deployment.yaml             # Depot controller Deployment (optional)
│
├── server-serviceaccount.yaml        # Server SA
├── server-rbac.yaml                  # Server RBAC
├── server-deployment.yaml            # Server Deployment
├── server-service.yaml               # Server Service
├── server-ingress.yaml               # Server Ingress (optional)
└── server-poddisruptionbudget.yaml   # Server PDB (optional)

examples/
├── development.yaml                  # Development deployment config
├── production-with-depot.yaml        # Production with Depot
└── production-push-based.yaml        # Production with push-based CI
```

## Features

- **Modular Design**: Each service has its own templates organized by function (SA, RBAC, Deployment)
- **Optional Components**: Depot controller can be disabled for push-based CI/CD workflows
- **Production Ready**: Includes ingress, PDB, TLS support, and proper RBAC
- **Development Friendly**: Example configurations for different deployment scenarios
- **Flexible**: Easy customization through values.yaml

## Key Configuration Options

### Enable/Disable Services

- `version.enabled` - Core service (recommended: always true)
- `module.enabled` - Core service (recommended: always true)
- `depot.enabled` - Optional (set to true for pull-based workflows, false for push-based)
- `server.enabled` - API endpoint (recommended: always true)

### Server Configuration

- `server.replicaCount` - Number of server replicas
- `server.service.type` - LoadBalancer (default) or ClusterIP
- `server.ingress.enabled` - Enable Ingress for external access
- `server.tls.enabled` - Enable TLS (requires kerrareg-tls secret)
- `server.podDisruptionBudget.enabled` - Enable for high availability

### Resource Management

All services support custom resource limits:

```yaml
version:
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 1Gi
```

## Deployment Scenarios

### Scenario 1: Development (All services, minimal resources)

```bash
helm install kerrareg ./kerrareg \
  -n kerrareg --create-namespace \
  -f kerrareg/examples/development.yaml
```

### Scenario 2: Production with Pull-Based Workflow (Depot enabled)

```bash
helm install kerrareg ./kerrareg \
  -n kerrareg --create-namespace \
  -f kerrareg/examples/production-with-depot.yaml
```

### Scenario 3: Production with Push-Based CI (Depot disabled)

```bash
helm install kerrareg ./kerrareg \
  -n kerrareg --create-namespace \
  -f kerrareg/examples/production-push-based.yaml
```

## Troubleshooting

### Check deployment status:

```bash
kubectl get deployments -n kerrareg
kubectl get pods -n kerrareg
```

### View logs:

```bash
# Version controller
kubectl logs -n kerrareg -l app=version-controller

# Module controller
kubectl logs -n kerrareg -l app=module-controller

# Depot controller
kubectl logs -n kerrareg -l app=depot-controller

# Server
kubectl logs -n kerrareg -l app=server
```

### Verify RBAC:

```bash
kubectl get clusterroles | grep kerrareg
kubectl get clusterrolebindings | grep kerrareg
```

## Next Steps

After deployment:

1. Verify all pods are running: `kubectl get pods -n kerrareg`
2. Check server endpoint: `kubectl get svc -n kerrareg server`
3. Configure Terraform/OpenTofu to use your registry (see main README)
4. Create Module resources or enable Depot for automated module management

## For More Information

- See [main README](../../README.md) for architecture and configuration details
- See [chart README](./kerrareg/README.md) for detailed chart documentation
- Check example values files in `kerrareg/examples/` for different deployment scenarios
