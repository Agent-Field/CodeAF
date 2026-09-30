package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// This crosses the actual do, run, session, and HTTP roads with two valid
// credentials, so choosing the default account cannot pass unnoticed.
func TestDoWorkerKeepsItsSelectedProviderModelAndCredential(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "default-test-key")
	var mu sync.Mutex
	type call struct{ endpoint, model, authorization string }
	var calls []call
	endpoint := func(name string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var body struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode %s request: %v", name, err)
			}
			mu.Lock()
			calls = append(calls, call{name, body.Model, r.Header.Get("Authorization")})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"fixture account refused","code":403}}`))
		}))
		t.Cleanup(server.Close)
		return server
	}
	defaultServer, directServer := endpoint("default"), endpoint("direct")
	t.Setenv("CODEAF_BASE_URL", defaultServer.URL)
	if err := config.WriteSources(config.ProfileDir(), []config.PersistedSource{{
		ID: "custom-direct", Written: "direct", Key: "direct-test-key", Address: directServer.URL,
	}}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{task: "write out.txt", workspace: t.TempDir(), asJSON: true,
		model: "direct/model", planModel: "direct/model", checkModel: "direct/model",
		timeout: time.Minute, slots: bound(1), stdout: &stdout, stderr: &stderr,
	})
	if exitCodeOf(err) != 2 {
		t.Fatalf("exit=%v stdout=%s stderr=%s", err, &stdout, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != (call{"direct", "model", "Bearer direct-test-key"}) {
		t.Fatalf("do task changed its selected account: %+v", calls)
	}
	if receipt := decodeErrand(t, stdout.String()); receipt.Error != "your key was not accepted for this model" {
		t.Fatalf("direct refusal named another account: %q", receipt.Error)
	}
}

// A paid action remains paid when the next call in the same turn is refused.
func TestDoPaidActionThenAuthErrorKeepsItsCallTotals(t *testing.T) {
	beltRunEnv(t)
	seat := &beltSeat{script: []func(context.Context, []ai.Message) (*ai.Response, error){
		func(context.Context, []ai.Message) (*ai.Response, error) {
			reply := beltToolReply("printf hello > paid.txt")
			cost := 0.125
			reply.Usage.Cost = &cost
			return reply, nil
		},
	}, ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("API error (401): account refused")
	}}
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{task: "write paid.txt", workspace: t.TempDir(), asJSON: true,
		timeout: 20 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	})
	if exitCodeOf(err) != 2 {
		t.Fatalf("exit=%v stdout=%s stderr=%s", err, &stdout, &stderr)
	}
	fields := doEnvelopeFields(t, stdout.String())
	tokens, _ := fields["tokens"].(map[string]any)
	if fields["spend"] != 0.125 || fields["spend_work"] != 0.125 || tokens["in"] != float64(20) || tokens["out"] != float64(7) {
		t.Fatalf("paid call lost from incomplete receipt: %s", &stdout)
	}
	if seat.seen != 2 {
		t.Fatalf("calls=%d, want paid call and refusal", seat.seen)
	}
}

// The do door must carry its admitted profile and actual source set to workers.
func TestDoAuthSourceUsesTheAdmittedProfileAndProvider(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "saved profile"
		if direct {
			name = "direct provider"
		}
		t.Run(name, func(t *testing.T) {
			beltRunEnv(t)
			profile := config.ProfileDir()
			t.Setenv("OPENROUTER_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")
			if err := config.WriteAPIKey(profile, "saved-test-key"); err != nil {
				t.Fatal(err)
			}
			model := "test/model"
			if direct {
				t.Setenv("OPENROUTER_API_KEY", "default-test-key")
				if err := config.WriteSources(profile, []config.PersistedSource{{ID: "custom-direct", Written: "direct", Key: "direct-test-key", Address: "http://127.0.0.1:9/v1"}}); err != nil {
					t.Fatal(err)
				}
				model = "direct/model"
			}
			loaded, loadErr := config.Load()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			service, _ := loaded.Sources.For(model)
			if direct && service.Source.ID != "custom-direct" {
				t.Fatalf("direct fixture resolved to %q", service.Source.ID)
			}
			seat := &beltSeat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
				return nil, errors.New("API error (403): account refused")
			}}
			var stdout, stderr strings.Builder
			err := doErrand(doRequest{task: "write out.txt", workspace: t.TempDir(), asJSON: true,
				model: model, planModel: model, checkModel: model, timeout: 20 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
				newBeltCompleter: func(string) session.Completer { return seat },
			})
			if exitCodeOf(err) != 2 {
				t.Fatalf("exit=%v stdout=%s stderr=%s", err, &stdout, &stderr)
			}
			receipt := decodeErrand(t, stdout.String())
			want := "your key was not accepted for this model"
			if !direct {
				want += " — the key saved in your profile"
			}
			if receipt.Error != want {
				t.Fatalf("auth error=%q, want %q", receipt.Error, want)
			}
			if seat.seen != 1 {
				t.Fatalf("calls=%d, want one", seat.seen)
			}
			for _, key := range []string{"saved-test-key", "direct-test-key", "default-test-key"} {
				if strings.Contains(stdout.String()+stderr.String(), key) {
					t.Fatal("a key reached output")
				}
			}
		})
	}
}
