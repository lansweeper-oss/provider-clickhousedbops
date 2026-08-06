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

			changed := false
			for _, field := range []string{"current_grants", "grant_option"} {
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
