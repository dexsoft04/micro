package registry

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/api"
	"github.com/micro/micro/v3/service/api/resolver"
	"github.com/micro/micro/v3/service/api/router"
	reg "github.com/micro/micro/v3/service/registry"
)

// watchRefreshRegistry controls registry snapshots independently of the stale
// cache to exercise address replacement without relying on timer refreshes.
type watchRefreshRegistry struct {
	reg.Registry
	mu       sync.Mutex
	services map[string][]*reg.Service
	gets     []string
	getErr   error
	watcher  *watchRefreshWatcher
}

func (f *watchRefreshRegistry) GetService(name string, opts ...reg.GetOption) ([]*reg.Service, error) {
	o := reg.GetOptions{Domain: reg.DefaultDomain}
	for _, fn := range opts {
		fn(&o)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, o.Domain)
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, s := range f.services[o.Domain] {
		if s.Name == name {
			return f.services[o.Domain], nil
		}
	}
	return nil, reg.ErrNotFound
}
func (f *watchRefreshRegistry) ListServices(opts ...reg.ListOption) ([]*reg.Service, error) {
	o := reg.ListOptions{Domain: reg.DefaultDomain}
	for _, fn := range opts {
		fn(&o)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.services[o.Domain], nil
}
func (f *watchRefreshRegistry) Watch(opts ...reg.WatchOption) (reg.Watcher, error) {
	o := reg.WatchOptions{}
	for _, fn := range opts {
		fn(&o)
	}
	if o.Domain != reg.WildcardDomain {
		return nil, fmt.Errorf("unexpected watch domain %q", o.Domain)
	}
	return f.watcher, nil
}
func (f *watchRefreshRegistry) Stop() {
	if f.watcher != nil {
		f.watcher.Stop()
	}
}
func (f *watchRefreshRegistry) replace(domain string, ss ...*reg.Service) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services[domain] = ss
}
func (f *watchRefreshRegistry) lastGet() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.gets) == 0 {
		return "<none>"
	}
	return f.gets[len(f.gets)-1]
}

type watchRefreshWatcher struct {
	events    chan *reg.Result
	processed chan struct{}
	stopped   chan struct{}
	once      sync.Once
	delivered bool
}

func (w *watchRefreshWatcher) Next() (*reg.Result, error) {
	// watch() invokes Next only after process() returns, making this an exact
	// completion barrier rather than a sleep-based assumption.
	if w.delivered {
		w.processed <- struct{}{}
		w.delivered = false
	}
	select {
	case e := <-w.events:
		w.delivered = true
		return e, nil
	case <-w.stopped:
		return nil, fmt.Errorf("watchRefresh watcher stopped")
	}
}
func (w *watchRefreshWatcher) Stop() { w.once.Do(func() { close(w.stopped) }) }
func (w *watchRefreshWatcher) emit(t *testing.T, e *reg.Result) {
	t.Helper()
	w.events <- e
	select {
	case <-w.processed:
	case <-time.After(2 * time.Second):
		t.Fatal("watch processing did not finish")
	}
}

type watchRefreshResolver struct{ domain string }

func (r watchRefreshResolver) Resolve(*http.Request, ...resolver.ResolveOption) (*resolver.Endpoint, error) {
	return &resolver.Endpoint{Name: "igaoshou-match-srv", Domain: r.domain}, nil
}
func (r watchRefreshResolver) String() string { return "watchRefresh" }

