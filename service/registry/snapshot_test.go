package registry

import (
	"context"
	"errors"
	"testing"
	"time"
)

type snapshotProbe struct {
	Registry
	read func(context.Context, SnapshotQuery) (*Snapshot, error)
}

func (s snapshotProbe) Snapshot(ctx context.Context, q SnapshotQuery) (*Snapshot, error) {
	return s.read(ctx, q)
}

func TestSnapshotRequiresCapabilityAndBoundedScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	query := SnapshotQuery{Domain: "test", MaxNodes: 10, MaxBytes: 4096}
	if _, err := ReadSnapshot(ctx, nil, query); !errors.Is(err, ErrSnapshotUnsupported) {
		t.Fatalf("unsupported registry fell back: %v", err)
	}
	called := false
	source := snapshotProbe{read: func(context.Context, SnapshotQuery) (*Snapshot, error) {
		called = true
		return &Snapshot{Revision: "1"}, nil
	}}
	for _, q := range []SnapshotQuery{{Domain: "*", MaxNodes: 10, MaxBytes: 4096}, {Domain: "other/test", MaxNodes: 10, MaxBytes: 4096}, {Domain: "test", Service: "*", MaxNodes: 10, MaxBytes: 4096}, {Domain: "test", MaxBytes: 4096}, {Domain: "test", MaxNodes: 10}} {
		if _, err := ReadSnapshot(ctx, source, q); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
	if _, err := ReadSnapshot(context.Background(), source, query); err == nil {
		t.Fatal("unbounded context accepted")
	}
	if called {
		t.Fatal("invalid request reached adapter")
	}
	if got, err := ReadSnapshot(ctx, source, query); err != nil || got.Revision != "1" {
		t.Fatalf("valid empty snapshot: %v %v", got, err)
	}
}

func TestSnapshotRejectsPartialAndExpiredResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	q := SnapshotQuery{Domain: "test", Service: "game", MaxNodes: 1, MaxBytes: 4096}
	valid := func() *Snapshot {
		return &Snapshot{Revision: "opaque", Services: []*Service{{Name: "game", Metadata: map[string]string{"domain": "test"}, Nodes: []*Node{{Id: "owner"}}}}}
	}
	for name, corrupt := range map[string]func(*Snapshot){
		"revision": func(s *Snapshot) { s.Revision = "" },
		"domain":   func(s *Snapshot) { s.Services[0].Metadata["domain"] = "other" },
		"service":  func(s *Snapshot) { s.Services[0].Name = "other" },
		"node":     func(s *Snapshot) { s.Services[0].Nodes[0] = nil },
		"limit":    func(s *Snapshot) { s.Services[0].Nodes = append(s.Services[0].Nodes, &Node{Id: "second"}) },
	} {
		t.Run(name, func(t *testing.T) {
			s := valid()
			corrupt(s)
			source := snapshotProbe{read: func(context.Context, SnapshotQuery) (*Snapshot, error) { return s, nil }}
			if got, err := ReadSnapshot(ctx, source, q); err == nil || got != nil {
				t.Fatal("partial result accepted")
			}
		})
	}
	source := snapshotProbe{read: func(context.Context, SnapshotQuery) (*Snapshot, error) { cancel(); return valid(), nil }}
	if got, err := ReadSnapshot(ctx, source, q); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("expired result accepted")
	}
}
