package appwire

import "testing"

// A NEW JOB NAMES ITS WORKSTREAM BY ID: factory.job_create passes a ws_ id
// on to the factory, trimmed, and refuses anything else before any call.
func TestFactoryJobCreateTakesAWorkstreamID(t *testing.T) {
	ok := FactoryJobCreateParams{Factory: "factory_local", Repo: "acme/api", Title: "Retry deliveries", Workstream: " ws_01JD8ZQ7 "}
	if err := prepCreate(&ok); err != nil || ok.Workstream != "ws_01JD8ZQ7" {
		t.Fatalf("a workstream id = %q, %v", ok.Workstream, err)
	}
	for _, bad := range []string{"webhooks", "ws_", "ws_../x", "ws_a b"} {
		p := FactoryJobCreateParams{Factory: "factory_local", Repo: "acme/api", Title: "x", Workstream: bad}
		if err := prepCreate(&p); err == nil {
			t.Errorf("workstream %q was taken", bad)
		}
	}
}
