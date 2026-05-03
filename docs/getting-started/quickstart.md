---
tags:
  - quickstart
  - kind
  - local-development
search:
  boost: 2
---

# Local Quickstart (kind)

The fastest way to try OpenDepot is with a local [kind](https://kind.sigs.k8s.io/) cluster using the filesystem storage backend and `hostPath`. This avoids any cloud provider setup — no S3 bucket, no Azure Storage Account, no credentials, no ingress controller, and no TLS certificates. You'll have a fully functional registry in minutes using `kubectl port-forward` and the public `*.localtest.me` DNS service (all `*.localtest.me` hostnames resolve to `127.0.0.1`).

!!! note
    OpenTofu and Terraform require module registry hostnames to contain at least one dot. `localhost` alone is not valid. `opendepot.localtest.me` resolves to `127.0.0.1` via public DNS, making it a convenient dotted hostname for local testing without editing `/etc/hosts` or installing any ingress controller.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm 3](https://helm.sh/docs/intro/install/)
- [OpenTofu](https://opentofu.org/docs/intro/install/) or [Terraform](https://developer.hashicorp.com/terraform/install)

## Step 1: Create the Cluster

```bash
kind create cluster --name opendepot
```

## Step 2: Deploy with Helm

```bash
helm repo add opendepot https://tonedefdev.github.io/opendepot
helm repo update
helm install opendepot opendepot/opendepot \
  -n opendepot-system --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/data/modules \
  --set server.anonymousAuth=true \
  --wait
```

Verify all pods are running:

```bash
kubectl get pods -n opendepot-system
```

!!! note
    **Apple Silicon users:** If building from source, the default `PLATFORM` is `linux/arm64`. For Intel Macs or Linux, run `make deploy PLATFORM=linux/amd64`.

## Step 3: Port-Forward the Server

In a separate terminal, forward the OpenDepot server to a local port:

```bash
kubectl port-forward svc/server 8080:80 -n opendepot-system
```

The server is now reachable at `http://opendepot.localtest.me:8080` — no ingress controller or TLS certificate required. OpenTofu will resolve `opendepot.localtest.me` to `127.0.0.1` via public DNS and connect through the port-forward.

Verify service discovery is working:

```bash
curl http://opendepot.localtest.me:8080/.well-known/terraform.json
```

Expected output:

```json
{"modules.v1":"/opendepot/modules/v1/"}
```

## Step 4: Create a Test Module

Apply a `Module` resource that pulls a small public module from GitHub:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: terraform-aws-key-pair
  namespace: opendepot-system
spec:
  moduleConfig:
    provider: aws
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-key-pair
    fileFormat: zip
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: "2.0.0"
EOF
```

!!! note
    The Module CR name (`terraform-aws-key-pair`) must match the GitHub repository name, because the module controller uses it as the repository name when fetching archives if `spec.moduleConfig.name` is omitted.

Watch the Version resource sync:

```bash
kubectl get versions -n opendepot-system -w
```

Once `SYNCED` shows `true`, the module archive has been fetched from GitHub and stored in the local filesystem.

## Step 5: Use the Registry with OpenTofu

Create a working directory with a Terraform/OpenTofu config and a `.tofurc` (or `.terraformrc`) that points OpenTofu at your local registry:

```bash
mkdir /tmp/opendepot-test && cd /tmp/opendepot-test

cat > main.tf <<'EOF'
module "key_pair" {
  source  = "opendepot.localtest.me:8080/opendepot-system/terraform-aws-key-pair/aws"
  version = "2.0.0"
}
EOF

cat > .tofurc <<'EOF'
host "opendepot.localtest.me:8080" {
  services = {
    "modules.v1" = "http://opendepot.localtest.me:8080/opendepot/modules/v1/"
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu init
```

The `.tofurc` `host` block overrides the default HTTPS protocol discovery for this hostname, allowing plain HTTP over the port-forward. You should see OpenTofu download the module from your local OpenDepot instance:

```
Initializing modules...
Downloading opendepot.localtest.me:8080/opendepot-system/terraform-aws-key-pair/aws 2.0.0 for key_pair...
- key_pair in .terraform/modules/key_pair

OpenTofu has been successfully initialized!
```

## Step 6: (Optional) Test with Authentication

To test OpenDepot's Kubernetes-native auth, redeploy with `anonymousAuth` disabled:

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  --set server.anonymousAuth=false \
  --set server.useBearerToken=true \
  --wait
```

Create a ServiceAccount and bind it to a read-only role:

```bash
kubectl create serviceaccount test-user -n opendepot-system

kubectl create role opendepot-reader -n opendepot-system \
  --resource=modules.opendepot.defdev.io,versions.opendepot.defdev.io \
  --verb=get,list,watch

kubectl create rolebinding test-user-reader -n opendepot-system \
  --role=opendepot-reader \
  --serviceaccount=opendepot-system:test-user
```

Generate a short-lived token and set it in `.tofurc`:

```bash
TOKEN=$(kubectl create token test-user -n opendepot-system --duration=1h)

cat > /tmp/opendepot-test/.tofurc <<EOF
host "opendepot.localtest.me:8080" {
  services = {
    "modules.v1" = "http://opendepot.localtest.me:8080/opendepot/modules/v1/"
  }
  token = "${TOKEN}"
}
EOF

TF_CLI_CONFIG_FILE=/tmp/opendepot-test/.tofurc tofu init
```

OpenTofu sends the bearer token to OpenDepot, which forwards it to the Kubernetes API for authentication and RBAC authorization. This is the same flow used in production — no separate user database or API keys required.

## Step 7: (Optional) Test with a Depot

To test automatic version discovery from GitHub:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: test-depot
  namespace: opendepot-system
spec:
  global:
    moduleConfig:
      fileFormat: zip
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  moduleConfigs:
    - name: terraform-aws-key-pair
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 2.0.0, <= 2.1.1"
  providerConfigs:
    - name: random
      operatingSystems:
        - linux
      architectures:
        - amd64
      versionConstraints: "= 3.6.0"
      storageConfig:
        fileSystem:
          directoryPath: /data/modules
EOF
```

The Depot controller queries GitHub releases for modules and the HashiCorp Releases API for providers, creates `Module` and `Provider` resources for matching versions, and the pipeline syncs them to local storage automatically.

## Step 8: (Optional) Test with a Provider

Providers are synced from the [HashiCorp Releases API](https://releases.hashicorp.com) and served via the [Terraform Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol). Provider binaries can be large (the `aws` provider for a single OS/arch is ~700 MB), so this step is optional.

**Step 8a: Generate a GPG key for provider signing**

OpenTofu verifies a GPG signature over the `SHA256SUMS` file when installing a provider. Generate a dedicated key and store it as a Kubernetes Secret:

```bash
# Generate a key (no passphrase, batch mode)
gpg --batch --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Name-Real: OpenDepot Local
Name-Email: opendepot@local.test
Expire-Date: 0
%no-protection
EOF

KEY_ID=$(gpg --list-keys --with-colons opendepot@local.test | awk -F: '/^pub/{print $5}' | tail -1)
ASCII_ARMOR=$(gpg --armor --export "$KEY_ID")
PRIVATE_B64=$(gpg --armor --export-secret-keys "$KEY_ID" | base64 | tr -d '\n')

kubectl create secret generic opendepot-provider-gpg \
  --namespace opendepot-system \
  --from-literal=OPENDEPOT_PROVIDER_GPG_KEY_ID="$KEY_ID" \
  --from-literal=OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR="$ASCII_ARMOR" \
  --from-literal=OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64="$PRIVATE_B64"
```

**Step 8b: Redeploy OpenDepot with the provider controller and GPG secret**

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  --set provider.enabled=true \
  --set server.gpg.secretName=opendepot-provider-gpg \
  --wait
```

**Step 8c: Create a Provider resource**

```bash
cat <<EOF | kubectl apply -f -
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: aws
  namespace: opendepot-system
spec:
  providerConfig:
    name: aws
    operatingSystems:
      - linux
    architectures:
      - amd64
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: "5.80.0"
EOF
```

Watch the Version resource sync (this downloads ~700 MB from HashiCorp):

```bash
kubectl get versions -n opendepot-system -w
```

Once `SYNCED` shows `true`, the provider binary is stored in the local filesystem.

**Step 8d: Use the provider registry with OpenTofu**

```bash
mkdir /tmp/opendepot-provider-test && cd /tmp/opendepot-provider-test

cat > main.tf <<'EOF'
terraform {
  required_providers {
    aws = {
      source  = "opendepot.localtest.me:8080/opendepot-system/aws"
      version = "5.80.0"
    }
  }
}
EOF

cat > .tofurc <<'EOF'
host "opendepot.localtest.me:8080" {
  services = {
    "providers.v1" = "http://opendepot.localtest.me:8080/opendepot/providers/v1/"
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu init
```

The `.tofurc` `host` block overrides HTTPS protocol discovery for this hostname, allowing plain HTTP over the port-forward. OpenTofu will resolve `opendepot.localtest.me` to `127.0.0.1` and install the provider from your local OpenDepot instance:

```
Initializing provider plugins...
- Finding opendepot.localtest.me:8080/opendepot-system/aws versions matching "5.80.0"...
- Installing opendepot.localtest.me:8080/opendepot-system/aws v5.80.0...
- Installed opendepot.localtest.me:8080/opendepot-system/aws v5.80.0

OpenTofu has been successfully initialized!
```

## Step 9: (Optional) Test Trivy Scanning

This step shows Trivy scanning in action against the module from Step 4 and, if you completed Step 8, the provider as well.

**Step 9a: Enable scanning**

Kind uses a single-node cluster, so `ReadWriteOnce` access mode and the default storage class are sufficient. Set `offline=false` so Trivy downloads the vulnerability database directly rather than waiting for the CronJob to complete on a fresh cluster:

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  --set scanning.enabled=true \
  --set scanning.offline=false \
  --set scanning.cache.accessMode=ReadWriteOnce \
  --wait
```

!!! note
    `scanning.offline=false` is a convenience for local development. In production, leave `offline=true` (the default) and rely on the `trivy-db-updater` CronJob to keep the database current.

**Step 9b: Populate the Trivy database**

Manually trigger the `trivy-db-updater` CronJob so the database is ready before scans run:

```bash
kubectl create job trivy-db-init \
  --from=cronjob/trivy-db-updater \
  -n opendepot-system

kubectl wait --for=condition=complete \
  job/trivy-db-init \
  -n opendepot-system \
  --timeout=120s
```

**Step 9c: Trigger a module scan**

Force-resync the module from Step 4 to trigger an IaC scan:

```bash
kubectl patch module terraform-aws-key-pair -n opendepot-system \
  --type merge -p '{"spec":{"forceSync":true}}'
```

Watch the Version resource reconcile, then inspect the IaC findings:

```bash
kubectl get versions -n opendepot-system -w
# wait for SYNCED=true, then Ctrl-C

kubectl get version terraform-aws-key-pair-2.0.0 \
  -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

You should see something like:

```json
{
  "scannedAt": "2026-05-03T02:11:00Z",
  "findings": [
    {
      "vulnerabilityID": "AVD-AWS-0057",
      "pkgName": "aws_key_pair",
      "installedVersion": "",
      "severity": "LOW",
      "title": "Key pair does not use a modern key algorithm"
    }
  ]
}
```

Module IaC findings contain Trivy rule IDs (e.g. `AVD-AWS-0057`) rather than CVE identifiers. An empty `findings` array means no misconfigurations were detected.

**Step 9d: (Requires Step 8) Inspect provider scan results**

If you completed Step 8, the provider binary and source scans run automatically. Force-resync the provider to ensure scan results are populated:

```bash
kubectl patch provider aws -n opendepot-system \
  --type merge -p '{"spec":{"forceSync":true}}'

kubectl get versions -n opendepot-system -w
# wait for aws Version to show SYNCED=true, then Ctrl-C
```

Check the binary scan on the Version resource (per OS/arch):

```bash
kubectl get version aws-5.80.0-linux-amd64 \
  -n opendepot-system \
  -o jsonpath='{.status.binaryScan}' | jq .
```

```json
{
  "scannedAt": "2026-05-03T02:12:00Z",
  "findings": [
    {
      "vulnerabilityID": "CVE-2024-24790",
      "pkgName": "stdlib",
      "installedVersion": "1.22.3",
      "fixedVersion": "1.22.4",
      "severity": "CRITICAL",
      "title": "net/netip: Unexpected behavior from Is methods for IPv4-mapped IPv6 addresses"
    }
  ]
}
```

Check the source scan on the Provider resource (shared across all OS/arch variants):

```bash
kubectl get provider aws \
  -n opendepot-system \
  -o jsonpath='{.status.sourceScan}' | jq .
```

```json
{
  "scannedAt": "2026-05-03T02:12:05Z",
  "version": "5.80.0",
  "findings": []
}
```

Provider binary findings contain CVE identifiers and package version details. The source scan covers `go.mod` dependencies — an empty `findings` array means no vulnerable dependencies were detected.

## Cleanup

```bash
kubectl port-forward svc/server 8080:80 -n opendepot-system  # stop with Ctrl-C
kind delete cluster --name opendepot
```


