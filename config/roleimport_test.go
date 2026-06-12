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
// (status.atProvider) so the initializer under test can read/write the "id" field.
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

func staticResolver(uuid string, found bool, err error) func(client.Client) roleUUIDResolver {
	return func(_ client.Client) roleUUIDResolver {
		return func(_ context.Context, _ xpresource.Managed) (string, bool, error) {
			return uuid, found, err
		}
	}
}

func TestRoleImportInitializer(t *testing.T) {
	const realUUID = "11111111-2222-3333-4444-555555555555"

	cases := map[string]struct {
		startID    string // "" means key absent
		resolveID  string
		resolveOK  bool
		resolveErr error

		wantID      string
		wantErr     bool
		wantResolve bool // whether the resolver must have been consulted
	}{
		"AdoptsExistingRoleOnRestore": {
			// The bug: a restored role already exists, so it must be adopted by its
			// real UUID instead of re-created. Without the fix the id stayed at the
			// sentinel and the provider issued a conflicting CREATE ROLE.
			startID:     "",
			resolveID:   realUUID,
			resolveOK:   true,
			wantID:      realUUID,
			wantResolve: true,
		},
		"SeedsSentinelWhenRoleAbsent": {
			startID:     "",
			resolveOK:   false,
			wantID:      sentinelUUID,
			wantResolve: true,
		},
		"ReplacesSentinelWithRealUUIDWhenFound": {
			startID:     sentinelUUID,
			resolveID:   realUUID,
			resolveOK:   true,
			wantID:      realUUID,
			wantResolve: true,
		},
		"LeavesRealUUIDUntouched": {
			// Post-import/creation: a real UUID is present, resolver must not run.
			startID:     realUUID,
			wantID:      realUUID,
			wantResolve: false,
		},
		"ReturnsErrorForRetryOnLookupFailure": {
			// Must surface the error so the reconcile retries, never force-create.
			startID:    "",
			resolveErr: errors.New("clickhouse unreachable"),
			wantErr:    true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resolved := false
			mk := func(kube client.Client) roleUUIDResolver {
				return func(ctx context.Context, mg xpresource.Managed) (string, bool, error) {
					resolved = true
					return tc.resolveID, tc.resolveOK, tc.resolveErr
				}
			}

			obs := map[string]any{}
			if tc.startID != "" {
				obs["id"] = tc.startID
			}
			mg := &fakeManaged{Managed: &fake.Managed{}, obs: obs}

			init := roleImportInitializer(mk)(nil)
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
			if got := mg.obs["id"]; got != tc.wantID {
				t.Errorf("id = %v, want %v", got, tc.wantID)
			}
			if resolved != tc.wantResolve {
				t.Errorf("resolver consulted = %v, want %v", resolved, tc.wantResolve)
			}
		})
	}
}
