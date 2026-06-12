package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/terraform"

	clusterv1beta1 "github.com/lansweeper-oss/provider-clickhousedbops/apis/cluster/v1beta1"
	namespacedv1beta1 "github.com/lansweeper-oss/provider-clickhousedbops/apis/namespaced/v1beta1"
)

const (
	// error messages
	errNoProviderConfig     = "no providerConfigRef provided"
	errGetProviderConfig    = "cannot get referenced ProviderConfig"
	errTrackUsage           = "cannot track ProviderConfig usage"
	errExtractCredentials   = "cannot extract credentials"
	errUnmarshalCredentials = "cannot unmarshal clickhousedbops credentials as JSON"
)

// ConnParams holds the ClickHouse connection parameters extracted from a
// ProviderConfig's credentials secret. It mirrors the subset of the Terraform
// provider configuration needed to open a direct connection (e.g. to look up an
// existing resource by name before the Terraform observe cycle).
type ConnParams struct {
	Host     string
	Port     uint16
	Protocol string
	Username string
	Password string
}

// ResolveConnParams resolves the ClickHouse connection parameters for the
// ProviderConfig referenced by mg, using the same credential extraction as
// TerraformSetupBuilder. It is used by initializers that must talk to ClickHouse
// directly, since the controller's Terraform client is not available during the
// Initialize phase.
func ResolveConnParams(ctx context.Context, crClient client.Client, mg resource.Managed) (ConnParams, error) {
	pcSpec, err := resolveProviderConfig(ctx, crClient, mg)
	if err != nil {
		return ConnParams{}, fmt.Errorf("cannot resolve provider config: %w", err)
	}

	data, err := resource.CommonCredentialExtractor(ctx, pcSpec.Credentials.Source, crClient, pcSpec.Credentials.CommonCredentialSelectors)
	if err != nil {
		return ConnParams{}, fmt.Errorf(errExtractCredentials+": %w", err)
	}
	creds := map[string]any{}
	if err := json.Unmarshal(data, &creds); err != nil {
		return ConnParams{}, fmt.Errorf(errUnmarshalCredentials+": %w", err)
	}

	p := ConnParams{}
	p.Host, _ = creds["host"].(string)
	p.Protocol, _ = creds["protocol"].(string)
	p.Port = parsePort(creds["port"])
	if ac, ok := creds["auth_config"].(map[string]any); ok {
		p.Username, _ = ac["username"].(string)
		p.Password, _ = ac["password"].(string)
	}

	if p.Host == "" || p.Port == 0 || p.Username == "" {
		return ConnParams{}, errors.New("incomplete clickhouse connection parameters in credentials")
	}
	return p, nil
}

// parsePort tolerates the port being encoded as a JSON number, json.Number or
// string in the credentials secret.
func parsePort(v any) uint16 {
	switch t := v.(type) {
	case float64:
		return uint16(t)
	case json.Number:
		n, _ := t.Int64()
		return uint16(n)
	case string:
		n, _ := strconv.Atoi(t)
		return uint16(n)
	default:
		return 0
	}
}

// TerraformSetupBuilder builds Terraform a terraform.SetupFn function which
// returns Terraform provider setup configuration
func TerraformSetupBuilder(version, providerSource, providerVersion string) terraform.SetupFn {
	return func(ctx context.Context, client client.Client, mg resource.Managed) (terraform.Setup, error) {
		ps := terraform.Setup{
			Version: version,
			Requirement: terraform.ProviderRequirement{
				Source:  providerSource,
				Version: providerVersion,
			},
		}

		pcSpec, err := resolveProviderConfig(ctx, client, mg)
		if err != nil {
			return terraform.Setup{}, fmt.Errorf("cannot resolve provider config: %w", err)
		}

		data, err := resource.CommonCredentialExtractor(ctx, pcSpec.Credentials.Source, client, pcSpec.Credentials.CommonCredentialSelectors)
		if err != nil {
			return terraform.Setup{}, fmt.Errorf(errExtractCredentials+": %w", err)
		}
		creds := map[string]any{}
		if err := json.Unmarshal(data, &creds); err != nil {
			return terraform.Setup{}, fmt.Errorf(errUnmarshalCredentials+": %w", err)
		}

		// Set credentials in Terraform provider configuration.
		ps.Configuration = map[string]any{
			"host":        creds["host"],
			"port":        creds["port"],
			"protocol":    creds["protocol"],
			"auth_config": creds["auth_config"],
		}
		return ps, nil
	}
}

