---
tags:
  - authentication
  - kubernetes
  - security
search:
  boost: 2
---

# Authenticating with OpenDepot

OpenDepot supports three authentication methods: OIDC JWTs via Dex, Kubernetes bearer tokens, and base64-encoded kubeconfigs. Bearer tokens and OIDC are recommended for production; kubeconfig is primarily for development.

### Method 1: OIDC via Dex (Recommended for Production)

OIDC authentication enables single sign-on (SSO) via existing identity providers without distributing credentials or kubeconfigs. Dex is bundled as a Helm subchart that acts as an OIDC identity broker, federating upstream IdPs (Entra ID, Okta, GitHub, LDAP, etc.) and issuing standard OIDC JWTs. The OpenDepot server validates these JWTs locally via JWKS, enabling the `tofu login` workflow.

For full setup instructions — enabling Dex, configuring connectors, registering the static client, and authenticating with `tofu login` — see [OIDC Authentication (Dex)](configuration/oidc.md).

#### Browse Endpoint Authentication Enforcement

When `server.oidc.enabled: true` and `server.anonymousAuth: false`, the browse API endpoints (`/opendepot/ui/v1/*`) require a valid `Authorization: Bearer <token>` header. Any request without a valid token returns `401 Unauthorized` — including requests with an invalid or expired JWT. This applies to all nine browse endpoints used by the Registry Explorer, Depots graph, and Stats pages.

A valid token with no matching `GroupBinding` is still accepted and receives public-only visibility. The gate only blocks callers who present no token or a token that fails all verification paths.

When `server.anonymousAuth: true` or OIDC is not configured, this gate is not active and browse endpoints remain accessible without authentication.

!!! warning
    Set `server.anonymousAuth: false` together with `server.oidc.enabled: true` in production to prevent unauthenticated access to the browse API.

#### Registry Explorer UI OIDC

When the Registry Explorer UI is deployed with `ui.oidc.enabled: true`, the UI authenticates users via an OIDC authorization code flow using a dedicated Dex client (default client ID: `opendepot-ui`). This client is separate from the `tofu login` client (`opendepot`) and issues tokens with a different audience.

By default, the server only validates tokens issued to the primary client ID (`--oidc-client-id`). To allow UI-issued tokens to be accepted by the browse and stats endpoints, set `ui.oidc.clientId` in Helm values (it defaults to `opendepot-ui` and takes effect automatically when `ui.oidc.enabled: true`):

```yaml
ui:
  oidc:
    enabled: true
    clientId: opendepot-ui
    clientSecretName: opendepot-ui-oidc-secret
```

When `ui.oidc.enabled: true` and `ui.oidc.clientId` is non-empty, the chart passes `--oidc-ui-client-id={{ ui.oidc.clientId }}` to the server. The server creates a second OIDC verifier for this client ID so that UI-issued tokens are accepted on browse and stats endpoints.

The UI requires an HTTPS issuer in production and accepts discovered authorization, token, and JWKS endpoints only when they share the configured issuer origin. This prevents discovery metadata from redirecting server-side requests or credentials to another host. `ui.oidc.authzUrl` is an explicit browser authorization override and may use another origin; insecure HTTP endpoints are enabled only when `global.developmentMode: true`.

The Dex `opendepot-ui` client must include `trustedPeers: [opendepot]` so that Dex embeds the server's audience in UI-issued tokens. Without this, the server rejects the token even when `--oidc-ui-client-id` is configured:

```yaml
dex:
  config:
    staticClients:
      - id: opendepot-ui
        name: OpenDepot UI
        secret: <ui-client-secret>
        trustedPeers:
          - opendepot
        redirectURIs:
          - https://<ui-host>/auth/callback
```

!!! note
    Without `--oidc-ui-client-id` configured, the Stats page and browse endpoints return zeroes or empty results for users authenticated through the UI's OIDC client.

#### GroupBinding Access Control

When [GroupBinding](guides/groupbinding.md) resources are deployed, the server enforces fine-grained access control after OIDC authentication: the user's groups claim is extracted from the JWT and matched against `GroupBinding` expressions to determine which modules and providers they may access. The groups claim is **required** — requests with no claim, or with a claim that matches no `GroupBinding`, are denied with `403 Forbidden`.

