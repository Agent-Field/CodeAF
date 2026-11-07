package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
)

// AN APP-RUN ENGINE KEEPS ITS CONNECTIONS ACROSS LAUNCHES. A credential a
// door gave this launch (here an AgentField key) reaches the app as
// credential.changed, carrying the key for the app's secure store; the next
// launch's keys.set hands it back and every reader has it again, with no
// profile write. A forgotten one reaches the app as a removal.
func TestACredentialADoorKeptComesBackAtTheNextLaunch(t *testing.T) {
	profile := appDayProfile(t)
	first := config.NewAppCredentials()
	restore := config.UseAppCredentials(first)
	rec := newAppRecorder()
	b := newAppBackend(rec, appOpenInProcess)
	first.Keep(config.AppAgentFieldProvider, "af-key-0000000001")
	i := rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.CredentialChanged)
		return event == appwire.EventCredentialChanged && ok && c.Provider == config.AppAgentFieldProvider
	})
	rec.mu.Lock()
	kept := rec.data[i].(appwire.CredentialChanged)
	rec.mu.Unlock()
	if !kept.Kept || kept.Key != "af-key-0000000001" {
		t.Fatalf("the app was told %+v", kept)
	}
	first.Forget(config.AppJiraProvider)
	i = rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.CredentialChanged)
		return event == appwire.EventCredentialChanged && ok && c.Provider == config.AppJiraProvider
	})
	rec.mu.Lock()
	gone := rec.data[i].(appwire.CredentialChanged)
	rec.mu.Unlock()
	if gone.Kept || gone.Key != "" {
		t.Fatalf("a removal was told as %+v", gone)
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
}