func toSharedPCSpec(pc *clusterv1beta1.ProviderConfig) (*namespacedv1beta1.ProviderConfigSpec, error) {
	if pc == nil {
		return nil, nil
	}
	data, err := json.Marshal(pc.Spec)
	if err != nil {
		return nil, err
	}

	var mSpec namespacedv1beta1.ProviderConfigSpec
	err = json.Unmarshal(data, &mSpec)
	return &mSpec, err
}

func resolveProviderConfig(ctx context.Context, crClient client.Client, mg resource.Managed) (*namespacedv1beta1.ProviderConfigSpec, error) {
	switch managed := mg.(type) {
	case resource.LegacyManaged: //nolint:staticcheck
		return resolveLegacy(ctx, crClient, managed)
	case resource.ModernManaged:
		return resolveModern(ctx, crClient, managed)
	default:
		return nil, errors.New("resource is not a managed resource")
	}
}

func resolveLegacy(ctx context.Context, client client.Client, mg resource.LegacyManaged) (*namespacedv1beta1.ProviderConfigSpec, error) { //nolint:staticcheck
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return nil, errors.New(errNoProviderConfig)
	}
	pc := &clusterv1beta1.ProviderConfig{}
	if err := client.Get(ctx, types.NamespacedName{Name: configRef.Name}, pc); err != nil {
		return nil, fmt.Errorf(errGetProviderConfig+": %w", err)
	}

	t := resource.NewLegacyProviderConfigUsageTracker(client, &clusterv1beta1.ProviderConfigUsage{})
	if err := t.Track(ctx, mg); err != nil {
		return nil, fmt.Errorf(errTrackUsage+": %w", err)
	}

	return toSharedPCSpec(pc)
}

func resolveModern(ctx context.Context, crClient client.Client, mg resource.ModernManaged) (*namespacedv1beta1.ProviderConfigSpec, error) {
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return nil, errors.New(errNoProviderConfig)
	}

	pcRuntimeObj, err := crClient.Scheme().New(namespacedv1beta1.SchemeGroupVersion.WithKind(configRef.Kind))
	if err != nil {
		return nil, fmt.Errorf("unknown GVK for ProviderConfig"+": %w", err)
	}
	pcObj, ok := pcRuntimeObj.(client.Object)
	if !ok {
		// This indicates a programming error, types are not properly generated
		return nil, fmt.Errorf("provider config type %T is not a client.Object; this indicates a code generation issue", pcRuntimeObj)
	}

	// Namespace will be ignored if the PC is a cluster-scoped type
	if err := crClient.Get(ctx, types.NamespacedName{Name: configRef.Name, Namespace: mg.GetNamespace()}, pcObj); err != nil {
		return nil, fmt.Errorf(errGetProviderConfig+": %w", err)
	}

	var pcSpec namespacedv1beta1.ProviderConfigSpec
	pcu := &namespacedv1beta1.ProviderConfigUsage{}
	switch pc := pcObj.(type) {
	case *namespacedv1beta1.ProviderConfig:
		pcSpec = pc.Spec
		if pcSpec.Credentials.SecretRef != nil {
			pcSpec.Credentials.SecretRef.Namespace = mg.GetNamespace()
		}
	case *namespacedv1beta1.ClusterProviderConfig:
		pcSpec = pc.Spec
	default:
		return nil, errors.New("unknown provider config type")
	}
	t := resource.NewProviderConfigUsageTracker(crClient, pcu)
	if err := t.Track(ctx, mg); err != nil {
		return nil, fmt.Errorf(errTrackUsage+": %w", err)
	}
	return &pcSpec, nil
}
