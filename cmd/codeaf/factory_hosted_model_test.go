package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

func TestHostedCompleterKeepsEachCallsKeyAndUsesTheDefaultBase(t *testing.T) {
	type received struct{ auth, model string }
	var mu sync.Mutex
	var receivedCalls []received
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != modelapi.ChatURL("/api/v1") {
			http.NotFound(w, r)
			return
		}
		var request struct{ Model string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		receivedCalls = append(receivedCalls, received{r.Header.Get("Authorization"), request.Model})
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"gen-test\",\"model\":\"stub/scripted\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"cost\":0.0123}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)
	oldBase := factoryWorkstreamAgentBase
	factoryWorkstreamAgentBase = upstream.URL + "/api/v1"
	t.Cleanup(func() { factoryWorkstreamAgentBase = oldBase })
	t.Setenv(calllog.EnvVar, "")

	profile := t.TempDir()
	t.Cleanup(func() { calllog.Open("") })
	factory := hostedCompleterFor(profile)
	const orgKey, ownKey = "sk-test-org-0001", "sk-test-own-0001"
	const model = "openrouter/stub/scripted"
	org := factory("", orgKey)(model)
	if got := factory(upstream.URL+"/api/v1", orgKey)(model); got != org {
		t.Fatal("the same base, model and key did not reuse its adapter")
	}
	own := factory("", ownKey)(model)
	if own == org {
		t.Fatal("a different key reused the first adapter")
	}
	for _, c := range []modelapi.Completer{org, own, org} {
		var billed float64
		ctx := provider.WithBilling(context.Background(), func(bill provider.Billed) { billed += bill.Cost })
		answer, err := c.CompleteWithMessages(ctx, []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}})
		if err != nil || answer == nil || answer.Text() != "hi" || billed != 0.0123 {
			t.Fatalf("reply %v, billed $%g, error %v", answer, billed, err)
		}
	}
	want := []received{
		{"Bearer " + orgKey, "stub/scripted"},
		{"Bearer " + ownKey, "stub/scripted"},
		{"Bearer " + orgKey, "stub/scripted"},
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(receivedCalls, want) {
		t.Fatalf("upstream got %+v, want %+v", receivedCalls, want)
	}
	if path := calllog.Path(); !strings.HasPrefix(path, filepath.Join(profile, "hosted-model")+string(os.PathSeparator)) {
		t.Fatalf("provider call log is outside the isolated directory: %s", path)
	}
	if _, err := os.Stat(calllog.Path()); err != nil {
		t.Fatalf("provider call log: %v", err)
	}
}

// P1-1/P1-5 at the door the operator uses: the settings the organization's
// server reads from its environment become the hosted config, and one that is
// wrong stops the start with a sentence that never repeats a value.
func TestP1_HostedSettingsReadFromTheEnvironment(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	good := strings.Repeat("ab", 32)
	cfgFor := func(values map[string]string) (*serve.HostedConfig, error) {
		hosted := &serve.HostedConfig{}
		err := hostedP1Settings(hosted, nil, func(name string) string { return values[name] })
		return hosted, err
	}
	hosted, err := cfgFor(map[string]string{
		"CODEAF_SERVER_KEY": " " + good + " ", "CODEAF_HOSTED_SESSION_CAP_USD": "2.50",
		"CODEAF_HOSTED_GIT_HOSTS": "https://github.com, https://git.example.test:8443,",
	})
	if err != nil || hosted.ServerKey != good || hosted.SessionCapUSD != 2.5 ||
		!slices.Equal(hosted.GitHosts, []string{"https://github.com", "https://git.example.test:8443"}) || hosted.CompleterFor == nil {
		t.Fatalf("settings: %+v, %v", hosted, err)
	}
	if hosted, err = cfgFor(nil); err != nil || hosted.SessionCapUSD != 0 || hosted.GitHosts != nil || hosted.CompleterFor == nil {
		t.Fatalf("with nothing set the defaults stand: %+v, %v", hosted, err)
	}
	for name, values := range map[string]map[string]string{
		"a cap of nothing":                 {"CODEAF_HOSTED_SESSION_CAP_USD": "0"},
		"a cap in words":                   {"CODEAF_HOSTED_SESSION_CAP_USD": "five"},
		"a git host that is not an origin": {"CODEAF_HOSTED_GIT_HOSTS": "github.com/org/repo"},
		"a server key of the wrong size":   {"CODEAF_SERVER_KEY": "abc123"},
	} {
		_, err := cfgFor(values)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		for _, v := range values {
			if len(v) > 5 && strings.Contains(err.Error(), v) {
				t.Errorf("%s: the sentence repeats the value: %v", name, err)
			}
		}
	}
}

// P2-5
// The factory reads a hosted heartbeat from one second through one minute and refuses unusable values at start.
func TestP2_HostedHeartbeatSetting(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 5 * time.Second}, {"  1500ms ", 1500 * time.Millisecond},
		{"1s", time.Second}, {"1m", time.Minute},
		{"zero", 0}, {"999ms", 0}, {"1m1ns", 0}, {"-5s", 0}, {"0", 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			hosted := &serve.HostedConfig{}
			err := hostedFactorySettings(hosted, nil, func(name string) string {
				if name == "CODEAF_HOSTED_HEARTBEAT" {
					return tc.value
				}
				return ""
			})
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), "CODEAF_HOSTED_HEARTBEAT") {
					t.Fatalf("unusable heartbeat did not stop startup with the setting's name: %v", err)
				}
			} else if err != nil || hosted.Heartbeat != tc.want {
				t.Fatalf("heartbeat = %v, %v, want %v", hosted.Heartbeat, err, tc.want)
			}
		})
	}
}
