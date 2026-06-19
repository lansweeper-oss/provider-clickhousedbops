package clients

import (
	"context"

	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/lansweeper-oss/provider-clickhousedbops/config"
)

// Lives here, not in config, so the ClickHouse client and apis packages stay out of the
// code generator's import graph. Mirrors NewRoleUUIDResolver.
func NewConnParamsResolver(kube client.Client) config.ConnParamsResolver {
	return func(ctx context.Context, mg xpresource.Managed) (config.ConnInfo, error) {
		p, err := ResolveConnParams(ctx, kube, mg)
		if err != nil {
			return config.ConnInfo{}, err
		}
		info := config.ConnInfo{Host: p.Host, Port: p.Port, Protocol: p.Protocol}
		if k := p.SecretKeys; k != nil {
			info.Keys = config.ConnectionKeyOverrides{
				Username:        k.Username,
				Host:            k.Host,
				Port:            k.Port,
				Protocol:        k.Protocol,
				Password:        k.Password,
				PasswordEncoded: k.PasswordEncoded,
				PasswordSHA256:  k.PasswordSha256,
			}
		}
		return info, nil
	}
}
