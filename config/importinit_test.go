package config

import (
	"context"
	"errors"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fakeManaged is a managed resource that also exposes the Terraform observation
// (status.atProvider) so the initializer under test can read/write the id field.
type fakeManaged struct {
	*fake.Managed
	obs    map[string]any
	getErr error
	setErr error
}

func (f *fakeManaged) GetObservation() (map[string]any, error) { return f.obs, f.getErr }

func (f *fakeManaged) SetObservation(o map[string]any) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.obs = o
	return nil
}

func TestAdoptByNameInitializer(t *testing.T) {
	const realUUID = "11111111-2222-3333-4444-555555555555"

	cases := map[string]struct {
		resourceName string
		field        string // observation key the initializer writes ("id" or "uuid")

		startVal      string // "" means key absent
		startExternal string // pre-set crossplane.io/external-name annotation
		noResolver    bool   // simulate code generation / no injection
		resolveID     string
		resolveOK     bool
		resolveErr    error

		wantVal          string
		wantExternalName string // crossplane.io/external-name expected after Initialize
		wantErr          bool
		wantResolve      bool // whether the resolver must have been consulted
	}{
		"AdoptsExistingResourceOnRestore": {
			// The bug: a restored resource already exists, so it must be adopted by its
			// real UUID instead of re-created. Without the fix the id stayed at the
			// sentinel and the provider issued a conflicting create.
			resourceName:     "clickhousedbops_role",
			field:            "id",
			startVal:         "",
			resolveID:        realUUID,
			resolveOK:        true,
			wantVal:          realUUID,
			wantExternalName: realUUID,
			wantResolve:      true,
		},
		"SeedsSentinelWhenResourceAbsent": {
			resourceName: "clickhousedbops_role",
			field:        "id",
			startVal:     "",
			resolveOK:    false,
			wantVal:      sentinelUUID,
			wantResolve:  true,
		},
		"ReplacesSentinelWithRealUUIDWhenFound": {
			resourceName:     "clickhousedbops_role",
			field:            "id",
			startVal:         sentinelUUID,
			resolveID:        realUUID,
			resolveOK:        true,
			wantVal:          realUUID,
			wantExternalName: realUUID,
			wantResolve:      true,
		},
		"LeavesRealUUIDUntouched": {
			// Post-import/creation: a real UUID is present, resolver must not run.
			resourceName: "clickhousedbops_role",
			field:        "id",
			startVal:     realUUID,
			wantVal:      realUUID,
			wantResolve:  false,
		},
		"ReturnsErrorForRetryOnLookupFailure": {
			// Must surface the error so the reconcile retries, never force-create.
			resourceName: "clickhousedbops_role",
			field:        "id",
			startVal:     "",
			resolveErr:   errors.New("clickhouse unreachable"),
			wantErr:      true,
		},
		"SeedsSentinelWhenNoResolverWired": {
			// Code generation / no injection: preserve the force-create behavior.
			resourceName: "clickhousedbops_role",
			field:        "id",
			startVal:     "",
			noResolver:   true,
			wantVal:      sentinelUUID,
		},
		"AdoptsDatabaseOnUUIDField": {
			// database seeds the "uuid" observation field rather than "id".
			resourceName:     "clickhousedbops_database",
			field:            "uuid",
			startVal:         "",
			resolveID:        realUUID,
			resolveOK:        true,
			wantVal:          realUUID,
			wantExternalName: realUUID,
			wantResolve:      true,
		},
		"SeedsSentinelOnUUIDFieldWhenAbsent": {
			resourceName: "clickhousedbops_database",
			field:        "uuid",
			startVal:     "",
			resolveOK:    false,
			wantVal:      sentinelUUID,
			wantResolve:  true,
		},
		"ExplicitUUIDExternalNameWins": {
			// Import by UUID: a pinned external name that is a UUID takes precedence
			// and the name resolver is never consulted.
			resourceName:     "clickhousedbops_role",
			field:            "id",
			startExternal:    realUUID,
			startVal:         "",
			resolveID:        "99999999-9999-9999-9999-999999999999",
			resolveOK:        true,
			wantVal:          realUUID,
			wantExternalName: realUUID,
			wantResolve:      false,
		},
		"ClusterPrefixedUUIDExternalNameStripped": {
			resourceName:     "clickhousedbops_role",
			field:            "id",
			startExternal:    "mycluster:" + realUUID,
			startVal:         "",
			resolveOK:        true,
			resolveID:        "99999999-9999-9999-9999-999999999999",
			wantVal:          realUUID,
			wantExternalName: realUUID,
			wantResolve:      false,
		},
		"NonUUIDExternalNameFallsThroughToResolver": {
			// The crossplane default external name is the resource name (not a UUID),
			// so resolution by spec.forProvider.name still runs.
			resourceName:     "clickhousedbops_role",
			field:            "id",
			startExternal:    "myrole",
			startVal:         "",
			resolveID:        realUUID,
			resolveOK:        true,
			wantVal:          realUUID,
			wantExternalName: realUUID,
			wantResolve:      true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resolved := false
			if !tc.noResolver {
				SetResolverFactory(tc.resourceName, func(_ client.Client) UUIDResolver {
					return func(_ context.Context, _ xpresource.Managed) (string, bool, error) {
						resolved = true
						return tc.resolveID, tc.resolveOK, tc.resolveErr
					}
				})
			}
			t.Cleanup(func() { delete(resolverFactories, tc.resourceName) })

			obs := map[string]any{}
			if tc.startVal != "" {
				obs[tc.field] = tc.startVal
			}
			mg := &fakeManaged{Managed: &fake.Managed{}, obs: obs}
			if tc.startExternal != "" {
				meta.SetExternalName(mg, tc.startExternal)
			}

			init := adoptByNameInitializer(tc.resourceName, tc.field)(nil)
			err := init.Initialize(context.Background(), mg)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := mg.obs[tc.field]; got != tc.wantVal {
				t.Errorf("%s = %v, want %v", tc.field, got, tc.wantVal)
			}
			if got := meta.GetExternalName(mg); got != tc.wantExternalName {
				t.Errorf("external-name = %q, want %q", got, tc.wantExternalName)
			}
			if resolved != tc.wantResolve {
				t.Errorf("resolver consulted = %v, want %v", resolved, tc.wantResolve)
			}
		})
	}
}
