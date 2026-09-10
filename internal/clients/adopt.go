package clients

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/lansweeper-oss/provider-clickhousedbops/config"
)

// sha256HexRe matches a lowercase hex SHA256 digest; validation makes the hash
// safe to interpolate into SQL.
var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type connResolver func(ctx context.Context, kube client.Client, mg xpresource.Managed) (ConnParams, error)
type stmtExec func(ctx context.Context, params ConnParams, sql string) error

// NewUserAdoptPasswordApplier returns the AdoptHook for clickhousedbops_user:
// it applies the spec's password hash to an adopted user, whose live password is
// otherwise unknown and unrepairable (Observe never reads it, Update only
// renames). Fires only on explicit UUID import - users have no name resolver (#104).
func NewUserAdoptPasswordApplier(kube client.Client) config.AdoptHook {
	return func(ctx context.Context, mg xpresource.Managed) error {
		return applyAdoptedUserPassword(ctx, kube, mg, ResolveConnParams, execStatement)
	}
}

func applyAdoptedUserPassword(ctx context.Context, kube client.Client, mg xpresource.Managed, resolve connResolver, exec stmtExec) error {
	paved, err := fieldpath.PaveObject(mg)
	if err != nil {
		return fmt.Errorf("cannot pave managed resource: %w", err)
	}
	name, err := paved.GetString("spec.forProvider.name")
	if err != nil {
		return fmt.Errorf("cannot read spec.forProvider.name: %w", err)
	}
	// Optional - absent on single node / ClickHouse Cloud.
	cluster, _ := paved.GetString("spec.forProvider.clusterName")

	hash, found, err := readPasswordHash(ctx, kube, mg, paved)
	if err != nil {
		return err
	}
	if !found {
		// No hash to apply = adopted user keeps its unknown password (#104).
		// Only reachable: autoGeneratePassword without writeConnectionSecretToRef.
		return fmt.Errorf("cannot apply password to adopted user %q: spec.forProvider.passwordSha256HashSecretRef is not set (autoGeneratePassword requires writeConnectionSecretToRef)", name)
	}

	params, err := resolve(ctx, kube, mg)
	if err != nil {
		return fmt.Errorf("cannot resolve connection params: %w", err)
	}

	if err := exec(ctx, params, alterUserPasswordSQL(name, cluster, hash)); err != nil {
		// ClickHouse errors may echo the statement; redact the hash before it
		// reaches the Synced condition.
		redacted := errors.New(strings.ReplaceAll(err.Error(), hash, "[redacted]"))
		return fmt.Errorf("cannot apply password hash to adopted user %q: %w", name, redacted)
	}
	return nil
}

// readPasswordHash loads and validates the hash from
// spec.forProvider.passwordSha256HashSecretRef; found=false means the ref is absent.
func readPasswordHash(ctx context.Context, kube client.Client, mg xpresource.Managed, paved *fieldpath.Paved) (hash string, found bool, err error) {
	refName, err := paved.GetString("spec.forProvider.passwordSha256HashSecretRef.name")
	if fieldpath.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cannot read passwordSha256HashSecretRef: %w", err)
	}
	refKey, err := paved.GetString("spec.forProvider.passwordSha256HashSecretRef.key")
	if err != nil {
		return "", false, fmt.Errorf("cannot read passwordSha256HashSecretRef.key: %w", err)
	}
	ns, _ := paved.GetString("spec.forProvider.passwordSha256HashSecretRef.namespace")
	if ns == "" {
		ns = mg.GetNamespace()
	}

	s := &corev1.Secret{}
	if err := kube.Get(ctx, types.NamespacedName{Namespace: ns, Name: refName}, s); err != nil {
		return "", false, fmt.Errorf("cannot read password hash secret %s/%s: %w", ns, refName, err)
	}
	raw, ok := s.Data[refKey]
	if !ok {
		return "", false, fmt.Errorf("key %q not found in secret %s/%s", refKey, ns, refName)
	}
	hash = strings.ToLower(strings.TrimSpace(string(raw)))
	if !sha256HexRe.MatchString(hash) {
		return "", false, fmt.Errorf("value of key %q in secret %s/%s is not a sha256 hex digest", refKey, ns, refName)
	}
	return hash, true, nil
}

// alterUserPasswordSQL builds the ALTER USER statement; hash pre-validated,
// name backtick-quoted, cluster escaped as string literal.
func alterUserPasswordSQL(name, cluster, hash string) string {
	onCluster := ""
	if cluster != "" {
		onCluster = fmt.Sprintf(" ON CLUSTER '%s'", escapeStringLit(cluster))
	}
	return fmt.Sprintf("ALTER USER %s%s IDENTIFIED WITH sha256_hash BY '%s'", quoteIdent(name), onCluster, hash)
}

// quoteIdent backtick-quotes a ClickHouse identifier, escaping backslashes and backticks.
func quoteIdent(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "`", "\\`")
	return "`" + s + "`"
}

// escapeStringLit escapes for a single-quoted ClickHouse string literal;
// backslashes before quotes, or `x\'` would re-open the literal.
func escapeStringLit(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// execStatement runs one statement using provider config connection params.
func execStatement(ctx context.Context, params ConnParams, sql string) error {
	conn, err := openConn(params)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	return conn.Exec(ctx, sql)
}
