package main

import (
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	if needed.Plugin != "google" || needed.AskID != appAskID(agent.ID, q) || needed.Agent != agent.ID {
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

func TestAppPluginsListKeyCheckability(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	b.profileDir = t.TempDir()
	list, err := b.ListPlugins()
	if err != nil {
		t.Fatal(err)
	}
	keys, checkable := 0, 0
	for _, plugin := range list.Plugins {
		if plugin.Kind != "key" {
			continue
		}
		keys++
		raw, err := json.Marshal(plugin)
		if err != nil {
			t.Fatal(err)
		}
		want := `"key_checkable":false`
		if plugin.ID == "breezy" || plugin.ID == "monaco" {
			want = `"key_checkable":true`
			checkable++
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s: missing %s", plugin.ID, want)
		}
	}
	if keys != 99 || checkable != 2 {
		t.Fatalf("keys = %d, checkable = %d; want 99 and 2", keys, checkable)
	}
}

func TestAppPluginsKeyResultReportsOnlyPassedProbes(t *testing.T) {
	for _, tt := range []struct {
		name, id, key, failure string
		status, calls          int
		checked                bool
	}{
		{name: "unchecked", id: "stripe", key: "fixture-key"},
		{name: "checked", id: "breezy", key: "fixture-key", status: 200, calls: 1, checked: true},
		{name: "refused", id: "breezy", key: "fixture-key", status: 401, calls: 1, failure: "refused"},
		{name: "no answer", id: "breezy", key: "fixture-key", calls: 1, failure: "no_answer"},
		{name: "shape", id: "breezy", key: "two keys", failure: "shape"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			original := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://api.breezy.hr/v3/companies" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				if tt.status == 0 {
					return nil, fmt.Errorf("fixture: no answer")
				}
				return &http.Response{StatusCode: tt.status, Status: http.StatusText(tt.status), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			b := newFakeBackend(newAppRecorder(), nil)
			defer b.Close()
			b.profileDir = t.TempDir()
			answer, err := b.ConnectPluginKey(appwire.PluginKeyParams{ID: tt.id, Key: tt.key})
			if err != nil || answer.Error != tt.failure {
				t.Fatalf("answer = %+v, err = %v; want failure %q", answer, err, tt.failure)
			}
			raw, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			if want := fmt.Sprintf(`"checked":%t`, tt.checked); !strings.Contains(string(raw), want) {
				t.Errorf("answer = %s; want %s", raw, want)
			}
			if calls != tt.calls {
				t.Errorf("probe requests = %d; want %d", calls, tt.calls)
			}
			if got := v3Connect(b.profileDir).Connected(tt.id); got != (tt.failure == "") {
				t.Errorf("connected = %t; failure = %q", got, tt.failure)
			}
		})
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
	if _, err := b.AnswerInbox(appInboxID(agent.ID, appAskID(agent.ID, q)), "1", "", "", ""); err != nil {
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
	if got := rec.data[i].(appwire.PluginsNeededDone).AskID; got != appAskID(agent.ID, q) {
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

func TestAppPluginsAskIDsAreUniqueAcrossAgents(t *testing.T) {
	rec := newAppRecorder()
	work1, work2 := t.TempDir(), t.TempDir()
	conv1, conv2 := &fakeConversation{}, &fakeConversation{}
	conversations := map[string]*fakeConversation{work1: conv1, work2: conv2}
	b := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conversations[ask.Workspace], Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	defer b.Close()
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	b.mint = func(work string) (string, error) { return filepath.Join(work, "transcript.jsonl"), nil }
	first, err := mustCreate(t, b, appwire.CreateAsk{Workspace: work1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mustCreate(t, b, appwire.CreateAsk{Workspace: work2})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("fixture must create distinct agents")
	}
	q := session.ConnectQuestion("connect-1", "Google", false, "", false)
	q.Subject.Name = "google"
	askIDs := make([]string, 0, 2)
	for _, item := range []struct {
		agent appwire.Agent
		conv  *fakeConversation
	}{{first, conv1}, {second, conv2}} {
		item.conv.onAnswer = func(answer session.Answer) {
			item.conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
		}
		item.conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
		i := rec.waitFor(t, time.Second, func(event string, data any) bool {
			needed, ok := data.(appwire.PluginsNeeded)
			return ok && needed.Agent == item.agent.ID
		})
		rec.mu.Lock()
		askIDs = append(askIDs, rec.data[i].(appwire.PluginsNeeded).AskID)
		rec.mu.Unlock()
	}
	if askIDs[0] == askIDs[1] {
		t.Errorf("two agents emitted the same ask_id: %q", askIDs[0])
	}
	for i, item := range []struct {
		agent appwire.Agent
		conv  *fakeConversation
	}{{first, conv1}, {second, conv2}} {
		if i == 0 {
			if err := b.AnswerAskWith(item.agent.ID, askIDs[i], appwire.AskAnswer{Option: "2"}); err != nil {
				t.Fatal(err)
			}
		} else {
			item.conv.mu.Lock()
			end := item.conv.endLane
			item.conv.mu.Unlock()
			end()
		}
		rec.waitFor(t, time.Second, func(event string, data any) bool {
			done, ok := data.(appwire.PluginsNeededDone)
			return ok && done.AskID == askIDs[i]
		})
	}
}
