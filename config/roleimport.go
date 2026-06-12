package config

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RoleUUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

// Injected at runtime so the ClickHouse client (and the apis it pulls in) stays out of
// the code generator's import graph, which would otherwise break `make generate`.
var roleResolverFactory func(client.Client) RoleUUIDResolver

func SetRoleResolverFactory(f func(client.Client) RoleUUIDResolver) {
	roleResolverFactory = f
}

// CREATE ROLE is not idempotent (unlike GRANT), so a restored role that already exists
// must be adopted by its real UUID rather than re-created — otherwise reconcile fails with
// "already exists in `replicated`".
func roleImportInitializer() config.NewInitializerFn {
	return func(kube client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for role import initializer: %w", err)
			}
			if val, _ := obs["id"].(string); val != "" && val != sentinelUUID {
				return nil
			}

			if obs == nil {
				obs = make(map[string]any)
			}

			if roleResolverFactory == nil {
				obs["id"] = sentinelUUID
				return tr.SetObservation(obs)
			}

			uuid, found, err := roleResolverFactory(kube)(ctx, mg)
			if err != nil {
				return fmt.Errorf("cannot resolve role UUID for import: %w", err)
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
