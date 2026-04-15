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

// PasswordValidator returns an InitializerFn that enforces mutual exclusivity of
// password configuration methods: either autoGeneratePassword OR passwordSha256HashSecretRef
// may be set, but not both. User-provided plaintext is detected at reconcile time from
// writeConnectionSecretToRef.
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

			_, err = paved.GetValue("spec.forProvider.passwordSha256HashSecretRef")
			hashRefSet := err == nil

			if autoGenSet && hashRefSet {
				return fmt.Errorf("autoGeneratePassword and passwordSha256HashSecretRef are mutually exclusive - only one may be set")
			}
			if !autoGenSet && !hashRefSet {
				return fmt.Errorf("either autoGeneratePassword or passwordSha256HashSecretRef must be set")
			}

			return nil
		})
	}
}

// updateSecretWithHash computes the SHA256 hash of the plaintext password
// and writes it to the secret under passwordHashKey.
func updateSecretWithHash(ctx context.Context, c client.Client, secretNamespace, secretName string, plaintext []byte) error {
	s := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: secretNamespace, Name: secretName}, s); err != nil {
		return fmt.Errorf("cannot get secret for hash update: %w", err)
	}

	sum := sha256.Sum256(plaintext)
	hash := hex.EncodeToString(sum[:])

	if s.Data == nil {
		s.Data = make(map[string][]byte)
	}
	s.Data[passwordHashKey] = []byte(hash)

	if err := xpresource.NewAPIPatchingApplicator(c).Apply(ctx, s); err != nil {
		return fmt.Errorf("cannot write hash to secret: %w", err)
	}

	return nil
}

const defaultPasswordKey = "password"

// PasswordUserProvided returns an InitializerFn that processes user-provided plaintext
// passwords. The key to read from the secret is taken from spec.forProvider.secretPasswordKey
// (defaults to "password"). The secret is the one referenced by writeConnectionSecretToRef.
// Controller reads the plaintext, computes SHA256 hash, writes it under "hash" in the same
// secret, and auto-sets passwordSha256HashSecretRef.
func PasswordUserProvided() config.NewInitializerFn {
	return func(c client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			paved, err := fieldpath.PaveObject(mg)
			if err != nil {
				return fmt.Errorf("cannot pave object: %w", err)
			}

			passwordKey, err := paved.GetString("spec.forProvider.secretPasswordKey")
			if fieldpath.IsNotFound(err) || passwordKey == "" {
				passwordKey = defaultPasswordKey
			} else if err != nil {
				return fmt.Errorf("cannot get secretPasswordKey: %w", err)
			}

			secretName, ns, ok := resolveConnectionSecretRef(mg)
			if !ok {
				return nil // No writeConnectionSecretToRef, nothing to do
			}

			s := &corev1.Secret{}
			err = c.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, s)
			if xpresource.IgnoreNotFound(err) != nil {
				return fmt.Errorf("cannot get connection secret: %w", err)
			}

			// If secret doesn't exist or no plaintext under the configured key, nothing to do
			if err != nil || len(s.Data[passwordKey]) == 0 {
				return nil
			}

			// Always recompute hash from plaintext to support password rotation.
			// If hash matches current plaintext, updateSecretWithHash is still called but
			// the Apply is a no-op from Kubernetes' perspective (same data).
			plaintext := s.Data[passwordKey]
			sum := sha256.Sum256(plaintext)
			newHash := hex.EncodeToString(sum[:])
			if string(s.Data[passwordHashKey]) == newHash {
				return setPasswordHashSecretRef(mg, secretName, ns, passwordHashKey)
			}

			if err := updateSecretWithHash(ctx, c, ns, secretName, plaintext); err != nil {
				return err
			}

			return setPasswordHashSecretRef(mg, secretName, ns, passwordHashKey)
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
				return setPasswordHashSecretRef(mg, secretName, ns, passwordHashKey)
			}

			pw, err := password.Generate()
			if err != nil {
				return fmt.Errorf("cannot generate password: %w", err)
			}
			if err := applyPasswordSecret(ctx, c, secretName, ns, pw); err != nil {
				return err
			}
			return setPasswordHashSecretRef(mg, secretName, ns, passwordHashKey)
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
func setPasswordHashSecretRef(mg xpresource.Managed, secretName, secretNamespace, key string) error {
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
