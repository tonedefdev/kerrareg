---
tags:
  - rbac
  - kubernetes
  - security
---

# Kubernetes RBAC

The Helm chart creates ServiceAccounts and RBAC resources for each controller automatically when `rbac.create: true` (the default).

## Controller Permissions

| Controller | Resource | Verbs |
|-----------|----------|-------|
| Depot | `depots` | create, delete, get, list, patch, update, watch |
| Depot | `depots/finalizers` | update |
| Depot | `depots/status` | get, patch, update |
| Depot | `modules` | create, get, list, patch, update, watch |
| Depot | `providers` | create, get, list, patch, update, watch |
| Depot | `secrets` | get |
| Module | `modules` | create, delete, get, list, patch, update, watch |
| Module | `modules/finalizers` | update |
| Module | `modules/status` | get, patch, update |
| Module | `versions` | create, delete, get, list, patch, update, watch |
| Version | `modules` | get, list, watch |
| Version | `modules/status` | get, patch, update |
| Version | `providers` | get, list, watch |
| Version | `providers/status` | get, patch, update |
| Version | `versions` | create, delete, get, list, patch, update, watch |
| Version | `versions/finalizers` | update |
| Version | `versions/status` | get, patch, update |
| Version | `secrets` | get |
| Version | `configmaps` | create, get, list, patch, update, watch |
| Provider | `providers` | create, delete, get, list, patch, update, watch |
| Provider | `providers/finalizers` | update |
| Provider | `providers/status` | get, patch, update |
| Provider | `versions` | create, delete, get, list, patch, update, watch |
| Server | `versions` | get, list, watch |
| Server | `modules` | get, list |
| Server | `providers` | get, list, watch |
| Server | `depots` | get, list, watch |
| Server | `groupbindings` | get, list, watch |
| Server | `namespaces` | get, list, watch |
| Server | `configmaps` | get, list, watch |

!!! note
    The `depots` rule is only added to the server `ClusterRole` when `ui.enabled: true`. When the UI is disabled the server never calls the depots API and the rule is omitted.

## Namespace-Scoped RBAC (Production Recommendation)

By default, OpenDepot creates `ClusterRole` and `ClusterRoleBinding` resources so controllers can watch the entire cluster. For production deployments, set `rbac.scopeToNamespace: true`:

```yaml
rbac:
  scopeToNamespace: true
```

When enabled, the chart creates `Role`/`RoleBinding` objects instead. This:

- Limits all controller permissions to the install namespace (`global.namespace`).
- Reduces blast radius if a controller is compromised.

The `secrets: [get]` permission required for GitHub App authentication is always granted through dedicated namespace-scoped `Role` objects, regardless of `rbac.scopeToNamespace`. Controller `ClusterRole` objects never grant Secret access.

!!! warning
    `rbac.scopeToNamespace: true` requires all `Module`, `Provider`, `Version`, and `Depot` resources to reside in the same namespace as the controllers. Do not enable this if your resources span multiple namespaces.

## Pipeline Publisher Role Example

This is a least-privilege RBAC example for pipelines that need to create or update `Module` resources.
For end-to-end pipeline workflow setup, see [CI/CD Pipelines](guides/cicd.md).

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: opendepot-ci-publisher
  namespace: opendepot-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: opendepot-module-publisher
  namespace: opendepot-system
rules:
  - apiGroups: ["opendepot.defdev.io"]
    resources: ["modules"]
    verbs: ["create", "update", "patch", "get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: opendepot-ci-publisher-binding
  namespace: opendepot-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: opendepot-module-publisher
subjects:
  - kind: ServiceAccount
    name: opendepot-ci-publisher
    namespace: opendepot-system
```

## GroupBinding and Stats Page Visibility

The Stats page (`GET /opendepot/ui/v1/stats`) aggregates only the resources visible to the authenticated user — for OIDC users with a matching `GroupBinding`, module/provider counts, version counts, storage bytes, and security findings are all scoped to that binding's permitted resources. Users without a `GroupBinding` (or in anonymous-auth / bearer-token mode) see all resources in labelled namespaces.

See [GroupBinding Access Control](guides/groupbinding.md) for full expression syntax and setup instructions.
