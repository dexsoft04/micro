package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/micro/micro/v3/service/registry"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

var _ registry.Snapshotter = (*etcdRegistry)(nil)

type snapshotKV interface {
	Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
}

// snapshotRangeClient retains the existing authenticated connection and retry
// implementation while applying a per-request receive bound.
type snapshotRangeClient struct {
	etcdserverpb.KVClient
	maxBytes int
}

func (c snapshotRangeClient) Range(ctx context.Context, req *etcdserverpb.RangeRequest, opts ...grpc.CallOption) (*etcdserverpb.RangeResponse, error) {
	bounded := append([]grpc.CallOption(nil), opts...)
	return c.KVClient.Range(ctx, req, append(bounded, grpc.MaxCallRecvMsgSize(c.maxBytes))...)
}

func (e *etcdRegistry) Snapshot(ctx context.Context, query registry.SnapshotQuery) (*registry.Snapshot, error) {
	if err := query.Validate(ctx); err != nil {
		return nil, err
	}
	if e.client == nil {
		return nil, fmt.Errorf("registry snapshot client is unavailable")
	}
	kv := clientv3.NewKVFromKVClient(snapshotRangeClient{KVClient: clientv3.RetryKVClient(e.client), maxBytes: query.MaxBytes}, e.client)
	return readSnapshot(ctx, kv, query)
}

func readSnapshot(ctx context.Context, kv snapshotKV, query registry.SnapshotQuery) (*registry.Snapshot, error) {
	if err := query.Validate(ctx); err != nil {
		return nil, err
	}
	p := prefixWithDomain(query.Domain) + "/"
	if query.Service != "" {
		p = servicePath(query.Domain, query.Service) + "/"
	}
	// One entry is one node in this adapter. Read one extra to distinguish a
	// complete result at the bound from truncation without issuing a second read.
	limit := int64(query.MaxNodes)
	if limit == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("registry snapshot node budget is too large")
	}
	response, err := kv.Get(ctx, p, clientv3.WithPrefix(), clientv3.WithLimit(limit+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response == nil || response.Header == nil || response.Header.Revision <= 0 || response.More || response.Count != int64(len(response.Kvs)) || len(response.Kvs) > query.MaxNodes || (*etcdserverpb.RangeResponse)(response).Size() > query.MaxBytes {
		return nil, fmt.Errorf("incomplete or excessive registry snapshot")
	}
	result := &registry.Snapshot{Revision: strconv.FormatInt(response.Header.Revision, 10), Services: make([]*registry.Service, 0, len(response.Kvs))}
	seen := make(map[string]bool, len(response.Kvs))
	domainPrefix := prefixWithDomain(query.Domain) + "/"
	for _, entry := range response.Kvs {
		if entry == nil || !strings.HasPrefix(string(entry.Key), p) || seen[string(entry.Key)] || !utf8.Valid(entry.Value) {
			return nil, fmt.Errorf("invalid registry snapshot entry")
		}
		seen[string(entry.Key)] = true
		parts := strings.Split(strings.TrimPrefix(string(entry.Key), domainPrefix), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid registry snapshot key")
		}
		var service registry.Service
		if err := json.Unmarshal(entry.Value, &service); err != nil || service.Name == "" || serializeServiceName(service.Name) != parts[0] || (query.Service != "" && service.Name != query.Service) || service.Metadata["domain"] != query.Domain || len(service.Nodes) != 1 || service.Nodes[0] == nil || service.Nodes[0].Id == "" || strings.ReplaceAll(service.Nodes[0].Id, "/", "-") != parts[1] {
			return nil, fmt.Errorf("invalid registry snapshot identity")
		}
		result.Services = append(result.Services, &service)
	}
	if err := result.Validate(query); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
