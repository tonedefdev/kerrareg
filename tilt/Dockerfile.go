FROM aquasec/trivy:0.72.0 AS trivy-source

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

FROM golang:1.25 AS server-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY services/server/go.mod services/server/go.sum services/server/
RUN go work init ./api/v1alpha1 ./pkg/storage ./pkg/utils ./services/server \
  && cd services/server && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/utils/ pkg/utils/
COPY services/server/ services/server/
RUN cd services/server && CGO_ENABLED=0 go build -o /workspace/bin/server . \
  && chown -R 65532:65532 /workspace
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.25 AS depot-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/registry/go.mod pkg/registry/
COPY services/depot/go.mod services/depot/go.sum services/depot/
RUN go work init ./api/v1alpha1 ./pkg/github ./pkg/registry ./services/depot \
  && cd services/depot && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/github/ pkg/github/
COPY pkg/registry/ pkg/registry/
COPY services/depot/ services/depot/
RUN cd services/depot && CGO_ENABLED=0 go build -o /workspace/bin/depot-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.25 AS module-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY services/module/go.mod services/module/go.sum services/module/
RUN go work init ./api/v1alpha1 ./pkg/utils ./services/module \
  && cd services/module && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/utils/ pkg/utils/
COPY services/module/ services/module/
RUN cd services/module && CGO_ENABLED=0 go build -o /workspace/bin/module-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.25 AS provider-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY services/provider/go.mod services/provider/go.sum services/provider/
RUN go work init ./api/v1alpha1 ./pkg/utils ./services/provider \
  && cd services/provider && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/utils/ pkg/utils/
COPY services/provider/ services/provider/
RUN cd services/provider && CGO_ENABLED=0 go build -o /workspace/bin/provider-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.25 AS version-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/registry/go.mod pkg/registry/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY services/version/go.mod services/version/go.sum services/version/
RUN go work init ./api/v1alpha1 ./pkg/github ./pkg/registry ./pkg/storage ./pkg/utils ./services/version \
  && cd services/version && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/github/ pkg/github/
COPY pkg/registry/ pkg/registry/
COPY pkg/storage/ pkg/storage/
COPY pkg/utils/ pkg/utils/
COPY services/version/ services/version/
RUN cd services/version && CGO_ENABLED=0 go build -o /workspace/bin/version-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM version-runtime AS version-dev
USER 0
COPY --from=trivy-source /usr/local/bin/trivy /usr/local/bin/trivy
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532