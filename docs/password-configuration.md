# User Password Configuration

This document describes the two methods for configuring passwords when creating a ClickHouse User resource.

## Overview

When creating a User resource, you must choose ONE of these mutually exclusive password configuration methods:

1. **Auto-generate Password** (`autoGeneratePassword: true`)
2. **Bring Your Own Password (BYOP)** - store plaintext in `writeConnectionSecretToRef`

In both cases, the same Kubernetes Secret (referenced by `writeConnectionSecretToRef`) is used for all password data. See [Validation](#validation) for error cases.

---

## Method 1: Auto-generate Password

Let the provider generate a secure random password.

### How it works

1. Set `autoGeneratePassword: true`
2. Controller generates a cryptographically secure random password
3. Writes to the secret referenced by `writeConnectionSecretToRef`:
  - `password`: plaintext password
  - `hash`: SHA256 hash
4. Auto-sets `passwordSha256HashSecretRef` pointing to that secret
5. Idempotent: if `hash` already exists in the secret, skips generation - **the hash is not recomputed or compared against the plaintext**; existence alone is the signal that the controller has already run

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

Resulting secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: myuser-credentials
  namespace: default
data:
  password: <base64-plaintext>
  hash: <base64-sha256-hash>
```

### Password rotation

Deleting the `writeConnectionSecretToRef` secret triggers rotation:
on the next reconcile the controller detects the secret is gone, generates a new password, recreates the secret
 and updates ClickHouse.

> **Warning:** deleting the secret causes a new password to be generated. Any application using the old plaintext
`password` key will break until updated.

Deleting and recreating the User resource has the same effect.
Deleting only the secret (keeping the User resource) behaves identically - it is not an error state.

## Method 2: Bring Your Own Password (BYOP)

Store a plaintext password in the `writeConnectionSecretToRef` secret.
The controller computes the hash and keeps it in sync.

### How it works

1. Create the secret referenced by `writeConnectionSecretToRef` with the plaintext password under a key
  (default: `password`, configurable via `spec.forProvider.secretPasswordKey`).
2. Controller reads the plaintext, computes SHA256 hash, writes it back under `hash`.
3. Auto-sets `passwordSha256HashSecretRef` pointing to that secret.
4. Supports password rotation - see below.

### Example

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: myuser-credentials
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
  writeConnectionSecretToRef:
    name: myuser-credentials
    namespace: default
  providerConfigRef:
    name: default
```

After reconciliation, the same secret will also contain `hash`:

```yaml
data:
  password: bXktc2VjdXJlLXBhc3N3b3JkLTEyMw==
  hash: <computed-sha256-hash>
```

### Custom key

If your plaintext is stored under a different key than `password`:

```yaml
spec:
  forProvider:
    name: myuser
    secretPasswordKey: "mypassword"
```

The controller will read `secret["mypassword"]` instead.

### BYOP lifecycle and edge cases

#### Password rotation

Update the plaintext in the secret. On the next reconcile the controller:

1. Reads the new plaintext
2. Computes SHA256 and compares to the existing `hash` in the secret
3. Detects mismatch → writes new hash
4. Terraform provider sees the hash change → updates the ClickHouse user password

```bash
kubectl patch secret myuser-credentials -p '{"stringData":{"password":"new-password"}}'
```

> **Note:** Password changes in ClickHouse are not immediate.
> This provider's controller only watches the User managed resource itself, it does not watch the
> `writeConnectionSecretToRef` secret for changes.
> Reconciliation is poll-based (default: 1 minute), so the ClickHouse user password will be updated
> on the next poll cycle after the secret is updated.

#### Deleting the User resource while keeping the secret

> **Warning:** Crossplane manages the secret referenced by `writeConnectionSecretToRef`.
> When the User resource is deleted, **Crossplane will also delete the secret**, even if you created
> it manually with the plaintext password.
> Back up any credentials you need before deleting the `User` resource.

#### Deleting the secret while keeping the User resource

If `autoGeneratePassword: true`, a deleted secret triggers automatic password regeneration.
With BYOP (`autoGeneratePassword: false`), there is no plaintext to recover from, so the controller cannot self-heal.

If the secret is deleted while the User resource still exists:

- The controller cannot find the secret,
  + skips BYOP processing,
  + `passwordSha256HashSecretRef` still points to the now-deleted secret.
- The Terraform provider will fail to read the hash,
  + reconcile errors until the secret is restored.
- **Recovery:** recreate the secret with the plaintext password under the same key.
  On the next reconcile, the controller recomputes and writes the hash, and reconciliation resumes.

#### Deleting only the `hash` key from the secret

If `hash` is removed but `password` remains:

- Controller computes hash from plaintext → no existing hash to compare against → writes new hash.
- Reconciliation continues normally. This is effectively a forced re-sync.

#### Deleting only the `password` key from the secret

If `password` (or your custom `secretPasswordKey`) is removed but `hash` remains:

- Controller finds no plaintext → skips BYOP processing entirely
- The existing hash remains in `passwordSha256HashSecretRef` → ClickHouse user is unchanged
- **Recovery:** restore the plaintext key in the secret

#### Reusing an existing secret for a new User resource

If the secret already exists and contains a `hash` key (e.g. from a previously deleted User),
the controller will compute the hash from the plaintext and compare:

- If hashes match → no write, reconciliation continues using the existing password
- If hashes differ → new hash written, ClickHouse user updated to match the current plaintext

This means the new User will get the password currently in the secret, not necessarily the one from the previous User.

---

## Method 3: Reference Hash Secret directly

If you have a pre-computed SHA256 hash and don't want to store plaintext in the cluster, set
`passwordSha256HashSecretRef` manually. This bypasses both methods above.

```yaml
spec:
  forProvider:
    name: myuser
    passwordSha256HashSecretRef:
      name: myuser-credentials
      key: hash
```

> This method is specified here only for illustrative purposes.
> From a user point of view the `passwordSha256HashSecretRef` field should be treated as internal.

---

## Validation

The validator enforces: exactly one of `autoGeneratePassword` or `passwordSha256HashSecretRef` must be set.
Methods 1 and 2 auto-set `passwordSha256HashSecretRef`, so you only need to set it manually for method 3.

> **Initializer ordering:** for BYOP, `passwordSha256HashSecretRef` is not present on the first reconcile:
it gets set by the BYOP initializer.
The BYOP initializer therefore runs **before** the validator so that by the time validation runs,
the field is already populated.

### Invalid: both set

```yaml
# ❌ INVALID
spec:
  forProvider:
    autoGeneratePassword: true
    passwordSha256HashSecretRef:
      name: myuser-credentials
      key: hash
```

Error:

```
autoGeneratePassword and passwordSha256HashSecretRef are mutually exclusive - only one may be set
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
either autoGeneratePassword or passwordSha256HashSecretRef must be set
```

---

## Security Considerations

- **BYOP**: plaintext lives in Kubernetes. Enable encryption at rest and restrict Secret access via RBAC.
- **Auto-generate**: plaintext also written to Kubernetes (under `password` key). Same precautions apply.
- **Hash-only (method 3)**: avoids plaintext in cluster entirely.
- All methods: the Terraform provider only ever sees the SHA256 hash, never the plaintext.

## See Also

- [User Examples](../examples/)
- [E2E Tests](../e2e/tests/)
- [Main README - Configuring User Passwords](../README.md#configuring-user-passwords)
