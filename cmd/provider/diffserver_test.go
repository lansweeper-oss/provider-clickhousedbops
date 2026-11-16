package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// The manifests below reference ProviderConfigs and Secrets that exist
// nowhere, and cluster names whose plan checks would query ClickHouse if the
// provider had a client: the offline diff server needs none of them.
const (
	databaseActual = `
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: Database
metadata:
  name: logs
  annotations:
    crossplane.io/external-name: 0b5b1b8e-6a5b-4b8e-9a7e-1f2d3c4b5a69
spec:
  providerConfigRef:
    name: missing
  forProvider:
    name: logs
    clusterName: cluster
    comment: application logs
status:
  atProvider:
    uuid: 0b5b1b8e-6a5b-4b8e-9a7e-1f2d3c4b5a69
    name: logs
    clusterName: cluster
    comment: application logs
`
	databaseRenamed = `
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: Database
metadata:
  name: logs
  annotations:
    crossplane.io/external-name: 0b5b1b8e-6a5b-4b8e-9a7e-1f2d3c4b5a69
spec:
  providerConfigRef:
    name: missing
  forProvider:
    name: logs-v2
    clusterName: cluster
    comment: application logs
`
	namespacedRole = `
apiVersion: clickhousedbops.m.crossplane.io/v1alpha1
kind: Role
metadata:
  name: reader
  namespace: default
spec:
  providerConfigRef:
    kind: ClusterProviderConfig
    name: missing
  forProvider:
    name: reader
    clusterName: cluster
`
)

// TestDiffServerPlan drives the diff gRPC server the way a client does, over a
// unix socket, wired exactly as main wires it.
func TestDiffServerPlan(t *testing.T) {
	ctx := context.Background()
	s, scheme, err := newDiffServer(logging.NewNopLogger())
	require.NoError(t, err)

	// A short path: unix socket paths are limited to ~104 bytes on macOS.
	dir, err := os.MkdirTemp("", "diff")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "diff.sock")

	serveCtx, cancel := context.WithCancel(ctx)
	served := make(chan error, 1)
	go func() { served <- s.Serve(serveCtx, "unix", sock, scheme) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-served)
	})

	conn, err := grpc.NewClient("unix://"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := diffv1alpha1.NewPlanServiceClient(conn)

	cases := map[string]struct {
		desired, actual string
		action          diffv1alpha1.Action
		changes         map[string]string
	}{
		"ClusterDatabaseCreate": {
			desired: databaseActual,
			action:  diffv1alpha1.Action_ACTION_CREATE,
			changes: map[string]string{
				"spec.forProvider.name":        "<unset> -> logs",
				"spec.forProvider.clusterName": "<unset> -> cluster",
				"spec.forProvider.comment":     "<unset> -> application logs",
			},
		},
		"ClusterDatabaseNoOp": {
			desired: databaseActual,
			actual:  databaseActual,
			action:  diffv1alpha1.Action_ACTION_NO_OP,
		},
		"ClusterDatabaseRenameReplaces": {
			desired: databaseRenamed,
			actual:  databaseActual,
			action:  diffv1alpha1.Action_ACTION_REPLACE,
			changes: map[string]string{
				"spec.forProvider.name": "logs -> logs-v2 (requires replace)",
			},
		},
		"NamespacedRoleCreate": {
			desired: namespacedRole,
			action:  diffv1alpha1.Action_ACTION_CREATE,
			changes: map[string]string{
				"spec.forProvider.name":        "<unset> -> reader",
				"spec.forProvider.clusterName": "<unset> -> cluster",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			callCtx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			rsp, err := client.Plan(callCtx, &diffv1alpha1.PlanRequest{
				DesiredResource: asStruct(t, tc.desired),
				ActualResource:  asStruct(t, tc.actual),
			}, grpc.WaitForReady(true))
			require.NoError(t, err)
			assert.Equal(t, tc.action.String(), rsp.GetAction().String())
			assert.Equal(t, tc.changes, declaredChanges(rsp))
		})
	}
}

// declaredChanges renders the changes to fields the desired resource declares
// as "actual -> planned", leaving out the Terraform provider's own defaults.
func declaredChanges(rsp *diffv1alpha1.PlanResponse) map[string]string {
	var changes map[string]string
	for _, c := range rsp.GetChanges() {
		if c.GetOrigin() != diffv1alpha1.Origin_ORIGIN_DESIRED_STATE {
			continue
		}
		change := renderValue(c.GetActual()) + " -> " + renderValue(c.GetPlanned())
		if c.GetRequiresReplace() {
			change += " (requires replace)"
		}
		if changes == nil {
			changes = map[string]string{}
		}
		changes[c.GetField()] = change
	}
	return changes
}

func renderValue(v *diffv1alpha1.FieldValue) string {
	switch {
	case v == nil:
		return "<unset>"
	case v.GetValue() != nil:
		return fmt.Sprintf("%v", v.GetValue().AsInterface())
	default:
		return v.GetAbsence().String()
	}
}

// asStruct converts a YAML manifest into a plan request's protobuf Struct. An
// empty manifest is nil: a resource that does not exist yet.
func asStruct(t *testing.T, manifest string) *structpb.Struct {
	t.Helper()
	if manifest == "" {
		return nil
	}
	j, err := yaml.ToJSON([]byte(manifest))
	require.NoError(t, err)
	s := &structpb.Struct{}
	require.NoError(t, s.UnmarshalJSON(j))
	return s
}
