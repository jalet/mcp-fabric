# Changelog

All notable changes to the MCP Fabric project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Task CRD and controller** for autonomous, multi-step agent workflows. A
  `Task` runs an orchestrator Job that iterates over a PRD, dispatches each item
  to a worker agent, runs quality gates, and (optionally) clones a Git repo and
  opens a pull request when all items pass. See the
  [Running Tasks guide](docs/tasks.md) and the
  [CRD reference](docs/CRD-REFERENCE.md#task).
- The worker is **co-located as a native sidecar** in the orchestrator Job,
  sharing the workspace volume; the orchestrator reaches it over loopback.
- The Task Job runs under the worker's ServiceAccount to support **IRSA** for
  model access (annotate the SA with `eks.amazonaws.com/role-arn`).
- `Agent.spec.standalone` (default `true`). Set `false` for agents used only as
  Task workers — the operator skips the standalone Deployment/Service.
- Task metrics: `mcpfabric_task_info`, `mcpfabric_task_iteration`,
  `mcpfabric_task_completed_tasks`, `mcpfabric_task_total_tasks` (see
  [METRICS.md](METRICS.md)).

### Changed

- **Breaking (metrics):** `mcpfabric_reconcile_duration_seconds` now carries a
  `result` label, and the `task` controller is added to reconcile metrics. See
  the Breaking Changes section in [METRICS.md](METRICS.md).
- `QualityGate.failurePolicy` accepts `Fail` or `Ignore` (the unimplemented
  `Retry` value was removed).
- Automatic pull-request creation is supported for **GitHub only**; for other
  providers the branch is pushed but no PR is opened.
- Developer tooling moved from Make to [mise](https://mise.jdx.dev); see
  [DEVELOPMENT.md](DEVELOPMENT.md) and run `mise tasks`.
- Tuned agent deployment readiness and liveness probe parameters for more
  tolerant health checking. Readiness probe now uses `TimeoutSeconds: 5` and
  `FailureThreshold: 6` (Kubernetes defaults are 1 and 3 respectively). Liveness
  probe uses `TimeoutSeconds: 10` and `FailureThreshold: 6`. Agent pods may take
  longer to transition to NotReady status during transient failures, reducing
  unnecessary restarts for slow-starting or resource-constrained agents.
