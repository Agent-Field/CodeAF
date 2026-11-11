package appwire

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestMachinePolicyMethodsAreAbsentWithoutCapability(t *testing.T) {
	names := []string{"cache.status", "cache.clean", "autonomy.get", "autonomy.set", "harness.list", "settings.crew.get", "settings.crew.set"}
	hello := withMachinePolicyFeatures(Hello{}, &fakeEngine{})
	for _, name := range names {
		if slices.Contains(hello.Features, name) {
			t.Fatalf("unsupported feature %s", name)
		}
	}
	w := startGraphWire(t, &fakeEngine{})
	w.next()
	for _, name := range names {
		raw, _ := json.Marshal(map[string]any{"id": 1, "method": name, "params": map[string]any{}})
		w.send(string(raw))
		err := w.next()["error"].(map[string]any)
		if err["code"] != CodeUnknownMethod {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
