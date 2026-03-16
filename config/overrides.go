package config

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/types/comments"
	tfschema "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

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
				"spec.forProvider.passwordSecretRef",
				"spec.forProvider.autoGeneratePassword",
			))
		r.TerraformResource.Schema["password_sha256_hash_wo"].Description = "SHA256 hash of the password to authenticate the user." +
			" If you set autoGeneratePassword to true, the Secret referenced here will be" +
			" created or updated with the generated password if it does not already contain one."
	})
}
