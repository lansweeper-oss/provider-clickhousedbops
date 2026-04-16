# Importing Existing ClickHouse Users

This document describes how to import existing ClickHouse users into Crossplane management using the User resource.

## Overview

The import workflow has two phases:

1. **Observe-only (read-only)**: Import the user without managing it.
2. **Manage (create/update/delete)**: Take control of password and user properties.

## Phase 1: Observe-Only Import

Start with observe-only mode to safely read existing users without touching them.

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

## Phase 2: Transition to Manage Mode

Once the user is imported and you want to manage it with Crossplane, change the management policy.
You must now provide a password via one of three methods:

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
