# Helm Chart Structure Summary

## Directory Layout

```
chart/
├── README.md                              # Quick start guide
└── kerrareg/
    ├── Chart.yaml                         # Chart metadata
    ├── values.yaml                        # Default values
    ├── README.md                          # Detailed documentation
    ├── .helmignore                        # Helm ignore patterns
    ├── templates/
    │   ├── namespace.yaml                 # Namespace creation
    │   ├── crds-info.yaml                 # CRD installation info
    │   │
    │   ├── version-serviceaccount.yaml    # Version controller
    │   ├── version-rbac.yaml
    │   ├── version-deployment.yaml
    │   │
    │   ├── module-serviceaccount.yaml     # Module controller
    │   ├── module-rbac.yaml
    │   ├── module-deployment.yaml
    │   │
    │   ├── depot-serviceaccount.yaml      # Depot controller (optional)
    │   ├── depot-rbac.yaml
    │   ├── depot-deployment.yaml
    │   │
    │   ├── server-serviceaccount.yaml     # Server API
    │   ├── server-rbac.yaml
    │   ├── server-deployment.yaml
    │   ├── server-service.yaml
    │   ├── server-ingress.yaml
    │   └── server-poddisruptionbudget.yaml
    │
    └── examples/
        ├── development.yaml               # Dev deployment
        ├── production-with-depot.yaml     # Prod with Depot
        └── production-push-based.yaml     # Prod without Depot
```

## Key Features

### 1. Organized by Service
- Each service (Version, Module, Depot, Server) has dedicated templates
- Clear separation of ServiceAccount, RBAC, and Deployment concerns
- Easy to understand and maintain

### 2. Optional Depot Component
- Depot is disabled by default (set `depot.enabled: false`)
- Enable for pull-based workflows: `--set depot.enabled=true`
- Perfect for migration scenarios or managing external modules

### 3. Production Ready
- Configurable replicas for high availability
- Pod Disruption Budget support
- TLS/HTTPS support
- Ingress support with flexible configuration
- Resource requests and limits

### 4. Flexible Configuration
- Global settings (namespace, image pull policy)
- Per-service configuration (replicas, resources, images)
- Example values for different scenarios

### 5. RBAC Support
- Automatic service account creation
- Proper role and role binding generation
- Principle of least privilege for each controller

## Quick Deployment Commands

```bash
# Basic deployment
helm install kerrareg ./chart/kerrareg -n kerrareg --create-namespace

# With Depot enabled
helm install kerrareg ./chart/kerrareg -n kerrareg --create-namespace --set depot.enabled=true

# With custom values
helm install kerrareg ./chart/kerrareg -n kerrareg --create-namespace -f chart/kerrareg/examples/production-with-depot.yaml

# Development setup
helm install kerrareg ./chart/kerrareg -n kerrareg --create-namespace -f chart/kerrareg/examples/development.yaml
```

## Important Notes

1. **CRDs**: Must be installed separately before deploying the chart:
   ```bash
   cd services/version && make install
   cd ../module && make install
   cd ../depot && make install
   ```

2. **Image Repositories**: Update image repositories in values.yaml to match your registry:
   ```yaml
   version:
     image:
       repository: your-registry/kerrareg/version
   ```

3. **Namespace**: Chart creates the namespace by default but can use an existing one

4. **RBAC**: Chart creates service accounts and RBAC by default, but can be disabled

## Usage Examples in Chart

### Disable Depot (Push-based CI)
```bash
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set depot.enabled=false
```

### Enable TLS
```bash
# First create TLS secret
kubectl create secret tls kerrareg-tls \
  --cert=path/to/cert.crt \
  --key=path/to/key.key \
  -n kerrareg

# Then deploy with TLS enabled
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set server.tls.enabled=true
```

### Use Ingress Instead of LoadBalancer
```bash
helm install kerrareg ./chart/kerrareg \
  -n kerrareg --create-namespace \
  --set server.service.type=ClusterIP \
  --set server.ingress.enabled=true \
  --set 'server.ingress.hosts[0].host=kerrareg.example.com' \
  --set 'server.ingress.hosts[0].paths[0].path=/' \
  --set 'server.ingress.hosts[0].paths[0].pathType=Prefix'
```
