# Build the manager binary
FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go.work and all module manifests for workspace-aware builds
COPY go.work go.work
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/server/go.mod services/server/go.sum services/server/
COPY services/version/go.mod services/version/go.sum services/version/

# Download dependencies (workspace off since not all modules are copied yet)
RUN cd services/module && GOWORK=off go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY services/module/cmd/ services/module/cmd/
COPY services/module/internal/ services/module/internal/

# Build
RUN cd services/module && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/module-controller cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/module-controller .
USER 65532:65532

ENTRYPOINT ["/module-controller"]
