# API Reference

MCP Fabric Gateway exposes HTTP and MCP (Model Context Protocol) endpoints for agent invocation and management.

## Gateway Endpoints

The gateway listens on port `8080` for HTTP traffic and `9090` for metrics.

### POST /v1/invoke

Invoke an agent with a query.

**Request:**
```json
{
  "agent": "text-assistant",
  "intent": "manipulate text",
  "query": "Reverse the string 'Hello World'",
  "tenantId": "team-alpha",
  "correlationId": "req-12345",
  "input": {},
  "metadata": {}
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent` | string | No | Target agent name (bypasses intent routing) |
| `intent` | string | No | Intent string for regex-based routing |
| `query` | string | Yes | The query/prompt for the agent |
| `tenantId` | string | No | Tenant ID for sticky session routing |
| `correlationId` | string | No | Correlation ID for request tracking |
| `input` | object | No | Structured input data |
| `metadata` | object | No | Additional metadata |

**Response (Success):**
```json
{
  "success": true,
  "result": {
    "response": "The reversed string is 'dlroW olleH'"
  },
  "agent": "text-assistant",
  "correlationId": "req-12345",
  "latencyMs": 1234
}
```

**Response (Error):**
```json
{
  "success": false,
  "error": "no matching route found",
  "correlationId": "req-12345"
}
```

**Status Codes:**
- `200` - Success
- `400` - Bad request (missing query, no route match with reject enabled)
- `404` - No agent available
- `500` - Agent execution error
- `503` - Circuit breaker open or queue full

### GET /v1/agents

List available agents.

**Response:**
```json
{
  "agents": [
    "mcp-fabric-agents/aws-api",
    "mcp-fabric-agents/aws-docs",
    "mcp-fabric-agents/text-assistant"
  ]
}
```

### GET /v1/routes

List active routing rules.

**Response:**
```json
{
  "routes": ["explicit-aws-api", "cost-intent", "docs-intent"],
  "count": 3
}
```

### GET /healthz

Health check endpoint.

**Response:**
```json
{
  "status": "ok"
}
```

## MCP Protocol Endpoints

The gateway implements the [Model Context Protocol](https://spec.modelcontextprotocol.io/) for tool discovery and invocation.

### POST /mcp

JSON-RPC 2.0 endpoint for MCP requests.

**Supported Methods:**

#### initialize

Initialize an MCP session.

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {
      "name": "my-client",
      "version": "1.0.0"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2024-11-05",
    "capabilities": {
      "tools": {
        "listChanged": true
      }
    },
    "serverInfo": {
      "name": "mcp-fabric-gateway",
      "version": "1.0.0"
    }
  }
}
```

#### tools/list

List available tools from all agents.

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/list"
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "aws-api__manage_aws",
        "description": "Execute AWS CLI commands",
        "inputSchema": {
          "type": "object",
          "properties": {
            "request": {
              "type": "string",
              "description": "What you want to do with AWS"
            }
          },
          "required": ["request"]
        }
      },
      {
        "name": "text-assistant__manipulate_text",
        "description": "Manipulate text using string tools",
        "inputSchema": {
          "type": "object",
          "properties": {
            "request": {
              "type": "string"
            }
          },
          "required": ["request"]
        }
      }
    ]
  }
}
```

Tool names are prefixed with the agent name: `{agent}__{tool_name}`

#### tools/call

Execute a tool on an agent.

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "text-assistant__manipulate_text",
    "arguments": {
      "request": "Reverse the string 'Hello'"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"response\": \"olleH\"}"
      }
    ]
  }
}
```

#### ping

Health check.

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "ping"
}
```

### GET /mcp/sse

Server-Sent Events endpoint for MCP streaming.

Connect via SSE to receive real-time notifications:

```bash
curl -N http://localhost:8080/mcp/sse
```

**Events:**

- `endpoint` - Initial connection with session endpoint
- `message` - JSON-RPC responses
- `notifications/tools/list_changed` - Tool list has changed

**Example Session:**

```
event: endpoint
data: /mcp/sse?sessionId=abc123

event: message
data: {"jsonrpc":"2.0","id":1,"result":{...}}
```

## Routing Logic

1. If `request.agent` is specified → route directly to that agent
2. Else match `request.intent` against regex rules (by priority, highest first)
3. Filter to ready backends only
4. Select backend using:
   - **Consistent hashing** if `tenantId` or `correlationId` provided (sticky sessions)
   - **Weighted random** otherwise
5. Forward to agent's `/invoke` endpoint

## Circuit Breaker

Each route has a circuit breaker to prevent cascade failures:

| Setting | Default | Description |
|---------|---------|-------------|
| `maxConcurrent` | 100 | Max concurrent requests |
| `maxQueueSize` | 50 | Max queued requests |
| `queueTimeout` | 30s | Max time in queue |

When limits are exceeded:
- Returns `503 Service Unavailable`
- Error type: `queue_full` or `queue_timeout`

## Agent Endpoints

Each agent pod exposes:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/invoke` | POST | Execute agent query |
| `/healthz` | GET | Health check |

**Agent /invoke Request:**
```json
{
  "query": "What are our cloud costs?",
  "input": {},
  "metadata": {}
}
```

**Agent /invoke Response:**
```json
{
  "success": true,
  "result": {
    "response": "Based on AWS Cost Explorer..."
  }
}
```

## Error Codes

| HTTP Status | MCP Error Code | Description |
|-------------|----------------|-------------|
| 400 | -32600 | Invalid request |
| 404 | -32601 | Method not found |
| 500 | -32603 | Internal error |
| 503 | - | Circuit breaker / queue full |

## Configuration

### Gateway Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GATEWAY_ADDR` | `:8080` | HTTP listen address |
| `METRICS_ADDR` | `:9090` | Metrics listen address |
| `ROUTES_FILE` | `/etc/gateway/routes.json` | Routes config file |
| `ENABLE_MCP` | `true` | Enable MCP endpoints |
| `WATCH_NAMESPACE` | `` | Namespace to watch agents (empty = all) |

### Routes ConfigMap

The gateway reads routing rules from a ConfigMap mounted at `/etc/gateway/routes.json`:

```json
{
  "rules": [
    {
      "name": "explicit-text-assistant",
      "priority": 100,
      "match": {
        "agent": "text-assistant"
      },
      "backends": [
        {
          "agentName": "text-assistant",
          "namespace": "mcp-fabric-agents",
          "endpoint": "text-assistant.mcp-fabric-agents.svc.cluster.local:8080",
          "weight": 100,
          "ready": true
        }
      ]
    }
  ],
  "defaults": {
    "circuitBreaker": {
      "maxConcurrent": 100,
      "maxQueueSize": 50,
      "queueTimeout": "30s"
    },
    "rejectUnmatched": false
  }
}
```
