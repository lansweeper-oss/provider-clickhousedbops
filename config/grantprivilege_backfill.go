package config

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// backfillGrantPrivilegeDefaults seeds current_grants and grant_option in
// status.atProvider when they are missing. Upstream v1.11.0 added these fields
// with RequiresReplace; resources created before that version lack them in
// their observation, producing a diff on ForceNew fields that triggers an
// unsupported replacement. Seeding the upstream defaults (false) eliminates
// the diff for pre-existing resources.
func backfillGrantPrivilegeDefaults() config.NewInitializerFn {
	return func(_ client.Client) managed.Initializer {
		return managed.InitializerFn(func(_ context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for grant_privilege backfill: %w", err)
			}
			if obs == nil {
				obs = make(map[string]any)
			}

			backfillFields := map[string]bool{
				"current_grants": true,
				"grant_option":   true,
			}

			// Only backfill when the observation contains real data from a previous reconcile.
			// On the very first reconcile atProvider is empty; seeding defaults here would make
			// upjet believe state already exists and skip copying forProvider params (like
			// grantee_user_name) into the TF state, breaking the Read call.
			hasRealFields := false
			for k := range obs {
				if !backfillFields[k] {
					hasRealFields = true
					break
				}
			}
			if !hasRealFields {
				return nil
			}

			changed := false
			for field := range backfillFields {
				if _, exists := obs[field]; !exists {
					obs[field] = false
					changed = true
				}
			}
			if !changed {
				return nil
			}
			return tr.SetObservation(obs)
		})
	}
}
