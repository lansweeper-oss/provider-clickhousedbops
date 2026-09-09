package clients

import (
	"context"
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

// sha256HexRe matches a lowercase hex-encoded SHA256 digest. The hash is
// validated before being interpolated into the ALTER USER statement, so no
// other escaping is needed for it.
var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type connResolver func(ctx context.Context, kube client.Client, mg xpresource.Managed) (ConnParams, error)
type stmtExec func(ctx context.Context, params ConnParams, sql string) error

// NewUserAdoptPasswordApplier returns the AdoptHook for clickhousedbops_user.
// Adoption imports an existing ClickHouse user whose password is unknown, while
// the connection secret advertises the password the spec intends. Left alone,
// the two diverge silently: Observe never reads password state and Update only
// renames, so the MR reports Synced/Healthy with a dead credential. The hook
// closes that gap by applying the spec's password hash to the adopted user.
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
	if err != nil || !found {
		return err
	}

	params, err := resolve(ctx, kube, mg)
	if err != nil {
		return fmt.Errorf("cannot resolve connection params: %w", err)
	}

	if err := exec(ctx, params, alterUserPasswordSQL(name, cluster, hash)); err != nil {
		return fmt.Errorf("cannot apply password hash to adopted user %q: %w", name, err)
	}
	return nil
}

// readPasswordHash loads and validates the SHA256 hash referenced by
// spec.forProvider.passwordSha256HashSecretRef. found=false without error means
// no password management is configured (the password initializers always set
// the ref for the autoGeneratePassword and passwordSecretRef flows before
// adoption runs).
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

// alterUserPasswordSQL builds the ALTER USER statement. The hash is already
// validated as hex-64, the identifier is backtick-quoted and the cluster name
// is escaped as a string literal.
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

// escapeStringLit escapes a value for use inside a single-quoted ClickHouse
// string literal. Backslashes must be escaped before quotes, otherwise an
// input like `x\'` would re-open the literal.
func escapeStringLit(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// execStatement runs a single statement against ClickHouse using the provider
// config connection parameters.
func execStatement(ctx context.Context, params ConnParams, sql string) error {
	conn, err := openConn(params)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	return conn.Exec(ctx, sql)
}
