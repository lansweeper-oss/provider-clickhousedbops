package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/v2/pkg/password"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const passwordHashKey = "hash"

// PasswordValidator enforces mutual exclusivity:
//   - autoGeneratePassword and passwordSecretRef are mutually exclusive
//   - at least one of autoGeneratePassword, passwordSecretRef, or passwordSha256HashSecretRef must be set
//
// passwordSha256HashSecretRef is auto-set by PasswordRefProcessor and PasswordGenerator after the
// first reconcile, so it is intentionally allowed alongside passwordSecretRef (not treated as a
// user-facing method choice).
func PasswordValidator() config.NewInitializerFn {
	return func(_ client.Client) managed.Initializer {
		return managed.InitializerFn(func(_ context.Context, mg xpresource.Managed) error {
			paved, err := fieldpath.PaveObject(mg)
			if err != nil {
				return fmt.Errorf("cannot pave object: %w", err)
			}

			autoGen, err := paved.GetBool("spec.forProvider.autoGeneratePassword")
			if err != nil && !fieldpath.IsNotFound(err) {
				return fmt.Errorf("cannot get autoGeneratePassword: %w", err)
			}
			autoGenSet := err == nil && autoGen

			_, err = paved.GetValue("spec.forProvider.passwordSecretRef")
			secretRefSet := err == nil

			_, err = paved.GetValue("spec.forProvider.passwordSha256HashSecretRef")
			hashRefSet := err == nil

			if autoGenSet && secretRefSet {
				return fmt.Errorf("autoGeneratePassword and passwordSecretRef are mutually exclusive - only one may be set")
			}
			if !autoGenSet && !secretRefSet && !hashRefSet {
				return fmt.Errorf("one of autoGeneratePassword or passwordSecretRef must be set")
			}

			return nil
		})
	}
}

// extractPasswordSecretRef extracts name, key, and namespace from spec.forProvider.passwordSecretRef.
// Returns empty name if the field is not set.
func extractPasswordSecretRef(mg xpresource.Managed) (name, key, namespace string, err error) {
	paved, err := fieldpath.PaveObject(mg)
	if err != nil {
		return "", "", "", fmt.Errorf("cannot pave object: %w", err)
	}

	_, err = paved.GetValue("spec.forProvider.passwordSecretRef")
	if fieldpath.IsNotFound(err) {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", fmt.Errorf("cannot get passwordSecretRef: %w", err)
	}

	nameVal, _ := paved.GetValue("spec.forProvider.passwordSecretRef.name")
	name, _ = nameVal.(string)
	if name == "" {
		return "", "", "", fmt.Errorf("passwordSecretRef.name is required and must be a string")
	}

	keyVal, _ := paved.GetValue("spec.forProvider.passwordSecretRef.key")
	key, _ = keyVal.(string)
	if key == "" {
		return "", "", "", fmt.Errorf("passwordSecretRef.key is required and must be a string")
	}

	nsVal, _ := paved.GetValue("spec.forProvider.passwordSecretRef.namespace")
	namespace, _ = nsVal.(string)
	if namespace == "" {
		namespace = mg.GetNamespace()
	}

	return name, key, namespace, nil
}

// PasswordRefProcessor reads the plaintext password from the user-owned secret referenced by
// passwordSecretRef, computes its SHA256 hash, and writes the hash back to the same secret
// under key "hash". It then auto-sets passwordSha256HashSecretRef pointing to that secret
// so the Terraform provider can read the hash. writeConnectionSecretToRef is not required
// when using this method.
//
// Supports password rotation: on every reconcile the hash is recomputed from the current
// plaintext and compared to the existing hash. A mismatch triggers a hash update, which
// causes the Terraform provider to update the ClickHouse user.
func PasswordRefProcessor() config.NewInitializerFn {
	return func(c client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			secretName, secretKey, secretNamespace, err := extractPasswordSecretRef(mg)
			if err != nil {
				return err
			}
			if secretName == "" {
				return nil // passwordSecretRef not set, nothing to do
			}

			// Single fetch: extract both plaintext and existing hash.
			s := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: secretNamespace, Name: secretName}, s); err != nil {
				return fmt.Errorf("cannot read passwordSecretRef secret %s/%s: %w", secretNamespace, secretName, err)
			}

			plaintext, ok := s.Data[secretKey]
			if !ok {
				return fmt.Errorf("key %q not found in secret %s/%s", secretKey, secretNamespace, secretName)
			}

			sum := sha256.Sum256(plaintext)
			newHash := hex.EncodeToString(sum[:])

			if string(s.Data[passwordHashKey]) == newHash {
				return setPasswordHashSecretRef(mg, secretName, secretNamespace)
			}

			if err := applyHashSecret(ctx, c, s, newHash); err != nil {
				return err
			}

			return setPasswordHashSecretRef(mg, secretName, secretNamespace)
		})
	}
}

