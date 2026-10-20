package config

import (
	"context"
	"testing"
)

func TestIdWithStub_GetIDFn(t *testing.T) {
	e := idWithStub()

	cases := map[string]struct {
		externalName string
		wantID       string
	}{
		"CompositeKeyWithSeparator": {
			externalName: "SELECT:testdb::testuser:",
			wantID:       "SELECT:testdb::testuser:",
		},
		"EmptyExternalName": {
			externalName: "",
			wantID:       "",
		},
		"PlainK8sName": {
			externalName: "testgrantprivilege",
			wantID:       "",
		},
		"SingleColon": {
			externalName: "a:b",
			wantID:       "a:b",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := e.GetIDFn(context.Background(), tc.externalName, nil, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantID {
				t.Errorf("GetIDFn(%q) = %q, want %q", tc.externalName, got, tc.wantID)
			}
		})
	}
}

func TestIdWithStub_GetExternalNameFn(t *testing.T) {
	e := idWithStub()

	cases := map[string]struct {
		tfstate  map[string]any
		wantName string
		wantErr  bool
	}{
		"StateWithID": {
			tfstate:  map[string]any{"id": "SELECT:testdb::testuser:"},
			wantName: "SELECT:testdb::testuser:",
		},
		"StateWithoutID": {
			// Framework resources like GrantPrivilege have no "id" attribute.
			// GetExternalNameFn should return "" without error.
			tfstate: map[string]any{
				"privilege_name":    "SELECT",
				"database_name":     "testdb",
				"grantee_user_name": "testuser",
			},
			wantName: "",
		},
		"EmptyState": {
			tfstate:  map[string]any{},
			wantName: "",
		},
		"NilState": {
			tfstate:  nil,
			wantName: "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := e.GetExternalNameFn(tc.tfstate)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantName {
				t.Errorf("GetExternalNameFn = %q, want %q", got, tc.wantName)
			}
		})
	}
}

func TestIdWithClusterName_GetIDFn(t *testing.T) {
	e := idWithClusterName()

	cases := map[string]struct {
		externalName string
		params       map[string]any
		wantID       string
	}{
		"EmptyExternalNameSeedsSentinel": {
			externalName: "",
			params:       map[string]any{"name": "testuser"},
			wantID:       sentinelUUID,
		},
		"NameEqualsExternalNameSeedsSentinel": {
			externalName: "testuser",
			params:       map[string]any{"name": "testuser"},
			wantID:       sentinelUUID,
		},
		"RealUUIDUsedDirectly": {
			externalName: "real-uuid",
			params:       map[string]any{"name": "testuser"},
			wantID:       "real-uuid",
		},
		"ClusterNamePrefixed": {
			externalName: "real-uuid",
			params:       map[string]any{"name": "testuser", "cluster_name": "mycluster"},
			wantID:       "mycluster:real-uuid",
		},
		"EmptyExternalNameWithCluster": {
			externalName: "",
			params:       map[string]any{"name": "testuser", "cluster_name": "mycluster"},
			wantID:       "mycluster:" + sentinelUUID,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := e.GetIDFn(context.Background(), tc.externalName, tc.params, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantID {
				t.Errorf("GetIDFn(%q) = %q, want %q", tc.externalName, got, tc.wantID)
			}
		})
	}
}

func TestIdWithClusterNameDatabase_GetExternalNameFn(t *testing.T) {
	e := idWithClusterNameDatabase()

	cases := map[string]struct {
		tfstate  map[string]any
		wantName string
	}{
		"UUIDPresent": {
			tfstate:  map[string]any{"uuid": "11111111-2222-3333-4444-555555555555"},
			wantName: "11111111-2222-3333-4444-555555555555",
		},
		"UUIDWithClusterPrefix": {
			tfstate:  map[string]any{"uuid": "mycluster:11111111-2222-3333-4444-555555555555"},
			wantName: "11111111-2222-3333-4444-555555555555",
		},
		"FallsBackToIDWhenNoUUID": {
			tfstate:  map[string]any{"id": "some-id"},
			wantName: "some-id",
		},
		"SentinelUUID": {
			// The sentinel is a placeholder for "not created yet". Writing it to the
			// annotation after a failed create makes the resource look up the sentinel
			// forever, so the external name stays empty instead.
			tfstate:  map[string]any{"uuid": sentinelUUID},
			wantName: "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := e.GetExternalNameFn(tc.tfstate)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantName {
				t.Errorf("GetExternalNameFn = %q, want %q", got, tc.wantName)
			}
		})
	}
}

func TestIDFromClusterName(t *testing.T) {
	getID := IDFromClusterName(sep)

	cases := map[string]struct {
		externalName string
		params       map[string]any
		wantID       string
	}{
		"EmptyExternalNameSeedsSentinel": {
			externalName: "",
			params:       map[string]any{"name": "testdb"},
			wantID:       sentinelUUID,
		},
		"NameEqualsExternalNameSeedsSentinel": {
			externalName: "testdb",
			params:       map[string]any{"name": "testdb"},
			wantID:       sentinelUUID,
		},
		"RealUUIDUsedDirectly": {
			externalName: "11111111-2222-3333-4444-555555555555",
			params:       map[string]any{"name": "testdb"},
			wantID:       "11111111-2222-3333-4444-555555555555",
		},
		"SentinelUUIDUsedDirectly": {
			externalName: sentinelUUID,
			params:       map[string]any{"name": "testdb"},
			wantID:       sentinelUUID,
		},
		"ClusterNamePrefixed": {
			externalName: "11111111-2222-3333-4444-555555555555",
			params:       map[string]any{"name": "testdb", "cluster_name": "mycluster"},
			wantID:       "mycluster:11111111-2222-3333-4444-555555555555",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := getID(context.Background(), tc.externalName, tc.params, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantID {
				t.Errorf("IDFromClusterName(%q) = %q, want %q", tc.externalName, got, tc.wantID)
			}
		})
	}
}

func TestNameFromState_GetExternalNameFn(t *testing.T) {
	e := nameFromState()

	cases := map[string]struct {
		tfstate  map[string]any
		wantName string
	}{
		"StateWithName":    {tfstate: map[string]any{"name": "s3_prod", "cluster_name": "c1"}, wantName: "s3_prod"},
		"StateWithoutName": {tfstate: map[string]any{}, wantName: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := e.GetExternalNameFn(tc.tfstate)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantName {
				t.Errorf("GetExternalNameFn() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

func TestExternalNameFromClusterName_IgnoresSentinel(t *testing.T) {
	cases := map[string]struct {
		tfstate  map[string]any
		wantName string
	}{
		"RealUUID": {
			tfstate:  map[string]any{"id": "mycluster:11111111-2222-3333-4444-555555555555"},
			wantName: "11111111-2222-3333-4444-555555555555",
		},
		"Sentinel": {
			tfstate:  map[string]any{"id": sentinelUUID},
			wantName: "",
		},
		"ClusterPrefixedSentinel": {
			tfstate:  map[string]any{"id": "mycluster:" + sentinelUUID},
			wantName: "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ExternalNameFromClusterName(sep)(tc.tfstate)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantName {
				t.Errorf("external name = %q, want %q", got, tc.wantName)
			}
		})
	}
}
