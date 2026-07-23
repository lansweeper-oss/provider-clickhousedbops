---
id: "005-1093"
title: "E2e chainsaw: adopt-by-name observe for four importable resources"
status: pending
priority: P2
type: task
created: 2026-07-23T10:27:17.235Z
updated: 2026-07-23T10:27:17.235Z
dependencies: []
plan: "plans/import-by-name-nofork.md"
plan_step: "Step 6"
---

# E2e chainsaw: adopt-by-name observe for four importable resources

## Problem Statement

The no-fork import-by-name regression has no e2e coverage. Add chainsaw tests creating a managed resource then a sibling Observe-only MR referencing the same ClickHouse object by name, asserting Synced=True and Ready=True (proves adopt-by-name resolves the UUID and observe finds the row). Clean up sibling before teardown.

## Acceptance Criteria

- [ ] chainsaw test creates managed resource, waits Ready, then applies sibling with managementPolicies Observe and same spec.forProvider.name
- [ ] assert sibling reaches Synced True and Ready True within timeout (proves adopt, not re-create)
- [ ] sibling deleted in finally so it does not interfere with teardown
- [ ] coverage for all four importable resources: database (uuid field), user, role, settings_profile (id field)
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

