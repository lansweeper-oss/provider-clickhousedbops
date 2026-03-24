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

// NamedResourceIDInitializer seeds the "id" field in status.atProvider with
// sentinelUUID before the first Terraform observe cycle.
//
// Background: resources like clickhousedbops_user and clickhousedbops_settings_profile
// have a UUID "id" attribute that the provider uses for Read lookups. Without this
// initializer, upjet seeds the TF state with id=<resource-name> (e.g. "testuser"),
// and the provider tries to execute WHERE id = UUID('testuser'), which causes a
// ClickHouse parse error instead of returning "not found".
//
// Seeding id=sentinelUUID (a valid UUID format) causes the provider to return zero
// rows, which upjet interprets as "not found" and proceeds with resource creation.
// After creation the provider writes the real UUID to state; the initializer then
// leaves it untouched on subsequent reconciles.
func NamedResourceIDInitializer() config.NewInitializerFn {
	return func(_ client.Client) managed.Initializer {
		return managed.InitializerFn(func(_ context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for ID initializer: %w", err)
			}
			if idVal, _ := obs["id"].(string); idVal != "" && idVal != sentinelUUID {
				// Real UUID already set (post-creation) — leave it alone.
				return nil
			}
			if obs == nil {
				obs = make(map[string]any)
			}
			obs["id"] = sentinelUUID
			return tr.SetObservation(obs)
		})
	}
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
		// Remove "id" from the TF schema so that upjet's EnsureTFState does not
		// force id=<resource-name> into the TF state on the first reconcile.
		// When hasTFID=false, EnsureTFState uses status.atProvider.id (seeded
		// with sentinelUUID by NamedResourceIDInitializer) instead. This prevents
		// ClickHouse from receiving WHERE id=UUID("testuser") which causes a parse
		// error (code 376). The "id" field still appears in status.atProvider after
		// creation because it is populated from the provider's TF state output.
		delete(r.TerraformResource.Schema, "id")
		desc, _ := comments.New("If true, the password will be auto-generated and"+
			" stored in the Secret referenced by the passwordSecretRef field.",
			comments.WithTFTag("-"))
		r.TerraformResource.Schema["auto_generate_password"] = &tfschema.Schema{
			Type:        tfschema.TypeBool,
			Optional:    true,
			Description: desc.String(),
		}
		r.InitializerFns = append(r.InitializerFns,
			NamedResourceIDInitializer(),
			PasswordGenerator(
				"spec.forProvider.passwordSha256HashSecretRef",
				"spec.forProvider.autoGeneratePassword",
			))
		r.TerraformResource.Schema["password_sha256_hash_wo"].Description = "SHA256 hash of the password to authenticate the user." +
			" If you set autoGeneratePassword to true, the Secret referenced here will be" +
			" created or updated with the generated password if it does not already contain one."
	})
	p.AddResourceConfigurator("clickhousedbops_settings_profile", func(r *config.Resource) {
		// Same hasTFID=false trick as for clickhousedbops_user — prevents name-based
		// id from being written to TF state on first reconcile, avoiding UUID parse errors.
		delete(r.TerraformResource.Schema, "id")
		r.InitializerFns = append(r.InitializerFns, NamedResourceIDInitializer())
	})
	p.AddResourceConfigurator("clickhousedbops_role", func(r *config.Resource) {
		// Same hasTFID=false trick — role lookup also uses UUID-based WHERE id=UUID(...).
		delete(r.TerraformResource.Schema, "id")
		r.InitializerFns = append(r.InitializerFns, NamedResourceIDInitializer())
	})
}
