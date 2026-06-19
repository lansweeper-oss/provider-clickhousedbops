package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeKube(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("cannot add corev1 to scheme: %v", err)
	}
	return clientfake.NewClientBuilder().WithScheme(s).Build()
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestWriteConnectionSecret(t *testing.T) {
	const (
		secName = "conn"
		secNS   = "default"
		user    = "my_user"
	)
	base := ConnInfo{Host: "ch.example.com", Port: 8443, Protocol: "https"}

	cases := map[string]struct {
		info         ConnInfo
		plaintext    string
		existingHash string

		wantData   map[string]string // exact decoded value per key
		wantAbsent []string
	}{
		"PlaintextFullShape": {
			info:      base,
			plaintext: "p@ss w/rd&x",
			wantData: map[string]string{
				keyUsername:  user,
				keyHost:      "ch.example.com",
				keyPort:      "8443",
				keyProtocol:  "https",
				keyPassword:  "p@ss w/rd&x",
				keyPwEncoded: url.QueryEscape("p@ss w/rd&x"),
				keyPwSHA256:  sha256hex("p@ss w/rd&x"),
			},
			wantAbsent: []string{"password", "hash"},
		},
		"HashOnlyNoPlaintext": {
			info:         base,
			plaintext:    "",
			existingHash: "deadbeef",
			wantData: map[string]string{
				keyUsername: user,
				keyHost:     "ch.example.com",
				keyPort:     "8443",
				keyProtocol: "https",
				keyPwSHA256: "deadbeef",
			},
			wantAbsent: []string{keyPassword, keyPwEncoded, "password", "hash"},
		},
		"OverriddenKeys": {
			info: ConnInfo{
				Host: "h", Port: 9440, Protocol: "nativesecure",
				Keys: ConnectionKeyOverrides{
					Username:       "ch_user",
					Host:           "ch_host",
					PasswordSHA256: "ch_hash",
				},
			},
			plaintext: "pw",
			wantData: map[string]string{
				"ch_user":    user,
				"ch_host":    "h",
				keyPort:      "9440", // not overridden -> default
				keyProtocol:  "nativesecure",
				keyPassword:  "pw", // not overridden -> default
				keyPwEncoded: "pw",
				"ch_hash":    sha256hex("pw"),
			},
			wantAbsent: []string{keyUsername, keyHost, keyPwSHA256},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kube := newFakeKube(t)
			mg := &fake.Managed{}
			meta.SetExternalName(mg, user)

			err := writeConnectionSecret(context.Background(), kube, mg, secName, secNS, tc.plaintext, tc.existingHash, tc.info)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			s := &corev1.Secret{}
			if err := kube.Get(context.Background(), types.NamespacedName{Namespace: secNS, Name: secName}, s); err != nil {
				t.Fatalf("cannot get written secret: %v", err)
			}
			if s.Type != xpresource.SecretTypeConnection {
				t.Errorf("secret type = %v, want %v", s.Type, xpresource.SecretTypeConnection)
			}
			for k, want := range tc.wantData {
				if got := string(s.Data[k]); got != want {
					t.Errorf("data[%q] = %q, want %q", k, got, want)
				}
			}
			for _, k := range tc.wantAbsent {
				if _, ok := s.Data[k]; ok {
					t.Errorf("data[%q] present, want absent", k)
				}
			}
		})
	}
}

func TestResolveConnInfo(t *testing.T) {
	mg := &fake.Managed{}

	t.Run("NotWired", func(t *testing.T) {
		SetConnParamsResolverFactory(nil)
		if _, err := resolveConnInfo(context.Background(), nil, mg); err == nil {
			t.Fatalf("expected error when resolver not wired")
		}
	})

	t.Run("WrapsResolverError", func(t *testing.T) {
		SetConnParamsResolverFactory(func(_ client.Client) ConnParamsResolver {
			return func(_ context.Context, _ xpresource.Managed) (ConnInfo, error) {
				return ConnInfo{}, errors.New("incomplete connection parameters")
			}
		})
		t.Cleanup(func() { SetConnParamsResolverFactory(nil) })
		if _, err := resolveConnInfo(context.Background(), nil, mg); err == nil {
			t.Fatalf("expected error from resolver")
		}
	})

	t.Run("ReturnsInfo", func(t *testing.T) {
		want := ConnInfo{Host: "h", Port: 1, Protocol: "http"}
		SetConnParamsResolverFactory(func(_ client.Client) ConnParamsResolver {
			return func(_ context.Context, _ xpresource.Managed) (ConnInfo, error) { return want, nil }
		})
		t.Cleanup(func() { SetConnParamsResolverFactory(nil) })
		got, err := resolveConnInfo(context.Background(), nil, mg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}
