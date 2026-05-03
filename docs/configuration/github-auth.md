---
tags:
  - configuration
  - github
  - authentication
---

# GitHub Authentication

For private repositories and to avoid GitHub API rate limits, create a GitHub App and store its credentials as a Kubernetes Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: opendepot-github-application-secret
  namespace: opendepot-system
type: Opaque
data:
  githubAppID: <base64-encoded-app-id>
  githubInstallID: <base64-encoded-install-id>
  githubPrivateKey: <base64-encoded-private-key>
```
!!! warning
    The private key must be base64-encoded **before** being added to the Secret's `data` field (i.e., it is double base64-encoded: once for the PEM content, once by Kubernetes). The controller decodes both layers automatically.

Then enable authenticated access in your module config:

```yaml
githubClientConfig:
  useAuthenticatedClient: true
```

## Provider Source Scanning

The same `opendepot-github-application-secret` Secret and `githubClientConfig` field are also supported for **provider source scanning**. This is useful when the provider's source repository is private or when unauthenticated requests exceed GitHub API rate limits during source scans.

Set `githubClientConfig` on the `providerConfig` in your `Provider` resource:

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    githubClientConfig:
      useAuthenticatedClient: true
```

!!! note
    If the `opendepot-github-application-secret` Secret is missing or the authenticated client cannot be created, the Version controller falls back to an unauthenticated client automatically. Source scanning continues without interruption.

No new Secret is required if modules in the same namespace already use GitHub App authentication — the controller reads the same Secret for both.
