# Plan: Well-shaped ClickHouse connection Secret

## Context

Today the provider's connection Secret (`writeConnectionSecretToRef`) carries only
`password` + `hash`, written by the custom `PasswordGenerator` initializer
(`config/password_generator.go:286` `applyPasswordSecret`). Consumers therefore
can't connect straight away — host/port/protocol/username live only in the
ProviderConfig, and a Crossplane composition has to assemble a usable secret by
hand (a go-templating function building `clickhouse_*` keys).

Goal: have the **provider itself** emit the full, ready-to-use shape so the
composition can stop reshaping. Mirror the provider-sql idea (return a populated
connection-detail map) but adapted to this Upjet provider, where the connection
Secret is written by initializers, not a hand-written `Create()`.

Decisions (confirmed):
- **Full shape on ALL password paths** (autoGenerate **and** passwordSecretRef).
  Bring-your-own password must be mirrored into the connection secret because the
  user deletes their original secret after "import".
- **`clickhouse_*` keys only** in the connection secret — drop `password`/`hash`.
- `clickhouse_database` is **not** emitted (provider has no database field on
  User; it comes from a separate DB resource and stays in the composition).
- Use the **real port** from the ProviderConfig (`ConnParams.Port`), not a
  protocol-derived guess.

## Target secret shape (type `connection.crossplane.io/v1alpha1`)

| Key | Value (raw bytes; k8s base64-encodes on write) | Source |
|-----|-----|--------|
| `clickhouse_username` | username | external name / `spec.forProvider.name` |
| `clickhouse_host` | host | `ConnParams.Host` |
| `clickhouse_port` | port | `strconv.Itoa(ConnParams.Port)` |
| `clickhouse_protocol` | `http`/`https`/`nativesecure` | `ConnParams.Protocol` |
| `clickhouse_password` | plaintext | generated pw / passwordSecretRef plaintext |
| `clickhouse_password_encoded` | `url.QueryEscape(pw)` | derived |
| `clickhouse_password_sha256` | `hex(sha256(pw))` | derived (matches old `hash`) |

Hash-only path (`passwordSha256HashSecretRef` with no plaintext): emit metadata
keys + `clickhouse_password_sha256` (from the referenced hash); skip
plaintext/encoded.

## Key reuse (do NOT reinvent)

- `internal/clients/clients.go:38` `ResolveConnParams(ctx, crClient, mg)` —
  already returns Host/Port/Protocol/Username/Password from the ProviderConfig
  secret, and its doc comment says it's **"Needed by initializers"**. This is the
  host/port source the explanation calls the hard part — it already exists.
- `config/password_generator.go:240` `resolveConnectionSecretRef(mg)` — resolves
  the connection-secret name/ns for both namespaced and cluster-scoped MRs.
- No import cycle: `config` → `internal/clients` is one-way (`clients` imports
  only `apis/*`).

## Implementation

### 1. New file `config/connection_secret.go`

Shared writer producing the `clickhouse_*` shape, replacing the secret-writing
responsibility currently in `applyPasswordSecret`.

```go
const (
    keyUsername  = "clickhouse_username"
    keyHost      = "clickhouse_host"
    keyPort      = "clickhouse_port"
    keyProtocol  = "clickhouse_protocol"
    keyPassword  = "clickhouse_password"
    keyPwEncoded = "clickhouse_password_encoded"
    keyPwSHA256  = "clickhouse_password_sha256"
)

// writeConnectionSecret upserts the full clickhouse_* connection shape into the
// secret named secretName/ns. plaintext may be "" (hash-only path); when empty,
// password/encoded are skipped and sha256 is taken from existingHash.
func writeConnectionSecret(ctx context.Context, c client.Client, mg xpresource.Managed,
    secretName, ns, plaintext, existingHash string) error {
    cp, err := clients.ResolveConnParams(ctx, c, mg) // host/port/protocol
    if err != nil { return fmt.Errorf("cannot resolve connection params: %w", err) }

    s := &corev1.Secret{}
    s.SetName(secretName); s.SetNamespace(ns)
    s.Type = xpresource.SecretTypeConnection
    _ = c.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, s) // IgnoreNotFound
    if s.Data == nil { s.Data = map[string][]byte{} }

    s.Data[keyUsername] = []byte(meta.GetExternalName(mg)) // fallback spec.forProvider.name
    s.Data[keyHost]     = []byte(cp.Host)
    s.Data[keyPort]     = []byte(strconv.Itoa(int(cp.Port)))
    s.Data[keyProtocol] = []byte(cp.Protocol)

    sha := existingHash
    if plaintext != "" {
        sum := sha256.Sum256([]byte(plaintext)); sha = hex.EncodeToString(sum[:])
        s.Data[keyPassword]  = []byte(plaintext)
        s.Data[keyPwEncoded] = []byte(url.QueryEscape(plaintext))
    }
    if sha != "" { s.Data[keyPwSHA256] = []byte(sha) }

    return xpresource.NewAPIPatchingApplicator(c).Apply(ctx, s)
}
```

