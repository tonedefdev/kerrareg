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
| Depot | `secrets` | get, list, watch |
| Module | `modules` | create, delete, get, list, patch, update, watch |
| Module | `modules/finalizers` | update |
| Module | `modules/status` | get, patch, update |
| Module | `versions` | create, get, list, patch, update, watch |
| Version | `modules` | get, list, watch |
| Version | `modules/status` | get, patch, update |
| Version | `providers` | get |
| Version | `providers/status` | get, patch, update |
| Version | `versions` | create, delete, get, list, patch, update, watch |
| Version | `versions/finalizers` | update |
| Version | `versions/status` | get, patch, update |
| Version | `secrets` | get, list, watch |
| Provider | `providers` | create, delete, get, list, patch, update, watch |
| Provider | `providers/finalizers` | update |
| Provider | `providers/status` | get, patch, update |
| Provider | `versions` | create, delete, get, list, patch, update, watch |
| Server | `versions` | get, list, watch |
| Server | `modules` | get, list |

## CI/CD ServiceAccount

For CI/CD pipelines that need to create or update `Module` resources:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kerrareg-ci-publisher
  namespace: kerrareg-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kerrareg-module-publisher
  namespace: kerrareg-system
rules:
  - apiGroups: ["kerrareg.io"]
    resources: ["modules"]
    verbs: ["create", "update", "patch", "get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kerrareg-ci-publisher-binding
  namespace: kerrareg-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kerrareg-module-publisher
subjects:
  - kind: ServiceAccount
    name: kerrareg-ci-publisher
    namespace: kerrareg-system
```

