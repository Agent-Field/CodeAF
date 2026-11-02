package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The read asks at temperature 0, so the same issue read twice lands in the
// same band.
func TestTheFactoryReadAsksAtTemperatureZero(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	got := make(chan *float64, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Temperature *float64 `json:"temperature"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		got <- req.Temperature
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer endpoint.Close()
	settings := config.Config{APIKey: "sk-test", BaseURL: endpoint.URL, Model: "profile/default", ProfileDir: oneModelProfile(t)}
	call, ok := factoryTriageCall(func() (config.Config, error) { return settings, nil })
	if !ok {
		t.Fatal("no triage call")
	}
	if _, err := call(context.Background(), "read this"); err != nil {
		t.Fatal(err)
	}
	if temp := <-got; temp == nil || *temp != 0 {
		t.Fatalf("the read's temperature = %v, want 0", temp)
	}
}