func watchRefreshService(domain, version, node, address string) *reg.Service {
	return &reg.Service{Name: "igaoshou-match-srv", Version: version, Metadata: map[string]string{"domain": domain},
		Nodes:     []*reg.Node{{Id: node, Address: address, Metadata: map[string]string{"domain": domain}}},
		Endpoints: []*reg.Endpoint{{Name: "Grade.ListConfig", Metadata: api.Encode(&api.Endpoint{Name: "Grade.ListConfig", Handler: "rpc", Method: []string{"GET"}, Path: []string{"/grade/list-config"}})}},
	}
}
func watchRefreshAddresses(t *testing.T, r *registryRouter) []string {
	t.Helper()
	req, err := http.NewRequest("GET", "http://example.invalid/grade/list-config", nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := r.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	var addresses []string
	for _, s := range service.Services {
		for _, n := range s.Nodes {
			addresses = append(addresses, n.Address)
		}
	}
	sort.Strings(addresses)
	return addresses
}
func TestWatchNamespaceAddressRefresh(t *testing.T) {
	for _, domain := range []string{"igaoshou", reg.DefaultDomain} {
		t.Run(domain, func(t *testing.T) {
			watcher := &watchRefreshWatcher{events: make(chan *reg.Result, 1), processed: make(chan struct{}, 1), stopped: make(chan struct{})}
			f := &watchRefreshRegistry{services: make(map[string][]*reg.Service), watcher: watcher}
			r := &registryRouter{exit: make(chan bool), opts: router.NewOptions(router.WithRegistry(f), router.WithResolver(watchRefreshResolver{domain})), rc: f, refreshChan: make(chan string, 1), namespaces: make(map[string]*namespaceEntry)}
			old := watchRefreshService(domain, "v-old", "old-node", "192.0.2.10:8080")
			newer := watchRefreshService(domain, "v-new", "new-node", "192.0.2.20:8080")
			f.replace(domain, old)
			// Keep the independently watched lower cache stale throughout the test.
			stale := &watchRefreshRegistry{services: map[string][]*reg.Service{domain: {old}}}
			r.rc = stale
			if err := r.refreshNamespace(domain); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { defer close(done); r.watch() }()
			defer func() {
				r.Close()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("watch did not stop")
				}
			}()
			assert := func(stage string, want []string) {
				t.Helper()
				got := watchRefreshAddresses(t, r)
				t.Logf("domain=%s stage=%s route_addresses=%v", domain, stage, got)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: got %v want %v", stage, got, want)
				}
			}
			assert("initial", []string{"192.0.2.10:8080"})
			f.replace(domain, old, newer)
			watcher.emit(t, &reg.Result{Action: "create", Service: newer})
			assert("create", []string{"192.0.2.10:8080", "192.0.2.20:8080"})
			t.Logf("create event lookup_domain=%s", f.lastGet())
			if f.lastGet() != domain {
				t.Fatalf("lookup domain = %s, want %s", f.lastGet(), domain)
			}
			f.replace(domain, newer)
			watcher.emit(t, &reg.Result{Action: "delete", Service: old})
			t.Logf("delete event lookup_domain=%s", f.lastGet())
			assert("delete", []string{"192.0.2.20:8080"})
			if err := r.refreshNamespace(domain); err != nil {
				t.Fatal(err)
			}
			assert("manual-refresh", []string{"192.0.2.20:8080"})
		})
	}
}

func TestWatchRefreshPreservesDomainsAndFailureBehavior(t *testing.T) {
	for _, tc := range []struct {
		name       string
		nodeOnly   bool
		omitDomain bool
		failure    error
	}{
		{name: "service_metadata"}, {name: "node_metadata", nodeOnly: true},
		{name: "default_domain", omitDomain: true},
		{name: "registry_error", failure: fmt.Errorf("registry unavailable")},
		{name: "last_node_missing", failure: reg.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			domain := "igaoshou"
			if tc.omitDomain {
				domain = reg.DefaultDomain
			}
			old := watchRefreshService(domain, "v1", "old", "192.0.2.10:8080")
			newer := watchRefreshService(domain, "v2", "new", "192.0.2.20:8080")
			other := watchRefreshService("other", "v1", "other", "192.0.2.30:8080")
			if tc.nodeOnly || tc.omitDomain {
				newer.Metadata = nil
			}
			if tc.omitDomain {
				newer.Nodes[0].Metadata = nil
			}
			f := &watchRefreshRegistry{services: map[string][]*reg.Service{domain: {newer}}, getErr: tc.failure}
			stale := &watchRefreshRegistry{services: map[string][]*reg.Service{domain: {old}}}
			r := &registryRouter{exit: make(chan bool), opts: router.NewOptions(router.WithRegistry(f), router.WithResolver(watchRefreshResolver{domain})), rc: stale, refreshChan: make(chan string, 1), namespaces: make(map[string]*namespaceEntry)}
			r.store(domain, []*reg.Service{old})
			r.store("other", []*reg.Service{other})
			r.process(nil)
			r.process(&reg.Result{})
			r.process(&reg.Result{Action: "update", Service: newer})
			want := []string{"192.0.2.20:8080"}
			if tc.failure != nil {
				want = []string{"192.0.2.10:8080"}
			}
			if got := watchRefreshAddresses(t, r); !reflect.DeepEqual(got, want) {
				t.Fatalf("route=%v want=%v", got, want)
			}
			if f.lastGet() != domain {
				t.Fatalf("lookup domain=%q want=%q", f.lastGet(), domain)
			}
			r.opts.Resolver = watchRefreshResolver{"other"}
			if got := watchRefreshAddresses(t, r); !reflect.DeepEqual(got, []string{"192.0.2.30:8080"}) {
				t.Fatalf("other domain changed: %v", got)
			}
		})
	}
}
