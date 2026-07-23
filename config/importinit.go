package config

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UUIDResolver looks up the provider-assigned UUID of an existing resource by
// its name. found is false when no such resource exists.
type UUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

// resolverFactories maps a Terraform resource name to the factory that builds
// its UUIDResolver. Injected at runtime so the ClickHouse client (and the apis
// it pulls in) stays out of the code generator's import graph, which would
// otherwise break `make generate`.
var resolverFactories = map[string]func(client.Client) UUIDResolver{}

// SetResolverFactory registers the UUIDResolver factory for a resource.
func SetResolverFactory(resourceName string, f func(client.Client) UUIDResolver) {
	resolverFactories[resourceName] = f
}

// adoptByNameInitializer seeds the observation identifier (field) before the
// first Terraform observe cycle so upjet's UUID-based Read can adopt a
// pre-existing resource instead of re-creating it.
//
// No-fork upjet calls the provider's Read (which looks up by UUID) rather than
// ImportState (the only path that resolves a name to a UUID). Without this, a
// resource that already exists in ClickHouse cannot be imported by name: the
// sentinel UUID matches no row, the provider reports "not found", and upjet
// issues a create that fails for non-idempotent resources (e.g. CREATE ROLE
// "already exists in `replicated`").
//
// When the resolver finds the resource, its real UUID is seeded so upjet
// imports it. When absent (or no resolver is wired), the sentinelUUID is seeded
// so the provider reports "not found" and creation proceeds. A real UUID
// already present (post-import/creation) is left untouched.
func adoptByNameInitializer(resourceName, field string) config.NewInitializerFn {
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
			if val, _ := obs[field].(string); val != "" && val != sentinelUUID {
				// Real UUID already set (post-import/creation) - leave it alone.
				return nil
			}
			if obs == nil {
				obs = make(map[string]any)
			}

			factory := resolverFactories[resourceName]
			if factory == nil {
				obs[field] = sentinelUUID
				return tr.SetObservation(obs)
			}

			uuid, found, err := factory(kube)(ctx, mg)
			if err != nil {
				return fmt.Errorf("cannot resolve UUID for import of %s: %w", resourceName, err)
			}
			if found {
				obs[field] = uuid
			} else {
				obs[field] = sentinelUUID
			}
			return tr.SetObservation(obs)
		})
	}
}
