package config

import (
	"context"
	"errors"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillGrantPrivilegeDefaults(t *testing.T) {
	cases := map[string]struct {
		obs     map[string]any
		getErr  error
		setErr  error
		wantObs map[string]any
		wantErr bool
	}{
		"SeedsBothFieldsWhenMissing": {
			obs: map[string]any{
				"privilege_name": "SELECT",
			},
			wantObs: map[string]any{
				"privilege_name":  "SELECT",
				"current_grants":  false,
				"grant_option":    false,
			},
		},
		"SeedsOnlyMissingField": {
			obs: map[string]any{
				"grant_option": true,
			},
			wantObs: map[string]any{
				"grant_option":   true,
				"current_grants": false,
			},
		},
		"NoOpWhenBothPresent": {
			obs: map[string]any{
				"current_grants": true,
				"grant_option":   true,
			},
			wantObs: map[string]any{
				"current_grants": true,
				"grant_option":   true,
			},
		},
		"HandlesNilObservation": {
			obs: nil,
			wantObs: map[string]any{
				"current_grants": false,
				"grant_option":   false,
			},
		},
		"ReturnsGetObservationError": {
			getErr:  errors.New("storage error"),
			wantErr: true,
		},
		"ReturnsSetObservationError": {
			obs:     map[string]any{},
			setErr:  errors.New("write error"),
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mg := &fakeManaged{
				Managed: &fake.Managed{},
				obs:     tc.obs,
				getErr:  tc.getErr,
				setErr:  tc.setErr,
			}

			init := backfillGrantPrivilegeDefaults()(nil)
			err := init.Initialize(context.Background(), mg)

			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantObs, mg.obs)
		})
	}
}
