package clients

import (
	"context"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// offlineProvider never configures a ClickHouse client. Every resource of the
// ClickHouse Terraform provider skips its server-side plan checks (replicated
// storage, capability flags) when it has no client, so plans are computed
// without credentials and without network access.
type offlineProvider struct {
	fwprovider.Provider
}

func (offlineProvider) Configure(context.Context, fwprovider.ConfigureRequest, *fwprovider.ConfigureResponse) {
}

// OfflineTerraformSetupBuilder returns the terraform.SetupFn used by the diff
// server. It ignores the ProviderConfig entirely: no credentials are read and
// no connection to ClickHouse is opened.
func OfflineTerraformSetupBuilder(frameworkProvider fwprovider.Provider) terraform.SetupFn {
	return func(context.Context, client.Client, resource.Managed) (terraform.Setup, error) {
		return terraform.Setup{
			FrameworkProvider: offlineProvider{Provider: frameworkProvider},
			Configuration:     map[string]any{},
		}, nil
	}
}
