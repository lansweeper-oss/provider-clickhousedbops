package config

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UUIDResolver looks up the provider-assigned UUID of an already-existing
// ClickHouse object (role/user/settings_profile) by its name. found=false means
// the object does not exist yet and the resource should be created.
type UUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

// RoleUUIDResolver is kept as an alias for backwards compatibility with the
// original role-only API.
type RoleUUIDResolver = UUIDResolver

// resolverFactories maps a Terraform resource name (e.g. "clickhousedbops_role")
// to a factory that builds its UUIDResolver. Populated at runtime from main so
// the ClickHouse client (and the apis it pulls in) stays out of the code
// generator's import graph, which would otherwise break `make generate`.
var resolverFactories = map[string]func(client.Client) UUIDResolver{}

// SetUUIDResolverFactory registers the resolver factory for a resource.
func SetUUIDResolverFactory(resourceName string, f func(client.Client) UUIDResolver) {
	resolverFactories[resourceName] = f
}

// SetRoleResolverFactory registers the role resolver. Retained for backwards
// compatibility; delegates to SetUUIDResolverFactory.
func SetRoleResolverFactory(f func(client.Client) RoleUUIDResolver) {
	SetUUIDResolverFactory("clickhousedbops_role", f)
}

// uuidImportInitializer seeds status.atProvider.id before the first Terraform
// observe so the provider looks the object up by its real UUID.
//
// CREATE ROLE/USER/SETTINGS PROFILE are NOT idempotent (unlike GRANT), so an
// object that already exists (e.g. after a backup restore) must be adopted by its
// real UUID rather than re-created — otherwise reconcile fails with
// "already exists in `replicated`".
//
// The seed is only trusted when it is a real UUID (and not the sentinel). Any
// other value — empty, the sentinel, or the object NAME leaked into the id slot
// by a name-keyed refresh — triggers a fresh resolve. This keeps the provider
// lookup keyed by the real UUID so crossplane.io/external-name stays stable.
func uuidImportInitializer(resourceName string) config.NewInitializerFn {
	return func(kube client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for %s import initializer: %w", resourceName, err)
			}
			// A real provider-assigned UUID is already present: nothing to do.
			if val, _ := obs["id"].(string); isUUID(val) && val != sentinelUUID {
				return nil
			}

			if obs == nil {
				obs = make(map[string]any)
			}

			factory := resolverFactories[resourceName]
			if factory == nil {
				obs["id"] = sentinelUUID
				return tr.SetObservation(obs)
			}

			uuid, found, err := factory(kube)(ctx, mg)
			if err != nil {
				return fmt.Errorf("cannot resolve UUID for %s import: %w", resourceName, err)
			}
			if found {
				obs["id"] = uuid
			} else {
				obs["id"] = sentinelUUID
			}
			return tr.SetObservation(obs)
		})
	}
}

// roleImportInitializer is the role-specific initializer. Retained for
// backwards compatibility; delegates to the generic uuidImportInitializer.
func roleImportInitializer() config.NewInitializerFn {
	return uuidImportInitializer("clickhousedbops_role")
}
