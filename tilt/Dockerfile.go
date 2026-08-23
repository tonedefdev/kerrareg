FROM aquasec/trivy:0.74.0 AS trivy-source

FROM golang:1.25.13 AS server-runtime
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
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.25.13 AS depot-runtime
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

FROM golang:1.25.13 AS module-runtime
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

FROM golang:1.25.13 AS provider-runtime
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

FROM golang:1.25.13 AS version-runtime
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
USER 65532:65532