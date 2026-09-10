package clients

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v2 "github.com/crossplane/crossplane/apis/v2/core/v2"

	"github.com/lansweeper-oss/provider-clickhousedbops/apis/namespaced/clickhousedbops/v1alpha1"
)

const (
	testHash   = "aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f"
	importUUID = "11111111-2222-3333-4444-555555555555"
)

func testUser(name string, mut ...func(*v1alpha1.User)) *v1alpha1.User {
	u := &v1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: "user-" + name, Namespace: "test-ns"},
		Spec: v1alpha1.UserSpec{
			ForProvider: v1alpha1.UserParameters{
				Name: &name,
				PasswordSha256HashSecretRef: &v2.LocalSecretKeySelector{
					LocalSecretReference: v2.LocalSecretReference{Name: "user-password"},
					Key:                  "hash",
				},
			},
		},
	}
	meta.SetExternalName(u, importUUID)
	for _, m := range mut {
		m(u)
	}
	return u
}

func hashSecret(key, value string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "user-password", Namespace: "test-ns"},
		Data:       map[string][]byte{key: []byte(value)},
	}
}

func TestApplyAdoptedUserPassword(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	okResolve := func(_ context.Context, _ client.Client, _ xpresource.Managed) (ConnParams, error) {
		return ConnParams{Host: "ch.example", Port: 9440, Username: "default", Protocol: "nativesecure"}, nil
	}

	cases := map[string]struct {
		user       *v1alpha1.User
		secret     *corev1.Secret
		resolveErr error
		execErr    error

		lookupUUID   string // UUID returned for the name lookup; defaults to importUUID
		lookupAbsent bool   // name lookup finds no user
		lookupErr    error

		wantSQL      string // "" means exec must not be called
		wantErrPart  string // "" means no error expected
		wantErrClean string // substring that must NOT appear in the error
	}{
		"AppliesHashOnAdoption": {
			user:    testUser("app_user"),
			secret:  hashSecret("hash", testHash),
			wantSQL: "ALTER USER `app_user` IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
		},
		"HonoursClusterName": {
			user: testUser("app_user", func(u *v1alpha1.User) {
				c := "main"
				u.Spec.ForProvider.ClusterName = &c
			}),
			secret:  hashSecret("hash", testHash),
			wantSQL: "ALTER USER `app_user` ON CLUSTER 'main' IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
		},
		"EscapesBackticksInUserName": {
			user:    testUser("we`ird"),
			secret:  hashSecret("hash", testHash),
			wantSQL: "ALTER USER `we\\`ird` IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
		},
		"EscapesBackslashesInUserName": {
			user:    testUser("back\\slash"),
			secret:  hashSecret("hash", testHash),
			wantSQL: "ALTER USER `back\\\\slash` IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
		},
		"EscapesQuotesAndBackslashesInClusterName": {
			// \ escaped before ' so a trailing backslash can't re-open the literal.
			user: testUser("app_user", func(u *v1alpha1.User) {
				c := "c\\'x"
				u.Spec.ForProvider.ClusterName = &c
			}),
			secret:  hashSecret("hash", testHash),
			wantSQL: "ALTER USER `app_user` ON CLUSTER 'c\\\\\\'x' IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
		},
		"RejectsInvalidHash": {
			user:        testUser("app_user"),
			secret:      hashSecret("hash", "not-a-sha256-hash'; DROP USER admin --"),
			wantErrPart: "not a sha256 hex digest",
		},
		"ErrorWithoutHashSecretRef": {
			// No hash ref = adoption would keep the unknown password (#104);
			// must fail loudly.
			user: testUser("app_user", func(u *v1alpha1.User) {
				u.Spec.ForProvider.PasswordSha256HashSecretRef = nil
			}),
			wantErrPart: "passwordSha256HashSecretRef is not set",
		},
		"ErrorWhenSecretMissing": {
			user:        testUser("app_user"),
			wantErrPart: "cannot read password hash secret",
		},
		"ErrorWhenKeyMissing": {
			user:        testUser("app_user"),
			secret:      hashSecret("wrongkey", testHash),
			wantErrPart: "key \"hash\" not found",
		},
		"ResolveErrorPropagates": {
			user:        testUser("app_user"),
			secret:      hashSecret("hash", testHash),
			resolveErr:  errors.New("no provider config"),
			wantErrPart: "no provider config",
		},
		"ExecErrorPropagates": {
			user:        testUser("app_user"),
			secret:      hashSecret("hash", testHash),
			execErr:     errors.New("connection refused"),
			wantSQL:     "ALTER USER `app_user` IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
			wantErrPart: "connection refused",
		},
		"ErrorOnUUIDNameMismatch": {
			// The pinned UUID and spec name identify different users: applying
			// the password by name would rewrite an unrelated user's credential.
			user:        testUser("app_user"),
			secret:      hashSecret("hash", testHash),
			lookupUUID:  "99999999-9999-9999-9999-999999999999",
			wantErrPart: "does not match",
		},
		"ErrorWhenNamedUserAbsent": {
			user:         testUser("app_user"),
			secret:       hashSecret("hash", testHash),
			lookupAbsent: true,
			wantErrPart:  "no ClickHouse user named",
		},
		"LookupErrorPropagates": {
			user:        testUser("app_user"),
			secret:      hashSecret("hash", testHash),
			lookupErr:   errors.New("system.users unreachable"),
			wantErrPart: "system.users unreachable",
		},
		"ClusterPrefixedExternalNameMatches": {
			user: testUser("app_user", func(u *v1alpha1.User) {
				meta.SetExternalName(u, "main:"+importUUID)
			}),
			secret:  hashSecret("hash", testHash),
			wantSQL: "ALTER USER `app_user` IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
		},
		"ExecErrorRedactsHash": {
			// ClickHouse errors echo the statement; the hash must not leak
			// into the Synced condition.
			user:         testUser("app_user"),
			secret:       hashSecret("hash", testHash),
			execErr:      errors.New("DB::Exception: Syntax error near IDENTIFIED WITH sha256_hash BY '" + testHash + "'"),
			wantSQL:      "ALTER USER `app_user` IDENTIFIED WITH sha256_hash BY '" + testHash + "'",
			wantErrPart:  "[redacted]",
			wantErrClean: testHash,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := fake.NewClientBuilder().WithScheme(scheme)
			if tc.secret != nil {
				b = b.WithObjects(tc.secret)
			}
			kube := b.Build()

			resolve := okResolve
			if tc.resolveErr != nil {
				resolve = func(_ context.Context, _ client.Client, _ xpresource.Managed) (ConnParams, error) {
					return ConnParams{}, tc.resolveErr
				}
			}

			gotSQL := ""
			exec := func(_ context.Context, _ ConnParams, sql string) error {
				gotSQL = sql
				return tc.execErr
			}

			lookup := func(_ context.Context, _ ConnParams, _, _, name, _ string) (string, bool, error) {
				if tc.lookupErr != nil {
					return "", false, tc.lookupErr
				}
				if tc.lookupAbsent {
					return "", false, nil
				}
				if tc.lookupUUID != "" {
					return tc.lookupUUID, true, nil
				}
				return importUUID, true, nil
			}

			err := applyAdoptedUserPassword(context.Background(), kube, tc.user, resolve, exec, lookup)

			if tc.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErrPart)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErrClean != "" && err != nil && strings.Contains(err.Error(), tc.wantErrClean) {
				t.Fatalf("error %q leaks %q", err, tc.wantErrClean)
			}
			// Unconditional: wantSQL "" means exec must not have been called,
			// error cases included.
			if gotSQL != tc.wantSQL {
				t.Errorf("sql = %q, want %q", gotSQL, tc.wantSQL)
			}
		})
	}
}
