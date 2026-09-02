package clients

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

const defaultCacheTTL = 30 * time.Minute

type cachedConfigEntry struct {
	resourceData   any
	dataSourceData any
	createdAt      time.Time
}

// cachingProvider works around a memory leak in upjet's Plugin Framework path.
//
// On every reconcile, upjet's TerraformPluginFrameworkConnector.Connect() calls
// configureProvider(), which creates a new providerserver and invokes the
// underlying TF provider's Configure(). For our ClickHouse TF provider, each
// Configure() call opens a new connection pool (clickhouse.Open). However,
// upjet's Disconnect() is a no-op and never closes these pools.
//
// This wrapper intercepts Configure() and returns a cached client when the
// configuration (host, port, credentials) hasn't changed. The cache is keyed
// by a SHA-256 hash of the serialized config. This reduces connection pool
// creation from once-per-reconcile to once-per-unique-config.
//
// Entries are evicted after a configurable TTL (--client-cache-ttl / CLIENT_CACHE_TTL)
// so that credential rotations are picked up and stale entries don't accumulate.
type cachingProvider struct {
	inner  provider.Provider
	logger logging.Logger
	mu     sync.Mutex
	cache  map[string]*cachedConfigEntry
	ttl    time.Duration
	now    func() time.Time
}

// NewCachingProvider wraps a provider.Provider so that repeated Configure calls
// with identical configuration reuse the previously created client.
// The ttl controls how long a cached client lives before re-creation.
// Set to 0 to disable caching entirely; negative values fall back to 30m default.
func NewCachingProvider(inner provider.Provider, logger logging.Logger, ttl time.Duration) provider.Provider {
	if ttl < 0 {
		ttl = defaultCacheTTL
	}
	return &cachingProvider{
		inner:  inner,
		logger: logger,
		cache:  make(map[string]*cachedConfigEntry),
		ttl:    ttl,
		now:    time.Now,
	}
}

func (p *cachingProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	p.inner.Metadata(ctx, req, resp)
}

func (p *cachingProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	p.inner.Schema(ctx, req, resp)
}

func (p *cachingProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	h := configHash(req)
	now := p.now()

	p.mu.Lock()
	p.evictExpired(now)
	if entry, ok := p.cache[h]; ok {
		p.mu.Unlock()
		resp.ResourceData = entry.resourceData
		resp.DataSourceData = entry.dataSourceData
		return
	}
	p.mu.Unlock()

	p.inner.Configure(ctx, req, resp)
	if resp.Diagnostics.HasError() || resp.ResourceData == nil {
		return
	}

	p.logger.Debug("Cached new provider configuration", "hash", h[:12])

	p.mu.Lock()
	p.cache[h] = &cachedConfigEntry{
		resourceData:   resp.ResourceData,
		dataSourceData: resp.DataSourceData,
		createdAt:      now,
	}
	p.mu.Unlock()
}

// evictExpired removes entries older than TTL. Must be called with p.mu held.
func (p *cachingProvider) evictExpired(now time.Time) {
	for k, entry := range p.cache {
		if now.Sub(entry.createdAt) > p.ttl {
			p.logger.Debug("Evicting expired provider configuration", "hash", k[:12])
			delete(p.cache, k)
		}
	}
}

func (p *cachingProvider) Resources(ctx context.Context) []func() resource.Resource {
	return p.inner.Resources(ctx)
}

func (p *cachingProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return p.inner.DataSources(ctx)
}

func configHash(req provider.ConfigureRequest) string {
	raw := req.Config.Raw.String()
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}
