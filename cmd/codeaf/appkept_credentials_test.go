package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestQAReceivedProviderRefreshesAndReachesTheNativeKeeper(t *testing.T) {
	appDayProfile(t)
	credentials := config.NewAppCredentials()
	restore := config.UseAppCredentials(credentials)
	defer restore()
	rec := newAppRecorder()
	b := newAppBackend(rec, appOpenInProcess)
	defer b.Close()
	if !credentials.KeepIf("aiand", "", false, "FAKE-received-provider-key") {
		t.Fatal("the received provider key was not applied")
	}
	taken, err := b.CredentialsTake(context.Background())
	if err != nil || len(taken.Changes) != 1 || taken.Changes[0].Provider != "aiand" || taken.Changes[0].Key != "FAKE-received-provider-key" || taken.Changes[0].ExpectedKey == nil || *taken.Changes[0].ExpectedKey != "" {
		t.Fatal("the received provider key was not handed to the native keeper once")
	}
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		if event != appwire.EventProviderChanged {
			return false
		}
		changed := data.(appwire.ProviderChanged)
		return changed.Provider.ID == "aiand" && changed.Provider.SignedIn && changed.Key == ""
	})
}

func TestQASyncedCredentialQueueKeepsOriginalGuardAndDropsSupersededChanges(t *testing.T) {
	appDayProfile(t)
	keys := config.NewAppCredentials()
	defer config.UseAppCredentials(keys)()
	b := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer b.Close()
	provider := "aiand"
	keys.Set(provider, "FAKE-original-key")
	if !keys.KeepIf(provider, "FAKE-original-key", true, "FAKE-intermediate-key") ||
		!keys.KeepIf(provider, "FAKE-intermediate-key", true, "FAKE-current-key") {
		t.Fatal("app did not apply both synced changes")
	}
	pending, err := b.CredentialsTake(t.Context())
	if err != nil || len(pending.Changes) != 1 {
		t.Fatal("app did not combine the synced changes")
	}
	change := pending.Changes[0]
	if change.Key != "FAKE-current-key" || change.ExpectedKey == nil || *change.ExpectedKey != "FAKE-original-key" {
		t.Fatal("combined change did not keep the original comparison and the latest value")
	}
	if !keys.KeepIf(provider, "FAKE-current-key", true, "FAKE-late-sync-key") {
		t.Fatal("app did not apply the synced change")
	}
	keys.Remove(provider)
	pending, err = b.CredentialsTake(t.Context())
	if err != nil || len(pending.Changes) != 0 {
		t.Fatal("app took a change superseded by the local disconnect")
	}
}

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
