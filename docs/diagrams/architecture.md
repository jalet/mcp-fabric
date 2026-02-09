# Architecture Diagrams

## System Overview

```mermaid
flowchart TB
    subgraph clients["Clients"]
        cli["CLI / curl"]
        app["Applications"]
        mcp["MCP Clients"]
    end

    subgraph gateway_ns["mcp-fabric-gateway namespace"]
        gw["Gateway<br/>(Go HTTP Server)"]
        routes_cm["Routes ConfigMap<br/>(Compiled from Route CRs)"]
    end

    subgraph system_ns["mcp-fabric-system namespace"]
        op["Operator<br/>(Controller Runtime)"]
    end

    subgraph agents_ns["mcp-fabric-agents namespace"]
        agent1["Agent Pod 1<br/>+ Service<br/>+ ConfigMap<br/>+ NetworkPolicy"]
        agent2["Agent Pod 2<br/>+ Service<br/>+ ConfigMap<br/>+ NetworkPolicy"]
        agent3["Agent Pod N<br/>..."]
        task_job["Task Job<br/>(orchestrator + worker sidecar)<br/>+ Workspace PVC"]
    end

    subgraph crds["Custom Resources"]
        agent_cr["Agent CRs"]
        route_cr["Route CRs"]
        tool_cr["Tool CRs"]
        task_cr["Task CRs"]
    end

    subgraph external["External Services"]
        llm["LLM Provider<br/>(Bedrock/OpenAI)"]
        mcp_server["MCP Servers"]
        aws["AWS APIs"]
        git["Git (GitHub)"]
    end

    clients --> gw
    gw --> routes_cm
    gw --> agent1
    gw --> agent2
    gw --> agent3

    op --> agent_cr
    op --> route_cr
    op --> tool_cr
    op --> task_cr
    op --> |creates| agents_ns
    op --> |compiles| routes_cm

    agent1 --> llm
    agent1 --> mcp_server
    agent1 --> aws

    task_job --> llm
    task_job --> git
```

## Request Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant G as Gateway
    participant RT as Route Table
    participant CB as Circuit Breaker
    participant A as Agent Pod
    participant LLM as LLM Provider

    C->>G: POST /v1/invoke<br/>{agent, query}
    G->>RT: Match route
    RT-->>G: Backend selection
    G->>CB: Acquire slot
    CB-->>G: OK / Queued / Rejected
    G->>A: POST /invoke
    A->>A: Load config
    A->>A: Initialize tools
    A->>LLM: Send prompt + query
    LLM-->>A: Response
    A-->>G: {success, result}
    G->>CB: Release slot
    G-->>C: {success, result, latencyMs}
```

## Agent Lifecycle

```mermaid
stateDiagram-v2
    [*] --> Created: kubectl apply Agent CR
    Created --> Reconciling: Operator detects CR
    Reconciling --> Ready: All resources created
    Ready --> Reconciling: CR updated
    Ready --> Degraded: Pod unhealthy
    Degraded --> Ready: Pod recovers
    Ready --> Terminating: CR deleted
    Reconciling --> Terminating: CR deleted
    Terminating --> [*]: Resources cleaned up

    state Reconciling {
        [*] --> CreateSA: ServiceAccount
        CreateSA --> CreateCM: ConfigMap
        CreateCM --> CreateNP: NetworkPolicy
        CreateNP --> CreateDep: Deployment
        CreateDep --> CreateSvc: Service
        CreateSvc --> [*]
    }
```

## CRD Relationships

```mermaid
erDiagram
    Agent ||--o{ Tool : references
    Agent ||--o{ Secret : "envFrom"
    Route ||--o{ Agent : "routes to"
    Task ||--|| Agent : "workerRef / orchestratorRef"
    Task ||--o| Secret : "git credentials"

    Agent {
        string name
        string prompt
        object model
        array toolPackages
        array envFrom
        int replicas
        bool standalone
    }

    Route {
        string name
        array rules
        object defaults
    }

    Tool {
        string name
        string image
        string entryModule
        array tools
    }

    Task {
        string name
        object workerRef
        object taskSource
        array qualityGates
        object git
        object limits
    }
```

## Network Policy

```mermaid
flowchart LR
    subgraph allowed["Allowed Traffic"]
        direction TB
        gw_ns["Gateway Namespace"] -->|ingress| agent["Agent Pod"]
        agent -->|egress| dns["DNS (53)"]
        agent -->|egress| llm["LLM Provider"]
        agent -->|egress| fqdn["Allowed FQDNs"]
    end

    subgraph denied["Denied Traffic"]
        direction TB
        other["Other Pods"] -.->|blocked| agent2["Agent Pod"]
        agent2 -.->|blocked| internet["Internet"]
    end
```

## Component Interactions

```mermaid
flowchart TB
    subgraph operator["Operator"]
        ac["Agent Controller"]
        rc["Route Controller"]
        tc["Tool Controller"]
        tkc["Task Controller"]
    end

    subgraph k8s["Kubernetes API"]
        agent_cr["Agent CR"]
        route_cr["Route CR"]
        tool_cr["Tool CR"]
        task_cr["Task CR"]
        dep["Deployment"]
        svc["Service"]
        cm["ConfigMap"]
        np["NetworkPolicy"]
        sa["ServiceAccount"]
        job["Job"]
        pvc["Workspace PVC"]
    end

    subgraph gateway["Gateway"]
        api["HTTP API"]
        rt["Route Table"]
        sel["Backend Selector"]
        cb["Circuit Breaker"]
    end

    ac -->|watch| agent_cr
    ac -->|create/update| dep
    ac -->|create/update| svc
    ac -->|create/update| cm
    ac -->|create/update| np
    ac -->|create/update| sa

    rc -->|watch| route_cr
    rc -->|compile| routes_cm["Routes ConfigMap"]

    tc -->|watch| tool_cr
    tc -->|validate| tool_cr

    tkc -->|watch| task_cr
    tkc -->|create/update| job
    tkc -->|create/update| pvc

    api --> rt
    rt --> sel
    sel --> cb
    cb --> agents["Agent Pods"]
```

## Task Orchestration

```mermaid
flowchart TB
    task_cr["Task CR"] --> tkc["Task Controller"]
    tkc -->|reconcile| pvc["Workspace PVC<br/>(ReadWriteOnce)"]
    tkc -->|create| job

    subgraph job["Orchestrator Job Pod"]
        direction TB
        clone["init: git-clone<br/>clones repo into /workspace"]
        worker["sidecar: worker<br/>HTTP :8080 (restartPolicy: Always)"]
        orch["orchestrator<br/>loops over PRD, runs quality gates"]
        clone --> worker
        orch -->|"dispatch (127.0.0.1:8080)"| worker
        worker -. shares /workspace .- orch
    end

    orch -->|commit + push + PR| git["Git (GitHub)"]
    worker -->|model calls via IRSA| llm["LLM Provider"]
    job -->|logs: ORCHESTRATOR_RESULT| tkc
    tkc -->|update| status["Task.status<br/>phase, progress, commitSha, PR URL"]
```
