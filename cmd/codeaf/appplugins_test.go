package main

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAppPluginsShareStoreAndQuestionAnswer(t *testing.T) {
	conv := &fakeConversation{}
	b, rec, agent := workBackend(t, conv)
	b.profileDir = t.TempDir()
	m := v3Connect(b.profileDir)
	if err := m.SetCapabilityState("google", "mail-read", connect.StateOff); err != nil {
		t.Fatal(err)
	}
	d, err := b.DescribePlugin("google")
	if err != nil || d.Capabilities[0].State != connect.StateOff {
		t.Fatalf("describe = %+v %v", d, err)
	}
	q := session.ConnectQuestion("connect-1", "Google", false, "", false)
	q.Subject.Name = "google"
	conv.onAnswer = func(answer session.Answer) {
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
	}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventPluginsNeeded))
	rec.mu.Lock()
	needed := rec.data[i].(appwire.PluginsNeeded)
	rec.mu.Unlock()
	if needed.Plugin != "google" || needed.AskID != appAskID(q) || needed.Agent != agent.ID {
		t.Fatalf("needed = %+v", needed)
	}
	if err := b.AnswerAsk(agent.ID, needed.AskID, "2", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventPluginsNeededDone))
	asks, err := b.ListAsks(agent.ID)
	if err != nil || len(asks) != 0 {
		t.Fatalf("asks after not now: %+v %v", asks, err)
	}
}

func TestAppPluginsListKeyShapeAndProviderPolicy(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	b.profileDir = t.TempDir()
	list, err := b.ListPlugins()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Plugins) != len(v3Connect(b.profileDir).Services()) || len(list.Featured) != 4 || list.FeaturedWords["slack"].What != "Channels, threads and DMs" {
		t.Fatalf("list: %+v", list)
	}
	answer, err := b.ConnectPluginKey(appwire.PluginKeyParams{ID: "stripe", Key: "two keys"})
	if err != nil || answer.Error != "shape" {
		t.Fatalf("key shape: %+v %v", answer, err)
	}
	if err := b.SetPluginCapability(appwire.PluginCapabilityParams{ID: "google", Capability: "mail-send", State: connect.StateYes}); err != nil {
		t.Fatal(err)
	}
	if got := v3Connect(b.profileDir).CapabilityState("google", "mail-send"); got != connect.StateYes {
		t.Fatalf("capability store: %s", got)
	}
	if err := config.WriteSources(b.profileDir, []config.PersistedSource{{ID: "z-ai", Written: "Z.ai"}}); err != nil {
		t.Fatal(err)
	}
	settings, err := b.SetSettings(appwire.SettingsPatch{WhenPlanPaused: map[string]string{"z-ai": config.PlanPausedUseMeter}})
	if err != nil || settings.WhenPlanPaused["z-ai"] != config.PlanPausedUseMeter {
		t.Fatalf("policy: %+v %v", settings.WhenPlanPaused, err)
	}
	rows := config.PersistedSources(b.profileDir)
	if len(rows) != 1 || rows[0].PlanPaused != config.PlanPausedUseMeter {
		t.Fatalf("source store: %+v", rows)
	}
}

func TestAppPluginsInboxAnswersConnectedKey(t *testing.T) {
	conv := &fakeConversation{}
	b, rec, agent := workBackend(t, conv)
	b.profileDir = t.TempDir()
	q := session.ConnectQuestion("connect-key", "Stripe", true, "", true)
	q.Subject.Name = "stripe"
	conv.onAnswer = func(answer session.Answer) {
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
	}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventPluginsNeeded))
	// The bundled Stripe entry declares no probe, so this makes no network call.
	result, err := b.ConnectPluginKey(appwire.PluginKeyParams{ID: "stripe", Key: "fake-plugin-key"})
	if err != nil || result.Error != "" {
		t.Fatalf("connect: %+v %v", result, err)
	}
	if _, err := b.AnswerInbox(appInboxID(agent.ID, appAskID(q)), "1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 7*time.Second, appAskEvent(appwire.EventPluginsNeededDone))
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if len(conv.answers) != 1 || conv.answers[0].Key != "1" || conv.answers[0].Change != "" {
		t.Fatalf("answer: %+v", conv.answers)
	}
}

func TestAppPluginsQuestionDoneWhenConversationDies(t *testing.T) {
	conv := &fakeConversation{}
	_, rec, agent := workBackend(t, conv)
	q := session.ConnectQuestion("connect-dies", "Google", false, "", false)
	q.Subject.Name = "google"
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventPluginsNeeded))
	conv.mu.Lock()
	end := conv.endLane
	conv.mu.Unlock()
	end()
	i := rec.waitFor(t, 2*time.Second, appAskEvent(appwire.EventPluginsNeededDone))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := rec.data[i].(appwire.PluginsNeededDone).AskID; got != appAskID(q) {
		t.Fatalf("%s closed %s", agent.ID, got)
	}
}

