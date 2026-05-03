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
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/provider/go.mod services/provider/go.sum services/provider/
COPY services/server/go.mod services/server/go.sum services/server/

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/version && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY services/version/cmd/ services/version/cmd/
COPY services/version/internal/ services/version/internal/

# Build
RUN cd services/version && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/version-controller cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/version-controller .
# Copy the Trivy binary for optional provider vulnerability scanning
COPY --from=aquasec/trivy:0.70.0 /usr/local/bin/trivy /usr/local/bin/trivy
USER 65532:65532

ENTRYPOINT ["/version-controller"]
