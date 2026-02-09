# Running Tasks

A `Task` runs an autonomous, multi-step workflow: the operator launches an
orchestrator Job that iterates over a PRD (product requirements document),
dispatches each item to a worker agent, runs quality gates, and -- when all
items pass -- optionally commits the result and opens a pull request.

For the architecture (orchestrator Job + worker sidecar + shared workspace),
see [Architecture > Task Orchestration](architecture.md#task-orchestration).
For every field, see the [CRD reference](CRD-REFERENCE.md#task).

## Prerequisites

- MCP Fabric operator installed and CRDs applied (`mise run crds:install`).
- A worker agent image that serves `/invoke` over HTTP (the example
  `code-worker` does).
- For Git integration: a GitHub repository and a token.

## 1. Define the worker and orchestrator agents

Tasks reference two agents. The worker implements individual items; the
orchestrator runs the loop. Mark the worker `standalone: false` so the operator
does not deploy it as a standalone Service -- the Task controller co-locates it
as a sidecar instead.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: code-worker
  namespace: mcp-fabric-agents
spec:
  standalone: false          # used only as a Task worker (sidecar)
  image: ghcr.io/jalet/code-worker-agent:latest
  prompt: "You implement single, focused tasks from acceptance criteria."
  model:
    provider: bedrock
    modelId: amazon.nova-lite-v1:0
  envFrom:
    - secretRef:
        name: aws-bedrock-credentials
```

The example deployment ships both agents:

```bash
kubectl apply -f examples/deploy/agents/agent-task-orchestrator.yaml
kubectl apply -f examples/deploy/agents/agent-code-worker.yaml
```

## 2. Write the PRD

The PRD is JSON with a `stories` array (the alias `tasks` also works). Each
item has an `id`, `title`, `priority`, `acceptanceCriteria`, and a `passes`
flag the orchestrator flips as items complete.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-prd
  namespace: mcp-fabric-agents
data:
  prd.json: |
    {
      "project": "hello-feature",
      "stories": [
        {
          "id": "task-001",
          "title": "Create hello.txt",
          "priority": 1,
          "passes": false,
          "acceptanceCriteria": [
            "Create hello.txt containing 'Hello, World!'"
          ]
        }
      ]
    }
```

```bash
kubectl apply -f examples/deploy/tasks/example-prd-configmap.yaml
```

## 3. Create the credentials

**Git** -- a Secret with a `token` key (a GitHub PAT or equivalent):

```bash
kubectl -n mcp-fabric-agents create secret generic github-credentials \
  --from-literal=token=ghp_your_token
```

**Model access** -- the Task Job runs under the worker's ServiceAccount. On
EKS, grant Bedrock via IRSA by annotating that SA (the operator creates it and
preserves your annotations):

```bash
kubectl -n mcp-fabric-agents annotate sa code-worker \
  eks.amazonaws.com/role-arn=arn:aws:iam::<account>:role/<bedrock-role>
```

Off EKS (e.g. Kind), provide credentials via the worker agent's `envFrom`
secret instead.

## 4. Create the Task

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Task
metadata:
  name: example-task
  namespace: mcp-fabric-agents
spec:
  workerRef:
    name: code-worker
  taskSource:
    type: configmap
    configMapRef:
      name: example-prd
      key: prd.json
  git:
    url: https://github.com/you/your-repo.git
    branch: feat/example-task
    baseBranch: main
    credentialsSecret:
      name: github-credentials
    createPR: true
    draftPR: true
  qualityGates:
    - name: test
      command: ["go", "test", "./..."]
      failurePolicy: Fail        # Fail | Ignore
      timeout: 10m
  limits:
    maxIterations: 50
    iterationTimeout: 30m        # per-task worker timeout
    totalTimeout: 4h             # Job activeDeadlineSeconds
    maxConsecutiveFailures: 3
```

```bash
kubectl apply -f examples/deploy/tasks/example-task.yaml
```

## 5. Observe progress

```bash
kubectl -n mcp-fabric-agents get task example-task
# NAME           PHASE     ITERATION   PROGRESS   TOTAL   AGE
# example-task   Running   2           1          3       2m

# Orchestrator + worker run in one Job Pod:
kubectl -n mcp-fabric-agents get pods -l fabric.jarsater.ai/task=example-task
kubectl -n mcp-fabric-agents logs <pod> -c orchestrator
kubectl -n mcp-fabric-agents logs <pod> -c worker

# Final result (set when the Job finishes):
kubectl -n mcp-fabric-agents get task example-task \
  -o jsonpath='{.status.phase} {.status.pullRequestUrl}{"\n"}'
```

Phases: `Pending` → `Running` → `Completed` or `Failed`. Set `spec.paused: true`
to stop launching new work (note: it does not interrupt an in-flight Job).

## Notes and limitations

- **Progress is not live.** `status.completedTasks`/`currentIteration` are
  populated from the orchestrator's final result when the Job completes, not
  streamed during the run.
- **PR creation is GitHub-only.** For `gitlab`/`bitbucket` the branch is pushed
  but no PR is opened.
- **`failurePolicy`** supports `Fail` and `Ignore` only.
- **Workspace** is a per-Task `ReadWriteOnce` PVC shared by the orchestrator and
  worker sidecar; it is deleted with the Task.