func TestAppPluginsRoundTwoPauseFacts(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	b.profileDir = t.TempDir()
	reset := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	b.phaseNews(session.PhaseNews{Phase: provider.PhasePlanPaused, SourceID: "z-ai", PlanReset: reset})
	list, err := b.ListPlugins()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	if !strings.Contains(string(raw), `"plan_paused":{"resets_at":"`+reset+`"}`) {
		t.Fatalf("missing plan pause: %s", raw)
	}
	b.phaseNews(session.PhaseNews{Phase: provider.PhaseWriting, SourceID: "z-ai", Door: "pay-as-you-go"})
	list, _ = b.ListPlugins()
	raw, _ = json.Marshal(list)
	if !strings.Contains(string(raw), `"plan_paused"`) {
		t.Fatal("metered overflow erased plan pause")
	}
	b.phaseNews(session.PhaseNews{Phase: provider.PhaseWriting, SourceID: "z-ai", Door: "coding plan"})
	list, _ = b.ListPlugins()
	raw, _ = json.Marshal(list)
	if strings.Contains(string(raw), `"plan_paused"`) {
		t.Fatalf("successful plan still paused: %s", raw)
	}
	b.phaseNews(session.PhaseNews{Phase: provider.PhasePlanPaused, SourceID: "z-ai", PlanReset: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)})
	list, _ = b.ListPlugins()
	raw, _ = json.Marshal(list)
	if strings.Contains(string(raw), `"plan_paused"`) {
		t.Fatal("past reset still paused")
	}
}

func TestAppPluginsOllamaProbeDoesNotBlockList(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("probe path = %s", r.URL.Path)
		}
		close(entered)
		<-release
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()
	defer close(release)
	b, _, _ := appProviderTest(t)
	b.vendored = appStubbed("ollama", func(source *modelsource.Source) { source.Address = server.URL + "/v1" })
	read := func() appwire.PluginProvider {
		list, err := b.ListPlugins()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range list.Providers {
			if p.ID == "ollama" {
				return p
			}
		}
		t.Fatal("missing Ollama")
		return appwire.PluginProvider{}
	}
	view := read()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	if view.Running != nil {
		t.Fatal("probe blocked list or invented result")
	}
	deadline := time.Now().Add(time.Second)
	for view.Running == nil && time.Now().Before(deadline) {
		runtime.Gosched()
		view = read()
	}
	if view.Running == nil || *view.Running {
		t.Fatal("stalled Ollama did not time out as not running")
	}
}

func TestAppPluginsProviderAccountAndKeyVariable(t *testing.T) {
	b, _, profile := appProviderTest(t)
	t.Setenv("PLUGIN_PLAN_KEY", "fake-plugin-provider-key")
	if err := config.WriteSources(profile, []config.PersistedSource{{ID: "z-ai", Written: "z-ai", KeyEnv: "PLUGIN_PLAN_KEY", Door: "coding-plan"}}); err != nil {
		t.Fatal(err)
	}
	if err := codexauth.Save(profile, codexauth.Tokens{Email: "person@example.test", AccessToken: "fake-access", RefreshToken: "fake-refresh"}); err != nil {
		t.Fatal(err)
	}
	list, err := b.ListPlugins()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, p := range list.Providers {
		if p.ID == "z-ai" {
			seen++
			if p.KeyEnv != "PLUGIN_PLAN_KEY" {
				t.Fatalf("key variable = %q", p.KeyEnv)
			}
		}
		if p.ID == "codex" {
			seen++
			if p.Account != "person@example.test" {
				t.Fatalf("account = %q", p.Account)
			}
		}
		if p.ConnectedAt != "" {
			t.Fatalf("invented provider date = %q", p.ConnectedAt)
		}
	}
	if seen != 2 {
		t.Fatalf("metadata providers = %d", seen)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "fake-plugin-provider-key") || strings.Contains(string(raw), "fake-access") {
		t.Fatal("secret leaked in provider facts")
	}
}

func TestAppPluginsDefaultProviderKeyVariable(t *testing.T) {
	b, _, _ := appProviderTest(t)
	t.Setenv("OPENAI_API_KEY", "sk-or-fake-plugin-fallback")
	list, err := b.ListPlugins()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Providers {
		if p.ID == "openrouter" {
			if p.KeyEnv != "OPENAI_API_KEY" {
				t.Fatalf("key_env = %q, want OPENAI_API_KEY", p.KeyEnv)
			}
			return
		}
	}
	t.Fatal("missing OpenRouter")
}

func TestAppPluginsOllamaRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"models":[]}`)) }))
	defer server.Close()
	b, _, _ := appProviderTest(t)
	b.vendored = appStubbed("ollama", func(source *modelsource.Source) { source.Address = server.URL + "/v1" })
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		list, err := b.ListPlugins()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range list.Providers {
			if p.ID == "ollama" && p.Running != nil {
				if !*p.Running {
					t.Fatal("Ollama answered but running=false")
				}
				return
			}
		}
		runtime.Gosched()
	}
	t.Fatal("Ollama probe never became running")
}
