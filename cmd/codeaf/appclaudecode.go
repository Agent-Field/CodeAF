package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/claudeagent"
	"github.com/Agent-Field/codeaf/internal/claudemodel"
)

// claudeStatusTTL is how long one reading of Claude Code's sign-in is shown
// before it is read again. Reading it starts the program, so a settings page
// drawn many times a minute does not start it every time.
const claudeStatusTTL = time.Minute

var claudeStatusCache struct {
	sync.Mutex
	at      time.Time
	status  claudeagent.Status
	reading bool
}

// claudeStatus is the cached reading of Claude Code's sign-in on the machine
// this engine runs on, and whether there is one yet.
//
// A LISTING NEVER WAITS FOR CLAUDE CODE. Reading the sign-in starts the
// program, which takes seconds, and providers.list sits in front of
// models.list and every page that asks whether anything is connected. So a
// reading that is missing or stale is started in the background, the row is
// drawn from what is known now, and provider.changed redraws it when the
// reading lands.
func (b *appBackend) claudeStatus() (claudeagent.Status, bool) {
	claudeStatusCache.Lock()
	defer claudeStatusCache.Unlock()
	known := !claudeStatusCache.at.IsZero()
	if known && time.Since(claudeStatusCache.at) < claudeStatusTTL {
		return claudeStatusCache.status, true
	}
	if !claudeStatusCache.reading {
		claudeStatusCache.reading = true
		go b.readClaudeStatus()
	}
	return claudeStatusCache.status, known
}

// readClaudeStatus takes one reading and says the row changed.
func (b *appBackend) readClaudeStatus() {
	status := claudeagent.ReadStatus(context.Background())
	claudeStatusCache.Lock()
	claudeStatusCache.status, claudeStatusCache.at, claudeStatusCache.reading = status, time.Now(), false
	claudeStatusCache.Unlock()
	if b.emit != nil {
		b.providerIDChanged(claudemodel.ID, "")
	}
}

// claudeStatusNow reads the sign-in again and waits for it, for a person who
// has just pressed Check.
func (b *appBackend) claudeStatusNow() claudeagent.Status {
	status := claudeagent.ReadStatus(context.Background())
	claudeStatusCache.Lock()
	defer claudeStatusCache.Unlock()
	claudeStatusCache.status, claudeStatusCache.at = status, time.Now()
	return status
}

func (b *appBackend) forgetClaudeStatus() {
	claudeStatusCache.Lock()
	defer claudeStatusCache.Unlock()
	claudeStatusCache.at = time.Time{}
}

// claudeCodeRowPlan is the row's plan before and after a reading: with none
// yet, a program that is not there is said at once (finding it starts
// nothing), and one that is there says nothing until its sign-in is read.
func claudeCodeRowPlan(status claudeagent.Status, known bool) string {
	if known {
		return claudeCodePlanWord(status)
	}
	if _, err := claudeagent.Binary(); err != nil {
		return claudeCodePlanWord(claudeagent.Status{})
	}
	return ""
}

// claudeCodePlanWord is the provider row's plan: how the installed Claude Code
// is signed in, in the words a person uses for it.
func claudeCodePlanWord(status claudeagent.Status) string {
	switch {
	case !status.Installed:
		return "not installed on this computer"
	case !status.LoggedIn:
		return "installed, not signed in"
	case status.OnPlan() && status.Plan != "":
		return "Claude " + strings.ToUpper(status.Plan[:1]) + status.Plan[1:] + " plan"
	case status.OnPlan():
		// THE PLAN IS SAID ONLY WHEN CLAUDE CODE SAID IT: an unknown plan is
		// nothing, never a guess.
		return ""
	default:
		return "API key · billed per call"
	}
}

// claudeCodeCheck answers keys.check for Claude Code: whether it can be
// connected now, and the models it would bring.
func claudeCodeCheck(status claudeagent.Status) appwire.KeyCheck {
	switch {
	case !status.Installed:
		_, err := claudeagent.Binary()
		return appwire.KeyCheck{OK: false, Message: err.Error()}
	case !status.LoggedIn:
		return appwire.KeyCheck{OK: false, Message: "Claude Code is installed but not signed in: run `claude` once and sign in"}
	}
	found := "Found Claude Code"
	if status.Version != "" {
		found += " " + status.Version
	}
	check := appwire.KeyCheck{OK: true, Message: strings.Join(claudeWords(found, "signed in", claudeCodePlanWord(status)), " · ")}
	for _, m := range claudemodel.Models() {
		check.Models = append(check.Models, appwire.Model{ID: m.ID, Name: m.Name, Provider: claudemodel.ID})
	}
	return check
}

// claudeWords drops the empty words from a line, so a fact nobody reported leaves
// no stray separator behind.
func claudeWords(words ...string) []string {
	out := words[:0]
	for _, w := range words {
		if strings.TrimSpace(w) != "" {
			out = append(out, w)
		}
	}
	return out
}
