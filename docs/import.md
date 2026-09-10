# Resource import

This document describes the minimum parameters needed to import `provider-clickhousedbops` resources.

Typically you use the `crossplane.io/external-name` annotation to tell Crossplane which is the ID of
a given resource, so it can internally _import_ the resource.

In this provider, this is managed automatically by Crossplane: you do not need to set it manually.
Just set the required parameters in the resource `spec` and Crossplane handles the rest.

Parameters marked with **ref** support `Ref`/`Selector` fields for cross-resource references
(e.g. `settingsProfileIdRef`, `roleIdRef`).

## Importable resources

These resources use one or more parameters as their identity.
Set the parameters (directly or via a selector) and Crossplane populates the external name automatically.

| Resource | Required Identity Parameters |
|----------|------------------------------|
| `Database` | `clusterName`, `name` |
| `GrantPrivilege` | `granteeUserName`, `privilegeName`, `databaseName`, `tableName`, `columnName` |
| `GrantRole` | `clusterName`, `granteeUserName`, `roleName` |
| `Role` | `clusterName`, `name` |
| `Setting` | `name`, `settingsProfileId` (**ref**) |
| `SettingProfile` | `clusterName`, `name` |
| `SettingProfileAssociation` | `roleId` (**ref**), `settingsProfileId` (**ref**) |

> **Users cannot be imported by name.** Adopting a user by name would silently take
> over an existing user whose password the provider cannot observe or repair, so it
> is rejected: creating a `User` whose `name` already exists in ClickHouse fails
> loudly with `already exists` on `CREATE USER`. The import value for a `User` must
> be the user's UUID, never its name — pinned via the `crossplane.io/external-name`
> annotation; see [Importing a user](#importing-a-user-uuid-required).

## Example

To import an existing ClickHouse resource into Crossplane, create a manifest with:

1. The **required identity parameters** listed in the table above (under `spec.forProvider`).
2. `managementPolicies: ["Observe"]` so Crossplane reads the remote state without modifying it.
3. A `providerConfigRef` pointing to valid ClickHouse credentials.

You do **not** need to set the `crossplane.io/external-name` annotation, the provider
builds it automatically from the identity parameters and updates it after the first
successful observe.

### Importing a user (UUID required)

Users are the exception: name-based adoption is disabled for them (see the note
above), so the import value must be the user's UUID, never its name. Look it
up first:

```sql
SELECT toString(id) FROM system.users WHERE name = 'jane'
```

and pin it as the `crossplane.io/external-name` annotation (see
[Import by UUID (advanced)](#import-by-uuid-advanced)).

Cluster-scoped:

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: jane                   # any name you choose for the Crossplane resource
  annotations:
    crossplane.io/external-name: 4a8d6f1e-...   # UUID from system.users
spec:
  forProvider:
    clusterName: cluster       # must match the existing ClickHouse cluster name
    name: jane                 # must match the existing username in ClickHouse
  managementPolicies:
    - Observe                  # read-only: Crossplane will not create or modify the user
  providerConfigRef:
    name: default
```

Namespaced:

```yaml
apiVersion: clickhousedbops.m.crossplane.io/v1alpha1
kind: User
metadata:
  name: jane
  namespace: crossplane-system
  annotations:
    crossplane.io/external-name: 4a8d6f1e-...
spec:
  forProvider:
    clusterName: cluster
    name: jane
  managementPolicies:
    - Observe
  providerConfigRef:
    name: default
    kind: ClusterProviderConfig
```

When a user is imported by UUID **with full management policies** (not
observe-only) and a password is configured in the spec, the provider applies the
spec's password hash to the live user at import time (`ALTER USER ... IDENTIFIED
WITH sha256_hash`), so the connection secret is truthful from the moment of
adoption.

Further information about strategies when importing Users might be found in its
[dedicated document](user-import-workflow.md).

After applying, Crossplane will:

- Adopt the existing resource by its UUID — resolved from the identity `name` for
  name-importable resources (looking it up on the cluster when `clusterName` is
  set), or taken from the pinned `crossplane.io/external-name` annotation for users.
- Set `crossplane.io/external-name` to that UUID.
- Populate `status.atProvider` with the full remote state.
- Report the resource as `Ready` and `Synced` once the observe succeeds.

## Import by UUID (advanced)

Instead of a name, you may pin `crossplane.io/external-name` directly to the
resource's UUID. When the annotation is a UUID it takes precedence and name
resolution is skipped; the plain resource name (the crossplane default external
name) is not a UUID, so it falls through to name-based lookup. Both forms end up
adopting the same resource. For `User` resources this is the **only** import
path: name-based lookup is disabled for them.

## How adoption works internally

Background for maintainers (implementation in `config/importinit.go`):

- This provider runs in no-fork mode. upjet calls the Terraform provider's `Read`,
  which looks a resource up by its **UUID**, not by name. It never calls
  `ImportState` (the only provider path that resolves a name to a UUID). So the
  UUID must be known before the first observe.
- An initializer (`adoptByNameInitializer`) runs before observe. It determines the
  UUID from, in order: an external-name that is already a UUID; a lookup by
  `spec.forProvider.name` against the ClickHouse `system.*` tables; otherwise a
  sentinel UUID that matches no row (so the provider reports "not found" and
  creation proceeds). This avoids re-creating a resource that already exists —
  important for non-idempotent operations such as `CREATE ROLE`.
- The resolved UUID is written to **both** the observation and the external name,
  because the two resource shapes consume it differently: `database` keys `Read`
  off the seeded `status.atProvider.uuid`; `user`/`role`/`settings profile` keep an
  `id` attribute in their framework schema, and upjet rebuilds the Terraform state
  `id` from the external name (`GetIDFn`) just before `Read`, so the external name
  must carry the UUID. Neither value is persisted with an explicit `kube.Update`:
  the in-memory managed resource is reused through the same reconcile, and an
  update would reset the freshly seeded `status.atProvider` to the empty server
  value.
- Name resolution is cluster-aware: when `clusterName` is set the lookup runs
  across the cluster (`cluster(<name>, system.<table>)`), matching the provider's
  own behavior.
- No name resolver for `user`: name adoption would silently take over an
  existing user (issue #104), so a collision falls through to `CREATE USER` and
  fails with `already exists`. A pinned UUID external-name is the only import path.
- An `AdoptHook` (`internal/clients/adopt.go`) runs on adoption, before the
  identifier is seeded so failures retry. For `user` it applies the spec's
  password hash via `ALTER USER`. Observe-only resources skip it.
