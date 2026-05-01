# API Reference


## Service Discovery

```
GET /.well-known/terraform.json
```

**Response:**

```json
{
  "modules.v1": "/kerrareg/modules/v1/",
  "providers.v1": "/kerrareg/providers/v1/"
}
```

## List Module Versions

```
GET /kerrareg/modules/v1/{namespace}/{name}/{system}/versions
```

Returns all available versions of a module. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Module resource |
| `name` | Module name |
| `system` | Provider (e.g., `aws`, `azurerm`) |

## Download Module

```
GET /kerrareg/modules/v1/{namespace}/{name}/{system}/{version}/download
```

Returns `204 No Content` with an `X-Terraform-Get` header pointing to the storage-specific download URL. Requires authentication.

## Storage Download Endpoints (Modules)

These endpoints are called by OpenTofu/Terraform after receiving the `X-Terraform-Get` redirect. They validate the SHA256 checksum and stream the module archive.

```
GET /kerrareg/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}?fileChecksum={checksum}
GET /kerrareg/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}?fileChecksum={checksum}
GET /kerrareg/modules/v1/download/gcs/{bucket}/{name}/{fileName}?fileChecksum={checksum}
GET /kerrareg/modules/v1/download/fileSystem/{directory}/{name}/{fileName}?fileChecksum={checksum}
```

## List Provider Versions

```
GET /kerrareg/providers/v1/{namespace}/{type}/versions
```

Returns all available versions of a provider and the platforms each version supports. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `type` | Provider name (e.g., `aws`, `azurerm`) |

**Response:**

```json
{
  "versions": [
    {
      "version": "5.80.0",
      "protocols": ["6.0"],
      "platforms": [
        { "os": "linux", "arch": "amd64" },
        { "os": "linux", "arch": "arm64" }
      ]
    }
  ]
}
```

## Provider Package Metadata

```
GET /kerrareg/providers/v1/{namespace}/{type}/{version}/download/{os}/{arch}
```

Returns the download URL, SHA256 checksum, and GPG signing key for a specific provider binary. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `type` | Provider name |
| `version` | Provider version |
| `os` | Operating system (e.g., `linux`, `darwin`) |
| `arch` | CPU architecture (e.g., `amd64`, `arm64`) |

**Response:**

```json
{
  "protocols": ["6.0"],
  "os": "linux",
  "arch": "amd64",
  "filename": "terraform-provider-aws_5.80.0_linux_amd64.zip",
  "download_url": "https://.../kerrareg/providers/v1/download/kerrareg-system/aws/5.80.0",
  "shasum": "<hex-sha256>",
  "shasums_url": "https://.../kerrareg/providers/v1/kerrareg-system/aws/5.80.0/SHA256SUMS/linux/amd64",
  "shasums_signature_url": "https://.../kerrareg/providers/v1/kerrareg-system/aws/5.80.0/SHA256SUMS.sig/linux/amd64",
  "signing_keys": {
    "gpg_public_keys": [
      {
        "key_id": "<KEY_ID>",
        "ascii_armor": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n..."
      }
    ]
  }
}
```

## Provider Binary Download

```
GET /kerrareg/providers/v1/download/{namespace}/{type}/{version}
```

Streams the provider binary archive (`.zip`) directly from storage. Does **not** require client authentication — the server uses its own ServiceAccount per the Terraform Provider Registry Protocol.

## Provider SHA256SUMS

```
GET /kerrareg/providers/v1/{namespace}/{type}/{version}/SHA256SUMS/{os}/{arch}
```

Returns the `SHA256SUMS` text file for the specified provider version and platform. Does **not** require client authentication.

## Provider SHA256SUMS Signature

```
GET /kerrareg/providers/v1/{namespace}/{type}/{version}/SHA256SUMS.sig/{os}/{arch}
```

Returns the detached GPG signature over the `SHA256SUMS` file, signed with the key configured in `server.gpg.secretName`. Does **not** require client authentication.

