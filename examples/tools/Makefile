# Tools Makefile - Build ToolPackage container images

# Image URLs
STRING_TOOLS_IMG ?= ghcr.io/jarsater/string-tools:latest

.PHONY: all
all: docker-build

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

.PHONY: docker-build
docker-build: docker-build-string-tools ## Build all tool package images

.PHONY: docker-build-string-tools
docker-build-string-tools: ## Build string-tools image
	docker build --load -t $(STRING_TOOLS_IMG) -f string-tools/Dockerfile string-tools/