(Use `github.com/crossplane/crossplane-runtime/v2/pkg/meta` for `GetExternalName`;
fall back to paved `spec.forProvider.name` if external name empty.)

### 2. `config/password_generator.go`

- **autoGenerate path** (`PasswordGenerator`): replace `applyPasswordSecret(...)`
  with `writeConnectionSecret(ctx, c, mg, secretName, ns, pw, "")`. Remove
  `applyPasswordSecret` (and its `password`/`hash` writes).
- **passwordSecretRef path** (`PasswordRefProcessor`): it already reads plaintext
  at `:152`. After it writes the hash back to the user's own secret and sets the
  TF ref (unchanged — TF plumbing), additionally, **if**
  `resolveConnectionSecretRef(mg)` returns a ref, call
  `writeConnectionSecret(ctx, c, mg, name, ns, plaintext, newHash)`.
- **Idempotency** (`isPasswordHashAlreadySet`): check `keyPwSHA256` instead of
  `passwordHashKey` in the connection secret.
- **TF hash ref** (`setPasswordHashSecretRef`): for the autoGenerate case the ref
  points at the connection secret — change its `key` from `"hash"` to
  `keyPwSHA256`. The passwordSecretRef case points at the user's own secret under
  `"hash"` — leave unchanged.
- Keep `passwordHashKey = "hash"` for the user's own-secret write
  (`applyHashSecret`) — TF plumbing, not the connection secret.

### 3. `config/overrides.go` (doc only)

Update the `auto_generate_password` field description (`:113-117`) — currently
says keys `'password'`/`'hash'`; change to the `clickhouse_*` keys.

## Edge cases (and tests — std `testing`, the repo's convention)

- autoGenerate: secret has all 7 `clickhouse_*` keys, **no** `password`/`hash`;
  `passwordSha256HashSecretRef.key == clickhouse_password_sha256`.
- passwordSecretRef: connection secret has full shape incl. plaintext; user's own
  secret still gets `hash` + TF ref points there.
- `ResolveConnParams` incomplete creds → initializer fails closed (wrapped error).
- No `writeConnectionSecretToRef` set → skip connection write, no crash.
- Re-reconcile idempotent: existing `clickhouse_password_sha256` skips regen.
- observe-only policy → skipped (existing `isObserveOnly` guard).
- Special chars in password → verify `clickhouse_password_encoded` = `url.QueryEscape`.
- namespaced vs cluster connection ref → `resolveConnectionSecretRef` covers both.

Tests in `config/connection_secret_test.go`:
- table-test `writeConnectionSecret`: exact key set for plaintext vs hash-only.
- `PasswordGenerator` produces the new shape + repointed TF ref.
- `PasswordRefProcessor` writes connection secret when ref set, skips when not,
  still writes own-secret hash.

## Verification (end-to-end)

1. `go test ./config/...` / `make build`.
2. `make generate` (CRD field docs changed in step 3).
3. Apply a `User` with `autoGeneratePassword: true` + `writeConnectionSecretToRef`;
   `kubectl get secret <ref> -o yaml` shows all `clickhouse_*` keys populated.
4. Apply a `User` with `passwordSecretRef`; confirm connection secret carries the
   bring-your-own plaintext under `clickhouse_password`; delete the original
   secret and confirm the connection secret is self-sufficient.

## Files touched
- `config/connection_secret.go` (new)
- `config/password_generator.go`
- `config/overrides.go` (doc strings)
- `config/connection_secret_test.go` (new tests)
