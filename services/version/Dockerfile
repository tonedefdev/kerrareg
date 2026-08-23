# select final stage via build arg (default: no Trivy)
ARG INCLUDE_TRIVY=false

# Build the manager binary
FROM golang:1.25.13 AS builder
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
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/provider/go.mod services/provider/go.sum services/provider/
COPY services/server/go.mod services/server/go.sum services/server/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/version && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY pkg/registry/ pkg/registry/
COPY services/version/cmd/ services/version/cmd/
COPY services/version/internal/ services/version/internal/

# Build
RUN cd services/version && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/version-controller cmd/main.go

# trivy-source: pulled at build time; only copied into the final image when INCLUDE_TRIVY=true
FROM aquasec/trivy:0.74.0 AS trivy-source

# final image without Trivy binary
FROM gcr.io/distroless/static:nonroot AS final-false
WORKDIR /
COPY --from=builder /workspace/version-controller .
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with Trivy binary bundled
FROM gcr.io/distroless/static:nonroot AS final-true
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=trivy-source /usr/local/bin/trivy /usr/local/bin/trivy
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# select which final stage was built
FROM final-${INCLUDE_TRIVY}
