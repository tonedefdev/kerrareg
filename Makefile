REGISTRY ?= ghcr.io/tonedefdev/kerrareg
PLATFORM ?= linux/arm64
KIND_CLUSTER ?= kind
TAG ?= dev

SERVICES := server depot-controller module-controller version-controller

# Map service names to their build context directories
server_PATH := services/server
depot-controller_PATH := services/depot
module-controller_PATH := services/module
version-controller_PATH := services/version

# Map service names to their Docker build context (repo root for services that use local packages)
server_CONTEXT := .
depot-controller_CONTEXT := .
module-controller_CONTEXT := .
version-controller_CONTEXT := .

.PHONY: build load deploy clean $(addprefix build-,$(SERVICES)) $(addprefix load-,$(SERVICES))

## Build all images for the target platform
build: $(addprefix build-,$(SERVICES))

## Load all images into the kind cluster
load: $(addprefix load-,$(SERVICES))

## Build and load all images into the kind cluster
deploy: build load

CHART_PATH ?= chart/kerrareg
ISTIO_VERSION ?= 1.28.3

## Recreate kind cluster with Istio, TLS, gateway, Helm chart, and cloud-provider-kind
kind-restart:
	@echo "=== Deleting existing kind cluster ==="
	kind delete cluster --name $(KIND_CLUSTER)
	@echo "=== Creating kind cluster ==="
	kind create cluster --name $(KIND_CLUSTER)
	@echo "=== Loading images into kind ==="
	@$(MAKE) load
	@echo "=== Installing Istio ==="
	helm install istio-base istio/base -n istio-system --create-namespace --wait
	helm install istiod istio/istiod -n istio-system --wait
	@echo "=== Creating istio-ingress namespace ==="
	kubectl create namespace istio-ingress || true
	@echo "=== Installing Istio ingress gateway ==="
	helm install istio-ingress istio/gateway -n istio-ingress --wait || true
	@echo "=== Applying TLS secret ==="
	kubectl apply -f $(CHART_PATH)/tls-secret.yaml
	@echo "=== Applying Istio gateway ==="
	kubectl apply -f $(CHART_PATH)/gateway.yaml
	@echo "=== Deploying Helm chart ==="
	helm upgrade --install kerrareg $(CHART_PATH) -n kerrareg-system --create-namespace --wait
	@echo "=== Starting cloud-provider-kind ==="
	@pkill -f cloud-provider-kind 2>/dev/null || true
	@sleep 1
	sudo cloud-provider-kind &>/tmp/cloud-provider-kind.log &
	@echo "Waiting for cloud-provider-kind to start..."
	@sleep 10
	@echo "=== Port mappings ==="
	@docker port $$(docker ps -q --filter name=kindccm) 2>/dev/null || echo "No proxy container found yet — check: docker ps --filter name=kindccm"

define SERVICE_RULES
.PHONY: build-$(1) load-$(1)

build-$(1):
	docker build --platform $(PLATFORM) \
		-t $(REGISTRY)/$(1):$(TAG) \
		-f $($(1)_PATH)/Dockerfile \
		$($(1)_CONTEXT)

load-$(1):
	kind load docker-image $(REGISTRY)/$(1):$(TAG) --name $(KIND_CLUSTER)
endef

$(foreach svc,$(SERVICES),$(eval $(call SERVICE_RULES,$(svc))))

## Build and load a single service: make service NAME=server
service:
	@$(MAKE) build-$(NAME) load-$(NAME)

## Restart deployments in kerrareg-system after loading new images
restart:
	kubectl rollout restart deployment -n kerrareg-system

## Build, load, and restart all
redeploy: deploy restart

clean:
	@for svc in $(SERVICES); do \
		docker rmi $(REGISTRY)/$$svc:$(TAG) 2>/dev/null || true; \
	done
