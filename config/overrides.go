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

// sentinelUUIDInitializer sets a synthetic sentinelUUID before the first Terraform observe cycle,
// leaving it untouched once the provider has written a real UUID to it.
// This is needed for resources where the TF provider uses a UUID field for read lookups:
// an empty or name-based value causes ClickHouse to return a UUID parse error (code 376) instead of zero rows.
// Seeding a syntactically valid but non-existent UUID causes ClickHouse to return zero rows, which
// the provider maps to "not found", triggering resource creation.
func sentinelUUIDInitializer(field string) config.NewInitializerFn {
	return func(_ client.Client) managed.Initializer {
		return managed.InitializerFn(func(_ context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for %s initializer: %w", field, err)
			}
			if val, _ := obs[field].(string); val != "" && val != sentinelUUID {
				// Real UUID already set (post-creation) — leave it alone.
				return nil
			}
			if obs == nil {
				obs = make(map[string]any)
			}
			obs[field] = sentinelUUID
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
		// When reconciling a clickhousedbops_database resource, the provider calls a Read operation
		// to check if the resource exists by looking up the database by uuid.
		// But it reads that uuid from the previous Terraform state, not from the resource name.
		// Before that, the sentinelUUIDInitializer fakes the UUID (_sentinel_) into status.atProvider.uuid.
		// When upjet builds the Terraform state file from that observation, the provider now has a
		// valid UUID to send to ClickHouse.
		// ClickHouse finds no rows with that fake UUID and returns zero rows, which the provider correctly
		// maps to "not found", triggering resource creation.
		r.InitializerFns = append(r.InitializerFns, sentinelUUIDInitializer("uuid"))
		r.UseAsync = true
	})
	p.AddResourceConfigurator("clickhousedbops_user", func(r *config.Resource) {
		// Removing "id" from the schema keeps hasTFID=false, so EnsureTFState falls
		// back to status.atProvider.id (seeded with sentinelUUID). ClickHouse finds
		// no rows for the fake UUID and returns zero rows, which the provider maps to
		// "not found", triggering creation. After creation, atProvider holds the real
		// UUID and the initializer leaves it untouched on subsequent reconciles.
		// The "id" field still appears in status.atProvider because the provider
		// populates it from its own TF state output after a successful read.
		delete(r.TerraformResource.Schema, "id")
		desc, _ := comments.New("If true, a password is auto-generated and stored in"+
			" the secret referenced by writeConnectionSecretToRef under keys"+
			" 'password' (plaintext) and 'hash' (SHA256). The passwordSha256HashSecretRef"+
			" field is set automatically — no other password fields need to be configured.",
			comments.WithTFTag("-"))
		r.TerraformResource.Schema["auto_generate_password"] = &tfschema.Schema{
			Type:        tfschema.TypeBool,
			Optional:    true,
			Description: desc.String(),
		}
		r.InitializerFns = append(r.InitializerFns,
			sentinelUUIDInitializer("id"),
			PasswordGenerator("spec.forProvider.autoGeneratePassword"))
		// Remove write-only fields that require Terraform >=1.11.
		// This provider targets Terraform <=1.5; users must use
		// passwordSha256HashSecretRef (or autoGeneratePassword) instead.
		delete(r.TerraformResource.Schema, "password_sha256_hash_wo")
		delete(r.TerraformResource.Schema, "password_sha256_hash_wo_version")
	})
	p.AddResourceConfigurator("clickhousedbops_settings_profile", func(r *config.Resource) {
		// Same hasTFID=false trick as for clickhousedbops_user — prevents name-based
		// id from being written to TF state on first reconcile, avoiding UUID parse errors.
		delete(r.TerraformResource.Schema, "id")
		r.InitializerFns = append(r.InitializerFns, sentinelUUIDInitializer("id"))
	})
	p.AddResourceConfigurator("clickhousedbops_role", func(r *config.Resource) {
		// Same hasTFID=false trick — role lookup also uses UUID-based WHERE id=UUID(...).
		delete(r.TerraformResource.Schema, "id")
		r.InitializerFns = append(r.InitializerFns, sentinelUUIDInitializer("id"))
	})
}
