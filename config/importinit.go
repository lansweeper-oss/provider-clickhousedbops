package config

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UUIDResolver looks up the provider-assigned UUID of an existing resource by name
type UUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

// resolverFactories maps a Terraform resource name to the factory that builds its UUIDResolver.
var resolverFactories = map[string]func(client.Client) UUIDResolver{}

// SetResolverFactory registers the UUIDResolver factory for a resource.
func SetResolverFactory(resourceName string, f func(client.Client) UUIDResolver) {
	resolverFactories[resourceName] = f
}

// adoptByNameInitializer seeds the resource identifier before the first observe so
// Upjet's UUID-based Read adopts an existing resource instead of re-creating it.
// A real UUID already in the observation (post-import/creation) is re-verified by
// name, because ClickHouse assigns new UUIDs when access entities are re-created
// out-of-band (backup restore, replicated access-storage rebuild).
// See docs/import.md for the rationale.
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
				return verifyImportIdentifier(ctx, kube, mg, tr, obs, resourceName, field, val)
			}
			if obs == nil {
				obs = make(map[string]any)
			}
			return seedImportIdentifier(ctx, kube, mg, tr, obs, resourceName, field)
		})
	}
}

// verifyImportIdentifier re-resolves the UUID by name when the observation already
// holds a real UUID. When the entity was re-created in ClickHouse with a new UUID,
// the stored one is stale: the UUID-based Read then reports "not found" and the
// reconciler issues a create that fails with "already exists". Reseeding the resolved
// UUID re-adopts the entity instead. When the name resolves to nothing the stored
// UUID is kept, so Read confirms the absence and a legitimate re-create proceeds.
func verifyImportIdentifier(ctx context.Context, kube client.Client, mg xpresource.Managed, tr terraformedObservation, obs map[string]any, resourceName, field, current string) error {
	factory := resolverFactories[resourceName]
	if factory == nil {
		return nil
	}
	id, found, err := factory(kube)(ctx, mg)
	if err != nil {
		return fmt.Errorf("cannot verify UUID of %s: %w", resourceName, err)
	}
	if !found || id == current {
		return nil
	}
	return seedIdentifier(mg, tr, obs, field, id)
}

// seedImportIdentifier honors an external name that is a UUID (import by UUID),
// otherwise resolves the UUID from spec.forProvider.name (import by name), or seeds
// the sentinel (force-create) when the resource is absent or no resolver is wired.
func seedImportIdentifier(ctx context.Context, kube client.Client, mg xpresource.Managed, tr terraformedObservation, obs map[string]any, resourceName, field string) error {
	// A pinned external name that is a UUID takes precedence over name resolution.
	// The Crossplane default external name is the resource name, which is not a UUID and falls through.
	if en := stripClusterPrefix(meta.GetExternalName(mg), sep); en != "" {
		if _, err := uuid.Parse(en); err == nil {
			return seedIdentifier(mg, tr, obs, field, en)
		}
	}

	id, found := sentinelUUID, false
	if factory := resolverFactories[resourceName]; factory != nil {
		var err error
		if id, found, err = factory(kube)(ctx, mg); err != nil {
			return fmt.Errorf("cannot resolve UUID for import of %s: %w", resourceName, err)
		}
	}
	if !found {
		// Absent (or no resolver): seed the sentinel so the provider reports "not found" and creation proceeds
		obs[field] = sentinelUUID
		return tr.SetObservation(obs)
	}
	return seedIdentifier(mg, tr, obs, field, id)
}

// seedIdentifier writes id to both the observation and the external name
func seedIdentifier(mg xpresource.Managed, tr terraformedObservation, obs map[string]any, field, id string) error {
	meta.SetExternalName(mg, id)
	obs[field] = id
	return tr.SetObservation(obs)
}
