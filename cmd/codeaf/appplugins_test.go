package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/session"
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