// PasswordGenerator returns an InitializerFn that generates a password when
// toggleFieldPath resolves to true. The caller only needs to set
// autoGeneratePassword: true and writeConnectionSecretToRef - no other
// password fields are required.
//
// The initializer stores the plaintext password under key "password" and the
// SHA256 hash under key "hash" in the secret referenced by
// writeConnectionSecretToRef. It then auto-sets passwordSha256HashSecretRef
// on the spec so the Terraform provider can read the hash, without the user
// having to configure it manually.
//
// Both namespaced resources (LocalConnectionSecretWriterTo, namespace implicit
// from the MR) and cluster-scoped resources (ConnectionSecretWriterTo,
// namespace explicit in the ref) are supported.
func PasswordGenerator(toggleFieldPath string) config.NewInitializerFn {
	return func(c client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			paved, err := fieldpath.PaveObject(mg)
			if err != nil {
				return fmt.Errorf("cannot pave object: %w", err)
			}

			gen, err := paved.GetBool(toggleFieldPath)
			if xpresource.Ignore(fieldpath.IsNotFound, err) != nil {
				return fmt.Errorf("cannot get %s: %w", toggleFieldPath, err)
			}
			if !gen {
				return nil
			}

			secretName, ns, ok := resolveConnectionSecretRef(mg)
			if !ok {
				return nil
			}

			// Check idempotency: if the hash is already stored, skip generation.
			s := &corev1.Secret{}
			err = c.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, s)
			if xpresource.IgnoreNotFound(err) != nil {
				return fmt.Errorf("cannot get connection secret: %w", err)
			}
			if err == nil && len(s.Data[passwordHashKey]) != 0 {
				// Hash already present; just ensure the spec ref is set.
				return setPasswordHashSecretRef(mg, secretName, ns)
			}

			pw, err := password.Generate()
			if err != nil {
				return fmt.Errorf("cannot generate password: %w", err)
			}
			if err := applyPasswordSecret(ctx, c, secretName, ns, pw); err != nil {
				return err
			}
			return setPasswordHashSecretRef(mg, secretName, ns)
		})
	}
}

// resolveConnectionSecretRef returns the connection secret name and namespace
// for both namespaced (LocalConnectionSecretWriterTo) and cluster-scoped
// (ConnectionSecretWriterTo) managed resources. Returns ok=false if the
// resource does not implement either interface or the ref is nil.
func resolveConnectionSecretRef(mg xpresource.Managed) (name, ns string, ok bool) {
	if lw, ok := mg.(xpresource.LocalConnectionSecretWriterTo); ok {
		ref := lw.GetWriteConnectionSecretToReference()
		if ref == nil {
			return "", "", false
		}
		return ref.Name, mg.GetNamespace(), true
	}
	if cw, ok := mg.(xpresource.ConnectionSecretWriterTo); ok {
		ref := cw.GetWriteConnectionSecretToReference()
		if ref == nil {
			return "", "", false
		}
		return ref.Name, ref.Namespace, true
	}
	return "", "", false
}

// applyHashSecret writes the SHA256 hash into input secret. Caller must have already fetched s.
// Does not overwrite s.Type — preserves the existing secret type (e.g. Opaque for user-owned secrets).
func applyHashSecret(ctx context.Context, c client.Client, s *corev1.Secret, hash string) error {
	if s.Data == nil {
		s.Data = make(map[string][]byte, 1)
	}
	s.Data[passwordHashKey] = []byte(hash)

	if err := xpresource.NewAPIPatchingApplicator(c).Apply(ctx, s); err != nil {
		return fmt.Errorf("cannot apply secret: %w", err)
	}
	return nil
}

// applyPasswordSecret writes the plaintext password under key "password" and
// its SHA256 hash under key "hash" into the named secret.
// We should **never** call this method when coming from passwordSecretRef, since that would
// potentially exfiltrate any secret in the cluster which the provider has access to.
func applyPasswordSecret(ctx context.Context, c client.Client, secretName, ns, pw string) error {
	sum := sha256.Sum256([]byte(pw))
	hash := hex.EncodeToString(sum[:])

	s := &corev1.Secret{}
	s.SetName(secretName)
	s.SetNamespace(ns)
	s.Type = xpresource.SecretTypeConnection

	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, s); xpresource.IgnoreNotFound(err) != nil {
		return fmt.Errorf("cannot get connection secret: %w", err)
	}
	if s.Data == nil {
		s.Data = make(map[string][]byte, 2)
	}
	s.Data["password"] = []byte(pw)
	s.Data[passwordHashKey] = []byte(hash)

	if err := xpresource.NewAPIPatchingApplicator(c).Apply(ctx, s); err != nil {
		return fmt.Errorf("cannot apply connection secret: %w", err)
	}
	return nil
}

// setPasswordHashSecretRef sets spec.forProvider.passwordSha256HashSecretRef
// on the managed resource so upjet passes the hash to the Terraform provider.
// For cluster-scoped resources (namespace != "") the namespace is included in
// the ref; for namespaced resources upjet fills it in from the MR namespace.
// It uses a JSON round-trip to mutate the Go struct in place.
func setPasswordHashSecretRef(mg xpresource.Managed, secretName, secretNamespace string) error {
	key := passwordHashKey
	data, err := json.Marshal(mg)
	if err != nil {
		return fmt.Errorf("cannot marshal managed resource: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("cannot unmarshal managed resource: %w", err)
	}

	spec, ok := raw["spec"].(map[string]any)
	if !ok {
		return fmt.Errorf("spec field missing or not a map")
	}
	forProvider, ok := spec["forProvider"].(map[string]any)
	if !ok {
		forProvider = make(map[string]any)
		spec["forProvider"] = forProvider
	}
	ref := map[string]any{
		"name": secretName,
		"key":  key,
	}
	if secretNamespace != "" {
		ref["namespace"] = secretNamespace
	}
	forProvider["passwordSha256HashSecretRef"] = ref

	updated, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("cannot marshal updated resource: %w", err)
	}
	return json.Unmarshal(updated, mg)
}
