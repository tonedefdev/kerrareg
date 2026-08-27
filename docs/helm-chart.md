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

See [Installation](getting-started/installation.md) for prerequisites and deployment instructions.

## Global

| Value | Type | Description |
|-------|------|-------------|
| `global.namespace` | string | Namespace for all resources. Default: `opendepot-system` |
| `global.imagePullPolicy` | string | Image pull policy. Default: `IfNotPresent` |
| `global.image.tag` | string | Image tag for all services. Defaults to `Chart.AppVersion` when blank. |

## Server Configuration

### General

| Value | Type | Description |
|-------|------|-------------|
| `server.enabled` | bool | Deploy the server. Default: `true` |
| `server.replicaCount` | int | Number of replicas. Default: `1` |
| `server.anonymousAuth` | bool | Use the server's service account for unauthenticated module access. Default: `false` |
| `server.useBearerToken` | bool | Use bearer token auth instead of kubeconfig. Default: `true` |
| `server.image.repository` | string | Server image repository. Default: `ghcr.io/tonedefdev/opendepot/server` |
| `server.service.type` | string | Kubernetes Service type. Default: `LoadBalancer` |
| `server.service.port` | int | Service port. Default: `80` |
| `server.service.targetPort` | int | Container port. Default: `8080` |
| `server.tls.enabled` | bool | Enable TLS on the server. Default: `false` |
| `server.tls.certPath` | string | Path to TLS certificate. Default: `/etc/tls/tls.crt` |
| `server.tls.keyPath` | string | Path to TLS key. Default: `/etc/tls/tls.key` |
| `server.ingress.enabled` | bool | Enable Kubernetes Ingress. Default: `false` |
| `server.ingress.hosts` | list | Standard Ingress host/path rules. |
| `server.ingress.tls` | list | Standard Ingress TLS configuration. Default: `[]` |
| `server.ingress.istio.enabled` | bool | Enable Istio VirtualService. Default: `false` |
| `server.ingress.istio.hosts` | list | Istio VirtualService hosts. Default: `[opendepot.defdev.io]` |
| `server.resources.requests.cpu` | string | CPU request. Default: `100m` |
| `server.resources.requests.memory` | string | Memory request. Default: `128Mi` |
| `server.resources.limits.memory` | string | Memory limit. Default: `512Mi` |
| `server.nodeSelector` | map | Node selector. Default: `{}` |
| `server.tolerations` | list | Tolerations. Default: `[]` |
| `server.affinity` | map | Affinity rules. Default: `{}` |
| `server.podDisruptionBudget.enabled` | bool | Enable PodDisruptionBudget. Default: `false` |
| `server.podDisruptionBudget.minAvailable` | int | Minimum available pods. Default: `2` |

### OIDC Authentication

The `server.oidc` section enables OIDC JWT validation for production-ready single sign-on. See [Authenticating with OpenDepot](authentication.md) for detailed setup and examples.

