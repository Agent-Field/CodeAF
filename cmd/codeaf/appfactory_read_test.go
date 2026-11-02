package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// A PERSON'S READ, AGAINST THE REAL SERVER AND STORE: factory.read hands the
// factory exactly the chosen jobs, and its answer comes back whole.
func TestAppFactoryReadAgainstTheRealServer(t *testing.T) {
	var handed []int
	f := startActionFactoryWith(t, func(c *serve.Config) {
		c.Read = func(_ context.Context, ids []int) api.ReadResult {
			handed = ids
			return api.ReadResult{Read: ids, Skipped: []int{}, Failed: []int{}}
		}
	})
	alice := signedIn(t, f, 1, "alice")
	alice.w.mu.Lock()
	hello := alice.w.lines[0]
	alice.w.mu.Unlock()
	if !strings.Contains(hello, appwire.FeatureFactoryRead) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryRead)
	}
	one := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Title: "one"})
	f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Title: "two"})
	three := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Title: "three"})
	var got api.ReadResult
	alice.w.must("factory.read", map[string]any{"factory": alice.conn.ID, "jobs": []int{one.ID, three.ID}}, &got)
	if !reflect.DeepEqual(handed, []int{one.ID, three.ID}) || !reflect.DeepEqual(got.Read, []int{one.ID, three.ID}) {
		t.Fatalf("handed %v, answered %+v", handed, got)
	}
}
