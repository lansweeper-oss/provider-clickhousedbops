package clients

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/lansweeper-oss/provider-clickhousedbops/config"
)

// Resolvers live here, not in config, so the ClickHouse client and apis packages stay out
// of the code generator's import graph.

// NewRoleUUIDResolver resolves a role's UUID by name from system.roles.
func NewRoleUUIDResolver(kube client.Client) config.UUIDResolver {
	return newUUIDResolver(kube, "system.roles", "id")
}

// NewDatabaseUUIDResolver resolves a database's UUID by name from system.databases.
func NewDatabaseUUIDResolver(kube client.Client) config.UUIDResolver {
	return newUUIDResolver(kube, "system.databases", "uuid")
}

// NewUserUUIDResolver resolves a user's UUID by name from system.users.
func NewUserUUIDResolver(kube client.Client) config.UUIDResolver {
	return newUUIDResolver(kube, "system.users", "id")
}

// NewSettingsProfileUUIDResolver resolves a settings profile's UUID by name from system.settings_profiles.
func NewSettingsProfileUUIDResolver(kube client.Client) config.UUIDResolver {
	return newUUIDResolver(kube, "system.settings_profiles", "id")
}

// newUUIDResolver builds a UUIDResolver that looks up idField by the resource's
// spec.forProvider.name in the given system table. table and idField are
// package-internal constants (never user input). When spec.forProvider.clusterName
// is set the lookup runs across all replicas, mirroring the provider's WithCluster.
func newUUIDResolver(kube client.Client, table, idField string) config.UUIDResolver {
	return func(ctx context.Context, mg xpresource.Managed) (string, bool, error) {
		paved, err := fieldpath.PaveObject(mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot pave managed resource: %w", err)
		}
		name, err := paved.GetString("spec.forProvider.name")
		if err != nil {
			return "", false, fmt.Errorf("cannot read spec.forProvider.name: %w", err)
		}
		// Optional - absent on single node / ClickHouse Cloud.
		cluster, _ := paved.GetString("spec.forProvider.clusterName")

		params, err := ResolveConnParams(ctx, kube, mg)
		if err != nil {
			return "", false, fmt.Errorf("cannot resolve connection params: %w", err)
		}

		return findUUIDByName(ctx, params, table, idField, name, cluster)
	}
}

// findUUIDByName runs SELECT toString(<idField>) FROM <from> WHERE name = ?, where
// <from> is the plain table or cluster('<cluster>', <table>) when cluster is set.
// name is bound as a query parameter; cluster is single-quote escaped because it is
// a table-function identifier and cannot be bound.
func findUUIDByName(ctx context.Context, params ConnParams, table, idField, name, cluster string) (string, bool, error) {
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

	from := table
	if cluster != "" {
		from = fmt.Sprintf("cluster('%s', %s)", strings.ReplaceAll(cluster, "'", "\\'"), table)
	}
	query := fmt.Sprintf("SELECT toString(%s) AS id FROM %s WHERE name = ?", idField, from)

	rows, err := conn.Query(ctx, query, name)
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
