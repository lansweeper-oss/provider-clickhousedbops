---
id: 002-e6c4
title: Generalize adopt-by-name initializer and resolver registry in config
status: complete
priority: P1
type: feature
created: "2026-07-23T09:52:19.513Z"
updated: "2026-07-23T10:25:57.429Z"
dependencies: ["001-2341"]
plan: plans/import-by-name-nofork.md
plan_step: Step 2
started_at: "2026-07-23T09:53:30.370Z"
completed_at: "2026-07-23T10:25:57.428Z"
---

# Generalize adopt-by-name initializer and resolver registry in config

## Problem Statement

roleImportInitializer and SetRoleResolverFactory are role-specific. Need a generic adoptByNameInitializer(resourceName, field) backed by a resolver registry so database/user/settingsprofile can resolve name→UUID at observe time.

## Acceptance Criteria

- [x] UUIDResolver type and resolverFactories registry (keyed by resource name) in config/roleimport.go
- [x] SetResolverFactory(resourceName, f) replaces SetRoleResolverFactory
- [x] adoptByNameInitializer(resourceName, field) seeds real UUID when found, sentinelUUID when absent, skips when real UUID already present
- [x] Lookup failure returns error
- [x] TestRoleImportInitializer migrated to test the generalized initializer with both id and uuid fields
- [x] go test ./config/... passes

## Files

- config/roleimport.go
- config/roleimport_test.go

## Proof

- [x] [completeness] Completeness (UUIDResolver type, resolverFactories registry, SetResolverFactory, adoptByNameInitializer implemented; config tests pass)
- [x] [feature-availability] Feature availability (adoptByNameInitializer exported and consumed by overrides; parametrized by observation field name)
- [x] [robustness] Robustness (nil-resolver fallback to sentinel; observation nil-guard; GetObservation error wrapped)
- [x] [resilience] Resilience (resolver error surfaced so reconcile retries; never force-creates over existing)
- [x] [security] Security (no SQL here; seeds observation field only; sentinel is fixed constant)
- [x] [defense-in-depth] Defense in depth (real-UUID guard skips lookup; absent-name falls back to sentinel force-create)
- [~] [input-validation] Input validation (resourceName/field are code constants; no external input parsed in config layer)
- [x] [thread-safety] Thread safety (resolverFactories written only at startup via SetResolverFactory; read-only during reconcile)
- [x] [configurability] Configurability (field arg selects id vs uuid observation key; registry keyed per resource name)

## QA

None

## Work Log

### 2026-07-23T09:59:27.464Z - Added UUIDResolver type + resolverFactories registry + SetResolverFactory; generalized roleImportInitializer -> adoptByNameInitializer(resourceName,field); migrated test (id and uuid fields).


### 2026-07-23T10:25:44.919Z - Proof completeness set PROVEN: UUIDResolver type, resolverFactories registry, SetResolverFactory, adoptByNameInitializer implemented; config tests pass

### 2026-07-23T10:25:45.254Z - Proof robustness set PROVEN: nil-resolver fallback to sentinel; observation nil-guard; GetObservation error wrapped

### 2026-07-23T10:25:45.451Z - Proof resilience set PROVEN: resolver error surfaced so reconcile retries; never force-creates over existing

### 2026-07-23T10:25:45.612Z - Proof security set PROVEN: no SQL here; seeds observation field only; sentinel is fixed constant

### 2026-07-23T10:25:45.807Z - Proof defense-in-depth set PROVEN: real-UUID guard skips lookup; absent-name falls back to sentinel force-create

### 2026-07-23T10:25:45.961Z - Proof input-validation set NOT_APPLICABLE: resourceName/field are code constants; no external input parsed in config layer

### 2026-07-23T10:25:46.112Z - Proof thread-safety set PROVEN: resolverFactories written only at startup via SetResolverFactory; read-only during reconcile

### 2026-07-23T10:25:46.260Z - Proof configurability set PROVEN: field arg selects id vs uuid observation key; registry keyed per resource name

### 2026-07-23T10:25:57.249Z - Proof feature-availability set PROVEN: adoptByNameInitializer exported and consumed by overrides; parametrized by observation field name
