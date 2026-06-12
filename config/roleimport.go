package config

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/lansweeper-oss/provider-clickhousedbops/internal/clients"
)

// roleUUIDResolver resolves the real ClickHouse UUID of a role by its name.
// found is false when no role with that name exists yet.
type roleUUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

// roleImportInitializer seeds status.atProvider.id before the first Terraform
// observe cycle so that roles which already exist in ClickHouse (e.g. after a
// backup restore) are adopted instead of re-created.
//
// The clickhousedbops Terraform provider reads roles with WHERE id = <uuid>.
// On a fresh managed resource upjet has no id, so without help the provider
// observes "not found" and runs CREATE ROLE. After a backup restore the role
// already exists, and CREATE ROLE fails with "already exists in `replicated`"
// (unlike GRANT, CREATE ROLE is not idempotent). We avoid that by looking up the
// role's real UUID by name first:
//   - found     -> seed the real UUID; the TF Read finds the role and upjet
//     imports it without issuing CREATE ROLE.
//   - not found -> seed sentinelUUID, which matches no row, so the provider maps
//     it to "not found" and creates the role as before.
//
// On a lookup error we return the error so the reconcile retries, rather than
// falling back to a CREATE that would conflict with an existing role.
func roleImportInitializer(mkResolver func(client.Client) roleUUIDResolver) config.NewInitializerFn {
	return func(kube client.Client) managed.Initializer {
		resolve := mkResolver(kube)
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

			uuid, found, err := resolve(ctx, mg)
			if err != nil {
				return fmt.Errorf("cannot resolve role UUID for import: %w", err)
			}

			if obs == nil {
				obs = make(map[string]any)
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

// newRoleUUIDResolver is the production resolver: it reads the role name from the
// managed resource, resolves ClickHouse connection parameters from the referenced
// ProviderConfig, and queries system.roles by name.
func newRoleUUIDResolver(kube client.Client) roleUUIDResolver {
	return func(ctx context.Context, mg xpresource.Managed) (string, bool, error) {
		paved, err := fieldpath.PaveObject(mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot pave managed resource: %w", err)
		}
		name, err := paved.GetString("spec.forProvider.name")
		if err != nil {
			return "", false, fmt.Errorf("cannot read spec.forProvider.name: %w", err)
		}

		params, err := clients.ResolveConnParams(ctx, kube, mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot resolve connection params: %w", err)
		}

		return findRoleUUIDByName(ctx, params, name)
	}
}

// findRoleUUIDByName opens a direct ClickHouse connection and returns the UUID of
// the role with the given name, or found=false when no such role exists.
func findRoleUUIDByName(ctx context.Context, params clients.ConnParams, name string) (string, bool, error) {
	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", params.Host, params.Port)},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: params.Username,
			Password: params.Password,
		},
	}
	if params.Protocol == "nativesecure" {
		// Default verification, matching the provider's nativesecure connection.
		opts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return "", false, fmt.Errorf("cannot open clickhouse connection: %w", err)
	}
	defer conn.Close()

	rows, err := conn.Query(ctx, "SELECT toString(id) AS id FROM system.roles WHERE name = ?", name)
	if err != nil {
		return "", false, fmt.Errorf("error querying system.roles: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", false, fmt.Errorf("error iterating system.roles: %w", err)
		}
		// No role with that name.
		return "", false, nil
	}

	var uuid string
	if err := rows.Scan(&uuid); err != nil {
		return "", false, fmt.Errorf("error scanning role id: %w", err)
	}
	if uuid == "" {
		return "", false, nil
	}
	return uuid, true, nil
}