| Value | Type | Description |
|-------|------|-------------|
| `server.oidc.enabled` | bool | When true, enables OIDC JWT validation and advertises the `login.v1` service discovery endpoint. Default: `false` |
| `server.oidc.issuerUrl` | string | OIDC issuer URL (e.g., `https://opendepot.example.com/dex`). When blank and `dex.enabled: true`, auto-derives the in-cluster Dex service URL. |
| `server.oidc.clientId` | string | OIDC client ID. Must match the Dex static client `id`. Default: `"opendepot"` |
| `server.oidc.clientSecretName` | string | Name of a Kubernetes Secret containing the `clientSecret` key. When blank, the chart creates a Secret from `server.oidc.clientSecret`. |
| `server.oidc.clientSecret` | string | Dex client secret (only used if `clientSecretName` is blank). In production, use an external secret operator instead of storing plaintext here. |
| `server.oidc.groupsClaim` | string | JWT claim name containing the user's groups, used for [GroupBinding](guides/groupbinding.md) evaluation. When blank, defaults to `groups`. Set to `cognito:groups`, `roles`, etc. for non-standard IdPs. |
| `server.oidc.allowServiceAccountFallback` | bool | When true, Kubernetes ServiceAccount bearer tokens with a non-OIDC issuer are authenticated via the bearer-token path using the SA's own RBAC. GroupBinding is bypassed for SA tokens. Requires `server.oidc.enabled: true`. Default: `false` |
| `server.oidc.allowClientCredentials` | bool | When true, Dex tokens whose audience does not match the primary client ID are accepted. The token's `sub` claim is mapped to a virtual group `"client:<sub>"` and evaluated against `GroupBinding` resources. Requires a Dex `staticClient` with `grantTypes: ["client_credentials"]`. Default: `false` |
| `server.oidc.dexProxy.enabled` | bool | When true and OIDC is enabled, the server reverse-proxies `/dex/*` requests to the bundled Dex service so Dex never needs its own public ingress or hostname. Requires `dex.enabled: true` and `server.oidc.issuerUrl` set to the external, path-based URL matching `dex.config.issuer`. Set to `false` for external OIDC providers; ignored when OIDC is disabled. Default: `true` |
| `server.oidc.authzUrl` | string | Overrides the authorization URL advertised in `login.v1` of `/.well-known/terraform.json`. Leave blank to use the URL from the OIDC provider discovery document. Not needed when `server.oidc.dexProxy.enabled: true`. Use this when the server discovers Dex via an in-cluster address but CLI clients must reach Dex at a different address (e.g. a port-forwarded URL during local Kind testing). |
| `server.oidc.tokenUrl` | string | Overrides the token URL advertised in `login.v1` of `/.well-known/terraform.json`. Same use-case as `authzUrl`. Not needed when `server.oidc.dexProxy.enabled: true`. |

**Example (recommended — Dex proxied through the server):**

```yaml
server:
  oidc:
    enabled: true
    issuerUrl: https://opendepot.example.com/dex
    clientId: opendepot
    clientSecret: $(openssl rand -base64 32)
    clientSecretName: ""  # Use the above value; or set to "my-secret" to use external secret
    dexProxy:
      enabled: true
```

!!! warning
    When both `dex.enabled` and `server.oidc.enabled` are `true`, the Helm render fails if neither `server.oidc.clientSecret` nor `server.oidc.clientSecretName` is set. For production, pre-create a Kubernetes Secret and reference it via `server.oidc.clientSecretName`.

## Dex Configuration

The `dex` section deploys Dex as an OIDC identity provider. Dex federates upstream IdPs (GitHub, Entra ID, Okta, LDAP, etc.) and issues JWTs that the server validates locally.

| Value | Type | Description |
|-------|------|-------------|
| `dex.enabled` | bool | When true, deploys Dex as a subchart. Default: `false` |
| `dex.config.issuer` | string | Public issuer URL. Recommended (server-proxied): same host as the server ingress, e.g. `https://opendepot.example.com/dex`. In-cluster (no proxy): `http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex`. Separately exposed: `https://dex.example.com/dex` |
| `dex.config.connectors` | array | Array of upstream IdP connector configurations. See examples below. Default: `[]` |
| `dex.config.enablePasswordDB` | bool | When true, enables local username/password authentication (testing only). Default: `false` |
| `dex.config.staticPasswords` | array | Array of test users for local auth. Never enable in production. Default: `[]` |

**Basic Example (GitHub):**

```yaml
dex:
  enabled: true
  config:
    issuer: https://opendepot.example.com/dex
    connectors:
      - type: github
        id: github
        name: GitHub
        config:
          clientID: <github-oauth-app-client-id>
          clientSecret: <github-oauth-app-secret>
          redirectURI: https://opendepot.example.com/dex/callback
          org: my-org  # (optional) restrict to an org
```

