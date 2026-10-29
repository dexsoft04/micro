package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/registry"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

type snapshotGetFunc func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)

func (f snapshotGetFunc) Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	return f(ctx, key, opts...)
}

func snapshotFixture(t *testing.T) *clientv3.GetResponse {
	t.Helper()
	value, err := json.Marshal(&registry.Service{Name: "game", Version: "v1", Metadata: map[string]string{"domain": "test"}, Nodes: []*registry.Node{{Id: "owner", Address: "address"}}})
	if err != nil {
		t.Fatal(err)
	}
	return &clientv3.GetResponse{Header: &etcdserverpb.ResponseHeader{Revision: 42}, Count: 1, Kvs: []*mvccpb.KeyValue{{Key: []byte("/micro/registry/test/game/owner"), Value: value}}}
}

func TestSnapshotReadsOneCompleteLinearizableScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, service := range []string{"", "game"} {
		q := registry.SnapshotQuery{Domain: "test", Service: service, MaxNodes: 2, MaxBytes: 4096}
		get := snapshotGetFunc(func(got context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			want := "/micro/registry/test/"
			if service != "" {
				want += "game/"
			}
			op := clientv3.OpGet(key, opts...)
			// The exact request is inspected below through the public Op methods.
			if got != ctx || key != want || op.IsSerializable() || string(op.RangeBytes()) != clientv3.GetPrefixRangeEnd(want) {
				t.Fatal("scope, context or consistency changed")
			}
			return snapshotFixture(t), nil
		})
		result, err := readSnapshot(ctx, get, q)
		if err != nil || result.Revision != "42" || len(result.Services) != 1 {
			t.Fatalf("%+v %v", result, err)
		}
	}
	empty := snapshotGetFunc(func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return &clientv3.GetResponse{Header: &etcdserverpb.ResponseHeader{Revision: 43}}, nil
	})
	if s, err := readSnapshot(ctx, empty, registry.SnapshotQuery{Domain: "test", MaxNodes: 1, MaxBytes: 4096}); err != nil || len(s.Services) != 0 {
		t.Fatalf("empty scope: %v", err)
	}
}

func TestSnapshotRejectsIncompleteOrMalformedEntries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for name, change := range map[string]func(*clientv3.GetResponse){
		"header":    func(r *clientv3.GetResponse) { r.Header = nil },
		"revision":  func(r *clientv3.GetResponse) { r.Header.Revision = 0 },
		"more":      func(r *clientv3.GetResponse) { r.More = true },
		"count":     func(r *clientv3.GetResponse) { r.Count = 2 },
		"nil_entry": func(r *clientv3.GetResponse) { r.Kvs[0] = nil },
		"domain":    func(r *clientv3.GetResponse) { r.Kvs[0].Key = []byte("/micro/registry/other/game/owner") },
		"node":      func(r *clientv3.GetResponse) { r.Kvs[0].Key = []byte("/micro/registry/test/game/other") },
		"json":      func(r *clientv3.GetResponse) { r.Kvs[0].Value = []byte("{") },
		"duplicate": func(r *clientv3.GetResponse) { r.Kvs = append(r.Kvs, r.Kvs[0]); r.Count++ },
	} {
		t.Run(name, func(t *testing.T) {
			response := snapshotFixture(t)
			change(response)
			get := snapshotGetFunc(func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
				return response, nil
			})
			if result, err := readSnapshot(ctx, get, registry.SnapshotQuery{Domain: "test", MaxNodes: 2, MaxBytes: 4096}); err == nil || result != nil {
				t.Fatal("incomplete snapshot accepted")
			}
		})
	}
	get := snapshotGetFunc(func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return snapshotFixture(t), nil
	})
	if _, err := readSnapshot(ctx, get, registry.SnapshotQuery{Domain: "test", MaxNodes: 1, MaxBytes: 1}); err == nil {
		t.Fatal("byte bound ignored")
	}
}

type boundedRangeProbe struct {
	etcdserverpb.KVClient
	t *testing.T
}

func (p boundedRangeProbe) Range(_ context.Context, req *etcdserverpb.RangeRequest, opts ...grpc.CallOption) (*etcdserverpb.RangeResponse, error) {
	if req.Serializable || req.Limit != 3 {
		p.t.Fatal("unbounded or stale range")
	}
	last, ok := opts[len(opts)-1].(grpc.MaxRecvMsgSizeCallOption)
	if !ok || last.MaxRecvMsgSize != 1234 {
		p.t.Fatal("per-request receive bound missing")
	}
	return nil, context.Canceled
}

func TestSnapshotTransportBoundUsesSharedKVClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	kv := clientv3.NewKVFromKVClient(snapshotRangeClient{KVClient: boundedRangeProbe{t: t}, maxBytes: 1234}, nil)
	if _, err := readSnapshot(ctx, kv, registry.SnapshotQuery{Domain: "test", MaxNodes: 2, MaxBytes: 1234}); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}
