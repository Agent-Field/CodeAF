package session

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestModelIdentityTracksActualProviderModelAndPendingSwitch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		served := body.Model
		if body.Model == "vendor/alias" {
			served = "vendor/concrete"
		}
		fmt.Fprintf(w, "data: {\"id\":\"r\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"visible\"}}]}\n\n", served)
		w.(http.Flusher).Flush()
		if body.Model == "vendor/alias" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	dir := t.TempDir()
	a, err := New(Config{Workspace: t.TempDir(), Place: Place{Dir: dir}, Model: "vendor/alias", APIKey: "test", BaseURL: server.URL, OneModel: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Submit(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	// Wait for the streamed model identity, not merely the HTTP write.
	deadline := time.Now().Add(time.Second)
	for a.ModelIdentity().Active != "vendor/concrete" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := a.ModelIdentity(); got.Active != "vendor/concrete" || got.Selected != "vendor/alias" || got.Mode != "fixed" {
		t.Fatalf("in-flight identity: %+v", got)
	}
	for event := range events {
		if event.Kind == EventTextDelta {
			break
		}
	}
	a.SetModel("vendor/new")
	if got := a.ModelIdentity(); got.Active != "vendor/concrete" || got.Selected != "vendor/new" || !got.Pending {
		t.Fatalf("pending identity: %+v", got)
	}
	once.Do(func() { close(release) })
	collect(t, events)
	if got := a.ModelIdentity(); got.Active != "" || got.Last != "vendor/concrete" || got.Pending {
		t.Fatalf("finished identity: %+v", got)
	}
	a.SettleWrites()
	meta, err := LoadMeta(dir)
	if err != nil || meta.ModelIdentity == nil || meta.ModelIdentity.Last != "vendor/concrete" {
		t.Fatalf("saved identity: %+v, %v", meta.ModelIdentity, err)
	}
	events, err = a.Submit(ctx, "next")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if got := a.ModelIdentity(); got.Last != "vendor/new" {
		t.Fatalf("next request: %+v", got)
	}
}

func TestModelIdentityRestoresLastUseWithoutRevivingAnActiveRequest(t *testing.T) {
	dir := t.TempDir()
	if err := SaveMeta(dir, Meta{ID: "restored", Model: "vendor/next", ModelIdentity: &ModelIdentity{Epoch: "previous", Active: "vendor/stale", Last: "vendor/last", Revision: 5}}); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Place = Place{Dir: dir} })
	got := a.ModelIdentity()
	if got.Active != "" || got.Last != "vendor/last" || got.Epoch == "previous" || got.Epoch == "" {
		t.Fatalf("restored identity: %+v", got)
	}
}

func TestModelIdentityDeclaresAutomaticPolicyAndRecordsFallback(t *testing.T) {
	c := chained([]string{"other/model"}, blindCutStep(provider.CutSilent, ""), blindCutStep(provider.CutSilent, ""), func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil })
	a, _ := newTestAgent(t, c, nil)
	if a.ModelIdentity().Mode != "automatic" {
		t.Fatal("automatic policy was hidden")
	}
	events, err := a.Submit(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if got := a.ModelIdentity(); got.Selected != "test/model" || got.Last != "other/model" || got.Active != "" {
		t.Fatalf("fallback identity: %+v", got)
	}
}

func TestModelIdentityFixedPolicyBlocksFallbackAndPersists(t *testing.T) {
	c := chained([]string{"other/model"})
	a, _ := newTestAgent(t, c, func(c *Config) { c.Place = Place{Dir: t.TempDir()} })
	if err := a.SetAutomaticModels(false); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.nextFallback(context.Background(), "test/model", nil); ok {
		t.Fatal("fixed selection allowed a fallback")
	}
	if got := a.ModelIdentity(); got.Mode != "fixed" {
		t.Fatalf("policy: %+v", got)
	}
	a.SettleWrites()
	meta, err := LoadMeta(a.config.Place.Dir)
	if err != nil || meta.AutomaticModels == nil || *meta.AutomaticModels {
		t.Fatalf("saved policy: %+v %v", meta.AutomaticModels, err)
	}
	if err := a.SetAutomaticModels(true); err != nil {
		t.Fatal(err)
	}
	if next, ok := a.nextFallback(context.Background(), "test/model", nil); !ok || next != "other/model" {
		t.Fatalf("explicit automatic selection: %q %v", next, ok)
	}
	hard, _ := newTestAgent(t, chained(nil), func(c *Config) { c.OneModel = true })
	if err := hard.SetAutomaticModels(true); err == nil {
		t.Fatal("hard one-model launch was overridden")
	}
}

func TestModelIdentityChangingPolicyDoesNotRewriteAnInFlightRequest(t *testing.T) {
	a, _ := newTestAgent(t, chained(nil), nil)
	a.startModelRequest("vendor/fallback", "provider")
	if err := a.SetAutomaticModels(false); err != nil {
		t.Fatal(err)
	}
	if got := a.ModelIdentity(); got.Mode != "fixed" || got.RequestMode != "automatic" || !got.Pending || got.Active != "vendor/fallback" {
		t.Fatalf("in-flight policy: %+v", got)
	}
	a.endModelRequest("vendor/fallback")
	a.startModelRequest("vendor/preferred", "provider")
	if got := a.ModelIdentity(); got.RequestMode != "fixed" || got.Pending {
		t.Fatalf("next request: %+v", got)
	}
	a.endModelRequest("vendor/preferred")
}

func TestModelIdentityKeepsCanonicalEvidenceWhenAStreamFails(t *testing.T) {
	a, _ := newTestAgent(t, chained(nil), nil)
	a.startModelRequest("vendor/alias", "provider")
	a.servedModel("vendor/concrete")
	a.endModelRequest("vendor/alias")
	if got := a.ModelIdentity(); got.Last != "vendor/concrete" || got.Requested != "vendor/alias" {
		t.Fatalf("cut stream: %+v", got)
	}
}
