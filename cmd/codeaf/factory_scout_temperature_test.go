package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/scout"
	"github.com/Agent-Field/codeaf/internal/home"
)

// A LOOK'S RUBRIC IS SCORED AT TEMPERATURE 0, and its tool turns are not.
func TestTheLooksScoringCallAsksAtTemperatureZero(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	got := make(chan *float64, 2)
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
	chat := factoryScoutChat(func() (config.Config, error) { return settings, nil })
	msgs := []scout.Message{{Role: "user", Text: "look"}}
	if _, err := chat(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}
	if temp := <-got; temp != nil {
		t.Fatalf("a tool turn asked at temperature %v", *temp)
	}
	if _, err := chat(scout.WithScoring(context.Background()), msgs); err != nil {
		t.Fatal(err)
	}
	if temp := <-got; temp == nil || *temp != 0 {
		t.Fatalf("the scoring call's temperature = %v, want 0", temp)
	}
}
