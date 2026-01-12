# Troubleshooting Guide

This guide covers common issues and their solutions when running MCP Fabric.

## Operator Issues

### CRDs Not Installing

**Symptom:** `kubectl apply -f operator/config/crd/bases/` fails or resources show `no matches for kind`

**Solution:**
```bash
# Ensure CRDs are applied first
kubectl apply -f operator/config/crd/bases/

# Verify CRDs are installed
kubectl get crds | grep fabric.jarsater.ai

# Expected output:
# agents.fabric.jarsater.ai
# routes.fabric.jarsater.ai
# tools.fabric.jarsater.ai
```

### Operator Pod Not Starting

**Symptom:** Operator pod stuck in `CrashLoopBackOff` or `Error`

**Check logs:**
```bash
kubectl -n mcp-fabric-system logs -l control-plane=controller-manager
```

**Common causes:**

1. **Missing RBAC permissions:**
   ```bash
   kubectl auth can-i --list --as=system:serviceaccount:mcp-fabric-system:mcp-fabric-operator
   ```

2. **Image pull error:**
   ```bash
   kubectl -n mcp-fabric-system describe pod -l control-plane=controller-manager
   # Look for "ImagePullBackOff" in Events
   ```

3. **Resource limits too low:**
   ```bash
   # Check if OOMKilled
   kubectl -n mcp-fabric-system get pod -l control-plane=controller-manager -o jsonpath='{.items[0].status.containerStatuses[0].lastState}'
   ```

### Agent Not Becoming Ready

**Symptom:** Agent CRD status shows not ready, pods not created

**Check operator logs:**
```bash
kubectl -n mcp-fabric-system logs -l control-plane=controller-manager | grep -i "error\|failed"
```

**Check agent status:**
```bash
kubectl -n mcp-fabric-agents describe agent <agent-name>
# Look at Status section and Events
```

**Common causes:**

1. **Tool not ready:**
   ```bash
   kubectl -n mcp-fabric-agents get tools
   # Ensure referenced tools show Ready status
   ```

2. **Invalid model configuration:**
   ```bash
   # Check agent spec for valid model provider and modelId
   kubectl -n mcp-fabric-agents get agent <agent-name> -o yaml
   ```

3. **Missing secret reference:**
   ```bash
   # Verify secret exists
   kubectl -n mcp-fabric-agents get secrets
   ```

## Gateway Issues

### Gateway Returns 404 for All Requests

**Symptom:** All `/v1/invoke` requests return `{"error": "no matching route found"}`

**Check routes:**
```bash
# Verify routes are synced
curl http://localhost:8080/v1/routes

# Check gateway logs for route loading
kubectl -n mcp-fabric-gateway logs -l app=mcp-fabric-gateway | grep -i route
```

**Solution:**
```bash
# Ensure routes are deployed
kubectl -n mcp-fabric-agents get routes

# Check gateway RBAC can list routes
kubectl auth can-i list routes.fabric.jarsater.ai --as=system:serviceaccount:mcp-fabric-gateway:mcp-fabric-gateway
```

### Gateway RBAC Forbidden Errors

**Symptom:** Gateway logs show `forbidden: User "system:serviceaccount:..." cannot list resource`

**Solution:**
```bash
# Verify RBAC resources exist
kubectl get clusterrole mcp-fabric-gateway
kubectl get clusterrolebinding mcp-fabric-gateway

# Check service account
kubectl -n mcp-fabric-gateway get serviceaccount mcp-fabric-gateway

# Reapply RBAC
kubectl apply -f deploy/kustomize/base/gateway/rbac.yaml
```

### Gateway Cannot Reach Agents

**Symptom:** Requests return `503` or timeout, logs show connection errors

**Check network connectivity:**
```bash
# From gateway pod, test agent endpoint
kubectl -n mcp-fabric-gateway exec -it deploy/mcp-fabric-gateway -- \
  curl -s http://text-assistant.mcp-fabric-agents.svc.cluster.local:8080/healthz
```

**Check DNS resolution:**
```bash
kubectl -n mcp-fabric-gateway exec -it deploy/mcp-fabric-gateway -- \
  nslookup text-assistant.mcp-fabric-agents.svc.cluster.local
```

**Check NetworkPolicy:**
```bash
# Verify egress is allowed to agents namespace
kubectl -n mcp-fabric-gateway get networkpolicy -o yaml
```

### Circuit Breaker Rejecting Requests

**Symptom:** Gateway returns `{"error": "queue_full"}` or `{"error": "queue_timeout"}`

**Check circuit breaker state:**
```bash
# Via Prometheus metrics
curl -s http://localhost:9090/api/v1/query?query=mcpfabric_circuit_breaker_state | jq

# Check queue depth
curl -s http://localhost:9090/api/v1/query?query=mcpfabric_circuit_breaker_waiting | jq
```

**Solutions:**

1. Scale up agents:
   ```bash
   kubectl -n mcp-fabric-agents patch agent <name> -p '{"spec":{"replicas":3}}' --type=merge
   ```

2. Increase circuit breaker limits in routes config

3. Check if agent is slow/unhealthy:
   ```bash
   kubectl -n mcp-fabric-agents logs -l agent=<agent-name> --tail=50
   ```

## Agent Issues

### Agent Pod CrashLoopBackOff

