# Libraries Makefile - Build shared library container images

# Image URLs
AGENT_LIBS_IMG ?= ghcr.io/jarsater/agent-libs:latest

.PHONY: all
all: docker-build

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

.PHONY: docker-build
docker-build: docker-build-agent-libs ## Build all library images

.PHONY: docker-build-agent-libs
docker-build-agent-libs: ## Build agent-libs image
	docker build --load -t $(AGENT_LIBS_IMG) -f agent-libs/Dockerfile agent-libs/
