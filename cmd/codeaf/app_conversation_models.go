package main

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func appDefaultFallback(profile string) string {
	first, _, _ := strings.Cut(settingsRow(profile, config.KeyModelFallbacks), ",")
	return strings.TrimSpace(first)
}

// The existing Settings fallback is copied once. A saved conversation's pair
// wins on reopen; changing another chat or the defaults never overwrites it.
func enableAppConversationModels(conv any, desired ...*session.ConversationModels) error {
	identity, ok := conv.(interface{ ModelIdentity() session.ModelIdentity })
	if !ok {
		return nil
	} // old/test doors remain readable, never falsely relabelled
	state := identity.ModelIdentity()
	if state.Models != nil {
		return nil
	}
	setter, ok := conv.(interface {
		SetConversationModels(session.ConversationModels) ([]session.ModelSwitch, error)
	})
	if !ok {
		return nil
	}
	main := state.Selected
	if main == "" {
		if m, ok := conv.(interface{ Model() string }); ok {
			main = m.Model()
		}
	}
	fallback := appDefaultFallback(config.ProfileDir())
	if state.LockedReason != "" || fallback == main {
		fallback = ""
	}
	pair := session.ConversationModels{Main: main, Fallback: fallback}
	if len(desired) > 0 && desired[0] != nil {
		pair = *desired[0]
	}
	_, err := setter.SetConversationModels(pair)
	return err
}

func appModelSwitchEntry(id string, n session.ModelSwitch) appwire.Entry {
	return appwire.Entry{ID: "model-switch-" + n.ID, Agent: id, At: n.At, Kind: appwire.EntryNotice, Text: n.Text, Quiet: true, NoticeKind: "model_switch"}
}

// A factory stage's model pair is conversation-local; its budget and posture
// remain behind the existing lent-stage fence. This intentionally bypasses only
// the old job-wide model preference, never those other controls.
func (b *appBackend) SetConversationModels(id string, pair session.ConversationModels) (appwire.Agent, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if _, _, err := b.ensureLocked(a); err != nil {
		return appwire.Agent{}, err
	}
	var live *appLive
	b.locked(func() { live = a.live })
	door, ok := live.conn.Conv.(interface {
		SetConversationModels(session.ConversationModels) ([]session.ModelSwitch, error)
	})
	if !ok {
		return appwire.Agent{}, fmt.Errorf("main/fallback selection requires a newer engine")
	}
	notes, err := door.SetConversationModels(pair)
	if err != nil {
		return appwire.Agent{}, err
	}
	var view appwire.Agent
	b.locked(func() {
		a.view.Model = live.conn.Conv.Model()
		if identity, ok := live.conn.Conv.(interface{ ModelIdentity() session.ModelIdentity }); ok {
			state := identity.ModelIdentity()
			a.view.ModelIdentity = &state
		}
		view = a.view
	})
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	for _, n := range notes {
		b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appModelSwitchEntry(id, n)})
	}
	return view, nil
}
