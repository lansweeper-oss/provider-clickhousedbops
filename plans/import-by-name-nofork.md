---
status: in_progress
approved_at: "2026-07-23T09:52:19.492Z"
updated: "2026-07-23T09:53:30.159Z"
started_at: "2026-07-23T09:53:30.159Z"
---
# Plan: Restore import-by-name for database/user/settings_profile (no-fork regression)

**Created:** 2026-07-23 | **Status:** Draft | **Effort:** M | **Branch:** fix/import-by-name-nofork

## Summary

No-fork (#61) removed the `terraform import` step that invoked the provider's `ImportState` (the only path that resolves a resource **name** to its provider UUID). Now observe calls `Read`, which looks up by UUID only, so adopting a pre-existing database/user/settings_profile by name fails (sentinel UUID matches no row → provider re-creates → "already exists"). `role` is unaffected because #44 already added an adopt-by-name initializer. Fix: generalize that proven `role` pattern to database, user, and settings_profile.

## Architecture Context

- Observe seeds the identifier via **status.atProvider observation**, not the external-name annotation. Initializers run before observe and write the id/uuid field into the observation; upjet builds TF state from it and calls provider `Read`.
- `role` (`config/roleimport.go` + `internal/clients/roleresolver.go`): `roleImportInitializer` resolves the real UUID by name from `system.roles` and seeds it; falls back to `sentinelUUID` (force-create) when absent. Adoptable.
- `database`/`user`/`settings_profile` (`config/overrides.go`): use `sentinelUUIDInitializer` — sentinel only, never adopt. This is the regression surface.
- Resolver lives in `internal/clients` and is injected via a factory var (`SetRoleResolverFactory` in `cmd/provider/main.go:158`) so the ClickHouse client stays out of the code generator's import graph.
- ClickHouse lookups (from provider `FindXByName`): `system.databases`→`toString(uuid)`, `system.users`→`toString(id)`, `system.settings_profiles`→`toString(id)`, all `WHERE name = ?`.

## Research Findings

- `config/overrides.go:97,142,164` wire `sentinelUUIDInitializer` for database("uuid")/user("id")/settings_profile("id"); `:175` wires `roleImportInitializer()` for role.
- `internal/clients/roleresolver.go` opens `clickhouse.Open` from `ResolveConnParams`; only handles `nativesecure` TLS; ignores `cluster_name`.
- `internal/dbops` in the provider module is an **internal** package — cannot be imported; must issue our own SQL (repo already depends on `github.com/ClickHouse/clickhouse-go/v2 v2.47.0` directly).
- Test scaffolding exists: `config/roleimport_test.go` (`fakeManaged` + table-driven), directly reusable for the generalized initializer.
- `database` keeps field `uuid`; `user`/`role`/`settings_profile` `delete(schema,"id")` and use observation field `id`.

## Security Considerations

- SQL uses parameterized query (`WHERE name = ?`) — no injection. Preserve this; never string-concat the name.
- `cluster_name` cannot be a bind parameter (it is an identifier inside `cluster(...)`); it is single-quote escaped. It originates from the resource spec (operator-controlled, not end-user input), so risk is low, but keep the escaping.
- No new credential surface; reuses existing `ResolveConnParams` credentials.

## Performance Considerations

- One extra ClickHouse query per resource only on reconciles where the id/uuid is empty or sentinel (i.e., pre-create / import). Post-adoption the initializer short-circuits (real UUID present). Negligible.

## Reference Mapping (porting the role pattern)

| Source concept | Source location | Target |
|---|---|---|
| `findRoleUUIDByName` (SQL) | `internal/clients/roleresolver.go` | generic `findUUIDByName(table, idField)` |
| `NewRoleUUIDResolver` | `internal/clients/roleresolver.go` | per-resource resolver factories |
| `RoleUUIDResolver` type + `roleResolverFactory` | `config/roleimport.go` | `UUIDResolver` + `resolverFactories` registry |
| `roleImportInitializer()` | `config/roleimport.go` | `adoptByNameInitializer(resourceName, field)` |
| `SetRoleResolverFactory` | `cmd/provider/main.go:158` | `SetResolverFactory(name, f)` calls for 4 resources |

## Decisions

- **Cluster-aware lookup (resolved):** resolver reads optional `spec.forProvider.cluster_name`. When set, query `FROM cluster(<name>, system.<table>)` (mirrors provider `WithCluster`); when null, plain `FROM system.<table>`. This also fixes the latent `role` bug (current role resolver ignores cluster and can miss rows on multi-replica clusters).

## Steps

### Step 1: Generic name→UUID resolver in internal/clients
- **Test:** none standalone (needs live ClickHouse); covered indirectly by Step 3 via injected fake.
- **Implement:** refactor `internal/clients/roleresolver.go` into a generic, cluster-aware resolver; keep `NewRoleUUIDResolver` behavior.
- **Code:**
```go
func newUUIDResolver(kube client.Client, table, idField string) config.UUIDResolver {
	return func(ctx context.Context, mg xpresource.Managed) (string, bool, error) {
		paved, err := fieldpath.PaveObject(mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot pave managed resource: %w", err)
		}
		name, err := paved.GetString("spec.forProvider.name")
		if err != nil {
			return "", false, fmt.Errorf("cannot read spec.forProvider.name: %w", err)
		}
		// Optional — absent on single node / ClickHouse Cloud.
		cluster, _ := paved.GetString("spec.forProvider.cluster_name")
		params, err := ResolveConnParams(ctx, kube, mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot resolve connection params: %w", err)
		}
		return findUUIDByName(ctx, params, table, idField, name, cluster)
	}
}

// findUUIDByName runs SELECT toString(<idField>) FROM <from> WHERE name = ?
// table/idField are internal constants, never user input (no injection surface).
// When cluster is set, query across all replicas via cluster(<cluster>, <table>),
// mirroring the provider's WithCluster; cluster is quote-escaped, never a bind arg.
func findUUIDByName(ctx context.Context, params ConnParams, table, idField, name, cluster string) (string, bool, error) {
	// ... clickhouse.Open as in findRoleUUIDByName ...
	from := table
	if cluster != "" {
		from = fmt.Sprintf("cluster('%s', %s)", strings.ReplaceAll(cluster, "'", "\\'"), table)
	}
	q := fmt.Sprintf("SELECT toString(%s) AS id FROM %s WHERE name = ?", idField, from)
	rows, err := conn.Query(ctx, q, name)
	// ... scan first row, return (uuid,true,nil) or ("",false,nil) ...
}

func NewRoleUUIDResolver(kube client.Client) config.UUIDResolver { return newUUIDResolver(kube, "system.roles", "id") }
func NewDatabaseUUIDResolver(kube client.Client) config.UUIDResolver { return newUUIDResolver(kube, "system.databases", "uuid") }
func NewUserUUIDResolver(kube client.Client) config.UUIDResolver { return newUUIDResolver(kube, "system.users", "id") }
func NewSettingsProfileUUIDResolver(kube client.Client) config.UUIDResolver { return newUUIDResolver(kube, "system.settings_profiles", "id") }
```
- **Constraint:** keep `WHERE name = ?` parameterized; `table`/`idField` are package constants only.
- **Validation:** `go build ./...`

### Step 2: Generalize resolver registry + adopt initializer in config
- **Test:** `config/roleimport_test.go` — retarget to the generalized initializer; add a case per field ("id" and "uuid").
- **Implement:** in `config/roleimport.go`, replace single `RoleUUIDResolver`/`roleResolverFactory` with a registry keyed by resource name; generalize `roleImportInitializer`.
- **Code:**
```go
type UUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

var resolverFactories = map[string]func(client.Client) UUIDResolver{}

func SetResolverFactory(resourceName string, f func(client.Client) UUIDResolver) {
	resolverFactories[resourceName] = f
}

// adoptByNameInitializer seeds the real UUID (found by name) into observation[field],
// falling back to sentinelUUID (force-create) when absent or no resolver is wired.
func adoptByNameInitializer(resourceName, field string) config.NewInitializerFn {
	return func(kube client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for %s initializer: %w", field, err)
			}
			if val, _ := obs[field].(string); val != "" && val != sentinelUUID {
				return nil // real UUID already set
			}
			if obs == nil {
				obs = make(map[string]any)
			}
			factory := resolverFactories[resourceName]
			if factory == nil {
				obs[field] = sentinelUUID
				return tr.SetObservation(obs)
			}
			uuid, found, err := factory(kube)(ctx, mg)
			if err != nil {
				return fmt.Errorf("cannot resolve UUID for import of %s: %w", resourceName, err)
			}
			if found {
				obs[field] = uuid
			} else {
				obs[field] = sentinelUUID
			}
			return tr.SetObservation(obs)
		})
	}
}
```
- **Depends on:** Step 1
- **Validation:** `go test ./config/...`

### Step 3: Wire adopt initializer for database/user/settings_profile
- **Test:** `config/roleimport_test.go` table cases already prove initializer logic for both fields; assert `overrides.go` uses `adoptByNameInitializer` (compile-level).
- **Implement:** `config/overrides.go` — replace the three `sentinelUUIDInitializer(...)` calls; keep everything else (password initializers, schema edits) unchanged.
- **Code:**
```go
// database (field "uuid")
r.InitializerFns = append(r.InitializerFns, adoptByNameInitializer("clickhousedbops_database", "uuid"))
// user (field "id") — keep PasswordValidator/RefProcessor/Generator ordering, swap sentinel for adopt
r.InitializerFns = append(r.InitializerFns,
	PasswordValidator(), PasswordRefProcessor(),
	adoptByNameInitializer("clickhousedbops_user", "id"),
	PasswordGenerator("spec.forProvider.autoGeneratePassword"))
// settings_profile (field "id")
r.InitializerFns = append(r.InitializerFns, adoptByNameInitializer("clickhousedbops_settings_profile", "id"))
// role: migrate roleImportInitializer() -> adoptByNameInitializer("clickhousedbops_role","id")
```
- **Constraint:** `sentinelUUIDInitializer` remains as the no-resolver fallback path inside `adoptByNameInitializer`; keep the standalone func only if still referenced, else remove.
- **Depends on:** Step 2
- **Validation:** `go test ./config/...`

### Step 4: Register resolver factories in main
- **Test:** none (composition root).
- **Implement:** `cmd/provider/main.go` — replace the single `SetRoleResolverFactory` with four `SetResolverFactory` calls.
- **Code:**
```go
config.SetResolverFactory("clickhousedbops_role", clients.NewRoleUUIDResolver)
config.SetResolverFactory("clickhousedbops_database", clients.NewDatabaseUUIDResolver)
config.SetResolverFactory("clickhousedbops_user", clients.NewUserUUIDResolver)
config.SetResolverFactory("clickhousedbops_settings_profile", clients.NewSettingsProfileUUIDResolver)
```
- **Depends on:** Step 1, Step 3
- **Validation:** `go build ./... && go test ./...`

### Step 5: Docs
- **Implement:** `docs/import.md` — confirm database/user/settingsProfile import-by-name works again (name-only).
- **Validation:** manual read.

### Step 6: E2e chainsaw — adopt-by-name observe (sibling resource)
- **Test:** `e2e/tests/**/observe-by-name/chainsaw-test.yaml` for database/user/role/settings_profile.
- **Implement:** create the managed resource, wait Ready, then apply a sibling `Observe`-only MR with the same `spec.forProvider.name`; assert `Synced=True`/`Ready=True`; delete sibling in `finally`.
- **Code:**
```yaml
- name: observe-sibling-by-name
  try:
  - apply:
      resource:
        apiVersion: clickhousedbops.crossplane.io/v1alpha1
        kind: Database
        metadata: { name: testdb-observe }
        spec:
          forProvider: { name: testdb }
          managementPolicies: ["Observe"]
          providerConfigRef: { name: default }
  - assert:
      timeout: 5m
      resource:
        apiVersion: clickhousedbops.crossplane.io/v1alpha1
        kind: Database
        metadata: { name: testdb-observe }
        status:
          conditions:
          - { type: Synced, status: "True" }
          - { type: Ready, status: "True" }
  finally:
  - delete: { ref: { apiVersion: clickhousedbops.crossplane.io/v1alpha1, kind: Database, name: testdb-observe } }
```
- **Constraint:** sibling must NOT create (managementPolicies Observe) — a re-create would prove the adopt path failed.
- **Validation:** `make chainsaw-e2e`

## Acceptance Criteria

- [ ] Importing an existing database/user/settings_profile by name adopts it (no re-create, no "already exists").
- [ ] Creating a brand-new resource still works (sentinel fallback path when name not found).
- [ ] Lookup failure returns an error (reconcile retries; never force-creates over an existing resource).
- [ ] `role` behavior unchanged (plus now cluster-aware).
- [ ] Import by name on a multi-replica cluster (cluster_name set) resolves the UUID via `cluster(...)`.
- [ ] `go test ./...` green; `golangci-lint run` clean.

## Checklist (non-TDD cleanup)

- [ ] Lint clean
- [ ] `docs/import.md` updated
- [ ] Remove `sentinelUUIDInitializer` if no longer referenced
- [ ] Manual verify against a live ClickHouse (import existing DB by name)
