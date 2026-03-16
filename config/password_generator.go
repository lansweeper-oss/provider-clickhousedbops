package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	v1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/password"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PasswordGenerator returns an InitializerFn that will generate a password
// for a resource if the toggle field is set to true and the secret referenced
// by the secretRefFieldPath is not found or does not have content corresponding
// to the password key.
//
// The SHA256 hash of the generated password is stored in the secret referenced
// by secretRefFieldPath (for use by the Terraform provider), while the plaintext
// password is stored in the managed resource's writeConnectionSecretToRef secret
// (for use by applications connecting to the database).
func PasswordGenerator(secretRefFieldPath, toggleFieldPath string) config.NewInitializerFn {
	return func(c client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			sel, generate, err := shouldGeneratePassword(ctx, c, mg, secretRefFieldPath, toggleFieldPath)
			if err != nil || !generate {
				return err
			}
			pw, err := password.Generate()
			if err != nil {
				return fmt.Errorf("cannot generate password: %w", err)
			}
			if err := applyHashSecret(ctx, c, mg, sel, pw); err != nil {
				return err
			}
			return applyConnectionSecret(ctx, c, mg, pw)
		})
	}
}

// shouldGeneratePassword returns the secret key selector and whether a new
// password should be generated. Returns an error if any field lookup fails
// unexpectedly.
func shouldGeneratePassword(ctx context.Context, c client.Client, mg xpresource.Managed, secretRefFieldPath, toggleFieldPath string) (*v1.SecretKeySelector, bool, error) {
	paved, err := fieldpath.PaveObject(mg)
	if err != nil {
		return nil, false, fmt.Errorf("cannot pave object: %w", err)
	}
	sel := &v1.SecretKeySelector{}
	if err := paved.GetValueInto(secretRefFieldPath, sel); err != nil {
		if xpresource.Ignore(fieldpath.IsNotFound, err) != nil {
			return nil, false, fmt.Errorf("cannot unmarshal %s into a secret key selector: %w", secretRefFieldPath, err)
		}
		return nil, false, nil
	}
	s := &corev1.Secret{}
	err = c.Get(ctx, types.NamespacedName{Namespace: sel.Namespace, Name: sel.Name}, s)
	if xpresource.IgnoreNotFound(err) != nil {
		return nil, false, fmt.Errorf("cannot get password secret: %w", err)
	}
	if err == nil && len(s.Data[sel.Key]) != 0 {
		return nil, false, nil
	}
	gen, err := paved.GetBool(toggleFieldPath)
	if xpresource.Ignore(fieldpath.IsNotFound, err) != nil {
		return nil, false, fmt.Errorf("cannot get the value of %s: %w", toggleFieldPath, err)
	}
	return sel, gen, nil
}

// applyHashSecret stores the SHA256 hash of pw in the secret referenced by sel.
func applyHashSecret(ctx context.Context, c client.Client, mg xpresource.Managed, sel *v1.SecretKeySelector, pw string) error {
	sum := sha256.Sum256([]byte(pw))
	hash := hex.EncodeToString(sum[:])

	s := &corev1.Secret{}
	s.SetName(sel.Name)
	s.SetNamespace(sel.Namespace)
	if err := c.Get(ctx, types.NamespacedName{Namespace: sel.Namespace, Name: sel.Name}, s); xpresource.IgnoreNotFound(err) != nil {
		return fmt.Errorf("cannot get password secret: %w", err)
	}
	if !meta.WasCreated(s) {
		// We don't want to own the Secret if it is created by someone
		// else, otherwise the deletion of the managed resource will
		// delete the Secret that we didn't create in the first place.
		meta.AddOwnerReference(s, meta.AsOwner(meta.TypedReferenceTo(mg, mg.GetObjectKind().GroupVersionKind())))
	}
	if s.Data == nil {
		s.Data = make(map[string][]byte, 1)
	}
	s.Data[sel.Key] = []byte(hash)
	if err := xpresource.NewAPIPatchingApplicator(c).Apply(ctx, s); err != nil {
		return fmt.Errorf("cannot apply password secret: %w", err)
	}
	return nil
}

// applyConnectionSecret stores the plaintext pw in the managed resource's
// writeConnectionSecretToRef secret under the "password" key.
func applyConnectionSecret(ctx context.Context, c client.Client, mg xpresource.Managed, pw string) error {
	connWriter, ok := mg.(xpresource.LocalConnectionSecretWriterTo)
	if !ok {
		return nil
	}
	connRef := connWriter.GetWriteConnectionSecretToReference()
	if connRef == nil {
		return nil
	}
	connSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: mg.GetNamespace(), Name: connRef.Name}, connSecret); xpresource.IgnoreNotFound(err) != nil {
		return fmt.Errorf("cannot get connection secret: %w", err)
	}
	connSecret.SetName(connRef.Name)
	connSecret.SetNamespace(mg.GetNamespace())
	if connSecret.Data == nil {
		connSecret.Data = make(map[string][]byte, 1)
	}
	connSecret.Data["password"] = []byte(pw)
	if err := xpresource.NewAPIPatchingApplicator(c).Apply(ctx, connSecret); err != nil {
		return fmt.Errorf("cannot apply connection secret: %w", err)
	}
	return nil
}
