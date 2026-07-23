---
id: 004-0121
title: Register four resolver factories in cmd/provider/main.go
status: complete
priority: P1
type: feature
created: "2026-07-23T09:52:19.581Z"
updated: "2026-07-23T10:26:57.250Z"
dependencies: ["001-2341", "003-b610"]
plan: plans/import-by-name-nofork.md
plan_step: Step 4
started_at: "2026-07-23T09:59:28.096Z"
completed_at: "2026-07-23T10:26:57.249Z"
---

# Register four resolver factories in cmd/provider/main.go

## Problem Statement

Only SetRoleResolverFactory is called in main.go. The three new resources (database, user, settingsprofile) have no resolver wired, so adoptByNameInitializer falls back to sentinelUUID and import-by-name stays broken.

## Acceptance Criteria

- [x] SetResolverFactory called for clickhousedbops_role, clickhousedbops_database, clickhousedbops_user, clickhousedbops_settings_profile
- [x] SetRoleResolverFactory call removed
- [x] go build ./... passes; go test ./... passes

## Files

- cmd/provider/main.go

## Proof

- [x] [completeness] Completeness (SetResolverFactory called for role/database/user/settings_profile; SetRoleResolverFactory removed; build+test pass)
- [x] [feature-availability] Feature availability (four factories registered before controller setup)
- [x] [robustness] Robustness (no error paths added; composition root only)
- [x] [resilience] Resilience (missing factory degrades to sentinel fallback in initializer)
- [x] [security] Security (no secrets/SQL added; factory wiring only)
- [x] [defense-in-depth] Defense in depth (clients package imports config; no import cycle; generator graph unaffected)
- [~] [input-validation] Input validation (resource names are code constants)
- [x] [thread-safety] Thread safety (registration runs once at startup before reconcilers start)
- [x] [configurability] Configurability (each resource mapped to its own resolver factory)

## QA

None

## Work Log

### 2026-07-23T09:59:28.198Z - main.go: SetResolverFactory for role/database/user/settings_profile; removed SetRoleResolverFactory.


### 2026-07-23T10:26:55.748Z - Proof completeness set PROVEN: SetResolverFactory called for role/database/user/settings_profile; SetRoleResolverFactory removed; build+test pass

### 2026-07-23T10:26:55.920Z - Proof feature-availability set PROVEN: four factories registered before controller setup

### 2026-07-23T10:26:56.096Z - Proof robustness set PROVEN: no error paths added; composition root only

### 2026-07-23T10:26:56.261Z - Proof resilience set PROVEN: missing factory degrades to sentinel fallback in initializer

### 2026-07-23T10:26:56.437Z - Proof security set PROVEN: no secrets/SQL added; factory wiring only

### 2026-07-23T10:26:56.642Z - Proof defense-in-depth set PROVEN: clients package imports config; no import cycle; generator graph unaffected

### 2026-07-23T10:26:56.773Z - Proof input-validation set NOT_APPLICABLE: resource names are code constants

### 2026-07-23T10:26:56.942Z - Proof thread-safety set PROVEN: registration runs once at startup before reconcilers start

### 2026-07-23T10:26:57.081Z - Proof configurability set PROVEN: each resource mapped to its own resolver factory
