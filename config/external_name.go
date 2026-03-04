package config

import (
	"context"
	"errors"
	"strings"

	"github.com/crossplane/upjet/v2/pkg/config"
)

const (
	sep = ":"
)

// ExternalNameConfigs contains all external name configurations for this
// provider.
var ExternalNameConfigs = map[string]config.ExternalName{
	"clickhousedbops_database":                    idWithclusterName(),
	"clickhousedbops_grant_privilege":             idWithStub(), // cannot be imported
	"clickhousedbops_grant_role":                  idWithStub(), // cannot be imported
	"clickhousedbops_role":                        idWithclusterName(),
	"clickhousedbops_setting":                     idWithStub(), // cannot be imported
	"clickhousedbops_settings_profile":             idWithclusterName(),
	"clickhousedbops_settings_profile_association": idWithStub(), // cannot be imported
	"clickhousedbops_user":                        idWithclusterName(),
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

func idWithclusterName() config.ExternalName {
	e := config.IdentifierFromProvider
	e.GetIDFn = IDFromClusterName(sep)
	e.GetExternalNameFn = ExternalNameFromClusterName(sep)
	return e
}

func idWithStub() config.ExternalName {
	e := config.IdentifierFromProvider
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
		name := parameters["name"].(string)
		cluster, ok := parameters["cluster_name"]
		if ok {
			return cluster.(string) + sep + name, nil
		}
		return name, nil
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
