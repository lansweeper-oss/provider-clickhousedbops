package config

import (
	"context"
	"testing"
)

// realUUID is a representative provider-assigned role UUID.
const realRoleUUID = "01499c10-0000-4000-8000-000000000000"

// TestRoleExternalNameNoFlap is the explicit regression test for the bug where
// crossplane.io/external-name for a null-cluster_name role alternated between the
// role UUID and the role NAME across reconciles, pinning Ready at Creating.
//
// Root cause: GetIDFn fell back to parameters["name"], which made upjet import the
// role by name (id=<name> in tfstate); a later refresh canonicalised id to the
// UUID. GetExternalNameFn reads tfstate["id"], so the computed external-name
// flapped name<->UUID. The fix removes the name fallback (sentinel instead), so
// the name can never enter the identity.
func TestRoleExternalNameNoFlap(t *testing.T) {
	e := idWithClusterName()
	params := map[string]any{"name": "tst_db_basic_ddl"}

	// Pre-create: external-name is empty (or defaulted to the k8s/param name).
	// GetIDFn must NEVER yield the role name; it must yield the sentinel so the
	// provider reports "not found" and creation proceeds.
	for _, externalName := range []string{"", "tst_db_basic_ddl"} {
		id, err := e.GetIDFn(context.Background(), externalName, params, nil)
		if err != nil {
			t.Fatalf("GetIDFn(%q) unexpected error: %v", externalName, err)
		}
		if id == "tst_db_basic_ddl" {
			t.Fatalf("GetIDFn(%q) returned the role NAME %q; this reintroduces the import-by-name flap", externalName, id)
		}
		if id != sentinelUUID {
			t.Fatalf("GetIDFn(%q) = %q, want sentinel %q", externalName, id, sentinelUUID)
		}
	}

	// Once the role exists, the external-name carries the real UUID. GetIDFn must
	// echo it back unchanged (no name fallback, no sentinel substitution).
	id, err := e.GetIDFn(context.Background(), realRoleUUID, params, nil)
	if err != nil {
		t.Fatalf("GetIDFn(uuid) unexpected error: %v", err)
	}
	if id != realRoleUUID {
		t.Fatalf("GetIDFn(uuid) = %q, want %q", id, realRoleUUID)
	}

	// GetExternalNameFn reads tfstate["id"] and must return the same UUID
	// deterministically on every observe (the value that flapped before).
	tfstate := map[string]any{"id": realRoleUUID, "name": "tst_db_basic_ddl"}
	for i := range 3 {
		got, err := e.GetExternalNameFn(tfstate)
		if err != nil {
			t.Fatalf("GetExternalNameFn iteration %d error: %v", i, err)
		}
		if got != realRoleUUID {
			t.Fatalf("GetExternalNameFn iteration %d = %q, want stable UUID %q", i, got, realRoleUUID)
		}
	}
}

// TestRoleIDAndExternalNameAgree asserts GetIDFn and GetExternalNameFn are mutual
// inverses for an existing role: the id GetIDFn produces from an external-name
// round-trips back to that same external-name through GetExternalNameFn.
func TestRoleIDAndExternalNameAgree(t *testing.T) {
	e := idWithClusterName()

	cases := map[string]struct {
		externalName string
		params       map[string]any
		wantID       string // id GetIDFn must produce
		wantExternal string // external-name GetExternalNameFn must recover
	}{
		"NullClusterExistingRole": {
			externalName: realRoleUUID,
			params:       map[string]any{"name": "tst_db_basic_ddl"},
			wantID:       realRoleUUID,
			wantExternal: realRoleUUID,
		},
		"ClusterQualifiedExistingRole": {
			externalName: realRoleUUID,
			params:       map[string]any{"name": "tst_db_basic_ddl", "cluster_name": "my_cluster"},
			wantID:       "my_cluster" + sep + realRoleUUID,
			wantExternal: realRoleUUID,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			id, err := e.GetIDFn(context.Background(), tc.externalName, tc.params, nil)
			if err != nil {
				t.Fatalf("GetIDFn error: %v", err)
			}
			if id != tc.wantID {
				t.Fatalf("GetIDFn = %q, want %q", id, tc.wantID)
			}

			// tfstate["id"] is what the provider stores; mirror the id GetIDFn made.
			tfstate := map[string]any{"id": id, "name": tc.params["name"]}
			got, err := e.GetExternalNameFn(tfstate)
			if err != nil {
				t.Fatalf("GetExternalNameFn error: %v", err)
			}
			if got != tc.wantExternal {
				t.Fatalf("GetExternalNameFn = %q, want %q (GetID/GetExternalName disagree)", got, tc.wantExternal)
			}
		})
	}
}

// TestRoleClusterQualifiedFreshCreate ensures the cluster-qualified pre-create
// path still produces cluster:sentinel (not cluster:name).
func TestRoleClusterQualifiedFreshCreate(t *testing.T) {
	e := idWithClusterName()
	params := map[string]any{"name": "tst_db_basic_ddl", "cluster_name": "my_cluster"}

	id, err := e.GetIDFn(context.Background(), "", params, nil)
	if err != nil {
		t.Fatalf("GetIDFn error: %v", err)
	}
	want := "my_cluster" + sep + sentinelUUID
	if id != want {
		t.Fatalf("GetIDFn = %q, want %q", id, want)
	}
}

// TestSharedConfigResources documents that the fix generalises: role, user and
// settings_profile all share idWithClusterName() and therefore the same
// non-flapping identity behaviour.
func TestSharedConfigResources(t *testing.T) {
	for _, name := range []string{"clickhousedbops_role", "clickhousedbops_user", "clickhousedbops_settings_profile"} {
		cfg, ok := ExternalNameConfigs[name]
		if !ok {
			t.Fatalf("%s missing from ExternalNameConfigs", name)
		}
		// Pre-create must resolve to sentinel, never the name, for every one.
		id, err := cfg.GetIDFn(context.Background(), "", map[string]any{"name": "some_name"}, nil)
		if err != nil {
			t.Fatalf("%s GetIDFn error: %v", name, err)
		}
		if id != sentinelUUID {
			t.Fatalf("%s GetIDFn pre-create = %q, want sentinel %q", name, id, sentinelUUID)
		}
	}
}
