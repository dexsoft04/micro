package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrSnapshotUnsupported = errors.New("authoritative registry snapshots are unsupported")

// Snapshotter is an optional capability. Success proves a complete, linearizable
// read of the requested scope. Implementations must not use discovery caches or
// silently skip malformed records. Failure must not return a partial snapshot.
type Snapshotter interface {
	Snapshot(context.Context, SnapshotQuery) (*Snapshot, error)
}

type SnapshotQuery struct {
	Domain   string
	Service  string // Empty selects every service in the concrete domain.
	MaxNodes int
	MaxBytes int // Bounds both the backend response and the serialized snapshot.
}

type Snapshot struct {
	Services []*Service
	Revision string // Opaque token identifying the backend's consistent read point.
}

// Validate requires an explicit scope and resource budget; wildcard discovery
// and unbounded reads cannot establish authoritative absence.
func (q SnapshotQuery) Validate(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("registry snapshot requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("registry snapshot requires a deadline")
	}
	if !snapshotIdentity(q.Domain) || (q.Service != "" && !snapshotIdentity(q.Service)) || q.MaxNodes <= 0 || q.MaxBytes <= 0 {
		return fmt.Errorf("invalid registry snapshot query")
	}
	return nil
}

func snapshotIdentity(s string) bool {
	return s != "" && s != "." && s != ".." && utf8.ValidString(s) && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "/\\*?[]") && strings.IndexFunc(s, unicode.IsControl) < 0
}

// ReadSnapshot never falls back to ListServices or GetService. Unsupported
// registries and incomplete results remain unknown to the caller.
func ReadSnapshot(ctx context.Context, source Registry, query SnapshotQuery) (*Snapshot, error) {
	if err := query.Validate(ctx); err != nil {
		return nil, err
	}
	reader, ok := source.(Snapshotter)
	if !ok {
		return nil, ErrSnapshotUnsupported
	}
	result, err := reader.Snapshot(ctx, query)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := result.Validate(query); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// Validate checks the public result envelope. Backend adapters must additionally
// establish consistency and completeness before returning it.
func (result *Snapshot) Validate(query SnapshotQuery) error {
	if result == nil || result.Revision == "" {
		return fmt.Errorf("incomplete registry snapshot")
	}
	seen, count := make(map[string]bool), 0
	for _, service := range result.Services {
		if service == nil || !snapshotIdentity(service.Name) || (query.Service != "" && service.Name != query.Service) || service.Metadata["domain"] != query.Domain || len(service.Nodes) == 0 {
			return fmt.Errorf("invalid registry snapshot service")
		}
		for _, node := range service.Nodes {
			if node == nil || node.Id == "" || !utf8.ValidString(node.Id) || strings.IndexFunc(node.Id, unicode.IsControl) >= 0 {
				return fmt.Errorf("invalid registry snapshot node")
			}
			key := service.Name + "\x00" + node.Id
			count++
			if seen[key] || count > query.MaxNodes {
				return fmt.Errorf("duplicate or excessive registry snapshot nodes")
			}
			seen[key] = true
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > query.MaxBytes {
		return fmt.Errorf("registry snapshot exceeds byte budget")
	}
	return nil
}
