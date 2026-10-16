package appwire

import (
	"encoding/json"
	"errors"
	"testing"
)

type checkoutDiffEngine struct {
	fakeEngine
	q CheckoutPathQuery
}

func (c *checkoutDiffEngine) CheckoutDiff(q CheckoutPathQuery) (CheckoutDiffResult, error) {
	c.q = q
	return CheckoutDiffResult{Revision: "r"}, nil
}

func (c *checkoutDiffEngine) CheckoutLocate(q CheckoutPathQuery) (CheckoutLocation, error) {
	c.q = q
	return CheckoutLocation{Revision: "r"}, nil
}

// BOTH METHODS TAKE A STATUS ROW'S PATH ONLY: nothing absolute, nothing that
// climbs, nothing empty reaches the engine; an engine without them is unknown.
func TestCheckoutDiffAndLocateRoutes(t *testing.T) {
	for _, name := range []string{"checkout.diff", "checkout.locate"} {
		m, ok := methods[name]
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		eng := &checkoutDiffEngine{}
		if _, err := m.call(eng, json.RawMessage(`{"agent":"a1","path":"sub/x.txt","revision":"abc"}`)); err != nil ||
			eng.q.Path != "sub/x.txt" || eng.q.Revision != "abc" {
			t.Fatalf("%s: %v %+v", name, err, eng.q)
		}
		var wire *Error
		for _, bad := range []string{`{}`, `{"agent":"a1"}`, `{"agent":"a1","path":"/etc/passwd"}`, `{"agent":"a1","path":"../x"}`,
			`{"agent":"a1","path":"a/./b"}`, `{"agent":"a1","path":"a\\b"}`, `{"agent":" ","path":"x"}`} {
			if _, err := m.call(eng, json.RawMessage(bad)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
				t.Fatalf("%s %s answered %v", name, bad, err)
			}
		}
		if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"agent":"a1","path":"x"}`)); !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
			t.Fatalf("%s on an engine without it answered %v", name, err)
		}
	}
}
