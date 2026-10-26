package prometheus

import (
	"reflect"
	"sync"
	"testing"

	"github.com/micro/micro/v3/service/metrics"
	prom "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Use the real private registry without starting an unrelated HTTP listener.
func boundCountReporter(opts ...metrics.Option) *Reporter {
	r := &Reporter{options: metrics.NewOptions(opts...), prometheusRegistry: prom.NewRegistry()}
	r.metrics = r.newMetricFamily()
	return r
}

func boundCountGather(t testing.TB, r *Reporter) []*dto.MetricFamily {
	t.Helper()
	families, err := r.prometheusRegistry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	return families
}

func boundCountValue(t testing.TB, r *Reporter) float64 {
	t.Helper()
	families := boundCountGather(t, r)
	if len(families) != 1 || len(families[0].Metric) != 1 {
		t.Fatalf("unexpected families: %v", families)
	}
	return families[0].Metric[0].GetCounter().GetValue()
}

func TestBindCountEquivalentAndTagSnapshot(t *testing.T) {
	defaults := metrics.Tags{"service": "match.service"}
	regular, bound := boundCountReporter(metrics.DefaultTags(defaults)), boundCountReporter(metrics.DefaultTags(defaults))
	tags := metrics.Tags{"path": "join, main", "outcome": "ok"}
	frozen := metrics.Tags{"path": tags["path"], "outcome": tags["outcome"]}
	add, err := bound.BindCount("match.events.total", tags)
	if err != nil || add == nil {
		t.Fatalf("bind: %v", err)
	}
	if err := regular.Count("match.events.total", 0, frozen); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(boundCountGather(t, regular), boundCountGather(t, bound)) {
		t.Fatal("binding differs from Count(0)")
	}
	// Caller changes after binding must neither change labels nor be read by Add.
	delete(tags, "outcome")
	tags["path"] = "other"
	tags["new"] = "label"
	defaults["service"] = "later"
	for _, value := range []int64{1, 0, 37, -1, 1 << 53, 3} {
		got, want := add(value), regular.Count("match.events.total", value, frozen)
		if got != want {
			t.Fatalf("value %d: bound=%v regular=%v", value, got, want)
		}
		if !reflect.DeepEqual(boundCountGather(t, regular), boundCountGather(t, bound)) {
			t.Fatalf("value %d changed output", value)
		}
	}
	same, err := bound.BindCount("match_events_total", frozen)
	if err != nil {
		t.Fatal(err)
	}
	if err := same(4); err != nil {
		t.Fatal(err)
	}
	if err := regular.Count("match.events.total", 4, frozen); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(boundCountGather(t, regular), boundCountGather(t, bound)) {
		t.Fatal("same normalized series differs")
	}
}

func TestBindCountErrorsMatchCount(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*Reporter)
		metric string
		tags   metrics.Tags
	}{
		{name: "nil_tags", metric: "events"},
		{name: "empty_tags", metric: "events", tags: metrics.Tags{}},
		{name: "invalid_name", metric: "bad-name"},
		{name: "invalid_label", metric: "events", tags: metrics.Tags{"bad-label": "x"}},
		{name: "default_collision", metric: "events", tags: metrics.Tags{"service": "duplicate"}},
		{name: "cardinality", metric: "events", tags: metrics.Tags{"other": "x"}, setup: func(r *Reporter) { _ = r.Count("events", 0, metrics.Tags{"path": "join"}) }},
		{name: "type_collision", metric: "events", setup: func(r *Reporter) { _ = r.Gauge("events", 0, nil) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			regular, bound := boundCountReporter(metrics.DefaultTags(metrics.Tags{"service": "match"})), boundCountReporter(metrics.DefaultTags(metrics.Tags{"service": "match"}))
			if test.setup != nil {
				test.setup(regular)
				test.setup(bound)
			}
			want := regular.Count(test.metric, 0, test.tags)
			add, got := bound.BindCount(test.metric, test.tags)
			if (got == nil) != (want == nil) || got != nil && got.Error() != want.Error() {
				t.Fatalf("bound=%v regular=%v", got, want)
			}
			if got != nil && add != nil {
				t.Fatal("failed binding returned callable")
			}
			if got == nil {
				if err := add(0); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(boundCountGather(t, regular), boundCountGather(t, bound)) {
				t.Fatal("error path changed registry output")
			}
		})
	}
	var r *Reporter
	add, err := r.BindCount("events", nil)
	if add != nil || err != ErrPrometheusPanic {
		t.Fatalf("nil receiver: %v", err)
	}
}

func TestBindCountConcurrentExactAndIsolated(t *testing.T) {
	r := boundCountReporter()
	tags := metrics.Tags{"actor": "original"}
	add, err := r.BindCount("events", tags)
	if err != nil {
		t.Fatal(err)
	}
	const workers, iterations = 16, 10000
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if err := add(3); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	// A caller-owned tag mutation is safe after BindCount has returned.
	tags["actor"] = "mutated"
	wg.Wait()
	if got := boundCountValue(t, r); got != workers*iterations*3 {
		t.Fatalf("count=%v", got)
	}
	other := boundCountReporter()
	otherAdd, err := other.BindCount("events", metrics.Tags{"actor": "original"})
	if err != nil {
		t.Fatal(err)
	}
	if err := otherAdd(7); err != nil {
		t.Fatal(err)
	}
	if boundCountValue(t, other) != 7 || boundCountValue(t, r) != workers*iterations*3 {
		t.Fatal("private registries are not isolated")
	}
}

func BenchmarkCountBinding(b *testing.B) {
	for _, parallel := range []bool{false, true} {
		mode := "serial"
		if parallel {
			mode = "parallel"
		}
		for _, bound := range []bool{false, true} {
			name := "Count"
			if bound {
				name = "BindCount"
			}
			b.Run(mode+"/"+name, func(b *testing.B) {
				r := boundCountReporter(metrics.DefaultTags(metrics.Tags{"service": "match"}))
				tags := metrics.Tags{"stage": "queue_register", "result": "success", "game": "ddz", "path": "join"}
				add := func(value int64) error { return r.Count("matching.stage.events.total", value, tags) }
				if bound {
					var err error
					add, err = r.BindCount("matching.stage.events.total", tags)
					if err != nil {
						b.Fatal(err)
					}
				}
				if err := add(0); err != nil {
					b.Fatal(err)
				} // Both paths have a warm registry/cache.
				b.ReportAllocs()
				b.ResetTimer()
				if parallel {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							if err := add(1); err != nil {
								b.Error(err)
								return
							}
						}
					})
				} else {
					for i := 0; i < b.N; i++ {
						if err := add(1); err != nil {
							b.Fatal(err)
						}
					}
				}
				b.StopTimer()
				if got := boundCountValue(b, r); got != float64(b.N) {
					b.Fatalf("count=%v want=%d", got, b.N)
				}
			})
		}
	}
}
