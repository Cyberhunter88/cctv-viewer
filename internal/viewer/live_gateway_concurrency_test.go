package viewer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveGatewayRouteConcurrency(t *testing.T) {
	g := &liveGateway{ctx: context.Background(), routes: map[string]*gatewayRoute{}, builds: map[string]*gatewayBuild{}}
	started := make(chan string, 3)
	release := make(chan struct{})
	type result struct {
		path  string
		route *gatewayRoute
		err   error
	}
	results := make(chan result, 3)
	var calls atomic.Int32
	create := func(path string) func() (*gatewayRoute, error) {
		return func() (*gatewayRoute, error) {
			calls.Add(1)
			started <- path
			<-release
			return &gatewayRoute{used: time.Now()}, nil
		}
	}
	go func() {
		r, err := g.route("a", create("a"))
		results <- result{"a", r, err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first route did not start")
	}
	go func() {
		r, err := g.route("a", create("duplicate"))
		results <- result{"a", r, err}
	}()
	go func() {
		r, err := g.route("b", create("b"))
		results <- result{"b", r, err}
	}()
	select {
	case path := <-started:
		if path != "b" {
			t.Fatal("duplicate route was built", path)
		}
	case <-time.After(time.Second):
		t.Fatal("second camera blocked behind first route")
	}
	close(release)
	first, second, third := <-results, <-results, <-results
	if first.route == nil || second.route == nil || third.route == nil || first.err != nil || second.err != nil || third.err != nil || calls.Load() != 2 {
		t.Fatal("routes were not built exactly once per path", calls.Load())
	}
	g.mu.Lock()
	a, b := g.routes["a"], g.routes["b"]
	g.mu.Unlock()
	for _, got := range []result{first, second, third} {
		if got.path == "a" && got.route != a || got.path == "b" && got.route != b {
			t.Fatal("route was not shared", got.path)
		}
	}
	if a == nil || b == nil || a == b {
		t.Fatal("route sharing failed")
	}
}
