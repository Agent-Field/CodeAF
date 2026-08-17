// STUB(connect-cap): replaced by the owner branch on merge.

// This file exists so the v3 settings surface can be built and tested against
// the capability contract while the engine half of it lands on its own branch.
// It holds NOTHING the owner branch would want to keep: the states live in
// process memory and are gone when aforge exits. Delete this file on merge —
// the three methods and the two types below are the whole of what the surface
// calls, and the real implementation answers the same signatures.

package connect

import (
	"fmt"
	"strings"
	"sync"
)

// Capability is one thing a connected service may do for you, in the sentence a
// person would use out loud. THE PHRASE IS THE WHOLE OF WHAT A SCREEN SHOWS —
// not the scope, not the API, not the verb the vendor calls it by.
type Capability struct {
	// ID is the stable key the state is stored under.
	ID string
	// Phrase is what a person reads: "read your mail".
	Phrase string
	// Acts says this capability CHANGES something out in the world rather than
	// only looking at it. It is what decides the default: looking is allowed,
	// acting asks first.
	Acts bool
}

// CapabilityState is the answer standing for one capability.
type CapabilityState string

const (
	// StateYes goes ahead without asking.
	StateYes CapabilityState = "yes"
	// StateAsk asks first, every time.
	StateAsk CapabilityState = "ask"
	// StateOff refuses, and the agent is told the service cannot do it.
	StateOff CapabilityState = "off"
)

// googleCapabilities is the set the surface is built against.
var googleCapabilities = []Capability{
	{ID: "mail-read", Phrase: "read your mail"},
	{ID: "mail-send", Phrase: "send mail as you", Acts: true},
	{ID: "calendar-read", Phrase: "read your calendar"},
	{ID: "calendar-write", Phrase: "put things on your calendar", Acts: true},
}

// stubStates is where a set state lives until the owner branch gives it a file.
// The key is service + "/" + capability.
var stubStates sync.Map

// Capabilities is what one service may be asked to do, in the order a screen
// lists them. A service nobody has described answers with nothing, and a screen
// that gets nothing shows nothing.
func (m *Manager) Capabilities(service string) []Capability {
	if strings.TrimSpace(service) != "google" {
		return nil
	}
	return append([]Capability(nil), googleCapabilities...)
}

// CapabilityState is where one capability stands: what was last set, or the
// default — looking is yes, acting asks.
func (m *Manager) CapabilityState(service, capability string) CapabilityState {
	found, ok := findCapability(service, capability)
	if !ok {
		return StateOff
	}
	if state, held := stubStates.Load(capabilityKey(service, capability)); held {
		return state.(CapabilityState)
	}
	if found.Acts {
		return StateAsk
	}
	return StateYes
}

// SetCapabilityState writes one answer. It refuses in plain language, which is
// the only part of this stub the surface depends on being real: a refusal is
// shown, never swallowed.
func (m *Manager) SetCapabilityState(service, capability string, state CapabilityState) error {
	if _, ok := findCapability(service, capability); !ok {
		return fmt.Errorf("%s cannot do that", service)
	}
	switch state {
	case StateYes, StateAsk, StateOff:
	default:
		return fmt.Errorf("%q is not an answer", string(state))
	}
	stubStates.Store(capabilityKey(service, capability), state)
	return nil
}

func capabilityKey(service, capability string) string {
	return strings.TrimSpace(service) + "/" + strings.TrimSpace(capability)
}

func findCapability(service, capability string) (Capability, bool) {
	if strings.TrimSpace(service) != "google" {
		return Capability{}, false
	}
	for _, c := range googleCapabilities {
		if c.ID == strings.TrimSpace(capability) {
			return c, true
		}
	}
	return Capability{}, false
}
