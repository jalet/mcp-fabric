# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /workspace

# Copy go mod files for both gateway and shared logging package
COPY gateway/go.mod gateway/go.sum ./gateway/
COPY pkg/logging/go.mod pkg/logging/go.sum ./pkg/logging/

# Download dependencies
RUN cd gateway && go mod download

# Copy source
COPY gateway/ ./gateway/
COPY pkg/logging/ ./pkg/logging/

# Build
RUN cd gateway && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o ../gateway ./cmd/gateway

# Runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/gateway .

USER 65534:65534

EXPOSE 8080

ENTRYPOINT ["/gateway"]
