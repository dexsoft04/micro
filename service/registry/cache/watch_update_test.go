package cache

import (
	"github.com/micro/micro/v3/service/registry"
	"reflect"
	"sync"
	"testing"
	"time"
)

func watchService(domain, name, node string) *registry.Service {
	return &registry.Service{Name: name, Version: "latest", Metadata: map[string]string{"domain": domain}, Nodes: []*registry.Node{{Id: node, Address: node + ":8080"}}}
}
func seededWatchCache(domain string) *cache {
	c := New(nil).(*cache)
	c.opts.TTL = time.Hour
	c.watched[domain] = watched{"match": true}
	c.set(domain, "match", []*registry.Service{watchService(domain, "match", "old")})
	return c
}
func cachedNodeIDs(t *testing.T, c *cache, domain string) []string {
	t.Helper()
	ss, err := c.GetService("match", registry.GetDomain(domain))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range ss {
		for _, n := range s.Nodes {
			ids = append(ids, n.Id)
		}
	}
	return ids
}
func TestWatchUpdateDomainAndImmutableSnapshots(t *testing.T) {
	c := seededWatchCache("igaoshou")
	defer c.Stop()
	c.watched["other"] = watched{"match": true}
	c.set("other", "match", []*registry.Service{watchService("other", "match", "other")})
	snapshot := c.services["igaoshou"]["match"]
	event := &registry.Result{Action: "create", Service: watchService("igaoshou", "match", "new")}
	c.update("igaoshou", event)
	if got := cachedNodeIDs(t, c, "igaoshou"); !reflect.DeepEqual(got, []string{"new", "old"}) {
		t.Fatalf("create=%v", got)
	}
	if len(snapshot[0].Nodes) != 1 || snapshot[0].Nodes[0].Id != "old" {
		t.Fatal("published snapshot changed")
	}
	if len(event.Service.Nodes) != 1 {
		t.Fatal("input event changed")
	}
	beforeDelete := c.services["igaoshou"]["match"]
	c.update("igaoshou", &registry.Result{Action: "delete", Service: watchService("igaoshou", "match", "old")})
	if got := cachedNodeIDs(t, c, "igaoshou"); !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatalf("delete=%v", got)
	}
	if len(beforeDelete[0].Nodes) != 2 {
		t.Fatal("delete mutated published snapshot")
	}
	if got := cachedNodeIDs(t, c, "other"); !reflect.DeepEqual(got, []string{"other"}) {
		t.Fatalf("cross-domain update: %v", got)
	}
	c.update("unwatched", event)
	c.update("igaoshou", &registry.Result{Action: "create", Service: watchService("igaoshou", "unknown", "unknown")})
	if len(c.services["unwatched"]) != 0 || c.services["igaoshou"]["unknown"] != nil {
		t.Fatal("unwatched event was cached")
	}
}
func TestWatchUpdateConcurrentReaders(t *testing.T) {
	c := seededWatchCache("igaoshou")
	defer c.Stop()
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				_, err := c.GetService("match", registry.GetDomain("igaoshou"))
				if err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 500; i++ {
		c.update("igaoshou", &registry.Result{Action: "create", Service: watchService("igaoshou", "match", "new")})
		c.update("igaoshou", &registry.Result{Action: "delete", Service: watchService("igaoshou", "match", "new")})
	}
	wg.Wait()
	if got := cachedNodeIDs(t, c, "igaoshou"); !reflect.DeepEqual(got, []string{"old"}) {
		t.Fatalf("survivor=%v", got)
	}
}
