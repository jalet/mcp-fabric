# Agents Makefile - Build agent container images

# Image URLs
AGENT_RUNNER_IMG ?= ghcr.io/jarsater/strands-agent-runner:latest
ENGINEERING_ARTIST_IMG ?= ghcr.io/jarsater/engineering-artist-agent:latest

.PHONY: all
all: docker-build

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

.PHONY: docker-build
docker-build: docker-build-default docker-build-engineering-artist ## Build all agent images

.PHONY: docker-build-default
docker-build-default: ## Build default agent runner image
	docker build --load -t $(AGENT_RUNNER_IMG) -f default/Dockerfile default/

.PHONY: docker-build-engineering-artist
docker-build-engineering-artist: ## Build Engineering Artist agent image
	docker build --load -t $(ENGINEERING_ARTIST_IMG) -f engineering-artist/Dockerfile engineering-artist/
