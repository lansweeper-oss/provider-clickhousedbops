package config

import (
	"context"
	"errors"
	"strings"

	"github.com/crossplane/upjet/v2/pkg/config"
)

const (
	// sentinelUUID is used as a placeholder Terraform ID for resources that have not yet been
	// created. It is a valid UUID format so ClickHouse can parse it without error. It must NOT
	// match any ClickHouse system database UUID, in particular the nil UUID
	// (00000000-0000-0000-0000-000000000000) is reserved for information_schema and would cause
	// the provider to return that database instead of "not found".
	// This is not a valid random UUID (version 4) so ClickHouse would never assign it to a real
	// database.
	sentinelUUID = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	sep          = ":"
)

// ExternalNameConfigs contains all external name configurations for this
// provider.
var ExternalNameConfigs = map[string]config.ExternalName{
	"clickhousedbops_database":                     idWithClusterNameDatabase(),
	"clickhousedbops_grant_privilege":              idWithStub(), // cannot be imported
	"clickhousedbops_grant_role":                   idWithStub(), // cannot be imported
	"clickhousedbops_role":                         idWithClusterName(),
	"clickhousedbops_setting":                      idWithStub(), // cannot be imported
	"clickhousedbops_settings_profile":             idWithClusterName(),
	"clickhousedbops_settings_profile_association": idWithStub(), // cannot be imported
	"clickhousedbops_user":                         idWithClusterName(),
}

// ExternalNameConfigured returns the list of possible external name
// configurations for this provider.
func ExternalNameConfigured() []string {
	l := make([]string, len(ExternalNameConfigs))
	i := 0
	for name := range ExternalNameConfigs {
		l[i] = name
		i++
	}
	return l
}

// ExternalNameConfigurations applies all external name configurations for each
// group resource separately.
func ExternalNameConfigurations() config.ResourceOption {
	return func(r *config.Resource) {
		if e, ok := ExternalNameConfigs[r.Name]; ok {
			r.ExternalName = e
		}
	}
}

func idWithClusterName() config.ExternalName {
	e := config.IdentifierFromProvider
	// Role/user/settings_profile are identified by a provider-assigned UUID
	// (tfstate["id"]). GetIDFn and GetExternalNameFn MUST agree on that UUID on
	// every observe, otherwise the external-name flaps between the UUID and the
	// role name and the resource never reaches Available.
	//
	// We deliberately do NOT fall back to parameters["name"] here. A name-based
	// fallback makes upjet run `terraform import <addr> <name>` (these resources
	// are importable by name), which writes id=<name> into tfstate; a later
	// refresh canonicalises id back to the UUID, so the external-name computed by
	// ExternalNameFromClusterName alternates name<->UUID across reconciles.
	// Mirroring the database path, we fall back to sentinelUUID instead: it
	// matches no real row, so the provider reports "not found" pre-create
	// (triggering creation), while restore-adopt still works because the
	// initializer seeds the real UUID into status.atProvider.id.
	e.GetIDFn = IDFromClusterName(sep)
	e.GetExternalNameFn = ExternalNameFromClusterName(sep)
	return e
}

// idWithClusterNameDatabase uses the "uuid" field from tfstate as external name.
func idWithClusterNameDatabase() config.ExternalName {
	e := config.IdentifierFromProvider
	e.GetIDFn = IDFromClusterName(sep)
	e.GetExternalNameFn = func(tfstate map[string]any) (string, error) {
		if uuidVal, ok := tfstate["uuid"].(string); ok && uuidVal != "" {
			// Strip the cluster name prefix if present (same as ExternalNameFromClusterName).
			if strings.Contains(uuidVal, sep) {
				return strings.Split(uuidVal, sep)[1], nil
			}
			return uuidVal, nil
		}
		// Fall back to the id-based extraction for safety.
		return ExternalNameFromClusterName(sep)(tfstate)
	}
	return e
}

// idWithStub extends config.IdentifierFromProvider with a custom GetIDFn for resources that use a
// provider-assigned composite key and cannot be imported.
// The composite key always contains ":" (e.g. "SELECT:testdb::testuser").
// Before creation, externalName is the plain K8s resource name which never contains ":".
// Returning "" in that case signals to upjet that there is no existing resource to look up, so it proceeds directly to creation.
func idWithStub() config.ExternalName {
	e := config.IdentifierFromProvider
	e.GetIDFn = func(_ context.Context, externalName string, _ map[string]any, _ map[string]any) (string, error) {
		if strings.Contains(externalName, sep) {
			return externalName, nil
		}
		return "", nil
	}
	// Return "" instead of an error when id is absent from tfstate. This
	// happens when terraform refresh signals "not found" and Terraform removes
	// the resource from state, leaving no id key in the attributes map.
	e.GetExternalNameFn = func(tfstate map[string]any) (string, error) {
		en, _ := config.IDAsExternalName(tfstate)
		return en, nil
	}
	return e
}

func ExtractIDFromState(tfstate map[string]any) (string, error) {
	id, ok := tfstate["id"]
	if !ok {
		return "", errors.New("id attribute missing from state file")
	}
	idStr, ok := id.(string)
	if !ok {
		return "", errors.New("value of id needs to be string")
	}
	return idStr, nil
}

func IDFromClusterName(sep string) func(context.Context, string, map[string]any, map[string]any) (string, error) {
	return func(_ context.Context, externalName string, parameters map[string]any, _ map[string]any) (string, error) {
		nameVal := parameters["name"]
		name, _ := nameVal.(string)
		// Before creation, externalName equals the K8s resource name (crossplane default) or is empty.
		// In either case the provider has not yet assigned a real UUID.
		// Use sentinelUUID so ClickHouse receives a syntactically valid UUID that matches no row,
		// causing the provider to signal "not found" and allowing upjet to proceed with creation.
		id := externalName
		if id == "" || id == name {
			id = sentinelUUID
		}
		if clusterVal, ok := parameters["cluster_name"]; ok {
			cluster, ok := clusterVal.(string)
			if ok && cluster != "" {
				return cluster + sep + id, nil
			}
		}
		return id, nil
	}
}

func ExternalNameFromClusterName(sep string) func(tfstate map[string]any) (string, error) {
	return func(tfstate map[string]any) (string, error) {
		idStr, err := ExtractIDFromState(tfstate)
		if err != nil {
			return "", err
		}
		if strings.Contains(idStr, sep) {
			return strings.Split(idStr, sep)[1], nil
		}
		return idStr, nil
	}
}
