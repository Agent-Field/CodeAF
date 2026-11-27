package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"
)

type reposRefreshEngine struct {
	factoryEngine
	calls []string
}

func (e *reposRefreshEngine) FactoryReposRefresh(_ context.Context, id string) (FactoryRepos, error) {
	e.calls = append(e.calls, id)
	return FactoryRepos{
		Watched: []string{"acme/on"}, Available: []FactoryRepo{{Full: "acme/new"}},
		AvailableReadAt: time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC),
	}, nil
}

type reposRefreshWithoutFactory struct {
	fakeEngine
}

func (*reposRefreshWithoutFactory) FactoryReposRefresh(context.Context, string) (FactoryRepos, error) {
	return FactoryRepos{}, nil
}

// Contract W1, W2 and W5: The optional read is available and announced only
// when the engine supports both the factory and the refresher.
func TestFactoryReposRefreshOptionalWire(t *testing.T) {
	enabled := &reposRefreshEngine{}
	ordinary := &factoryEngine{}
	refresherOnly := &reposRefreshWithoutFactory{}
	for _, tc := range []struct {
		name    string
		engine  Engine
		fake    *fakeEngine
		enabled bool
	}{
		{"supported", enabled, &enabled.fakeEngine, true},
		{"ordinary factory", ordinary, &ordinary.fakeEngine, false},
		{"refresher only", refresherOnly, &refresherOnly.fakeEngine, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := startWireOver(t, tc.engine, tc.fake)
			hello := w.next()
			features, _ := hello["data"].(map[string]any)["features"].([]any)
			if slices.Contains(features, any("factory.repos_refresh")) != tc.enabled {
				t.Errorf("hello: %v", hello)
			}
			w.send(`{"id":1,"method":"factory.repos_refresh","params":{"factory":" f "}}`)
			reply := w.next()
			if !tc.enabled {
				failure, _ := reply["error"].(map[string]any)
				if failure["code"] != CodeUnknownMethod {
					t.Fatalf("unsupported refresh: %v", reply)
				}
				return
			}
			raw, err := json.Marshal(reply["result"])
			if err != nil {
				t.Fatal(err)
			}
			var got FactoryRepos
			if err := json.Unmarshal(raw, &got); err != nil ||
				!slices.Equal(got.Watched, []string{"acme/on"}) ||
				len(got.Available) != 1 || got.Available[0].Full != "acme/new" ||
				!got.AvailableReadAt.Equal(time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)) {
				t.Fatalf("refresh: %v, %v", reply, err)
			}
		})
	}
	if !slices.Equal(enabled.calls, []string{"f"}) {
		t.Fatalf("refresh calls: %v", enabled.calls)
	}
}

// Contract W5: The refresh returns the same bad_params as the ordinary read
// and leaves the refresher alone.
func TestFactoryReposRefreshBadParams(t *testing.T) {
	e := &reposRefreshEngine{}
	w := startWireOver(t, e, &e.fakeEngine)
	w.next()
	for _, params := range []string{`{}`, `{"factory":15}`, `{"factory":" "}`, `[]`} {
		w.send(`{"id":1,"method":"factory.repos","params":` + params + `}`)
		ordinary := w.next()
		w.send(`{"id":2,"method":"factory.repos_refresh","params":` + params + `}`)
		refresh := w.next()
		failure, _ := refresh["error"].(map[string]any)
		if failure["code"] != CodeBadParams || !reflect.DeepEqual(refresh["error"], ordinary["error"]) {
			t.Errorf("%s: ordinary %v, refresh %v", params, ordinary, refresh)
		}
	}
	if len(e.calls) != 0 {
		t.Fatalf("refresh calls: %v", e.calls)
	}
}

// Contract W4: The generated schema describes the same input and result for
// both repository list methods.
func TestFactoryReposRefreshSchema(t *testing.T) {
	doc := Schema()["methods"].(map[string]any)
	if got := doc["factory.repos_refresh"]; !reflect.DeepEqual(got, doc["factory.repos"]) {
		t.Fatalf("refresh schema = %v, ordinary read = %v", got, doc["factory.repos"])
	}
}
