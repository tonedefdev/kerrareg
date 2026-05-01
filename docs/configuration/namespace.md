# Namespace-Scoped Mode

By default, Kerrareg controllers use `ClusterRole`/`ClusterRoleBinding` and watch resources across all namespaces. To restrict controllers to a single namespace, enable namespace-scoped mode:

```yaml
rbac:
  scopeToNamespace: true

global:
  namespace: my-kerrareg-namespace
```

When `rbac.scopeToNamespace` is `true`:

- RBAC resources are created as `Role`/`RoleBinding` scoped to `global.namespace`
- Each controller only watches and reconciles resources in that namespace
- The `WATCH_NAMESPACE` environment variable is automatically set on controller pods

This is useful in multi-tenant clusters or environments where cluster-wide permissions are not available.
