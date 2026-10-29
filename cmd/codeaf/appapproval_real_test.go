package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// readAgent calls one agent method over the harness's wire.
func (h *realActionHarness) readAgent(method string, params any) appwire.Agent {
	h.t.Helper()
	raw, err := h.w.call(method, params)
	if err != nil {
		h.t.Fatal(err)
	}
	var result struct {
		Agent appwire.Agent `json:"agent"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		h.t.Fatal(err)
	}
	return result.Agent
}

// The app moves the real conversation's gate through the terminal's own door,
// says who decided it, keeps it across a restart, and hands it back to Settings.
func TestAppApprovalPostureRoundTripRealEngine(t *testing.T) {
	h := newRealActionHarness(t)
	opened := h.readAgent("agents.open", map[string]string{"id": h.agent})
	standing := appStandingPosture(config.ProfileDir())
	if opened.Approval == nil || opened.Approval.Posture != standing || opened.Approval.Source != appwire.ApprovalFromSettings {
		t.Fatalf("an untouched conversation = %+v, want %q from settings", opened.Approval, standing)
	}
	for _, word := range []string{"ask", "guardian", "allow"} {
		t.Run(word, func(t *testing.T) {
			got := h.readAgent("agents.set_approval_posture", map[string]string{"id": h.agent, "posture": word})
			if got.Approval == nil || got.Approval.Posture != word || got.Approval.Source != appwire.ApprovalFromChat {
				t.Fatalf("set %q = %+v", word, got.Approval)
			}
			_, live, err := h.b.heldLive(h.agent)
			if err != nil {
				t.Fatal(err)
			}
			if got := live.conn.Conv.(appPostureDoor).ResolvedApprovalPosture(); got != word {
				t.Fatalf("the conversation's gate stands at %q, want %q", got, word)
			}
			h.restart()
			got = h.readAgent("agents.open", map[string]string{"id": h.agent})
			if got.Approval == nil || got.Approval.Posture != word || got.Approval.Source != appwire.ApprovalFromChat {
				t.Fatalf("reopened %q = %+v", word, got.Approval)
			}
		})
	}
	got := h.readAgent("agents.set_approval_posture", map[string]string{"id": h.agent, "posture": "auto"})
	if got.Approval == nil || got.Approval.Posture != standing || got.Approval.Source != appwire.ApprovalFromSettings {
		t.Fatalf("auto = %+v, want %q from settings", got.Approval, standing)
	}
	if _, err := h.w.call("agents.set_approval_posture", map[string]string{"id": h.agent, "posture": "deny"}); err == nil || err.Code != appwire.CodeBadParams {
		t.Fatalf("deny from the app = %v", err)
	}
}

// agents.create sets the new conversation's own effort and posture before its
// first turn, through the same dials the setters use.
func TestAppCreateSetsEffortAndPostureRealEngine(t *testing.T) {
	h := newRealActionHarness(t)
	raw, failure := h.w.call("agents.create", map[string]any{"workspace": h.cwd, "text": "say done", "effort": "low", "approval_posture": "guardian"})
	if failure != nil {
		t.Fatal(failure)
	}
	var result struct {
		Agent appwire.Agent `json:"agent"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	id := result.Agent.ID
	h.rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && done.Agent == id
	})
	_, live, err := h.b.heldLive(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := live.conn.Conv.(interface{ ConversationEffort() string }).ConversationEffort(); got != "low" {
		t.Fatalf("effort = %q", got)
	}
	if got := live.conn.Conv.(appPostureDoor).ResolvedApprovalPosture(); got != session.PostureGuardian {
		t.Fatalf("posture = %q", got)
	}
	opened := h.readAgent("agents.open", map[string]string{"id": id})
	if opened.Effort != "low" || opened.Approval == nil || opened.Approval.Posture != session.PostureGuardian || opened.Approval.Source != appwire.ApprovalFromChat {
		t.Fatalf("opened = effort %q, approval %+v", opened.Effort, opened.Approval)
	}
}

// Settings say what a chat that follows them stands at, in the ladder's words.
func TestAppSettingsSayTheStandingPosture(t *testing.T) {
	h := newRealActionHarness(t)
	settings, err := h.b.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ApprovalPosture != appStandingPosture(config.ProfileDir()) || settings.ApprovalPosture == "" {
		t.Fatalf("settings posture = %q", settings.ApprovalPosture)
	}
	ask := "prompt"
	if _, err := h.b.SetSettings(appwire.SettingsPatch{AsksBefore: &ask}); err != nil {
		t.Fatal(err)
	}
	if settings, _ = h.b.Settings(); settings.ApprovalPosture != session.PostureAsk && settings.ApprovalPosture != session.PostureGuardian {
		t.Fatalf("after Ask in Settings, posture = %q", settings.ApprovalPosture)
	}
	deny := "deny"
	if _, err := h.b.SetSettings(appwire.SettingsPatch{AsksBefore: &deny}); err != nil {
		t.Fatal(err)
	}
	if settings, _ = h.b.Settings(); settings.ApprovalPosture != session.PostureDeny {
		t.Fatalf("after Block in Settings, posture = %q", settings.ApprovalPosture)
	}
}
