# User Password Configuration

This document describes the methods for configuring passwords when creating a ClickHouse User resource.

## Overview

When creating a User resource, you must choose ONE of these mutually exclusive password configuration methods:

1. **Auto-generate Password** (`autoGeneratePassword: true`)
2. **Bring Your Own Password** (`passwordSecretRef`)

`writeConnectionSecretToRef` is required for Method 1 (the controller writes the generated password there).
For Method 2 it is not needed: the hash is written back to `passwordSecretRef` itself.

See [Validation](#validation) for error cases.

---

## Method 1: Auto-generate Password

Let the provider generate a secure random password.

### How it works

1. Set `autoGeneratePassword: true`.
2. Controller generates a cryptographically secure random password.
3. Writes to the secret referenced by `writeConnectionSecretToRef`:
   - `password`: plaintext password.
   - `hash`: SHA256 hash.
4. Auto-sets `passwordSha256HashSecretRef` pointing to that secret.
5. Idempotent: if `hash` already exists in the secret, skips generation.
  **The hash is not recomputed or compared against the plaintext**; existence alone is the signal
  that the controller has already run.

### Example

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: myuser
  namespace: default
spec:
  forProvider:
    name: myuser
    autoGeneratePassword: true
  writeConnectionSecretToRef:
    name: myuser-credentials
    namespace: default
  providerConfigRef:
    name: default
```

Resulting `myuser-credentials` secret:

```yaml
data:
  password: <base64-plaintext>
  hash: <base64-sha256-hash>
```

### Password rotation

Deleting the `writeConnectionSecretToRef` secret triggers rotation: the controller detects the secret is gone,
generates a new password, recreates the secret, and updates ClickHouse.
Deleting and recreating the User resource has the same effect.

> **Warning:** deleting the secret causes a new password to be generated.
> Any application using the old `password` key will break until updated.

## Method 2: Bring Your Own Password

Point to a user-owned secret containing a plaintext password.
The controller reads the plaintext, computes the hash, and writes the hash back to the **same secret**.
No `writeConnectionSecretToRef` needed.

### How it works

1. Create a secret with the plaintext password under the key of your choice.
2. Set `passwordSecretRef` pointing to that secret (`name`, `key`, optional `namespace`).
3. Controller reads the plaintext.
4. Computes SHA256 hash.
5. Writes hash back to the same secret under key `hash`.
6. Auto-sets `passwordSha256HashSecretRef` pointing to that secret.
7. Supports password rotation - see below.

### Example

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-password          # user-owned, NOT managed by Crossplane
  namespace: default
type: Opaque
stringData:
  password: "my-secure-password-123"
---
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: myuser
  namespace: default
spec:
  forProvider:
    name: myuser
    passwordSecretRef:
      name: my-password
      key: password
      # namespace is optional, defaults to resource namespace
  providerConfigRef:
    name: default
```

After reconciliation, the same secret also contains `hash`:

```yaml
data:
  password: bXktc2VjdXJlLXBhc3N3b3JkLTEyMw==
  hash: <b64encoded-computed-sha256-hash>
```

### Secret ownership

`my-password` is user-owned - Crossplane does not manage it and will **not** delete it when the User resource is deleted.
This is the key advantage over Method 1.

### Password rotation

Update the plaintext in `passwordSecretRef`. On the next reconcile the controller:

1. Reads the new plaintext.
2. Computes SHA256 and compares to the existing `hash` in the same secret.
3. Detects mismatch → writes new hash.
4. Terraform provider sees the hash change → updates the ClickHouse user password.

```bash
kubectl patch secret my-password -p '{"stringData":{"password":"new-password"}}'
```

> **Note:** Password changes in ClickHouse are not immediate.
> This provider's controller only watches the User managed resource itself, it does not watch `passwordSecretRef`
> for changes. Reconciliation is poll-based (default: 1 minute), so the ClickHouse user password will be updated
> on the next poll cycle after the secret is updated.

### Edge cases

#### Deleting the User resource while keeping `passwordSecretRef`

The secret is user-owned - Crossplane does not manage it and will not delete it.
Safe to delete and recreate the User resource.

#### Deleting `passwordSecretRef` while keeping the User resource

- Controller cannot read the plaintext → reconcile errors
- `passwordSha256HashSecretRef` points to the now-deleted secret → Terraform provider errors
- ClickHouse user password is not changed
- **Recovery:** restore the secret with the plaintext.
  Controller recomputes and writes the hash on the next reconcile

#### Deleting only the `hash` key from `passwordSecretRef`

Hash missing → mismatch → hash rewritten on next reconcile. Reconciliation continues normally.

#### Reusing an existing `passwordSecretRef` for a new User resource

The controller recomputes the hash from the current plaintext and compares to any existing `hash`:

- Match → no write, reconciliation continues using the existing password
- Mismatch → new hash written, ClickHouse user updated to match current plaintext

## Method 3: Reference Hash Secret directly

If you have a pre-computed SHA256 hash and don't want to store plaintext in the cluster at all,
set `passwordSha256HashSecretRef` manually. This bypasses both methods above.

```yaml
spec:
  forProvider:
    name: myuser
    passwordSha256HashSecretRef:
      name: my-hash-secret
      key: hash
```

> `passwordSha256HashSecretRef` is an internal field auto-set by Methods 1 and 2.
> Only set it manually for this use case.

---

## Validation

The validator enforces:
- `autoGeneratePassword` and `passwordSecretRef` are mutually exclusive.
- At least one of `autoGeneratePassword`, `passwordSecretRef`, or `passwordSha256HashSecretRef` must be set.

### Invalid: both set

```yaml
# ❌ INVALID
spec:
  forProvider:
    autoGeneratePassword: true
    passwordSecretRef:
      name: my-password
      key: password
```

Error:

```
autoGeneratePassword and passwordSecretRef are mutually exclusive - only one may be set.
```

### Invalid: neither set

```yaml
# ❌ INVALID
spec:
  forProvider:
    name: myuser
```

Error:

```
one of autoGeneratePassword or passwordSecretRef must be set.
```

---

## Security Considerations

- **Method 1 (auto-generate):** plaintext written to `writeConnectionSecretToRef`.
  Enable encryption at rest and restrict Secret access via RBAC.
- **Method 2 (BYOP):** plaintext and hash both stay in the user-owned `passwordSecretRef`.
  `writeConnectionSecretToRef` is not used.
- **Method 3 (hash-only):** no plaintext in cluster at all.
- All methods: the Terraform provider only ever sees the SHA256 hash, never the plaintext.

## Imported Users and Password Management

When a User resource is imported with an `Observe`-only management policy, password management
is fully skipped: the validator, generator, and ref processor all short-circuit. The user
retains whatever password ClickHouse currently has.

**The provider cannot detect whether the ClickHouse password differs from what is stored in
Kubernetes.** ClickHouse stores `password_sha256_hex` in `system.users`, but the Terraform
provider does not expose it in state, so there is no value to compare against.

### Taking control of an imported user's password

After verifying that `status.atProvider` matches the desired spec:

1. **Auto-generate a new password:** set `autoGeneratePassword: true` and
   `writeConnectionSecretToRef`, then switch the management policy to full.
   On the next reconcile the controller detects no hash in the connection secret,
   generates a new password, and pushes it to ClickHouse.

2. **Bring your own password:** create a secret with the desired plaintext, set
   `passwordSecretRef`, then switch the management policy to full.
   On the next reconcile the controller computes the hash and pushes it to ClickHouse.

In both cases the ClickHouse user password is replaced. Any application using the
old password will break until updated.

## See Also

- [User Examples](../examples/)
- [E2E Tests](../e2e/tests/)
- [Main README - Configuring User Passwords](../README.md#configuring-user-passwords)