**Check logs:**
```bash
kubectl -n mcp-fabric-agents logs -l agent=<agent-name> --previous
```

**Common causes:**

1. **Python import error** - Tool package not found or syntax error
2. **Model provider error** - Invalid credentials or region
3. **Memory limit** - OOMKilled, increase resources

**Solution for credentials:**
```bash
# Verify secret exists and has correct keys
kubectl -n mcp-fabric-agents get secret aws-bedrock-credentials -o jsonpath='{.data}' | base64 -d

# Check agent mounts the secret
kubectl -n mcp-fabric-agents get pod -l agent=<agent-name> -o yaml | grep -A10 envFrom
```

### Agent Cannot Connect to Model Provider

**Symptom:** Agent logs show SSL/TLS errors or connection refused to Bedrock/OpenAI

**DNS issues (common in Kind):**
```bash
# Patch CoreDNS to use public DNS
kubectl -n kube-system patch configmap coredns --type merge -p '
{
  "data": {
    "Corefile": ".:53 {\n    errors\n    health {\n       lameduck 5s\n    }\n    ready\n    kubernetes cluster.local in-addr.arpa ip6.arpa {\n       pods insecure\n       fallthrough in-addr.arpa ip6.arpa\n       ttl 30\n    }\n    prometheus :9153\n    forward . 9.9.9.9\n    cache 30\n    loop\n    reload\n    loadbalance\n}\n"
  }
}'
kubectl -n kube-system rollout restart deployment coredns
```

**Check network egress:**
```bash
# Verify NetworkPolicy allows egress
kubectl -n mcp-fabric-agents get networkpolicy -o yaml

# Test connectivity from agent pod
kubectl -n mcp-fabric-agents exec -it deploy/<agent-name> -- \
  curl -I https://bedrock-runtime.eu-north-1.amazonaws.com
```

### Tool Package Not Loading

**Symptom:** Agent logs show `ModuleNotFoundError` or tool not appearing in tools/list

**Check init container:**
```bash
kubectl -n mcp-fabric-agents logs -l agent=<agent-name> -c tool-loader
```

**Verify tool image:**
```bash
# Check Tool CRD
kubectl -n mcp-fabric-agents get tool <tool-name> -o yaml

# Ensure image exists
docker pull <tool-image>
```

## MCP Protocol Issues

### tools/list Returns Empty

**Symptom:** `tools/list` MCP call returns empty array

**Check:**
```bash
# Verify agents have tools
kubectl -n mcp-fabric-agents get agents -o jsonpath='{range .items[*]}{.metadata.name}: {.status.toolsCount}{"\n"}{end}'

# Check gateway tool aggregation logs
kubectl -n mcp-fabric-gateway logs -l app=mcp-fabric-gateway | grep -i "tool"
```

### SSE Connection Drops

**Symptom:** `/mcp/sse` connection closes unexpectedly

**Common causes:**

1. **Proxy timeout** - Configure longer timeouts in ingress/load balancer
2. **Gateway restart** - Check pod restarts
3. **Network instability** - Check for network policy changes

**Solution:**
```bash
# Check for pod restarts
kubectl -n mcp-fabric-gateway get pods -w

# Increase timeout if using ingress
# Add annotation: nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
```

## Monitoring Issues

For monitoring and metrics troubleshooting, see [METRICS.md](../METRICS.md#troubleshooting).

## Kind-Specific Issues

### Images Not Found in Kind

**Symptom:** `ImagePullBackOff` for locally built images

**Solution:**
```bash
# Load images into Kind
kind load docker-image <image-name>:<tag> --name mcp-fabric

# Verify image is loaded
docker exec mcp-fabric-control-plane crictl images | grep <image-name>
```

### Port Forwarding Not Working

**Symptom:** `kubectl port-forward` fails or connection refused

**Check service exists:**
```bash
kubectl get svc -A | grep <service-name>
```

**Use NodePort instead:**
```bash
# Check Kind node ports
kubectl get svc -A -o wide | grep NodePort

# Access via localhost:<nodePort>
```

### DNS Resolution Failing

**Symptom:** Pods cannot resolve external domains

**Solution:**
```bash
# Patch CoreDNS
kubectl -n kube-system patch configmap coredns --type merge -p '{"data":{"Corefile":".:53 {\n    errors\n    health\n    ready\n    kubernetes cluster.local in-addr.arpa ip6.arpa {\n       pods insecure\n       fallthrough in-addr.arpa ip6.arpa\n    }\n    forward . 9.9.9.9 8.8.8.8\n    cache 30\n    loop\n    reload\n    loadbalance\n}\n"}}'

kubectl -n kube-system rollout restart deployment coredns
```

## Getting Help

If you're still stuck:

1. **Check logs** with increased verbosity:
   ```bash
   kubectl -n <namespace> logs <pod> -v=6
   ```

2. **Describe resources** for events:
   ```bash
   kubectl describe <resource-type> <name> -n <namespace>
   ```

3. **Check events**:
   ```bash
   kubectl get events -n <namespace> --sort-by='.lastTimestamp'
   ```

4. **File an issue** at https://github.com/jarsater/mcp-fabric/issues with:
   - MCP Fabric version
   - Kubernetes version
   - Relevant logs
   - Steps to reproduce
