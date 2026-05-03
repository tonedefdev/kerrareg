---
tags:
  - helm
  - installation
---

# Helm Chart

The OpenDepot Helm chart is published to a GitHub Pages Helm repository:

```bash
helm repo add opendepot https://tonedefdev.github.io/opendepot
helm repo update
```

The chart source is also available at [`chart/opendepot/`](https://github.com/tonedefdev/opendepot/tree/main/chart/opendepot) in the repository.

See [Installation](getting-started/installation.md) for the full Helm values reference and deployment instructions.

## Scanning Values

The `scanning` section controls Trivy-based provider vulnerability scanning. See [Vulnerability Scanning](configuration/scanning.md) for full details.

```yaml
scanning:
  enabled: false
  cacheMountPath: /var/cache/trivy
  offline: true
  blockOnCritical: false
  blockOnHigh: false
  cache:
    storageClassName: ""
    accessMode: ReadWriteMany
    size: 1Gi
  dbUpdater:
    schedule: "0 2 * * *"
    image:
      repository: aquasec/trivy
      tag: "0.70.0"
```

