# Examples Makefile - Build all example agents, tools, and libraries

.PHONY: all
all: docker-build

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

.PHONY: docker-build
docker-build: docker-build-libs docker-build-agents docker-build-tools ## Build all example images

.PHONY: docker-build-agents
docker-build-agents: ## Build all example agent images
	$(MAKE) -C agents docker-build

.PHONY: docker-build-tools
docker-build-tools: ## Build all example tool images
	$(MAKE) -C tools docker-build

.PHONY: docker-build-libs
docker-build-libs: ## Build all example library images
	$(MAKE) -C libs docker-build