!!! warning
    If no `GroupBinding` resources exist in the server namespace, **all** OIDC-authenticated users are denied regardless of their groups. Deploy at least one `GroupBinding` before enabling OIDC in production.

See [Fine-Grained Access Control with GroupBinding](guides/groupbinding.md) for full expression syntax and setup instructions.

#### CI/CD with ServiceAccount Fallback

!!! danger "GroupBinding is bypassed for SA tokens"
    When SA fallback is enabled, ServiceAccount tokens skip GroupBinding entirely. The SA's Kubernetes RBAC is the only access control applied. This introduces a second, separate access control path alongside your OIDC + GroupBinding model — increasing audit surface and the blast radius of a compromised token.

    **This is a last resort.** Before enabling it, consider:

    - If pipelines only need to **publish** modules, use the [GitOps workflow](guides/gitops.md) — no pipeline credentials required at all.
    - If pipelines need to **read** the registry without cluster access, use [Dex Client Credentials](guides/cicd.md#registry-reads-with-dex-client-credentials) — pipelines get a Dex-issued token that respects GroupBinding.

See [CI/CD with ServiceAccount Fallback](configuration/oidc.md#cicd-with-serviceaccount-fallback) for setup details and the required RBAC.

#### Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| `missing Authorization header` | `.tofurc` missing `credentials` block | Add `credentials "host" { token = "..." }` block |
| `unauthorized` | JWT expired or invalid | Re-run `tofu login` to obtain a fresh JWT |
| `CrashLoopBackOff` on server pod | `server.oidc.issuerUrl` not set or misconfigured | Verify OIDC is enabled and issuer URL is correct; check pod logs |
| Browser opens to an in-cluster hostname (e.g. `opendepot-dex.opendepot-system.svc...`) | `server.oidc.issuerUrl` was left blank; chart derived the in-cluster service URL | Set both `dex.config.issuer` and `server.oidc.issuerUrl` to the external Dex URL and redeploy |
| Browser redirects to localhost but connection fails | Dex `redirectURI` not included in client config | Add all expected localhost ports (10000-10010) to Dex client redirectURIs |
| Stats page shows zeroes for UI-authenticated users | `ui.oidc.clientId` not propagated to the server | Ensure `ui.oidc.enabled: true` and `ui.oidc.clientId` is non-empty in Helm values; the chart passes `--oidc-ui-client-id` to the server automatically |

### Method 2: Managed Cluster Tokens

!!! note "Required server configuration"
    This method requires one of the following:

    - **`server.useBearerToken: true`** — server operates in pure bearer-token mode (no OIDC).
    - **`server.oidc.allowServiceAccountFallback: true`** — OIDC is the primary auth mode, but tokens whose `iss` claim does not match the OIDC issuer are forwarded to Kubernetes as bearer tokens.

    If OIDC is enabled without `allowServiceAccountFallback`, managed-cluster tokens are rejected because they are not Dex-issued JWTs.

Use the token issued by your cluster's native auth flow when the CI job already has access to the Kubernetes API. OpenDepot forwards that token to Kubernetes; if the API server accepts it, registry reads and module downloads work without Dex credentials.

The variable name is derived from the registry hostname: replace dots with underscores and convert to uppercase.

`opendepot.defdev.io` → `TF_TOKEN_OPENDEPOT_DEFDEV_IO`

=== "Amazon EKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(aws eks get-token \
      --cluster-name my-cluster \
      --region us-west-2 \
      --output json | jq -r '.status.token')

    tofu init
    tofu plan
    ```

=== "Azure AKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(az account get-access-token \
      --resource 6dae42f8-4368-4678-94ff-3960e28e3630 \
      --query accessToken -o tsv)

    tofu init
    tofu plan
    ```

=== "Google GKE"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(gcloud auth print-access-token)

    tofu init
    tofu plan
    ```

Tokens are short-lived and automatically rotate, making this the preferred option for CI/CD jobs when the cluster accepts the provider-issued token.

#### CI/CD Example

```yaml
name: Apply Infrastructure

on:
  push:
    branches: [main]

jobs:
  apply:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::ACCOUNT_ID:role/github-actions-role
          aws-region: us-west-2

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1

      - name: Set registry token
        run: |
          TOKEN=$(aws eks get-token --cluster-name my-cluster --region us-west-2 --output json | jq -r '.status.token')
          echo "TF_TOKEN_OPENDEPOT_DEFDEV_IO=$TOKEN" >> $GITHUB_ENV

      - run: tofu init
      - run: tofu plan
```

### Method 3: Base64-Encoded Kubeconfig (Local Development)

For development or environments where environment variables are not practical or OIDC is overkill, encode your kubeconfig and store it in a credentials file.

!!! note
    This method requires `server.useBearerToken: false` in your Helm values.

**1. Encode your kubeconfig:**

```bash
kubectl config view --raw | base64 | tr -d '\n' > /tmp/kubeconfig.b64
```

**2. Create `~/.terraform.d/credentials.tfrc.json`:**

```json
{
  "credentials": {
    "opendepot.defdev.io": {
      "token": "<contents-of-kubeconfig.b64>"
    }
  }
}
```

```bash
chmod 600 ~/.terraform.d/credentials.tfrc.json
```

### Authentication Comparison

| Feature | Bearer Token | Kubeconfig File | OIDC (Dex) | OIDC + SA Fallback |
|---------|---------------------|-----------------|------------|-------------------|
| Token Lifetime | Short-lived (auto-rotating) | Long-lived (manual rotation) | Short-lived (1 hour typical) | Mixed (SA token + JWT) |
| Security | High | Good | Highest | High |
| Setup Complexity | Low | Low | Medium | Medium |
| Credential Distribution | Via env var or shell | File-based | No distribution (SSO) | No distribution |
| Best For | Production, CI/CD | Development | Enterprise production (SSO) | OIDC orgs where pipelines must have direct cluster API access; prefer [GitOps](guides/gitops.md) or [Dex CC](guides/cicd.md#registry-reads-with-dex-client-credentials) first |
| `tofu login` Support | No | No | Yes | Yes (human users) |
| OpenTofu Support | All versions | All versions | All versions | All versions |
| Terraform Support | v1.2+ | All versions | v1.3+ | v1.3+ |
| IdP Integration | No | No | Yes (GitHub, Entra ID, Okta, LDAP, etc.) | Yes |

## Registry Explorer UI Authentication

The [Registry Explorer UI](guides/registry-explorer.md) has its own browser-based authentication flow that is separate from the `tofu login` CLI flow described above. The UI's OIDC client is registered independently and issues its own access tokens; it does not share a token with the CLI client.

### Sign-in / Sign-out flow

When `ui.oidc.enabled: true`, the Sidebar displays a **Sign in** button. Clicking it redirects the browser to `/auth/login`, which initiates the OIDC authorization code flow. After a successful redirect back from the identity provider, the session stores the resulting access token in an encrypted server-side cookie (iron-session).

The Sidebar footer then shows the authenticated user's display name (derived from the `name` or `preferred_username` JWT claim) and a **Sign out** button. Sign-out hits `/auth/logout`, which clears the session cookie.

### Session token forwarding

The UI is a Next.js application with server components. On every page render the server component reads the access token from the session cookie and forwards it in an `Authorization: Bearer <token>` header to each browse API call (`/opendepot/ui/v1/*`). The server uses this token to evaluate `GroupBinding` resources and extend visibility beyond the public set.

Unauthenticated users (no session cookie, or session without a token) receive only publicly-labelled resources.

### Provider unavailability (503)

If the OIDC provider is unreachable when a user attempts to sign in, `/auth/login` returns **HTTP 503**. This allows load balancers, health checks, and monitoring to distinguish provider unavailability from an application error (HTTP 500).

### Developer token input

!!! danger "Local development only"
    Developer token mode is controlled by the `ui.auth.devTokenInput.enabled` Helm value. It must be `false` in all production environments.

When `ui.auth.devTokenInput.enabled: true`, the Sidebar shows a text input labelled **Dev Bearer Token**. A developer can paste any raw Kubernetes SA token (a kubeconfig bearer token) directly into the field. The UI posts the value to `/auth/dev-token`, which stores it in the encrypted session as `devToken`. Subsequent server component renders prefer `devToken` over the OIDC access token. Submitting an empty value clears the stored `devToken`.

This allows testing the UI against a live cluster without configuring a full OIDC provider — paste a `kubectl create token` output and browse immediately.

See [Developer Token Input](guides/registry-explorer.md#3-set-helm-values) for configuration details.
