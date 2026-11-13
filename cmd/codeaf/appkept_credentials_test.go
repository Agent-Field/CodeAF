package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
)

// AN APP-RUN ENGINE KEEPS ITS CONNECTIONS ACROSS LAUNCHES. A credential a
// door gave this launch (here an AgentField key) is announced by an empty
// credentials.changed — THE KEY NEVER RIDES AN EVENT — and taken once with
// credentials.take; the next launch's keys.set hands it back and every reader
// has it again, with no profile write, and nothing is queued for it again. A
// forgotten one is taken as a removal.
func TestACredentialADoorKeptComesBackAtTheNextLaunch(t *testing.T) {
	profile := appDayProfile(t)
	first := config.NewAppCredentials()
	restore := config.UseAppCredentials(first)
	rec := newAppRecorder()
	b := newAppBackend(rec, appOpenInProcess)
	first.Keep(config.AppAgentFieldProvider, "af-key-0000000001")
	first.Forget(config.AppJiraProvider)
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		return event == appwire.EventCredentialsChanged && data == appwire.Empty{}
	})
	taken, err := b.CredentialsTake(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []appwire.CredentialChange{{Provider: config.AppAgentFieldProvider, Key: "af-key-0000000001"}, {Provider: config.AppJiraProvider, Removed: true}}
	if !reflect.DeepEqual(taken.Changes, want) {
		t.Fatalf("the app took %+v", taken.Changes)
	}
	if again, _ := b.CredentialsTake(context.Background()); len(again.Changes) != 0 {
		t.Fatalf("a change was answered twice: %+v", again.Changes)
	}
	b.Close()
	restore()

	// The next launch: a fresh process's keys, handed back by the app.
	defer config.UseAppCredentials(config.NewAppCredentials())()
	next := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer next.Close()
	if got := config.AgentFieldKeyAt(profile); got != "" {
		t.Fatalf("a fresh launch already has %q", got)
	}
	for provider, key := range map[string]string{config.AppAgentFieldProvider: "af-key-0000000001", config.AppAgentFieldNodeProvider: "node-secret-01", config.AppJiraProvider: "jira-token-01"} {
		if err := next.SetKey(provider, key); err != nil {
			t.Fatalf("keys.set %s refused: %v", provider, err)
		}
	}
	if config.AgentFieldKeyAt(profile) != "af-key-0000000001" || config.AgentFieldNodeSecretAt(profile) != "node-secret-01" || config.JiraTokenAt(profile) != "jira-token-01" {
		t.Fatal("a handed-back credential is not read")
	}
	if echoed, _ := next.CredentialsTake(context.Background()); len(echoed.Changes) != 0 {
		t.Fatalf("the hand-back was queued to be kept again: %+v", echoed.Changes)
	}
}
