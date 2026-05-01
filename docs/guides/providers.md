# Consuming Providers

Once providers are synced, declare them as required providers in your OpenTofu or Terraform configuration using the `<registry-host>/<namespace>/<name>` source format:

```hcl
terraform {
  required_providers {
    aws = {
      source  = "kerrareg.defdev.io/kerrareg-system/aws"
      version = "~> 5.80"
    }
    azurerm = {
      source  = "kerrareg.defdev.io/kerrareg-system/azurerm"
      version = ">= 4.0.0"
    }
  }
}
```

The source format is `<registry-host>/<namespace>/<name>`, where `<namespace>` is the Kubernetes namespace where the `Provider` resource lives and `<name>` matches `spec.providerConfig.name` (or the `Provider` resource name if `name` is omitted).

**Pointing OpenTofu at the provider registry**

Because Kerrareg serves providers at a custom host, you need a `host` block in your `.tofurc` or `.terraformrc` to tell OpenTofu where the `providers.v1` API lives:

```
host "kerrareg.defdev.io" {
  services = {
    "providers.v1" = "https://kerrareg.defdev.io/kerrareg/providers/v1/"
  }
}
```

With authentication (recommended for production):

```
host "kerrareg.defdev.io" {
  services = {
    "providers.v1" = "https://kerrareg.defdev.io/kerrareg/providers/v1/"
  }
  token = "<kubernetes-bearer-token>"
}
```

Or using the environment variable approach:

```bash
export TF_TOKEN_KERRAREG_DEFDEV_IO=$(aws eks get-token \
  --cluster-name my-cluster \
  --region us-west-2 \
  --output json | jq -r '.status.token')

tofu init
```

!!! note
    Provider artifact downloads (the binary, `SHA256SUMS`, and `SHA256SUMS.sig`) do not require client authentication. OpenTofu fetches these URLs after receiving the download metadata from the auth-protected `download` endpoint, and the Terraform Provider Registry Protocol does not forward credentials to artifact URLs. The server uses its own ServiceAccount for these requests. Security is enforced at the metadata tier where the download URL is issued.

**Adding a new provider version**

To publish a new version, append it to `spec.versions`:

```bash
kubectl patch provider aws -n kerrareg-system \
  --type json -p '[{"op":"add","path":"/spec/versions/-","value":{"version":"5.81.0"}}]'
```

The Provider controller creates new `Version` resources for every OS/architecture combination, and the Version controller fetches and stores the binaries automatically.

**Force re-sync**

```bash
kubectl patch provider aws -n kerrareg-system \
  --type merge -p '{"spec":{"forceSync":true}}'
```
