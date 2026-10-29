package cache

import (
	"context"

	"github.com/micro/micro/v3/service/registry"
)

// Snapshot bypasses discovery caches and delegates only to the authoritative
// capability. An unsupported backing registry remains unsupported.
func (c *cache) Snapshot(ctx context.Context, query registry.SnapshotQuery) (*registry.Snapshot, error) {
	return registry.ReadSnapshot(ctx, c.Registry, query)
}
