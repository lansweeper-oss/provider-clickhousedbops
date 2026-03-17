# Resource Identity Reference

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

You do **not** need to set the `crossplane.io/external-name` annotation — the provider
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

After applying, Crossplane will:
- Set `crossplane.io/external-name` to the username (`jane`).
- Populate `status.atProvider` with the full remote state.
- Report the resource as `Ready` and `Synced` once the observe succeeds.
