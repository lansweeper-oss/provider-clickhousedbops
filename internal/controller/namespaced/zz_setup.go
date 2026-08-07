// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/upjet/v2/pkg/controller"

	database "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/database"
	grantprivilege "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/grantprivilege"
	grantrole "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/grantrole"
	maskingpolicy "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/maskingpolicy"
	role "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/role"
	rowpolicy "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/rowpolicy"
	setting "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/setting"
	settingprofile "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/settingprofile"
	settingprofileassociation "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/settingprofileassociation"
	user "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/clickhousedbops/user"
	providerconfig "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/namespaced/providerconfig"
)

// Setup creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		database.Setup,
		grantprivilege.Setup,
		grantrole.Setup,
		maskingpolicy.Setup,
		role.Setup,
		rowpolicy.Setup,
		setting.Setup,
		settingprofile.Setup,
		settingprofileassociation.Setup,
		user.Setup,
		providerconfig.Setup,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupGated creates all controllers with the supplied logger and adds them to
// the supplied manager gated.
func SetupGated(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		database.SetupGated,
		grantprivilege.SetupGated,
		grantrole.SetupGated,
		maskingpolicy.SetupGated,
		role.SetupGated,
		rowpolicy.SetupGated,
		setting.SetupGated,
		settingprofile.SetupGated,
		settingprofileassociation.SetupGated,
		user.SetupGated,
		providerconfig.SetupGated,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupWebhookWithManager registers conversion webhooks for all resource kinds in the group.
func SetupWebhookWithManager(mgr ctrl.Manager) error {
	for _, setup := range []func(ctrl.Manager) error{
		database.SetupWebhookWithManager,
		grantprivilege.SetupWebhookWithManager,
		grantrole.SetupWebhookWithManager,
		maskingpolicy.SetupWebhookWithManager,
		role.SetupWebhookWithManager,
		rowpolicy.SetupWebhookWithManager,
		setting.SetupWebhookWithManager,
		settingprofile.SetupWebhookWithManager,
		settingprofileassociation.SetupWebhookWithManager,
		user.SetupWebhookWithManager,
		providerconfig.SetupWebhookWithManager,
	} {
		if err := setup(mgr); err != nil {
			return err
		}
	}
	return nil
}
