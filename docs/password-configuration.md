# User Password Configuration

This document describes the three methods for configuring passwords when creating a ClickHouse User resource.

## Overview

When creating a User resource, you must choose ONE of these mutually exclusive password configuration methods:

1. **Auto-generate Password** (`autoGeneratePassword: true`)
2. **Reference Plaintext Secret** (`passwordSecretRef`)
3. **Reference Hash Secret** (`passwordSha256HashSecretRef`)

The provider will fail immediately if multiple methods are configured or if none are configured.

## Method 1: Auto-generate Password

Let the provider generate a secure random password and store it in a Kubernetes Secret.

### How it works

1. User sets `autoGeneratePassword: true`
2. Controller generates a cryptographically secure random password
3. Password is stored in the secret referenced by `writeConnectionSecretToRef`
4. Secret contains two keys:
   - `password`: plaintext password
   - `hash`: SHA256 hash of the password
5. Controller auto-sets `passwordSha256HashSecretRef` for the Terraform provider

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

The generated secret (`myuser-credentials`) will contain:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: myuser-credentials
  namespace: default
data:
  password: <base64-plaintext>  # Auto-generated random password
  hash: <base64-sha256-hash>    # SHA256 hash of the password
```

### Use Case

- When you want the provider to manage password generation
- For new users where you don't have an existing password
- When you want both plaintext and hash readily available

## Method 2: Reference Plaintext Secret

Point to an existing Kubernetes Secret containing a plaintext password. The controller automatically computes the SHA256 hash and stores it in the same secret.

### How it works

1. User creates a secret with plaintext password
2. User sets `passwordSecretRef` pointing to that secret (name, key, optional namespace)
3. Controller reads the plaintext password from the secret
4. Controller computes SHA256 hash
5. Controller writes hash back to the same secret under key `hash`
6. Controller auto-sets `passwordSha256HashSecretRef` for the Terraform provider

### Example

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-password
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

After reconciliation, the secret will contain both keys:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-password
  namespace: default
data:
  password: bXktc2VjdXJlLXBhc3N3b3JkLTEyMw==  # Original plaintext
  hash: <computed-sha256-hash>                  # Auto-computed by controller
```

### Use Case

- When you have an existing plaintext password secret
- When you manage passwords outside of Kubernetes (e.g., from a secrets manager)
- When you want a simpler configuration without separate secrets
- No need for `writeConnectionSecretToRef`

### Cluster-Scoped Resources

For cluster-scoped Users, the `namespace` field is required:

```yaml
spec:
  forProvider:
    name: myuser
    passwordSecretRef:
      name: my-password
      key: password
      namespace: crossplane-system  # Required for cluster scope
```

## Method 3: Reference Hash Secret

Provide a reference to a secret that already contains the SHA256 hash of the password.

### How it works

1. You pre-compute or obtain the SHA256 hash of the password
2. You create a secret containing the hash
3. User sets `passwordSha256HashSecretRef` pointing to that secret (name, key, namespace)
4. Controller uses the hash as-is for the Terraform provider

### Example

```bash
# Pre-compute the SHA256 hash
PASSWORD="my-secure-password"
HASH=$(echo -n "$PASSWORD" | sha256sum | cut -d' ' -f1)
echo "Hash: $HASH"
```

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: user-password-hash
  namespace: default
type: Opaque
stringData:
  hash: "4b9bb80853f2d4b5d1a3e2c1f3c8b9e2f3c1d8e9f3a2b1c0d9e8f7a6b5c4d"
---
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: myuser
  namespace: default
spec:
  forProvider:
    name: myuser
    passwordSha256HashSecretRef:
      name: user-password-hash
      key: hash
  providerConfigRef:
    name: default
```

### Use Case

- When you have pre-computed or externally-sourced hashes
- When you cannot or do not want to store plaintext passwords in Kubernetes
- When integrating with external secrets management systems
- For compliance requirements that prohibit plaintext in cluster

## Validation

The provider enforces strict mutual exclusivity through initializer validation:

### Invalid: Multiple Methods

```yaml
# ❌ INVALID - combining two methods
spec:
  forProvider:
    name: myuser
    autoGeneratePassword: true
    passwordSecretRef:
      name: my-password
      key: password
```

Error:
```
autoGeneratePassword, passwordSecretRef, and passwordSha256HashSecretRef are mutually exclusive - only one may be set
```

### Invalid: No Method

```yaml
# ❌ INVALID - no password method specified
spec:
  forProvider:
    name: myuser
```

Error:
```
one of autoGeneratePassword, passwordSecretRef, or passwordSha256HashSecretRef must be set
```

## Updating Passwords

When you need to change a user's password:

### For autoGeneratePassword

The password is managed by the provider. To rotate it, delete and recreate the User resource.

### For passwordSecretRef

Update the plaintext password in the referenced secret. The controller will:
1. Detect the password change
2. Recompute the SHA256 hash
3. Update the hash key in the secret
4. Trigger a reconciliation to update ClickHouse

```bash
kubectl patch secret my-password -p '{"stringData":{"password":"new-password"}}'
```

### For passwordSha256HashSecretRef

Update the hash in the referenced secret:

```bash
NEW_HASH=$(echo -n "new-password" | sha256sum | cut -d' ' -f1)
kubectl patch secret user-password-hash -p "{\"stringData\":{\"hash\":\"$NEW_HASH\"}}"
```

## Security Considerations

- **Plaintext Secrets**: `passwordSecretRef` requires plaintext in Kubernetes. Use appropriate RBAC and encryption at rest.
- **Hash-Only Secrets**: `passwordSha256HashSecretRef` avoids storing plaintext but prevents the provider from validating password strength.
- **Auto-Generated**: `autoGeneratePassword` generates cryptographically secure passwords (min 32 characters, mixed case, numbers, symbols).
- **Secret Encryption**: Always enable Kubernetes Secret encryption at rest for any method.

## Troubleshooting

### "key not found in secret"

The secret exists but doesn't contain the specified key.

```bash
kubectl get secret my-password -o yaml
# Check that the key matches exactly (e.g., "password" vs "pwd")
```

### "cannot read passwordSecretRef"

The secret doesn't exist or is in a different namespace.

```bash
# Check secret exists
kubectl get secret my-password -n default

# For cluster-scoped resources, namespace must be explicit
# For namespaced resources, namespace defaults to resource namespace
```

### "autoGeneratePassword, passwordSecretRef, and passwordSha256HashSecretRef are mutually exclusive"

You've configured multiple password methods. Choose ONE:

```yaml
# ✓ CORRECT - only one method
spec:
  forProvider:
    name: myuser
    autoGeneratePassword: true
  writeConnectionSecretToRef:
    name: myuser-credentials
```

## See Also

- [User Examples](../examples/)
- [E2E Tests](../e2e/tests/)
- [Main README - Configuring User Passwords](../README.md#configuring-user-passwords)
