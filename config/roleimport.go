package config

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RoleUUIDResolver resolves the real ClickHouse UUID of a role by its name.
// found is false when no role with that name exists yet.
type RoleUUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

// roleResolverFactory builds a RoleUUIDResolver bound to a Kubernetes client. It
// is injected at runtime by the provider's main via SetRoleResolverFactory,
// which keeps the ClickHouse client (and the apis packages it transitively pulls
// in) out of this package's import graph. The code generator imports this
// package, and it deletes the generated deepcopy code for the apis packages
// before running, so any apis import here would break `make generate`.
//
// When nil (code generation, or unit tests without injection) the initializer
// falls back to the sentinel UUID, preserving the previous force-create behavior.
var roleResolverFactory func(client.Client) RoleUUIDResolver

// SetRoleResolverFactory wires the production RoleUUIDResolver. Call once during
// provider start-up, before controllers begin reconciling.
func SetRoleResolverFactory(f func(client.Client) RoleUUIDResolver) {
	roleResolverFactory = f
}

// roleImportInitializer seeds status.atProvider.id before the first Terraform
// observe cycle so roles that already exist in ClickHouse (e.g. after a backup
// restore) are adopted instead of re-created.
//
// The clickhousedbops Terraform provider reads roles with WHERE id = <uuid>. On a
// fresh managed resource upjet has no id, so without help the provider observes
// "not found" and runs CREATE ROLE. After a restore the role already exists, and
// CREATE ROLE (unlike GRANT) is not idempotent, so it fails with
// "already exists in `replicated`". The resolver looks the role up by name first:
//   - found        -> seed the real UUID; the TF Read finds it and upjet imports it.
//   - not found    -> seed sentinelUUID; the provider creates the role as before.
//   - lookup error -> return the error so the reconcile retries instead of forcing
//     a conflicting create.
//
// When no resolver is wired (code generation / tests) it seeds the sentinel,
// matching the previous behavior.
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
				// Real UUID already set (post-import or post-creation) - leave it alone.
				return nil
			}

			if obs == nil {
				obs = make(map[string]any)
			}

			if roleResolverFactory == nil {
				// No resolver wired (generation/tests): keep force-create behavior.
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
