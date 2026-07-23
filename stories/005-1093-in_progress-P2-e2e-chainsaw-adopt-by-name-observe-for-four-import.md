---
id: 005-1093
title: "E2e chainsaw: adopt-by-name observe for four importable resources"
status: in_progress
priority: P2
type: task
created: "2026-07-23T10:27:17.235Z"
updated: "2026-07-23T10:28:29.456Z"
dependencies: []
plan: plans/import-by-name-nofork.md
plan_step: Step 6
started_at: "2026-07-23T10:28:29.455Z"
---

# E2e chainsaw: adopt-by-name observe for four importable resources

## Problem Statement

The no-fork import-by-name regression has no e2e coverage. Add chainsaw tests creating a managed resource then a sibling Observe-only MR referencing the same ClickHouse object by name, asserting Synced=True and Ready=True (proves adopt-by-name resolves the UUID and observe finds the row). Clean up sibling before teardown.

## Acceptance Criteria

- [x] chainsaw test creates managed resource, waits Ready, then applies sibling with managementPolicies Observe and same spec.forProvider.name
- [x] assert sibling reaches Synced True and Ready True within timeout
- [x] sibling deleted in finally so it does not interfere with teardown
- [x] coverage for all four importable resources: database (uuid field), user, role, settings_profile
- [ ] make chainsaw-e2e passes locally against a ClickHouse test instance

## Files

- e2e/tests/

## Proof

- [ ] [completeness] Completeness
- [ ] [feature-availability] Feature availability
- [ ] [robustness] Robustness
- [ ] [resilience] Resilience
- [ ] [security] Security
- [ ] [defense-in-depth] Defense in depth
- [ ] [input-validation] Input validation
- [ ] [thread-safety] Thread safety
- [ ] [configurability] Configurability

## Work Log

### 2026-07-23T10:31:34.714Z - Added observe-by-name chainsaw tests for database/user/role/settingprofile (e2e/tests/cluster/*-observe-by-name). Create managed resource, apply sibling Observe-only MR by name, assert Synced+Ready, delete sibling in finally. YAML validated. Live 'make chainsaw-e2e' run pending (needs ClickHouse cluster).

### 2026-07-23T13:06:59.570Z - Correction: e2e runs fully in kind via 'make e2e' (ClickHouse deployed by setup.sh, native:9000). No external cluster. Starting run.

