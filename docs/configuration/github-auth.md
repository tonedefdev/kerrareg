# GitHub Authentication

For private repositories and to avoid GitHub API rate limits, create a GitHub App and store its credentials as a Kubernetes Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kerrareg-github-application-secret
  namespace: kerrareg-system
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
