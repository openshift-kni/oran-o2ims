# AGENTS.md

This file provides codebase guidance for AI coding agents working with this repository.

## Project Overview

O-Cloud Manager operator built on OpenShift and ACM (Red Hat Advanced Cluster Management). Implements the O-RAN O2 IMS specification for 5G infrastructure management: bare-metal inventory, cluster provisioning, firmware management, and alarm monitoring via REST APIs.

## Development Commands

Use `make help` to list all available targets. Key commands:

- `make ci-job` - Full CI pipeline locally (format, vet, lint, test, e2e, envtest, coverage, bundle-check). Run before submitting PRs. Includes `bundle-check`, which verifies the git tree is clean after code generation — a common CI failure when API or manifest changes aren't regenerated.
- `make generate && make manifests && make bundle` - Regenerate code after API changes.

### Lint by File Type

Run targeted checks instead of waiting for the full `ci-job`:

- Go files: `make golangci-lint`
- YAML files: `make yamllint`
- Shell scripts: `make shellcheck bashate`
- Markdown files: `make markdownlint` (not included in `ci-job` — run separately)
- All of the above except markdownlint: `make lint`

### Running a Single Test

Pass Ginkgo flags via `ginkgo_flags`:

```bash
make test ginkgo_flags="--focus='should handle alarm'"
make test-envtest ginkgo_flags="--focus='InventoryController'"
```

### Container Build

To build and push to a personal registry, set `IMAGE_TAG_BASE` and `VERSION`:

```bash
make IMAGE_TAG_BASE=quay.io/<your-username>/oran-o2ims VERSION=latest docker-build docker-push
```

## Architecture

### Multi-Service Binary

The project builds a single binary that runs as multiple services in separate containers. Each service has its own cmd package at `internal/service/{service}/cmd/`. The Deployment determines which subcommand each container runs.

### Service Architecture Pattern

Services follow a consistent initialization pattern in `internal/service/{service}/`:

1. Embedded OpenAPI spec for request validation
2. PostgreSQL connection pool (services with persistence)
3. Repository layer implementing a defined interface (`db/repo/`)
4. Infrastructure clients for cross-service communication
5. Authentication/authorization middleware
6. HTTP server with generated OpenAPI handlers

Shared infrastructure lives in `internal/service/common/` (middleware, DB helpers, server config).

REST API code under `generated/` is auto-generated from OpenAPI specs via `//go:generate`. Don't edit generated files — edit the `openapi.yaml` and run `make go-generate`.

### Provisioning Workflow Phases

The ProvisioningRequestController runs a multi-phase state machine:

1. **Validation** - Validate request against ClusterTemplate schema
2. **Hardware Provisioning** - Create NodeAllocationRequest, watch NAR status, create BMC secrets
3. **Cluster Installation** - Render and apply ClusterInstance, monitor ZTP progress
4. **Post-Provisioning** - Apply policy templates, monitor compliance
5. **Upgrades** - Handle cluster upgrade requests

Each phase sets typed conditions on the CR status. Condition helpers are in `internal/controllers/utils/conditions.go`. Condition types and reasons are defined in `api/provisioning/v1alpha1/conditions.go`.

### Database Schema Changes

This project has not reached GA, so there are no production databases to migrate. Do not add new incremental migration files. Instead, modify the existing baseline files in `internal/service/{service}/db/migrations/` in place.

### API Groups and Key CRDs

- `clcm.openshift.io` - ClusterTemplate, ProvisioningRequest, HardwareProfile
- `ocloud.openshift.io` - Inventory
- `plugins.clcm.openshift.io` - NodeAllocationRequest, AllocatedNode

### Design Decisions

The following are accepted design decisions. Do not flag these patterns as
issues during code review.

- **DD-001: In-memory filtering for AllocatedNode lookups.**
  `listAllocatedNodesForNAR` in the PR controller uses in-memory filtering
  instead of server-side `MatchingFields` because the fake Kubernetes client
  in unit tests does not support field selectors. Per-cluster node counts are
  small (1-11 nodes), so the cost is negligible. The Metal3 NAR controller
  uses a proper field index (`spec.nodeAllocationRequest`) since it runs with
  a real manager cache.

- **DD-002: Cluster-scoped ProvisioningRequest watch mappers.**
  Watch mappers that enqueue ProvisioningRequests (e.g.,
  `enqueueProvisioningRequestForNAR`) intentionally omit the namespace from
  `NamespacedName`. `ProvisioningRequest` is cluster-scoped (`scope: Cluster`
  in the CRD) and has no namespace.

## Contributing Requirements

- All commits must be signed off with DCO: `git commit -s`
- Run `make ci-job` before submitting PRs
- After API changes: `make generate && make manifests && make bundle`
- AI-generated code must use `Co-Authored-By` or `Assisted-By` trailer
- Run lint checks before committing (see [Lint by File Type](#lint-by-file-type))
- When making code changes, ensure test coverage for new code and functional
  changes. If a bug fix or new behavior is added without a corresponding test,
  write one. If an existing scenario is discovered to be untested (e.g., during
  code review), add a test for it in the same commit or PR. Tests should verify
  the specific behavior, not just increase line coverage.
