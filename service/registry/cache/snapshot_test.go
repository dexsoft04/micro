package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/registry"
)

type backingSnapshot struct {
	registry.Registry
	calls int
}

func (b *backingSnapshot) Snapshot(context.Context, registry.SnapshotQuery) (*registry.Snapshot, error) {
	b.calls++
	return &registry.Snapshot{Revision: "fresh"}, nil
}

func TestSnapshotBypassesCacheAndPreservesUnsupported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	q := registry.SnapshotQuery{Domain: "test", MaxNodes: 10, MaxBytes: 4096}
	b := &backingSnapshot{}
	c := &cache{Registry: b}
	for i := 0; i < 2; i++ {
		if _, err := registry.ReadSnapshot(ctx, c, q); err != nil {
			t.Fatal(err)
		}
	}
	if b.calls != 2 {
		t.Fatal("authoritative result was cached")
	}
	if _, err := registry.ReadSnapshot(ctx, &cache{}, q); !errors.Is(err, registry.ErrSnapshotUnsupported) {
		t.Fatalf("unsupported backing registry fell back: %v", err)
	}
}
