# OpenDepot

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/tonedefdev/opendepot/blob/main/LICENSE)
[![Helm](https://img.shields.io/badge/Helm_Chart-0.2.3-0F1689?logo=helm&logoColor=white)](https://github.com/tonedefdev/opendepot/tree/main/chart/opendepot)
[![Docs](https://img.shields.io/badge/Docs-tonedefdev.github.io-047df1?logo=materialformkdocs&logoColor=white)](https://tonedefdev.github.io/opendepot/)

<p align="center">
  <img src="img/opendepot.svg" width="400" />
</p>

A Kubernetes-native, self-hosted OpenTofu/Terraform module and provider registry that implements both the [Module Registry Protocol](https://opentofu.org/docs/internals/module-registry-protocol/) and the [Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol). OpenDepot gives organizations complete control over distribution, versioning, and storage — without relying on the public registry.

Compatible with **OpenTofu** (all versions) and **Terraform** (v1.2+).

## Documentation

Comprehensive documentation is available at **[tonedefdev.github.io/opendepot/](https://tonedefdev.github.io/opendepot/)**.

| Guide | Description |
|-------|-------------|
| [Installation](https://tonedefdev.github.io/opendepot/getting-started/installation/) | Deploy OpenDepot to Kubernetes using the Helm chart |
| [Quickstart](https://tonedefdev.github.io/opendepot/getting-started/quickstart/) | Get up and running locally in minutes |
| [Helm Chart Reference](https://tonedefdev.github.io/opendepot/helm-chart/) | Full values reference for the OpenDepot Helm chart |
| [Architecture](https://tonedefdev.github.io/opendepot/architecture/) | How OpenDepot works under the hood |
| [Authentication](https://tonedefdev.github.io/opendepot/authentication/) | GitHub App and token-based auth |
| [Kubernetes RBAC](https://tonedefdev.github.io/opendepot/rbac/) | Fine-grained access control for registry resources |


## Version Constraints

OpenDepot supports all standard OpenTofu/Terraform version constraint syntax:

| Syntax | Example | Meaning |
|--------|---------|---------|
| Exact | `1.2.0` | Only version 1.2.0 |
| Comparison | `>= 1.0.0, < 2.0.0` | Any 1.x version |
| Pessimistic | `~> 1.2.0` | >= 1.2.0, < 1.3.0 (bugfixes only) |
| Pessimistic (minor) | `~> 1.2` | >= 1.2.0, < 2.0.0 |
| Exclusion | `>= 1.0.0, != 1.5.0` | Any 1.x except 1.5.0 |

## Project Structure

```
opendepot/
├── api/v1alpha1/              # CRD type definitions
│   ├── types.go               # Depot, Module, Version, StorageConfig schemas
│   └── groupversion_info.go   # API group registration
├── chart/opendepot/            # Helm chart
│   ├── Chart.yaml
│   ├── values.yaml
│   ├── crds/                  # CRD manifests
│   └── templates/             # Deployment, RBAC, Service templates
├── examples/
│   └── internal-developer-portal/ # React + MUI demo portal for Depot->Module->Version visualization
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
│   ├── provider/              # Provider controller (version lifecycle for providers)
│   └── depot/                 # Depot controller (GitHub + HashiCorp discovery)
├── Makefile                   # Build, load, deploy targets
└── go.work                    # Go workspace (multi-module)
```

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
