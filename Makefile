# MCP Fabric Makefile

# Image URLs
OPERATOR_IMG ?= ghcr.io/jarsater/mcp-fabric-operator:latest
GATEWAY_IMG ?= ghcr.io/jarsater/mcp-fabric-gateway:latest

# Go settings
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Kubebuilder
CONTROLLER_GEN ?= $(shell which controller-gen 2>/dev/null)
ENVTEST ?= $(shell which setup-envtest 2>/dev/null)

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: generate
generate: ## Generate DeepCopy methods
	cd operator && go generate ./...

.PHONY: manifests
manifests: ## Generate CRD manifests
	@if [ -n "$(CONTROLLER_GEN)" ]; then \
		cd operator && $(CONTROLLER_GEN) crd:allowDangerousTypes=true paths="./api/..." output:crd:artifacts:config=config/crd/bases && \
		$(CONTROLLER_GEN) rbac:roleName=mcp-fabric-operator paths="./internal/controllers/..." output:rbac:artifacts:config=config/rbac; \
	else \
		echo "controller-gen not found, skipping manifest generation"; \
	fi

.PHONY: fmt
fmt: ## Format code
	cd operator && go fmt ./...
	cd gateway && go fmt ./...

.PHONY: vet
vet: ## Run go vet
	cd operator && go vet ./...
	cd gateway && go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	cd operator && golangci-lint run ./...
	cd gateway && golangci-lint run ./...

.PHONY: tidy
tidy: ## Run go mod tidy
	cd operator && go mod tidy
	cd gateway && go mod tidy

##@ Build

.PHONY: build
build: build-operator build-gateway ## Build all binaries

.PHONY: build-operator
build-operator: ## Build operator binary
	cd operator && CGO_ENABLED=0 go build -o bin/manager ./cmd/manager

.PHONY: build-gateway
build-gateway: ## Build gateway binary
	cd gateway && CGO_ENABLED=0 go build -o bin/gateway ./cmd/gateway

##@ Docker

.PHONY: docker-build
docker-build: generate manifests docker-build-operator docker-build-gateway ## Build operator and gateway Docker images

.PHONY: docker-build-operator
docker-build-operator: ## Build operator Docker image
	docker build --load -t $(OPERATOR_IMG) -f operator/Dockerfile .

.PHONY: docker-build-gateway
docker-build-gateway: ## Build gateway Docker image
	docker build --load -t $(GATEWAY_IMG) -f gateway/Dockerfile .

.PHONY: docker-push
docker-push: ## Push all Docker images
	docker push $(OPERATOR_IMG)
	docker push $(GATEWAY_IMG)

##@ Testing

.PHONY: test
test: test-operator test-gateway ## Run all tests

.PHONY: test-operator
test-operator: ## Run operator tests
	cd operator && go test ./... -coverprofile=coverage.out

.PHONY: test-gateway
test-gateway: ## Run gateway tests
	cd gateway && go test ./... -coverprofile=coverage.out

##@ CRDs

.PHONY: install-crds
install-crds: ## Install CRDs into cluster
	kubectl apply -f operator/config/crd/bases/

.PHONY: uninstall-crds
uninstall-crds: ## Uninstall CRDs from cluster
	kubectl delete -f operator/config/crd/bases/ --ignore-not-found

##@ Examples

.PHONY: examples
examples: ## Build all example images (agents, tools, libs)
	$(MAKE) -C examples docker-build

.PHONY: examples-agents
examples-agents: ## Build all example agent images
	$(MAKE) -C examples/agents docker-build

.PHONY: examples-tools
examples-tools: ## Build all example tool images
	$(MAKE) -C examples/tools docker-build

.PHONY: examples-libs
examples-libs: ## Build all example library images
	$(MAKE) -C examples/libs docker-build

##@ Monitoring

.PHONY: grafana-dashboards
grafana-dashboards: ## Generate Grafana dashboards using Kubebuilder plugin
	cd operator && kubebuilder edit --plugins grafana.kubebuilder.io/v1-alpha

##@ Clean

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf operator/bin gateway/bin
	rm -f operator/coverage.out gateway/coverage.out
