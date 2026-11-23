package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE APP'S DOOR ONTO A CONVERSATION'S OWN APPROVAL POSTURE.
//
// internal/session holds the posture (approvalposture.go) and the terminal's
// chip walks it (internal/tui3's approvalchip.go). The app reaches the same
// door through the conversation it holds: agents.set_approval_posture moves
// it, agents.create sets it before the first turn, and the Agent view says
// where it stands and who decided. Block is Settings' answer and is never set
// from here; a factory stage refuses the door (app_lend.go).

// appApprovalDial is a held conversation whose gate the app can read.
type appApprovalDial interface {
	ApprovalDial() bool
	ResolvedApprovalPosture() string
}

// appApprovalOf is what the Agent view says about a held conversation's gate:
// the resolved posture, "chat" when this conversation's own meta.json keeps a
// word other than auto, and what it would follow without one. Nil when the
// conversation has no gate.
func appApprovalOf(conv any, transcript string) *appwire.AgentApproval {
	dial, ok := conv.(appApprovalDial)
	if !ok || !dial.ApprovalDial() {
		return nil
	}
	posture := dial.ResolvedApprovalPosture()
	if posture == "" {
		return nil
	}
	source := appwire.ApprovalFromSettings
	if stored := appStoredApproval(transcript); stored != "" && stored != session.PostureAuto {
		source = appwire.ApprovalFromChat
	}
	// WHAT IT FOLLOWS IS THE GATE'S OWN STANDING, which reads the repository's
	// approval mode before Settings: settings.approval_posture is the profile's
	// answer only, and a chat in a repository that sets its own follows that.
	follows := ""
	if standing, ok := conv.(interface{ StandingApprovalPosture() string }); ok {
		follows = standing.StandingApprovalPosture()
	}
	if follows == "" && source == appwire.ApprovalFromSettings {
		follows = posture
	}
	return &appwire.AgentApproval{Posture: posture, Source: source, Follows: follows}
}

// reapproveHeld says again where each held chat's gate stands once the
// approval mode it may follow moved: the view it was held with says the old one.
func (b *appBackend) reapproveHeld() {
	type held struct {
		a          *appAgent
		conv       appConversation
		transcript string
	}
	var all []held
	b.locked(func() {
		for _, a := range b.agents {
			if a.live != nil {
				all = append(all, held{a, a.live.conn.Conv, a.transcript})
			}
		}
	})
	for _, h := range all {
		approval := appApprovalOf(h.conv, h.transcript)
		var view appwire.Agent
		changed, said := false, false
		b.locked(func() {
			was := h.a.view.Approval
			if changed = (was == nil) != (approval == nil) || (was != nil && *was != *approval); changed {
				h.a.view.Approval = approval
				view, said = h.a.view, h.a.saidLocked()
			}
		})
		if changed && said {
			b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
	}
}

// appStoredApproval is the posture this conversation itself keeps, "" when
// nobody set one. It is read from the folder because the held conversation
// is reached across a connection that carries only the resolved word.
func appStoredApproval(transcript string) string {
	if strings.TrimSpace(transcript) == "" {
		return ""
	}
	meta, err := session.LoadMeta(session.DirOf(transcript))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(meta.Approval)
}

// appStandingPosture is what Settings amount to in the posture ladder: the
// approval mode, with the guardian standing in front of Ask when its row is on.
// It is the profile's answer, as asks_before is.
func appStandingPosture(profile string) string {
	switch config.ToolApprovalModeAt(profile) {
	case "allow":
		return session.PostureAllow
	case "deny":
		return session.PostureDeny
	}
	if config.GuardianEnabledAt(profile) {
		return session.PostureGuardian
	}
	return session.PostureAsk
}

// SetApprovalPosture moves one conversation's gate, opening the conversation
// first if none is held. auto hands it back to Settings.
func (b *appBackend) SetApprovalPosture(id, posture string) (appwire.Agent, error) {
	posture, ok := appwire.AppApprovalPosture(posture)
	if !ok {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "posture is ask, guardian, allow or auto")
	}
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if err := b.lentFence(a, "approval posture"); err != nil {
		return appwire.Agent{}, err
	}
	if _, _, err := b.ensureLocked(a); err != nil {
		return appwire.Agent{}, err
	}
	var live *appLive
	var transcript string
	b.locked(func() {
		live, transcript = a.live, a.transcript
		live.lastUse = b.now()
	})
	if live.conn.Lent {
		return appwire.Agent{}, errLentStage("approval posture")
	}
	door, ok := live.conn.Conv.(appPostureDoor)
	if !ok {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeEngine, "this conversation has no dial onto what runs without asking; update the engine and reconnect")
	}
	if err := door.SetApprovalPosture(posture); err != nil {
		return appwire.Agent{}, appEngineError(err)
	}
	approval := appApprovalOf(live.conn.Conv, transcript)
	var view appwire.Agent
	b.locked(func() {
		a.view.Approval = approval
		view = a.view
	})
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return view, nil
}

// bootDials sets a created conversation's own effort and posture, which
// agents.create carried, before its first turn can run.
func bootDials(conv appConversation, boot *appDials) error {
	if boot == nil {
		return nil
	}
	if boot.effort != "" {
		dial, ok := conv.(interface{ SetConversationEffort(string) bool })
		if !ok || !dial.SetConversationEffort(boot.effort) {
			return appwire.Errorf(appwire.CodeEngine, "this conversation has no thinking dial; update the engine and reconnect")
		}
	}
	if boot.posture != "" {
		door, ok := conv.(appPostureDoor)
		if !ok {
			return appwire.Errorf(appwire.CodeEngine, "this conversation has no dial onto what runs without asking; update the engine and reconnect")
		}
		if err := door.SetApprovalPosture(boot.posture); err != nil {
			return appEngineError(err)
		}
	}
	return nil
}
