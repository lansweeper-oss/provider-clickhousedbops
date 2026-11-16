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
| `User` | `name` |

## Example

To import an existing ClickHouse resource into Crossplane, create a manifest with:

1. The **required identity parameters** listed in the table above (under `spec.forProvider`).
2. `managementPolicies: ["Observe"]` so Crossplane reads the remote state without modifying it.
3. A `providerConfigRef` pointing to valid ClickHouse credentials.

You do **not** need to set the `crossplane.io/external-name` annotation, the provider
builds it automatically from the identity parameters and updates it after the first
successful observe.

### Importing a user (cluster-scoped)

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: jane                   # any name you choose for the Crossplane resource
spec:
  forProvider:
    clusterName: cluster       # must match the existing ClickHouse cluster name
    name: jane                 # must match the existing username in ClickHouse
  managementPolicies:
    - Observe                  # read-only: Crossplane will not create or modify the user
  providerConfigRef:
    name: default
```

### Importing a user (namespaced)

```yaml
apiVersion: clickhousedbops.m.crossplane.io/v1alpha1
kind: User
metadata:
  name: jane
  namespace: crossplane-system
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

Further information about strategies when importing Users might be found in its
[dedicated document](user-import-workflow.md).

After applying, Crossplane will:
- Resolve the identity `name` to the resource's provider-assigned UUID and adopt the
  existing resource (looking it up on the cluster when `clusterName` is set).
- Set `crossplane.io/external-name` to that UUID.
- Populate `status.atProvider` with the full remote state.
- Report the resource as `Ready` and `Synced` once the observe succeeds.

## Import by UUID (advanced)

Instead of a name, you may pin `crossplane.io/external-name` directly to the
resource's UUID. When the annotation is a UUID it takes precedence and name
resolution is skipped; the plain resource name (the crossplane default external
name) is not a UUID, so it falls through to name-based lookup. Both forms end up
adopting the same resource.

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

## Security considerations

Adoption by name means a managed resource takes control of any existing ClickHouse
entity whose name matches `spec.forProvider.name`, whether Crossplane created it or
not. The provider does not check whether another managed resource already manages
that entity. It also cannot tell a takeover from the import workflow above: an
`Observe`-only resource that is later switched to full management looks the same in
both cases.

Treat permission to create or edit managed resources that reference a provider
config as equal to holding that provider config's ClickHouse credentials. For
example, anyone who can create a `User` against credentials that hold `ALTER USER`
can adopt an existing user, set its password and log in as that user. `ALTER USER`
is a global privilege in ClickHouse, so it cannot be limited to selected users.

Isolation is the responsibility of the platform team:

- **One provider config per tenant.** For namespaced resources, give each tenant a
  namespaced `ProviderConfig` in its own namespace. A namespaced `ProviderConfig`
  only serves resources and secrets in that namespace.
- **Restrict `ClusterProviderConfig` references.** Kubernetes RBAC does not stop a
  resource from referencing a provider config, because the provider reads it with
  its own service account. Use an admission policy (a `ValidatingAdmissionPolicy`
  is enough) to control which `providerConfigRef` each namespace may use. Only
  trusted users should be able to create cluster-scoped resources, since those can
  reference any cluster-scoped `ProviderConfig`.
- **Least-privilege credentials.** Grant each provider config only the privileges
  its tenant needs. Credentials that can manage users can manage every user on the
  server, so do not give user management to a tenant on a shared server unless it
  is trusted with all users there.
- **Protect the provider's admin user.** On self-managed ClickHouse, define the user
  that the provider config credentials log in as in `users.xml`. Users from configuration files
  are read-only to SQL, so a managed `User` that adopts it cannot change its
  password or drop it.
- **Duplicate resources.** Two managed resources with the same name on the same
  server overwrite each other, for example a `User` password. The provider does not
  detect this. To reject duplicates, use an admission controller that can look up
  other objects, such as Kyverno (`apiCall`) or Gatekeeper (referential data).
  `ValidatingAdmissionPolicy` cannot do this, because CEL cannot read other objects.
  `Observe`-only resources never write to ClickHouse, so they can safely coexist
  with a managed resource for the same entity.
