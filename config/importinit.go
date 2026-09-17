package config

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UUIDResolver looks up the provider-assigned UUID of an existing resource by name
type UUIDResolver func(ctx context.Context, mg xpresource.Managed) (uuid string, found bool, err error)

// resolverFactories maps a Terraform resource name to the factory that builds its UUIDResolver.
var resolverFactories = map[string]func(client.Client) UUIDResolver{}

// SetResolverFactory registers the UUIDResolver factory for a resource.
func SetResolverFactory(resourceName string, f func(client.Client) UUIDResolver) {
	resolverFactories[resourceName] = f
}

// adoptByNameInitializer seeds the resource identifier before the first observe so
// Upjet's UUID-based Read adopts an existing resource instead of re-creating it.
// A real UUID already in the observation (post-import/creation) is left untouched.
// See docs/import.md for the rationale.
func adoptByNameInitializer(resourceName, field string) config.NewInitializerFn {
	return func(kube client.Client) managed.Initializer {
		return managed.InitializerFn(func(ctx context.Context, mg xpresource.Managed) error {
			tr, ok := mg.(terraformedObservation)
			if !ok {
				return nil
			}
			obs, err := tr.GetObservation()
			if err != nil {
				return fmt.Errorf("cannot get observation for %s import initializer: %w", resourceName, err)
			}
			if val, _ := obs[field].(string); val != "" && val != sentinelUUID {
				return nil
			}
			if obs == nil {
				obs = make(map[string]any)
			}
			return seedImportIdentifier(ctx, kube, mg, tr, obs, resourceName, field)
		})
	}
}

// seedImportIdentifier honors an external name that is a UUID (import by UUID),
// otherwise resolves the UUID from spec.forProvider.name (import by name), or seeds
// the sentinel (force-create) when the resource is absent or no resolver is wired.
func seedImportIdentifier(ctx context.Context, kube client.Client, mg xpresource.Managed, tr terraformedObservation, obs map[string]any, resourceName, field string) error {
	if en := stripClusterPrefix(meta.GetExternalName(mg), sep); en != "" {
		if _, err := uuid.Parse(en); err == nil {
			return seedIdentifier(mg, tr, obs, field, en)
		}
	}

	id, found := sentinelUUID, false
	if factory := resolverFactories[resourceName]; factory != nil {
		var err error
		if id, found, err = factory(kube)(ctx, mg); err != nil {
			return fmt.Errorf("cannot resolve UUID for import of %s: %w", resourceName, err)
		}
	}
	if !found {
		if err := guardConflictOnCreate(ctx, kube, mg); err != nil {
			return err
		}
		obs[field] = sentinelUUID
		return tr.SetObservation(obs)
	}
	if err := guardConflictOnAdopt(ctx, kube, mg, id, field); err != nil {
		return err
	}
	return seedIdentifier(mg, tr, obs, field, id)
}

// guardConflictOnCreate checks name uniqueness before seeding the sentinel (create path).
func guardConflictOnCreate(ctx context.Context, kube client.Client, mg xpresource.Managed) error {
	if isObserveOnly(mg) {
		return nil
	}
	chName, _ := extractResourceName(mg)
	if chName == "" {
		return nil
	}
	return ensureNoPeerConflict(ctx, kube, mg, chName, "name", peerForProviderName)
}

// guardConflictOnAdopt checks UUID uniqueness before adopting an existing resource.
// field is the observation key (e.g. "id" or "uuid") used to find the peer's UUID
// in status.atProvider, since the external-name annotation may not be persisted.
func guardConflictOnAdopt(ctx context.Context, kube client.Client, mg xpresource.Managed, id, field string) error {
	if isObserveOnly(mg) {
		return nil
	}
	return ensureNoPeerConflict(ctx, kube, mg, id, "UUID", peerAtProvider(field))
}

// peerFieldExtractor reads a comparison value from an unstructured peer resource.
type peerFieldExtractor func(item *unstructured.Unstructured) string

// ensureNoPeerConflict lists all managed resources of the same GVK and rejects
// if any peer (different UID, non-observe) already holds the given value for the
// field described by extract.
func ensureNoPeerConflict(ctx context.Context, kube client.Client, mg xpresource.Managed, value, description string, extract peerFieldExtractor) error {
	gvk, err := resolveGVK(kube, mg)
	if err != nil {
		return fmt.Errorf("cannot determine GVK for %s conflict check: %w", description, err)
	}
	listGVK := schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(listGVK)
	if err := kube.List(ctx, list); err != nil {
		return fmt.Errorf("cannot list %s resources for %s conflict check: %w", gvk.Kind, description, err)
	}
	for i := range list.Items {
		item := &list.Items[i]
		if item.GetUID() == mg.GetUID() {
			continue
		}
		if isUnstructuredObserveOnly(item) {
			continue
		}
		if extract(item) == value {
			return fmt.Errorf(
				"%s %s/%s already manages ClickHouse resource with %s %q; "+
					"remove the conflicting resource first",
				gvk.Kind, item.GetNamespace(), item.GetName(), description, value,
			)
		}
	}
	return nil
}

// resolveGVK returns the GVK for a managed resource. It prefers the scheme
// (works even when controller-runtime cache strips TypeMeta) and falls back
// to GetObjectKind.
func resolveGVK(kube client.Client, mg xpresource.Managed) (schema.GroupVersionKind, error) {
	gvks, _, err := kube.Scheme().ObjectKinds(mg)
	if err == nil && len(gvks) > 0 {
		return gvks[0], nil
	}
	gvk := mg.GetObjectKind().GroupVersionKind()
	if gvk.Kind != "" {
		return gvk, nil
	}
	return schema.GroupVersionKind{}, fmt.Errorf("scheme lookup failed (%w) and TypeMeta empty", err)
}

// peerAtProvider returns an extractor that reads status.atProvider.<field> from a peer.
// This is more reliable than the external-name annotation, which may not be persisted.
func peerAtProvider(field string) peerFieldExtractor {
	return func(item *unstructured.Unstructured) string {
		val, _, _ := unstructured.NestedString(item.Object, "status", "atProvider", field)
		return val
	}
}

// peerForProviderName extracts spec.forProvider.name from a peer resource.
func peerForProviderName(item *unstructured.Unstructured) string {
	name, _, _ := unstructured.NestedString(item.Object, "spec", "forProvider", "name")
	return name
}

// extractResourceName reads spec.forProvider.name from a managed resource via JSON round-trip.
func extractResourceName(mg xpresource.Managed) (string, error) {
	data, err := json.Marshal(mg)
	if err != nil {
		return "", fmt.Errorf("cannot marshal managed resource: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("cannot unmarshal managed resource: %w", err)
	}
	spec, _ := raw["spec"].(map[string]any)
	fp, _ := spec["forProvider"].(map[string]any)
	name, _ := fp["name"].(string)
	return name, nil
}

// isUnstructuredObserveOnly checks if an unstructured resource has Observe-only management policy.
func isUnstructuredObserveOnly(item *unstructured.Unstructured) bool {
	policies, found, err := unstructured.NestedStringSlice(item.Object, "spec", "managementPolicies")
	if err != nil || !found {
		return false
	}
	return len(policies) == 1 && policies[0] == "Observe"
}

// seedIdentifier writes id to both the observation and the external name
func seedIdentifier(mg xpresource.Managed, tr terraformedObservation, obs map[string]any, field, id string) error {
	meta.SetExternalName(mg, id)
	obs[field] = id
	return tr.SetObservation(obs)
}
