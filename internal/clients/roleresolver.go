package clients

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/lansweeper-oss/provider-clickhousedbops/config"
)

// Lives here, not in config, so the ClickHouse client and apis packages stay out of the
// code generator's import graph.

// NewRoleUUIDResolver resolves an existing role's UUID by name from system.roles.
func NewRoleUUIDResolver(kube client.Client) config.UUIDResolver {
	return newUUIDResolverFromTable(kube, "system.roles")
}

// NewUserUUIDResolver resolves an existing user's UUID by name from system.users.
func NewUserUUIDResolver(kube client.Client) config.UUIDResolver {
	return newUUIDResolverFromTable(kube, "system.users")
}

// NewSettingsProfileUUIDResolver resolves an existing settings profile's UUID by
// name from system.settings_profiles.
func NewSettingsProfileUUIDResolver(kube client.Client) config.UUIDResolver {
	return newUUIDResolverFromTable(kube, "system.settings_profiles")
}

// newUUIDResolverFromTable builds a resolver that looks up the UUID by name in
// the given ClickHouse system table. table is a fixed internal constant (never
// user input), so interpolating it into the query is safe.
func newUUIDResolverFromTable(kube client.Client, table string) config.UUIDResolver {
	return func(ctx context.Context, mg xpresource.Managed) (string, bool, error) {
		paved, err := fieldpath.PaveObject(mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot pave managed resource: %w", err)
		}
		name, err := paved.GetString("spec.forProvider.name")
		if err != nil {
			return "", false, fmt.Errorf("cannot read spec.forProvider.name: %w", err)
		}

		params, err := ResolveConnParams(ctx, kube, mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot resolve connection params: %w", err)
		}

		return findUUIDByName(ctx, params, table, name)
	}
}

func findUUIDByName(ctx context.Context, params ConnParams, table, name string) (string, bool, error) {
	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", params.Host, params.Port)},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: params.Username,
			Password: params.Password,
		},
	}
	if params.Protocol == "nativesecure" {
		opts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return "", false, fmt.Errorf("cannot open clickhouse connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	rows, err := conn.Query(ctx, fmt.Sprintf("SELECT toString(id) AS id FROM %s WHERE name = ?", table), name)
	if err != nil {
		return "", false, fmt.Errorf("error querying %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", false, fmt.Errorf("error iterating %s: %w", table, err)
		}
		return "", false, nil
	}

	var uuid string
	if err := rows.Scan(&uuid); err != nil {
		return "", false, fmt.Errorf("error scanning id from %s: %w", table, err)
	}
	if uuid == "" {
		return "", false, nil
	}
	return uuid, true, nil
}
