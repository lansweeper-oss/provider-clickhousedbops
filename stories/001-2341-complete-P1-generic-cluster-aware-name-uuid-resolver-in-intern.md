---
id: 001-2341
title: Generic cluster-aware name→UUID resolver in internal/clients
status: complete
priority: P1
type: refactor
created: "2026-07-23T09:52:19.474Z"
updated: "2026-07-23T10:25:23.788Z"
dependencies: []
plan: plans/import-by-name-nofork.md
plan_step: Step 1
started_at: "2026-07-23T09:53:30.130Z"
completed_at: "2026-07-23T10:25:23.787Z"
---

# Generic cluster-aware name→UUID resolver in internal/clients

## Problem Statement

roleresolver.go is role-specific and ignores cluster_name. Need a generic findUUIDByName that queries system.<table> and supports cluster(<name>, system.<table>) when cluster_name is set, so database/user/settingsprofile resolvers can be built on it.

## Acceptance Criteria

- [x] findUUIDByName(ctx, params, table, idField, name, cluster) queries plain table when cluster is empty, cluster(escaped, table) when set
- [x] NewRoleUUIDResolver, NewDatabaseUUIDResolver, NewUserUUIDResolver, NewSettingsProfileUUIDResolver all implemented via newUUIDResolver
- [x] cluster name is single-quote escaped (not a bind param); name uses parameterized query
- [x] go build ./... passes

## Files

- internal/clients/roleresolver.go

## Proof

- [x] [completeness] Completeness (findUUIDByName + Role/Database/User/SettingsProfile resolver factories implemented; go build passes)
- [x] [feature-availability] Feature availability (4 exported factories return config.UUIDResolver; wired in main.go)
- [x] [robustness] Robustness (errors wrapped with percent-w; rows.Err checked; empty result returns not-found rather than erroring)
- [x] [resilience] Resilience (connection/query errors returned so reconcile retries, never force-create)
- [x] [security] Security (name bound as ? param; cluster single-quote escaped; table/idField are package constants)
- [x] [defense-in-depth] Defense in depth (no string-concat of name; identifier escaping for cluster table function)
- [x] [input-validation] Input validation (name via parameterized query; cluster escaped; no user-controlled table/column)
- [~] [thread-safety] Thread safety (stateless pure functions; conn opened/closed per call; no shared mutable state)
- [x] [configurability] Configurability (cluster-aware via spec.forProvider.cluster_name; plain table when empty)

## QA

None — resolver exercised via injected fake in config tests; live path is manual QA

## Work Log

### 2026-07-23T09:59:27.334Z - Generic cluster-aware findUUIDByName + 4 resolver factories (role/database/user/settingsprofile) in internal/clients; parameterized name, escaped cluster.


### 2026-07-23T10:25:08.330Z - Proof completeness set PROVEN: findUUIDByName + Role/Database/User/SettingsProfile resolver factories implemented; go build passes

### 2026-07-23T10:25:08.519Z - Proof feature-availability set PROVEN: 4 exported factories return config.UUIDResolver; wired in main.go

### 2026-07-23T10:25:08.867Z - Proof resilience set PROVEN: connection/query errors returned so reconcile retries, never force-create

### 2026-07-23T10:25:09.026Z - Proof security set PROVEN: name bound as ? param; cluster single-quote escaped; table/idField are package constants

### 2026-07-23T10:25:09.156Z - Proof defense-in-depth set PROVEN: no string-concat of name; identifier escaping for cluster table function

### 2026-07-23T10:25:09.336Z - Proof input-validation set PROVEN: name via parameterized query; cluster escaped; no user-controlled table/column

### 2026-07-23T10:25:09.477Z - Proof thread-safety set NOT_APPLICABLE: stateless pure functions; conn opened/closed per call; no shared mutable state

### 2026-07-23T10:25:09.638Z - Proof configurability set PROVEN: cluster-aware via spec.forProvider.cluster_name; plain table when empty

### 2026-07-23T10:25:23.575Z - Proof robustness set PROVEN: errors wrapped with percent-w; rows.Err checked; empty result returns not-found rather than erroring
