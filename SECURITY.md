# Security Policy

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability in MCP Fabric, please report it responsibly.

### How to Report

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please send an email to the maintainers with:

1. Description of the vulnerability
2. Steps to reproduce
3. Potential impact
4. Any suggested fixes (optional)

### What to Expect

- Acknowledgment within 48 hours
- Regular updates on the fix progress
- Credit in the security advisory (unless you prefer anonymity)

## Security Features

MCP Fabric implements defense-in-depth security measures:

### Pod Security

All agent pods run with hardened security contexts:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65534
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
  seccompProfile:
    type: RuntimeDefault
```

### Network Isolation

- Default deny network policies for all agent pods
- Ingress only allowed from the gateway namespace
- Egress restricted to:
  - DNS (UDP/TCP 53)
  - Explicitly configured model provider endpoints
  - Custom FQDNs specified in agent configuration

### Secrets Management

- Secrets are never stored in Custom Resources
- Use Kubernetes Secrets with `envFrom` for credentials
- Support for external secret management systems

### Authentication & Authorization

- Gateway can enforce authentication (configurable)
- Agents run with minimal ServiceAccount permissions
- RBAC policies limit operator access

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x.x   | :white_check_mark: |

## Security Best Practices

When deploying MCP Fabric:

### 1. Credential Management

```bash
# Create secrets using kubectl (not in manifests)
kubectl create secret generic aws-bedrock-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=your-key \
  --from-literal=AWS_SECRET_ACCESS_KEY=your-secret

# Or use external secrets operator
```

### 2. Network Policies

Ensure your cluster has a CNI that supports NetworkPolicies (Calico, Cilium, etc.):

```bash
# Verify NetworkPolicy support
kubectl get networkpolicies -A
```

### 3. RBAC

Review and restrict operator RBAC permissions for production:

```bash
# Check operator permissions
kubectl auth can-i --list --as=system:serviceaccount:mcp-fabric:operator
```

### 4. Resource Limits

Always set resource limits on agents to prevent resource exhaustion:

```yaml
resources:
  requests:
    memory: "256Mi"
    cpu: "100m"
  limits:
    memory: "512Mi"
    cpu: "500m"
```

### 5. Audit Logging

Enable Kubernetes audit logging to track API access:

```yaml
# In your audit policy
rules:
  - level: Metadata
    resources:
      - group: "fabric.jarsater.ai"
        resources: ["agents", "routes", "tools"]
```

### 6. Image Security

- Use specific image tags, not `latest`
- Scan images for vulnerabilities
- Use signed images when available

## Known Security Considerations

### LLM Prompt Injection

Agent system prompts may be vulnerable to prompt injection attacks. Mitigations:

- Validate and sanitize user inputs at the gateway
- Use structured input schemas
- Monitor agent responses for unexpected behavior

### Tool Execution

Tools executed by agents run with the agent's permissions. Ensure:

- Tools are audited for security vulnerabilities
- Tool packages come from trusted sources
- Tool permissions are minimized

### Data Exfiltration

Agents with network egress could potentially exfiltrate data. Mitigate by:

- Restricting `allowedFqdns` to minimum required
- Monitoring egress traffic
- Using data loss prevention tools

## Security Updates

Security patches are released as soon as fixes are available. Monitor:

- GitHub releases for security announcements
- Container image updates

## Compliance

MCP Fabric can be configured to meet various compliance requirements. Consult with your compliance team for specific guidance on:

- SOC 2
- HIPAA
- GDPR
- PCI DSS
