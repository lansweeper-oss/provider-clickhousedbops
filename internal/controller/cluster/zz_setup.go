// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/upjet/v2/pkg/controller"

	database "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/database"
	grantprivilege "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/grantprivilege"
	grantrole "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/grantrole"
	role "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/role"
	setting "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/setting"
	settingprofile "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/settingprofile"
	settingprofileassociation "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/settingprofileassociation"
	user "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/clickhousedbops/user"
	providerconfig "github.com/lansweeper-oss/provider-clickhousedbops/internal/controller/cluster/providerconfig"
)

// Setup creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		database.Setup,
		grantprivilege.Setup,
		grantrole.Setup,
		role.Setup,
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
		role.SetupGated,
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
