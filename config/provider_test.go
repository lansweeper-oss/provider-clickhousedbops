package config

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProviderResources(t *testing.T) {
	p := GetProvider()

	requiredResources := []string{
		"clickhousedbops_database",
		"clickhousedbops_grant_privilege",
		"clickhousedbops_grant_role",
		"clickhousedbops_role",
		"clickhousedbops_setting",
		"clickhousedbops_settings_profile",
		"clickhousedbops_settings_profile_association",
		"clickhousedbops_user",
	}

	for _, name := range requiredResources {
		r, ok := p.Resources[name]
		if !ok {
			t.Errorf("resource %q not found in provider", name)
			continue
		}
		if r.TerraformPluginFrameworkResource == nil {
			t.Errorf("resource %q: TerraformPluginFrameworkResource is nil", name)
		}
	}
}

func TestGrantPrivilegeResourceConfig(t *testing.T) {
	p := GetProvider()
	r, ok := p.Resources["clickhousedbops_grant_privilege"]
	if !ok {
		t.Fatal("clickhousedbops_grant_privilege not found in provider")
	}

	t.Run("HasFrameworkResource", func(t *testing.T) {
		if r.TerraformPluginFrameworkResource == nil {
			t.Fatal("TerraformPluginFrameworkResource is nil")
		}
	})

	t.Run("SchemaHasNoID", func(t *testing.T) {
		schemaResp := &fwresource.SchemaResponse{}
		r.TerraformPluginFrameworkResource.Schema(context.Background(), fwresource.SchemaRequest{}, schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Fatalf("schema error: %v", schemaResp.Diagnostics)
		}
		if _, hasID := schemaResp.Schema.GetAttributes()["id"]; hasID {
			t.Error("grant_privilege schema should NOT have 'id' attribute")
		}
	})

	t.Run("ExternalNameDisablesNameInitializer", func(t *testing.T) {
		if !r.ExternalName.DisableNameInitializer {
			t.Error("DisableNameInitializer should be true for IdentifierFromProvider")
		}
	})

	t.Run("HasBackfillInitializer", func(t *testing.T) {
		if len(r.InitializerFns) != 1 {
			t.Errorf("expected 1 InitializerFn (backfillGrantPrivilegeDefaults), got %d", len(r.InitializerFns))
		}
	})
}

// TestGrantPrivilegeTFValueRoundTrip verifies that a typical GrantPrivilege
// params map can be serialized to a tftypes.Value using the resource's schema
// type and deserialized back. This catches type mismatches that would cause
// silent failures in the upjet connector's state reconstruction path.
func TestGrantPrivilegeTFValueRoundTrip(t *testing.T) {
	p := GetProvider()
	r := p.Resources["clickhousedbops_grant_privilege"]

	schemaResp := &fwresource.SchemaResponse{}
	r.TerraformPluginFrameworkResource.Schema(context.Background(), fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema error: %v", schemaResp.Diagnostics)
	}
	tfType := schemaResp.Schema.Type().TerraformType(context.Background())

	cases := map[string]struct {
		params map[string]any
	}{
		"MinimalGrant": {
			params: map[string]any{
				"privilege_name":    "SELECT",
				"database_name":     "testdb",
				"grantee_user_name": "testuser",
			},
		},
		"FullGrant": {
			params: map[string]any{
				"privilege_name":    "SELECT",
				"database_name":     "testdb",
				"table_name":        "testtable",
				"column_name":       "testcolumn",
				"grantee_user_name": "testuser",
				"grant_option":      false,
			},
		},
		"RoleGrant": {
			params: map[string]any{
				"privilege_name":    "INSERT",
				"database_name":     "testdb",
				"grantee_role_name": "testrole",
			},
		},
		"WithCluster": {
			params: map[string]any{
				"privilege_name":    "SELECT",
				"database_name":     "testdb",
				"grantee_user_name": "testuser",
				"cluster_name":      "mycluster",
			},
		},
		"EmptyParams": {
			params: map[string]any{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tc.params)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			tfValue, err := tftypes.ValueFromJSONWithOpts(jsonBytes, tfType, tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
			if err != nil {
				t.Fatalf("ValueFromJSON: %v", err)
			}

			if tfValue.IsNull() {
				t.Fatal("resulting tftypes.Value should not be null")
			}

			var objMap map[string]tftypes.Value
			if err := tfValue.As(&objMap); err != nil {
				t.Fatalf("cannot convert to map: %v", err)
			}

			for key, val := range tc.params {
				tfVal, ok := objMap[key]
				if !ok {
					t.Errorf("key %q missing from TF value", key)
					continue
				}
				if tfVal.IsNull() {
					t.Errorf("key %q should not be null, expected %v", key, val)
				}
			}

			attrs := schemaResp.Schema.GetAttributes()
			for attrName := range attrs {
				if _, specified := tc.params[attrName]; !specified {
					tfVal := objMap[attrName]
					if !tfVal.IsNull() {
						t.Errorf("unspecified attr %q should be null, got non-null", attrName)
					}
				}
			}
		})
	}
}

