{
  description = "MCP Fabric / Agent Fabric development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };
      in
      {
        devShells.default = pkgs.mkShell {
          name = "mcp-fabric";

          buildInputs = with pkgs; [
            # Go development
            go_1_25
            gopls
            golangci-lint
            delve
            gotools
            go-tools

            # Kubernetes tools
            kubectl
            kubernetes-helm
            kustomize
            kubebuilder
            kind
            k9s
            stern
            kubectx
            # Container tools
            docker
            docker-compose
            skopeo
            crane

            # Code generation
            protobuf
            protoc-gen-go
            protoc-gen-go-grpc

            # General tools
            jq
            yq-go
            curl
            git
            gnumake
            envsubst

            # Testing
            ginkgo

            # Documentation
            mdbook

            # Python for MCP server
            (python312.withPackages (ps: with ps; [
              mcp
              httpx
              pip
            ]))
          ];

          shellHook = ''
            echo "MCP Fabric development environment"
            echo ""
            echo "Go version: $(go version)"
            echo "kubectl version: $(kubectl version --client -o json 2>/dev/null | jq -r '.clientVersion.gitVersion')"
            echo "kustomize version: $(kustomize version --short 2>/dev/null || echo 'not available')"
            echo ""
            echo "Available commands:"
            echo "  make build       - Build operator and gateway"
            echo "  make deploy      - Deploy to cluster"
            echo "  make test        - Run tests"
            echo ""

            # Check if kind cluster exists
            if kind get clusters 2>/dev/null | grep -q mcp-fabric; then
              echo "Kind cluster 'mcp-fabric' is running"
              kubectl config use-context kind-mcp-fabric 2>/dev/null || true
            fi

            # Set Go environment
            export GOPATH="$HOME/go"
            export PATH="$GOPATH/bin:$PATH"

            # Install controller-gen if not present
            if ! command -v controller-gen &> /dev/null; then
              echo "Installing controller-gen..."
              go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest 2>/dev/null || true
            fi

            # Kubebuilder assets for envtest
            export KUBEBUILDER_ASSETS="${pkgs.kubebuilder}/bin"
          '';

          # Environment variables
          GOPRIVATE = "github.com/jarsater/*";
          CGO_ENABLED = "0";
        };
      }
    );
}
