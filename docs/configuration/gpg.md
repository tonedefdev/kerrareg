# GPG Signing for Providers

The Terraform Provider Registry Protocol requires that providers ship a `SHA256SUMS` file and a detached GPG signature (`SHA256SUMS.sig`). OpenTofu downloads both and verifies the signature using the public key returned by the registry's package metadata endpoint. Kerrareg handles signing automatically — you provide the key, and the server signs on every request.

**Generating a key pair**

Use any GPG key management workflow you prefer. The key must have no passphrase so the server can sign without interactive input.

```bash
gpg --batch --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Name-Real: My Org Kerrareg
Name-Email: kerrareg@myorg.io
Expire-Date: 0
%no-protection
EOF
```

**Extracting key material**

```bash
KEY_ID=$(gpg --list-keys --with-colons kerrareg@myorg.io | awk -F: '/^pub/{print $5}' | tail -1)
ASCII_ARMOR=$(gpg --armor --export "$KEY_ID")
PRIVATE_B64=$(gpg --armor --export-secret-keys "$KEY_ID" | base64 | tr -d '\n')
```

**Creating the Kubernetes Secret**

```bash
kubectl create secret generic kerrareg-provider-gpg \
  --namespace kerrareg-system \
  --from-literal=KERRAREG_PROVIDER_GPG_KEY_ID="$KEY_ID" \
  --from-literal=KERRAREG_PROVIDER_GPG_ASCII_ARMOR="$ASCII_ARMOR" \
  --from-literal=KERRAREG_PROVIDER_GPG_PRIVATE_KEY_BASE64="$PRIVATE_B64"
```

**Referencing the Secret in Helm**

```bash
helm upgrade kerrareg chart/kerrareg \
  -n kerrareg-system \
  --reuse-values \
  --set server.gpg.secretName=kerrareg-provider-gpg \
  --wait
```

Or in your `values.yaml`:

```yaml
server:
  gpg:
    secretName: kerrareg-provider-gpg
```

!!! warning
    The `KERRAREG_PROVIDER_GPG_PRIVATE_KEY_BASE64` value must be the base64-encoded ASCII armor of the private key (i.e., the PEM-style block is base64-encoded). The server decodes it automatically before signing. Do not store the raw private key directly.

!!! note
    The ASCII-armored **public** key (`KERRAREG_PROVIDER_GPG_ASCII_ARMOR`) is returned verbatim in the provider package metadata response so OpenTofu can verify the signature without any out-of-band key exchange. OpenTofu will prompt the user to confirm a new signing key the first time a provider is installed from this registry — this is expected behavior.
