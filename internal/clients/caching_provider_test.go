package clients

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClient struct{ id int }

type fakeProvider struct {
	configureCalls atomic.Int64
	nextID         atomic.Int64
}

func (f *fakeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "fake"
}

func (f *fakeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{Required: true},
		},
	}
}

func (f *fakeProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	f.configureCalls.Add(1)
	id := f.nextID.Add(1)
	resp.ResourceData = &fakeClient{id: int(id)}
	resp.DataSourceData = resp.ResourceData
}

func (f *fakeProvider) Resources(_ context.Context) []func() resource.Resource       { return nil }
func (f *fakeProvider) DataSources(_ context.Context) []func() datasource.DataSource { return nil }

func makeConfigureRequest(host string) provider.ConfigureRequest {
	ty := tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"host": tftypes.String,
		},
	}
	val := tftypes.NewValue(ty, map[string]tftypes.Value{
		"host": tftypes.NewValue(tftypes.String, host),
	})
	return provider.ConfigureRequest{
		Config: tfsdk.Config{
			Raw: val,
			Schema: schema.Schema{
				Attributes: map[string]schema.Attribute{
					"host": schema.StringAttribute{Required: true},
				},
			},
		},
	}
}

func TestCachingProvider_ReusesClientForSameConfig(t *testing.T) {
	inner := &fakeProvider{}
	cached := NewCachingProvider(inner, logging.NewNopLogger(), 30*time.Minute)
	ctx := context.Background()
	req := makeConfigureRequest("clickhouse.example.com")

	resp1 := &provider.ConfigureResponse{}
	cached.Configure(ctx, req, resp1)
	require.False(t, resp1.Diagnostics.HasError())
	require.NotNil(t, resp1.ResourceData)

	resp2 := &provider.ConfigureResponse{}
	cached.Configure(ctx, req, resp2)
	require.False(t, resp2.Diagnostics.HasError())

	assert.Same(t, resp1.ResourceData, resp2.ResourceData, "same config should return same client")
	assert.Same(t, resp1.DataSourceData, resp2.DataSourceData, "same config should return same datasource client")
	assert.Equal(t, int64(1), inner.configureCalls.Load(), "inner Configure should be called only once")
}

func TestCachingProvider_DifferentConfigGetsDifferentClient(t *testing.T) {
	inner := &fakeProvider{}
	cached := NewCachingProvider(inner, logging.NewNopLogger(), 30*time.Minute)
	ctx := context.Background()

	resp1 := &provider.ConfigureResponse{}
	cached.Configure(ctx, makeConfigureRequest("host-a.example.com"), resp1)

	resp2 := &provider.ConfigureResponse{}
	cached.Configure(ctx, makeConfigureRequest("host-b.example.com"), resp2)

	assert.NotSame(t, resp1.ResourceData, resp2.ResourceData, "different config should return different client")
	assert.Equal(t, int64(2), inner.configureCalls.Load(), "inner Configure should be called for each unique config")
}

func TestCachingProvider_EvictsExpiredEntries(t *testing.T) {
	inner := &fakeProvider{}
	cp := &cachingProvider{
		inner:  inner,
		logger: logging.NewNopLogger(),
		cache:  make(map[string]*cachedConfigEntry),
		ttl:    10 * time.Minute,
		now:    time.Now,
	}

	ctx := context.Background()
	req := makeConfigureRequest("clickhouse.example.com")

	resp1 := &provider.ConfigureResponse{}
	cp.Configure(ctx, req, resp1)
	require.NotNil(t, resp1.ResourceData)
	assert.Equal(t, int64(1), inner.configureCalls.Load())

	// Advance clock past TTL
	cp.now = func() time.Time { return time.Now().Add(11 * time.Minute) }

	resp2 := &provider.ConfigureResponse{}
	cp.Configure(ctx, req, resp2)
	require.NotNil(t, resp2.ResourceData)

	assert.NotSame(t, resp1.ResourceData, resp2.ResourceData, "expired entry should be evicted and reconfigured")
	assert.Equal(t, int64(2), inner.configureCalls.Load(), "inner Configure should be called again after expiry")
}
