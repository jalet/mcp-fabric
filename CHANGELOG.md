# Changelog

All notable changes to the MCP Fabric project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- Tuned agent deployment readiness and liveness probe parameters for more tolerant
  health checking. Readiness probe now uses `TimeoutSeconds: 5` and
  `FailureThreshold: 6` (Kubernetes defaults are 1 and 3 respectively). Liveness
  probe uses `TimeoutSeconds: 10` and `FailureThreshold: 6`. Agent pods may take
  longer to transition to NotReady status during transient failures, reducing
  unnecessary restarts for slow-starting or resource-constrained agents.
