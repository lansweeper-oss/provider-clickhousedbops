package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Connection-secret keys. The provider emits a ready-to-use ClickHouse
// connection shape so consumers (and compositions) need not reassemble it from
// the ProviderConfig.
const (
	keyUsername  = "clickhouse_username"
	keyHost      = "clickhouse_host"
	keyPort      = "clickhouse_port"
	keyProtocol  = "clickhouse_protocol"
	keyPassword  = "clickhouse_password"
	keyPwEncoded = "clickhouse_password_encoded"
	keyPwSHA256  = "clickhouse_password_sha256"
)

// ConnInfo carries the server-level connection coordinates read from the
// ProviderConfig credentials secret. It deliberately excludes the admin
// username/password: the connection secret's username is the managed user's
// own name, not the provider's admin user.
type ConnInfo struct {
	Host     string
	Port     uint16
	Protocol string
	// Keys optionally renames the connection-secret keys. Empty fields keep the
	// clickhouse_* defaults.
	Keys ConnectionKeyOverrides
}

// ConnectionKeyOverrides optionally renames each connection-secret key. An empty
// field means "use the clickhouse_* default".
type ConnectionKeyOverrides struct {
	Username        string
	Host            string
	Port            string
	Protocol        string
	Password        string
	PasswordEncoded string
	PasswordSHA256  string
}

// orDefault returns override when non-empty, else def.
func orDefault(override, def string) string {
	if override != "" {
		return override
	}
	return def
}

// ConnParamsResolver resolves the ProviderConfig connection coordinates for a
// managed resource.
type ConnParamsResolver func(ctx context.Context, mg xpresource.Managed) (ConnInfo, error)

// Injected at runtime so the ClickHouse client (and the apis it pulls in) stays
// out of the code generator's import graph, mirroring SetRoleResolverFactory.
var connParamsResolverFactory func(client.Client) ConnParamsResolver

func SetConnParamsResolverFactory(f func(client.Client) ConnParamsResolver) {
	connParamsResolverFactory = f
}

// connectionUsername returns the managed user's name for the connection secret:
// the external name, falling back to spec.forProvider.name.
func connectionUsername(mg xpresource.Managed) string {
	if name := meta.GetExternalName(mg); name != "" {
		return name
	}
	paved, err := fieldpath.PaveObject(mg)
	if err != nil {
		return ""
	}
	name, _ := paved.GetString("spec.forProvider.name")
	return name
}

// resolveConnInfo resolves the ProviderConfig connection coordinates (and any
// key-name overrides) for the managed resource via the injected factory.
func resolveConnInfo(ctx context.Context, c client.Client, mg xpresource.Managed) (ConnInfo, error) {
	if connParamsResolverFactory == nil {
		return ConnInfo{}, fmt.Errorf("connection params resolver not wired")
	}
	info, err := connParamsResolverFactory(c)(ctx, mg)
	if err != nil {
		return ConnInfo{}, fmt.Errorf("cannot resolve connection params: %w", err)
	}
	return info, nil
}

// sha256KeyName returns the effective (override-aware) key under which the
// password SHA256 hash is stored in the connection secret.
func sha256KeyName(info ConnInfo) string {
	return orDefault(info.Keys.PasswordSHA256, keyPwSHA256)
}

// writeConnectionSecret upserts the full clickhouse_* connection shape into the
// secret named secretName/ns. plaintext may be "" (hash-only path); when empty,
// password/encoded are skipped and the sha256 key is taken from existingHash.
func writeConnectionSecret(ctx context.Context, c client.Client, mg xpresource.Managed, secretName, ns, plaintext, existingHash string, info ConnInfo) error {
	s := &corev1.Secret{}
	s.SetName(secretName)
	s.SetNamespace(ns)
	s.Type = xpresource.SecretTypeConnection
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, s); xpresource.IgnoreNotFound(err) != nil {
		return fmt.Errorf("cannot get connection secret: %w", err)
	}
	if s.Data == nil {
		s.Data = make(map[string][]byte, 7)
	}

	k := info.Keys
	s.Data[orDefault(k.Username, keyUsername)] = []byte(connectionUsername(mg))
	s.Data[orDefault(k.Host, keyHost)] = []byte(info.Host)
	s.Data[orDefault(k.Port, keyPort)] = []byte(strconv.Itoa(int(info.Port)))
	s.Data[orDefault(k.Protocol, keyProtocol)] = []byte(info.Protocol)

	sha := existingHash
	if plaintext != "" {
		sum := sha256.Sum256([]byte(plaintext))
		sha = hex.EncodeToString(sum[:])
		s.Data[orDefault(k.Password, keyPassword)] = []byte(plaintext)
		s.Data[orDefault(k.PasswordEncoded, keyPwEncoded)] = []byte(url.QueryEscape(plaintext))
	}
	if sha != "" {
		s.Data[orDefault(k.PasswordSHA256, keyPwSHA256)] = []byte(sha)
	}

	if err := xpresource.NewAPIPatchingApplicator(c).Apply(ctx, s); err != nil {
		return fmt.Errorf("cannot apply connection secret: %w", err)
	}
	return nil
}
