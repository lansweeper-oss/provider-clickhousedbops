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

// NewRoleUUIDResolver returns a config.RoleUUIDResolver that looks a role up by
// name in ClickHouse, using the connection parameters from the ProviderConfig
// referenced by the managed resource. It lives here (not in config) so the
// ClickHouse client and the apis packages stay out of the code generator's
// import graph.
func NewRoleUUIDResolver(kube client.Client) config.RoleUUIDResolver {
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

		return findRoleUUIDByName(ctx, params, name)
	}
}

// findRoleUUIDByName opens a direct ClickHouse connection and returns the UUID of
// the role with the given name, or found=false when no such role exists.
func findRoleUUIDByName(ctx context.Context, params ConnParams, name string) (string, bool, error) {
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
	defer func() { _ = conn.Close() }()

	rows, err := conn.Query(ctx, "SELECT toString(id) AS id FROM system.roles WHERE name = ?", name)
	if err != nil {
		return "", false, fmt.Errorf("error querying system.roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
