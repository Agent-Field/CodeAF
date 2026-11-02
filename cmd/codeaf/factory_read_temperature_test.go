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

// THE READ ASKS AT TEMPERATURE 0: the same issue read twice should land in the
// same band (live eval: 17 of 17 at 0 against 31 of 34 at the default).
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
