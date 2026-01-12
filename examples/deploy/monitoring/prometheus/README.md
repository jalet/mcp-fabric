# Prometheus Monitoring for MCP Fabric

This folder contains configuration for deploying [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) to monitor MCP Fabric components.

## Prerequisites

- Kubernetes cluster (kind, minikube, or cloud provider)
- [Helm](https://helm.sh/docs/intro/install/) v3+
- kubectl configured to access your cluster
- MCP Fabric operator and gateway deployed

## Quick Start

### Option 1: Kustomize (Recommended)

```bash
# Build and apply with Kustomize (requires --enable-helm)
kubectl kustomize deploy/samples/prometheus --enable-helm | kubectl apply -f -

# Or use kustomize directly
kustomize build --enable-helm deploy/samples/prometheus | kubectl apply -f -
```

### Option 2: Shell Script

```bash
./install.sh
```

This script will:
1. Add the prometheus-community Helm repo
2. Create a `monitoring` namespace
3. Install kube-prometheus-stack with MCP Fabric-optimized settings
4. Deploy the MCP Fabric Grafana dashboard

### Access Grafana

```bash
kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80
```

Open http://localhost:3000 and log in with:
- Username: `admin`
- Password: `admin`

### Access Prometheus

```bash
kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090
```

Open http://localhost:9090

## Verifying Metrics Collection

1. Open Prometheus UI at http://localhost:9090
2. Go to **Status > Targets**
3. Look for targets with labels:
   - `mcp-fabric-operator` - Operator controller metrics
   - `agent-gateway` - Gateway HTTP and MCP metrics

## Metrics Reference

For complete metrics documentation including:
- All available metrics by component
- PromQL query examples
- Alert rules
- ServiceMonitor configuration

See [METRICS.md](/METRICS.md).

## Grafana Dashboard

The `dashboards/` folder contains a pre-built dashboard for MCP Fabric. It's automatically loaded by the Grafana sidecar when you run `install.sh`.

Dashboard panels include:
- **Overview**: Agent status, request rates, error rates
- **Operator**: Reconciliation metrics by controller
- **Gateway**: Request latency, circuit breaker state
- **MCP Protocol**: Connection count, tool call rates
- **Agents**: GenAI request metrics

## Customization

### Change Namespace

```bash
MONITORING_NAMESPACE=custom-namespace ./install.sh
```

### Change Release Name

```bash
RELEASE_NAME=my-prometheus ./install.sh
```

### Modify Values

Edit `values.yaml` before running install, or use Helm directly:

```bash
helm upgrade prometheus prometheus-community/kube-prometheus-stack \
    -n monitoring \
    -f values.yaml \
    --set grafana.adminPassword=secure-password
```

## Cleanup

### Kustomize

```bash
kubectl kustomize deploy/samples/prometheus --enable-helm | kubectl delete -f -
kubectl delete namespace monitoring
```

### Shell Script

```bash
./uninstall.sh
```

## Troubleshooting

### Targets Not Appearing

1. Verify ServiceMonitor resources exist:
   ```bash
   kubectl get servicemonitors -A
   ```

2. Check Prometheus operator logs:
   ```bash
   kubectl logs -n monitoring -l app.kubernetes.io/name=prometheus-operator
   ```

3. Verify services have correct labels:
   ```bash
   kubectl get svc -A -l app.kubernetes.io/name=mcp-fabric-operator
   kubectl get svc -A -l app.kubernetes.io/name=agent-gateway
   ```

### No Metrics Data

1. Verify pods are running:
   ```bash
   kubectl get pods -n mcp-fabric-system
   kubectl get pods -n mcp-fabric-gateway
   ```

2. Test metrics endpoint directly:
   ```bash
   kubectl port-forward -n mcp-fabric-system svc/mcp-fabric-operator-metrics 8080:8080
   curl http://localhost:8080/metrics
   ```

### Dashboard Not Loading

1. Check Grafana sidecar logs:
   ```bash
   kubectl logs -n monitoring -l app.kubernetes.io/name=grafana -c grafana-sc-dashboard
   ```

2. Verify ConfigMap exists:
   ```bash
   kubectl get configmap mcp-fabric-dashboard -n monitoring
   ```
