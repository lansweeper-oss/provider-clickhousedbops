package config

import (
	"context"
	"errors"
	"testing"

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

		startVal   string // "" means key absent
		noResolver bool   // simulate code generation / no injection
		resolveID  string
		resolveOK  bool
		resolveErr error

		wantVal     string
		wantErr     bool
		wantResolve bool // whether the resolver must have been consulted
	}{
		"AdoptsExistingResourceOnRestore": {
			// The bug: a restored resource already exists, so it must be adopted by its
			// real UUID instead of re-created. Without the fix the id stayed at the
			// sentinel and the provider issued a conflicting create.
			resourceName: "clickhousedbops_role",
			field:        "id",
			startVal:     "",
			resolveID:    realUUID,
			resolveOK:    true,
			wantVal:      realUUID,
			wantResolve:  true,
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
			resourceName: "clickhousedbops_role",
			field:        "id",
			startVal:     sentinelUUID,
			resolveID:    realUUID,
			resolveOK:    true,
			wantVal:      realUUID,
			wantResolve:  true,
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
			resourceName: "clickhousedbops_database",
			field:        "uuid",
			startVal:     "",
			resolveID:    realUUID,
			resolveOK:    true,
			wantVal:      realUUID,
			wantResolve:  true,
		},
		"SeedsSentinelOnUUIDFieldWhenAbsent": {
			resourceName: "clickhousedbops_database",
			field:        "uuid",
			startVal:     "",
			resolveOK:    false,
			wantVal:      sentinelUUID,
			wantResolve:  true,
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
			if resolved != tc.wantResolve {
				t.Errorf("resolver consulted = %v, want %v", resolved, tc.wantResolve)
			}
		})
	}
}
