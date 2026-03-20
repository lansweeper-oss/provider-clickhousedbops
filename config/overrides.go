package config

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/types/comments"
	tfschema "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// terraformedObservation is the subset of upjet's resource.Terraformed that we
// need to read and write the Terraform observation (status.atProvider).
type terraformedObservation interface {
	GetObservation() (map[string]any, error)
	SetObservation(map[string]any) error
}

// DatabaseUUIDInitializer seeds the "uuid" field in status.atProvider with the
// nil-UUID sentinel before the first Terraform observe cycle.
//
// Background: clickhousedbops_database is a terraform-plugin-framework resource
// whose Read function looks up the database via "WHERE uuid = ?" using the
// "uuid" attribute from the prior Terraform state (not from the TF resource
// "id"). For a brand-new resource the Terraform state is empty so uuid is "",
// which causes ClickHouse to return a UUID parse error rather than zero rows.
//
// upjet's EnsureTFState merges status.atProvider into the Terraform state file
// (terraform.tfstate) without touching the Terraform config (main.tf.json), so
// the sentinel never triggers the framework's "read-only attribute" validation.
// Once ClickHouse receives the nil UUID it returns zero rows, the provider
// signals "not found", and upjet proceeds with resource creation. After
// creation upjet writes the real UUID back to status.atProvider, so subsequent
// observe cycles use the actual UUID.
func DatabaseUUIDInitializer() config.NewInitializerFn {
	return func(_ client.Client) managed.Initializer {
		return managed.InitializerFn(func(_ context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for database UUID initializer: %w", err)
			}
			if uuidVal, _ := obs["uuid"].(string); uuidVal != "" {
				// UUID already set (post-creation or user-provided) — leave it alone.
				return nil
			}
			if obs == nil {
				obs = make(map[string]any)
			}
			obs["uuid"] = sentinelUUID
			return tr.SetObservation(obs)
		})
	}
}

var gkvOverrideMap = map[string]schema.GroupVersionKind{
	"clickhousedbops_grant_privilege": {
		Group: "",
		Kind:  "GrantPrivilege",
	},
	"clickhousedbops_grant_role": {
		Group: "",
		Kind:  "GrantRole",
	},
	"clickhousedbops_settings_profile": {
		Group: "",
		Kind:  "SettingProfile",
	},
	"clickhousedbops_settings_profile_association": {
		Group: "",
		Kind:  "SettingProfileAssociation",
	},
}

func gvkOverride() config.ResourceOption {
	return func(r *config.Resource) {
		if r.ShortGroup == resourcePrefix {
			r.ShortGroup = ""
		}
		if gvk, ok := gkvOverrideMap[r.Name]; ok {
			r.ShortGroup = gvk.Group
			r.Kind = gvk.Kind
			if gvk.Version != "" {
				r.Version = gvk.Version
			}
		}
	}
}

func Configure(p *config.Provider) {
	p.AddResourceConfigurator("clickhousedbops_database", func(r *config.Resource) {
		// The database TF provider reads "uuid" from the prior TF state (not
		// from the resource id) when observing. An empty uuid causes ClickHouse
		// to return a UUID parse error instead of "not found".
		//
		// DatabaseUUIDInitializer seeds uuid=sentinelUUID in status.atProvider
		// before the first observe. upjet's EnsureTFState merges observation
		// into terraform.tfstate (not main.tf.json), so ClickHouse receives a
		// valid UUID, returns 0 rows, and the provider signals "not found".
		// After creation, atProvider holds the real UUID and the initializer
		// leaves it untouched on subsequent reconciles.
		//
		// The provider does not set "id" in its framework state, so hasTFID
		// remains false. EnsureTFState uses the len(attrs)==0 empty check: it
		// writes the state only when no state file exists (fresh resource), and
		// skips once the provider has populated it with the real UUID.
		r.InitializerFns = append(r.InitializerFns, DatabaseUUIDInitializer())
		r.UseAsync = true
	})
	p.AddResourceConfigurator("clickhousedbops_user", func(r *config.Resource) {
		desc, _ := comments.New("If true, the password will be auto-generated and"+
			" stored in the Secret referenced by the passwordSecretRef field.",
			comments.WithTFTag("-"))
		r.TerraformResource.Schema["auto_generate_password"] = &tfschema.Schema{
			Type:        tfschema.TypeBool,
			Optional:    true,
			Description: desc.String(),
		}
		r.InitializerFns = append(r.InitializerFns,
			PasswordGenerator(
				"spec.forProvider.passwordSha256HashSecretRef",
				"spec.forProvider.autoGeneratePassword",
			))
		r.TerraformResource.Schema["password_sha256_hash_wo"].Description = "SHA256 hash of the password to authenticate the user." +
			" If you set autoGeneratePassword to true, the Secret referenced here will be" +
			" created or updated with the generated password if it does not already contain one."
	})
}