// TestGrantPrivilegeStateReconstructionPath simulates the state reconstruction
// that happens in the upjet connector when observation is empty (first reconcile).
// When len(observation) == 0, upjet copies all params to state. This test verifies
// that the copied state produces a valid TF value for the GrantPrivilege schema.
func TestGrantPrivilegeStateReconstructionPath(t *testing.T) {
	p := GetProvider()
	r := p.Resources["clickhousedbops_grant_privilege"]

	schemaResp := &fwresource.SchemaResponse{}
	r.TerraformPluginFrameworkResource.Schema(context.Background(), fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema error: %v", schemaResp.Diagnostics)
	}
	tfType := schemaResp.Schema.Type().TerraformType(context.Background())

	params := map[string]any{
		"privilege_name":    "SELECT",
		"database_name":     "testdb",
		"grantee_user_name": "testuser",
	}

	// Simulate copyParameters: when observation is empty, upjet copies params to state
	observation := map[string]any{}
	state := make(map[string]any, len(params))
	maps.Copy(state, params)
	maps.Copy(state, observation)

	// Verify the state can be converted to a valid DynamicValue
	jsonBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	tfValue, err := tftypes.ValueFromJSONWithOpts(jsonBytes, tfType, tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
	if err != nil {
		t.Fatalf("state reconstruction should produce valid TF value: %v", err)
	}
	if tfValue.IsNull() {
		t.Fatal("reconstructed state should not be null")
	}

	// Verify the config value (what upjet passes as config to PlanResourceChange)
	// can also be constructed. Config = params with computed-only attrs removed.
	configValues := make(map[string]any, len(params))
	maps.Copy(configValues, params)
	configJSON, err := json.Marshal(configValues)
	if err != nil {
		t.Fatalf("json.Marshal config: %v", err)
	}
	configTFValue, err := tftypes.ValueFromJSONWithOpts(configJSON, tfType, tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
	if err != nil {
		t.Fatalf("config should produce valid TF value: %v", err)
	}
	if configTFValue.IsNull() {
		t.Fatal("config TF value should not be null")
	}

	// Verify that a null state (resource not found) can be constructed
	nullState := tftypes.NewValue(tfType, nil)
	if !nullState.IsNull() {
		t.Fatal("null state should be null")
	}

	// Verify that the empty-value construction (used by proposedState when prior is null)
	// produces a valid non-null value for the schema
	attrs := schemaResp.Schema.GetAttributes()
	emptyVals := make(map[string]tftypes.Value)
	for attrName, attr := range attrs {
		emptyVals[attrName] = tftypes.NewValue(attr.GetType().TerraformType(context.Background()), nil)
	}
	emptyState := tftypes.NewValue(tfType, emptyVals)
	if emptyState.IsNull() {
		t.Fatal("empty state should not be null")
	}
}

// TestGetExternalNameFnAfterCreate verifies that after a successful Create,
// GetExternalNameFn returns a value that, when passed back through GetIDFn,
// allows subsequent Observe calls to identify the resource.
//
// For idWithStub() resources (like GrantPrivilege), the TF state after Create
// has no "id" field. GetExternalNameFn returns "". On subsequent Observe,
// GetIDFn is NOT called (because the schema has no "id" attribute), so the
// empty external name doesn't prevent the resource from being found.
func TestGetExternalNameFnAfterCreate(t *testing.T) {
	e := idWithStub()

	// Simulate state after TF provider Create for GrantPrivilege.
	// No "id" attribute in the state.
	stateAfterCreate := map[string]any{
		"privilege_name":    "SELECT",
		"database_name":     "testdb",
		"grantee_user_name": "testuser",
		"grant_option":      false,
	}

	externalName, err := e.GetExternalNameFn(stateAfterCreate)
	if err != nil {
		t.Fatalf("GetExternalNameFn should not error: %v", err)
	}

	// For framework resources without "id", external name is always "".
	// This is expected behavior since the resource identity is tracked
	// via the opTracker's cached TF state, not via external name.
	if externalName != "" {
		t.Errorf("expected empty external name for state without 'id', got %q", externalName)
	}

	// Verify that GetIDFn with the resulting external name ("") returns ""
	id, err := e.GetIDFn(context.Background(), externalName, nil, nil)
	if err != nil {
		t.Fatalf("GetIDFn should not error: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty ID for external name %q, got %q", externalName, id)
	}
}
