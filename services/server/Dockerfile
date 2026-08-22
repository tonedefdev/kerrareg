# Build the manager binary
FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go.work and all module manifests for workspace-aware builds
COPY go.work go.work.sum ./
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/utils/go.mod pkg/utils/
COPY pkg/testutils/go.mod pkg/testutils/
COPY pkg/registry/go.mod pkg/registry/
COPY services/server/go.mod services/server/go.sum services/server/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/provider/go.mod services/provider/go.sum services/provider/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/server && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY pkg/registry/ pkg/registry/
COPY services/server/ services/server/

# Build
RUN cd services/server && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/server .

# Supply the same pinned OpenTofu binary used by the version controller.
FROM debian:bookworm-slim AS tofu-source
ARG TARGETARCH
ARG TOFU_VERSION=1.10.6
ARG TOFU_SHA256_AMD64=15b7bed76420b50da3e121769c43341df8cd57d751ca14e6dbe9c850124c6dac
ARG TOFU_SHA256_ARM64=a32f653d686a8cad9b9be82101eb5b5e834fbfa8d095842fa1820c5d27fad967
RUN apt-get update \
		&& apt-get install -y --no-install-recommends ca-certificates curl unzip \
		&& rm -rf /var/lib/apt/lists/*
RUN set -eux; \
		arch="${TARGETARCH:-amd64}"; \
		case "$arch" in \
			amd64) sha="$TOFU_SHA256_AMD64" ;; \
			arm64) sha="$TOFU_SHA256_ARM64" ;; \
			*) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
		esac; \
		curl -fsSL -o /tmp/tofu.zip \
			"https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_${arch}.zip"; \
		echo "${sha}  /tmp/tofu.zip" | sha256sum -c -; \
		unzip -o /tmp/tofu.zip tofu -d /usr/local/bin; \
		chmod 0755 /usr/local/bin/tofu

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/server .
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532

ENTRYPOINT ["/server"]
