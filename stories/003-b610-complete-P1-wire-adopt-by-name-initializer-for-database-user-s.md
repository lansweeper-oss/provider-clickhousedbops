---
id: 003-b610
title: Wire adopt-by-name initializer for database, user, settings_profile, role
status: complete
priority: P1
type: fix
created: "2026-07-23T09:52:19.544Z"
updated: "2026-07-23T10:26:12.297Z"
dependencies: ["002-e6c4"]
plan: plans/import-by-name-nofork.md
plan_step: Step 3
started_at: "2026-07-23T09:59:27.716Z"
completed_at: "2026-07-23T10:26:12.296Z"
---

# Wire adopt-by-name initializer for database, user, settings_profile, role

## Problem Statement

config/overrides.go uses sentinelUUIDInitializer for database/user/settings_profile, so pre-existing resources can never be adopted by name after no-fork. role uses roleImportInitializer which must migrate to the generalized version.

## Acceptance Criteria

- [x] clickhousedbops_database uses adoptByNameInitializer("clickhousedbops_database", "uuid")
- [x] clickhousedbops_user uses adoptByNameInitializer("clickhousedbops_user", "id") in correct position
- [x] clickhousedbops_settings_profile uses adoptByNameInitializer("clickhousedbops_settings_profile", "id")
- [x] clickhousedbops_role uses adoptByNameInitializer("clickhousedbops_role", "id")
- [x] sentinelUUIDInitializer removed if unreferenced
- [x] go test ./config/... passes

## Files

- config/overrides.go

## Proof

- [x] [completeness] Completeness (database/user/settings_profile/role all use adoptByNameInitializer; sentinelUUIDInitializer removed; config tests pass)
- [x] [feature-availability] Feature availability (four resources wired to adopt initializer with correct observation field)
- [x] [robustness] Robustness (user initializer ordering preserved between PasswordRefProcessor and PasswordGenerator)
- [x] [resilience] Resilience (adopt path inherits resolver error surfacing; sentinel fallback intact)
- [x] [security] Security (no new SQL; wiring only; database uuid field, others id field)
- [x] [defense-in-depth] Defense in depth (removed dead sentinelUUIDInitializer; single adopt path)
- [~] [input-validation] Input validation (static resource-name and field constants; no runtime input in overrides)
- [~] [thread-safety] Thread safety (configuration executed once at provider setup; no concurrency)
- [x] [configurability] Configurability (per-resource field id/uuid selected at wiring site)

## QA

None

## Work Log

### 2026-07-23T09:59:27.831Z - overrides.go: database/user/settings_profile/role all use adoptByNameInitializer; removed unused sentinelUUIDInitializer.


### 2026-07-23T10:26:10.714Z - Proof completeness set PROVEN: database/user/settings_profile/role all use adoptByNameInitializer; sentinelUUIDInitializer removed; config tests pass

### 2026-07-23T10:26:10.918Z - Proof feature-availability set PROVEN: four resources wired to adopt initializer with correct observation field

### 2026-07-23T10:26:11.068Z - Proof robustness set PROVEN: user initializer ordering preserved between PasswordRefProcessor and PasswordGenerator

### 2026-07-23T10:26:11.267Z - Proof resilience set PROVEN: adopt path inherits resolver error surfacing; sentinel fallback intact

### 2026-07-23T10:26:11.449Z - Proof security set PROVEN: no new SQL; wiring only; database uuid field, others id field

### 2026-07-23T10:26:11.596Z - Proof defense-in-depth set PROVEN: removed dead sentinelUUIDInitializer; single adopt path

### 2026-07-23T10:26:11.770Z - Proof input-validation set NOT_APPLICABLE: static resource-name and field constants; no runtime input in overrides

### 2026-07-23T10:26:11.948Z - Proof thread-safety set NOT_APPLICABLE: configuration executed once at provider setup; no concurrency

### 2026-07-23T10:26:12.087Z - Proof configurability set PROVEN: per-resource field id/uuid selected at wiring site
