---
tags:
  - configuration
  - kubernetes
  - rbac
---

# Namespace-Scoped Mode

By default, OpenDepot controllers use `ClusterRole`/`ClusterRoleBinding` and watch resources across all namespaces. To restrict controllers to a single namespace, enable namespace-scoped mode:

```yaml
rbac:
  scopeToNamespace: true

global:
  namespace: my-opendepot-namespace
```

When `rbac.scopeToNamespace` is `true`:

- RBAC resources are created as `Role`/`RoleBinding` scoped to `global.namespace`
- Each controller only watches and reconciles resources in that namespace
- The `WATCH_NAMESPACE` environment variable is automatically set on controller pods

This is useful in multi-tenant clusters or environments where cluster-wide permissions are not available.

## Provider Registry Origins

A default cluster-scoped OpenDepot installation can serve provider mirrors for both `registry.opentofu.org` and `registry.terraform.io` from separate Kubernetes namespaces. When the same provider namespace and type exist in both registries, place each origin in a separate Kubernetes namespace to avoid Version resource-name collisions.

An installation with `rbac.scopeToNamespace: true` watches only `global.namespace`. To serve provider origins from multiple Kubernetes namespaces, use the default cluster-scoped mode or deploy a separate namespace-scoped installation for each namespace.