**Entra ID (Azure AD) Example:**

```yaml
dex:
  enabled: true
  config:
    issuer: https://opendepot.example.com/dex
    connectors:
      - type: microsoft
        id: microsoft
        name: "Azure AD"
        config:
          clientID: <azure-app-id>
          clientSecret: <azure-app-secret>
          redirectURI: https://opendepot.example.com/dex/callback
          tenant: <azure-tenant-id>
```

For connector configuration details, refer to the [Dex Connector Documentation](https://dexidp.io/docs/connectors/).

!!! warning
    Never set `enablePasswordDB: true` or `staticPasswords` in production. Use real IdP connectors instead.

## Controllers

These values apply to `version`, `module`, `depot`, and `provider` independently — substitute `<service>` with the controller name:

| Value | Type | Description |
|-------|------|-------------|
| `<service>.enabled` | bool | Deploy the controller. Default: `true` (`provider`: `false`) |
| `<service>.replicaCount` | int | Number of replicas. Default: `1` |
| `<service>.image.repository` | string | Image repository. Default: `ghcr.io/tonedefdev/opendepot/<service>-controller` |
| `<service>.image.tag` | string | Overrides `global.image.tag` when set. |
| `<service>.resources.requests.cpu` | string | CPU request. Default: `100m` |
| `<service>.resources.requests.memory` | string | Memory request. Default: `512Mi` for `version`, `128Mi` for others |
| `<service>.resources.limits.memory` | string | Memory limit. Default: `4Gi` for `version`, `512Mi` for others |
| `<service>.nodeSelector` | map | Node selector. Default: `{}` |
| `<service>.tolerations` | list | Tolerations. Default: `[]` |
| `<service>.affinity` | map | Affinity rules. Default: `{}` |

!!! note
    The provider controller is disabled by default (`provider.enabled: false`). Enable it explicitly when you are ready to sync provider binaries — provider archives can be several hundred megabytes each.

## GPG Signing (Providers)

The server signs `SHA256SUMS` files for provider packages using a GPG key you supply. OpenTofu and Terraform verify this signature as part of the [Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol). See [GPG Signing for Providers](configuration/gpg.md) for full setup instructions.

| Value | Type | Description |
|-------|------|-------------|
| `server.gpg.secretName` | string | Name of the Kubernetes Secret containing GPG signing credentials (`OPENDEPOT_PROVIDER_GPG_KEY_ID`, `OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR`, `OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64`). Default: `""` |

## Service Account & RBAC

| Value | Type | Description |
|-------|------|-------------|
| `serviceAccount.create` | bool | Create service accounts. Default: `true` |
| `serviceAccount.annotations` | map | Annotations (use for IRSA/Workload Identity). Default: `{}` |
| `rbac.create` | bool | Create RBAC roles and bindings. Default: `true` |
| `rbac.scopeToNamespace` | bool | Use namespace-scoped Role/RoleBinding instead of ClusterRole/ClusterRoleBinding. Default: `false` |

## Storage

| Value | Type | Description |
|-------|------|-------------|
| `storage.filesystem.enabled` | bool | Enable shared volume for filesystem storage. Default: `false` |
| `storage.filesystem.mountPath` | string | Mount path inside containers. Default: `/data/modules` |
| `storage.filesystem.hostPath` | string | Use a hostPath volume (for local dev with kind). Default: `""` |
| `storage.filesystem.storageClassName` | string | StorageClass for PVC (requires `ReadWriteMany`). Default: `""` |
| `storage.filesystem.size` | string | PVC storage size. Default: `10Gi` |

See [Storage Backends](storage.md) for S3, Azure, and GCS configuration, which are set via environment variables rather than Helm values.

## UI Configuration

The `ui` section deploys the Registry Explorer frontend. See [Registry Explorer UI](guides/registry-explorer.md) for setup, OIDC login, and public visibility configuration.

| Value | Type | Description |
|-------|------|-------------|
| `ui.enabled` | bool | When true, deploys the Registry Explorer UI and NGINX proxy. Also suppresses `server-ingress.yaml` — migrate traffic to `ui.ingress` before enabling. Default: `false` |
| `ui.replicaCount` | int | Number of UI pod replicas. Default: `1` |
| `ui.image.repository` | string | UI container image repository. Default: `ghcr.io/tonedefdev/opendepot/ui` |
| `ui.image.tag` | string | Image tag. Defaults to `global.image.tag`, then the chart `appVersion`. |
| `ui.serverHost` | string | Upstream `host:port` that NGINX proxies registry requests to. Defaults to `server.<namespace>.svc.cluster.local:80` when blank. |
| `ui.sessionPasswordSecretName` | string | Name of a Kubernetes Secret with a `sessionPassword` key (min 32 chars). Required when `ui.enabled: true`. |
| `ui.oidc.enabled` | bool | Enables OIDC authorization code login in the UI. Default: `false` |
| `ui.oidc.issuerUrl` | string | Public HTTPS OIDC issuer URL. Discovered endpoints must share this origin. HTTP is accepted only with `global.developmentMode: true`. |
| `ui.oidc.clientId` | string | OIDC client ID for the UI. Default: `"opendepot-ui"`. When `ui.oidc.enabled: true` and non-empty, the chart also passes `--oidc-ui-client-id` to the server so UI-issued tokens are accepted on browse and stats endpoints. See [Registry Explorer UI OIDC](authentication.md#registry-explorer-ui-oidc). |
| `ui.oidc.clientSecretName` | string | Name of a Kubernetes Secret with a `clientSecret` key for the OIDC confidential client. |
| `ui.oidc.scopes` | string | Space-separated OIDC scopes. Default: `"openid profile email groups"` |
| `ui.oidc.callbackPath` | string | OIDC redirect URI path registered with the identity provider. Default: `"/auth/callback"` |
| `ui.auth.devTokenInput.enabled` | bool | When true, shows a developer bearer-token input in the UI. **Must be `false` in production.** Default: `false` |
| `ui.ingress.enabled` | bool | Creates a Kubernetes Ingress for the UI with split-path routing rules. Default: `false` |
| `ui.ingress.className` | string | Ingress class name. |
| `ui.ingress.annotations` | map | Annotations applied to the Ingress resource. |
| `ui.ingress.hosts` | list | Host and path rules. |
| `ui.ingress.tls` | list | TLS configuration for the Ingress. |

## Valkey Stats Store

Download statistics are persisted in a bundled [Valkey](https://valkey.io/) (Redis-compatible) instance deployed automatically alongside the server. No additional setup is required — Valkey is always deployed as part of the chart.

| Value | Type | Description |
|-------|------|-------------|
| `valkey.image.tag` | string | Valkey image tag. Defaults to the rolling Valkey 8 LTS Alpine variant, `"8-alpine"`, to minimize the operating-system package surface. |
| `valkey.resources` | map | Resource requests and limits for the Valkey pod |
| `valkey.dataStorage.enabled` | bool | Create a PVC for Valkey data. Default: `true` |
| `valkey.dataStorage.className` | string | StorageClass for the PVC. Leave blank for the cluster default. Default: `""` |
| `valkey.dataStorage.requestedSize` | string | PVC storage size. Default: `1Gi` |
| `valkey.auth.enabled` | bool | Enable Valkey ACL password authentication. Required outside development mode. Default: `true` |
| `valkey.auth.usersExistingSecret` | string | Name of a pre-existing Secret whose keys are ACL usernames and values are plaintext passwords. Default: `"opendepot-valkey-auth"` |
| `valkey.auth.aclUsers.default.permissions` | string | ACL permissions string for the default user. The default is scoped to `stats:*` keys and the exact commands used by the server, including `PING` for its startup connection check (e.g. `~stats:* &* -@all +PING +HSET +HINCRBY +HGET +HGETALL +INCR +GET +ZINCRBY +ZREVRANGEBYSCORE +ZREVRANGE +EXPIREAT`). Do not widen to `+@all` in production. |
| `server.stats.valkeyPasswordSecretName` | string | Name of the Secret injected as `OPENDEPOT_VALKEY_PASSWORD` into the server pod. Must match `valkey.auth.usersExistingSecret`. Default: `"opendepot-valkey-auth"` |
| `valkey.nodeSelector` | map | Node selector for the Valkey pod |
| `valkey.tolerations` | list | Tolerations for the Valkey pod |
| `valkey.affinity` | map | Affinity rules for the Valkey pod |

When `valkey.dataStorage.enabled: true` (the default), a PVC is created and mounted at `/data` in the Valkey pod. Set `valkey.dataStorage.enabled: false` to use ephemeral in-pod storage — suitable for local development or Kind clusters where no StorageClass is available. Stats are lost on pod restart when persistence is disabled.

Before installing, create the default Valkey ACL Secret in the deployment namespace:

```bash
kubectl create secret generic opendepot-valkey-auth \
  --from-literal=default="$(openssl rand -base64 32)" \
  --namespace opendepot-system
```

For production deployments, keep authentication enabled. If you choose another Secret name, set both `valkey.auth.usersExistingSecret` and `server.stats.valkeyPasswordSecretName` to that same name. The chart rejects disabled authentication outside `global.developmentMode` and rejects missing or mismatched Secret references. For regulated environments, use [External Secrets Operator](https://external-secrets.io/) or HashiCorp Vault to provision the Secret.

See [Download Tracking](guides/registry-explorer.md#download-tracking) for details on how stats are recorded and surfaced in the Registry Explorer UI.

## Scanning Values

The `scanning` section controls Trivy-based vulnerability scanning for modules and providers. See [Vulnerability Scanning](configuration/scanning.md) for full details.

| Value | Type | Description |
|-------|------|-------------|
| `scanning.enabled` | bool | Enable Trivy-based scanning. Switches the version-controller to the `-scanning` image variant and activates module IaC scanning. No PVC or CronJob is created at this level. Default: `false` |
| `scanning.providerScanning` | bool | Enable provider binary and source scanning. Requires `scanning.enabled: true`. Creates the Trivy DB PVC and `trivy-db-updater` CronJob and mounts the cache volume. Default: `false` |
| `scanning.cacheMountPath` | string | Mount path inside the version-controller container for the Trivy DB cache. Default: `/var/cache/trivy` |
| `scanning.offline` | bool | Pass `--offline-scan` to Trivy, preventing network calls during scans. Only applies to provider scanning. Default: `true` |
| `scanning.blockOnCritical` | bool | Halt reconciliation when CRITICAL findings are present (modules or providers). Default: `false` |
| `scanning.blockOnHigh` | bool | Halt reconciliation when HIGH findings are present (modules or providers). Default: `false` |
| `scanning.cache.storageClassName` | string | StorageClass for the Trivy cache PVC (must support ReadWriteMany for multi-node). Omitted from the PVC manifest when blank, allowing the cluster default to apply stably across upgrades. Default: `""` |
| `scanning.cache.accessMode` | string | Access mode for the Trivy cache PVC. Default: `ReadWriteMany` |
| `scanning.cache.size` | string | Size of the Trivy DB cache PVC. Default: `1Gi` |
| `scanning.dbUpdater.schedule` | string | Cron schedule for the Trivy DB update job. Default: `"0 2 * * *"` |
| `scanning.dbUpdater.image.repository` | string | Trivy image repository for the db-updater CronJob. Default: `aquasec/trivy` |
| `scanning.dbUpdater.image.tag` | string | Trivy image tag for the db-updater CronJob. Default: `"0.70.0"` |

