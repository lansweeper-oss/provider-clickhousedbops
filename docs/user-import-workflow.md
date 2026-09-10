# Importing Existing ClickHouse Users

This document describes how to import existing ClickHouse users into Crossplane management using the User resource.

## Overview

There are two routes:

1. **Direct import (recommended)**: import by UUID with full management policies
   and a password configured — the provider takes over the user in one step.
2. **Observe-first**: import read-only to inspect, optionally transition to
   manage mode later (with caveats, see below).

> **The import value for a user must be its UUID, never its name.** Unlike other
> resources, users are never adopted by name: implicit name adoption would let any
> manifest silently take over an existing user
> ([issue #104](https://github.com/lansweeper-oss/provider-clickhousedbops/issues/104)).
> Creating a `User` whose `name` already exists in ClickHouse without an explicit
> UUID import fails loudly with `already exists` on `CREATE USER`.

Find the UUID first and supply it as the resource's import value
(see [Import by UUID](import.md#import-by-uuid-advanced)):

```sql
SELECT toString(id) FROM system.users WHERE name = 'existing_user_in_clickhouse'
```

## Direct Import (recommended)

Import with full management policies and one of the password options from the
[transition section](#transitioning-to-manage-mode) below. At import time the
provider applies the spec's password hash to the live user in place
(`ALTER USER ... IDENTIFIED WITH sha256_hash`) — the user keeps its UUID, grants
and settings, and the connection secret is correct from the moment of adoption.

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: imported-user
spec:
  managementPolicies:
    - "*"
  forProvider:
    name: existing_user_in_clickhouse
    autoGeneratePassword: true      # or passwordSecretRef / passwordSha256HashSecretRef
  writeConnectionSecretToRef:
    name: imported-user-secret
    namespace: crossplane-system
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

## Observe-Only Import

Use observe-only mode to safely read existing users without touching them.

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: imported-user
spec:
  managementPolicies:
    - Observe
  forProvider:
    name: existing_user_in_clickhouse
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

In observe-only mode:
- No password fields are required (`autoGeneratePassword`, `passwordSecretRef`, `passwordSha256HashSecretRef`).
- Resource reads the user from ClickHouse and populates `status.atProvider`.
- No changes are made to ClickHouse or Kubernetes secrets.
- Safe to apply to an existing user without affecting it.

## Transitioning to Manage Mode

To take over an observe-only imported user, change the management policy and
provide a password via one of three methods.

> **Warning:** the in-place password apply only happens on the adopting
> reconcile — it does not re-fire when you flip an already-imported user from
> observe-only to manage. The password is then reconciled through the regular
> Terraform diff, and a password change deletes and recreates the user (new
> UUID, grants and settings lost). To keep the user intact, either use the
> [direct import](#direct-import-recommended) route, or provide the user's
> *current* password (Option B) so no diff arises.

### Option A: Auto-Generate New Password

Generates a new password and stores it in a Kubernetes secret.

> The user's password will change in ClickHouse.

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: imported-user
spec:
  managementPolicies:
    - "*"  # or ["Create", "LateInitialize", "Observe", "Update", "Delete"]
  forProvider:
    name: existing_user_in_clickhouse
    autoGeneratePassword: true
  writeConnectionSecretToRef:
    name: imported-user-secret
    namespace: crossplane-system
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

When you apply this:

1. Controller generates a random password.
2. Stores plaintext under key `password` and SHA256 hash under key `hash` in the secret.
3. Computes hash, stores in secret, and sets `passwordSha256HashSecretRef` pointing to it.
4. Terraform provider updates the ClickHouse user with the new password.
5. **User's password in ClickHouse changes** (login will break until you update client credentials).

**Use this when:**

- You're migrating user management to Crossplane.
- Password history is not important.
- You can rotate client credentials.

### Option B: Reference Existing Secret with Plaintext Password

You provide a Kubernetes secret containing the user's current plaintext password.

> User password in ClickHouse does not change.

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: imported-user
spec:
  managementPolicies:
    - "*"
  forProvider:
    name: existing_user_in_clickhouse
    passwordSecretRef:
      name: existing-password-secret
      key: password
      namespace: crossplane-system
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

Prerequisites:
- You must have the plaintext password (from ClickHouse admin, password manager, or initial setup)
- Create a Kubernetes secret containing it:

  ```bash
  kubectl create secret generic existing-password-secret \
    -n crossplane-system \
    --from-literal=password='the_plaintext_password'
  ```

When you apply the resource:

1. Controller reads plaintext from the secret.
2. Computes SHA256 hash of the plaintext.
3. Writes hash back to the same secret under key `hash`.
4. Sets `passwordSha256HashSecretRef` to point to that secret.
5. Terraform provider reads the hash and **verifies it matches ClickHouse** (no password change).
6. Supports password rotation: update plaintext in secret on next reconcile, hash changes, provider updates ClickHouse.

**Use this when:**

- You have the plaintext password available.
- Password continuity is important.
- You want to rotate passwords later by just updating the plaintext.

### Option C: Reference Secret with SHA256 Hash

If you don't have plaintext but have the SHA256 hash, create a secret with just the hash.
**User password in ClickHouse does not change.**

```bash
# Create secret with just the hash (compute offline or from ClickHouse logs)
kubectl create secret generic user-password-hash \
  -n crossplane-system \
  --from-literal=hash='abc123...'
```

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: imported-user
spec:
  managementPolicies:
    - "*"
  forProvider:
    name: existing_user_in_clickhouse
    passwordSha256HashSecretRef:
      name: user-password-hash
      key: hash
      namespace: crossplane-system
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

When you apply:

1. Controller uses the hash directly (no plaintext needed).
2. Terraform provider verifies hash matches ClickHouse.
3. **No password change** but also **no password rotation support** (can't rotate without plaintext).

**Use this when:**

- Plaintext password is unavailable.
- You only need to verify and manage the user without rotation.
- This is a read-heavy scenario (consider observe-only instead).

## Troubleshooting

### "one of autoGeneratePassword or passwordSecretRef must be set"
You switched to manage mode but forgot to add a password field.
Choose one of the three options above.

### Password mismatch after switching to manage
If using Option B and the plaintext doesn't match ClickHouse, the provider will reject the reconcile.
Verify the secret contains the correct plaintext.

### User reconciliation loops
If the user keeps reconciling, check logs for password hash mismatches.
For Option B, ensure the plaintext in the secret is up-to-date.
