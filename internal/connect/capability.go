package connect

// The capability model: what a person has agreed their accounts may be used
// for, said in their own sentences rather than in tool names.
//
// This is the law for the wiring wave, and it is written HERE rather than as a
// second package comment so that connect.go's — "the accounts layer" — goes on
// being the first thing a reader of the package is told. Two package comments
// are legal and get concatenated in filename order, which would have put the
// capability model ahead of the sentence explaining what the package is.
//
// A connection is one grant — the whole mailbox, the whole calendar — because
// that is the only shape a browser trip has. What a person actually wants to
// decide is finer than that and it is not technical: reading their mail is not
// sending it, and a meeting that lands on somebody else's calendar is not a
// meeting they read about. So each plug declares a handful of CAPABILITIES,
// each one a sentence, and each sentence carries one of three states.
//
// ── THE THREE STATES, FOR THE WIRING WAVE ──
//
// STATE OFF MEANS THE TOOL IS NOT ON THE BELT AND THE CAPABILITY IS ABSENT FROM
// THE SERVICES LISTING. Off is not a refusal at the gate: a refused call costs a
// turn, teaches the model to try again in different words, and puts a question
// in front of somebody who already answered it. A capability that is off is a
// capability the conversation never learns exists.
//
// STATE ASK MEANS THE APPROVAL GATE PROMPTS. The call is armed, the model may
// make it, and the person is asked before it runs — the ordinary consent prompt,
// with the recipient and the subject in it, exactly as internal/approval's floor
// under a blanket allow produces today.
//
// STATE YES MEANS THE NAMED-RULE ALLOW. It is the person saying the sentence
// internal/approval already understands: not the blanket default, which cannot
// vouch for a message it has not seen, but the rule that NAMES the tool. So yes
// on a capability that acts is worth exactly what `gmail_send:allow` is worth,
// and it is worth that because a person wrote it about that capability.
//
// THE MID-CHAT "ALWAYS" ANSWER IS THE SAME SENTENCE, SAID IN A DIFFERENT ROOM.
// When somebody answers "always" to a consent prompt, that answer must land here
// — SetCapabilityState(service, capability, StateYes) — and not in a second
// remembered-allow list of its own. ONE VOCABULARY, ONE STORE: a person who said
// always in a conversation must find that capability set to yes in the panel,
// and a person who sets it to off in the panel must stop being asked in the
// conversation. Two stores would drift the first time somebody used both.
//
// ── WHAT IS NOT DECIDED HERE ──
//
// This file is the model and the memory, and nothing else. It arms no tool,
// prompts nobody, and imports nothing of the surface. The wiring wave reads
// [Manager.ToolCapability] and [Manager.CapabilityState] at the two moments that
// matter — when a family is armed, and when a call is judged — and every law
// above is that wave's to keep.

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Capability is one sentence a person can say yes, ask or off to.
type Capability struct {
	// ID is the stable slug: what the panel keys a row on, what the tool map
	// points at, and what is written to disk. It outlives phrasing changes,
	// so a reworded sentence never loses somebody's answer.
	ID string
	// Phrase is the person's sentence — "read your mail", "send mail as you"
	// — written to be read on a line beside a control, in their vocabulary
	// and never in the machinery's.
	Phrase string
	// Acts reports that this capability REACHES OUTSIDE THIS MACHINE IN THE
	// PERSON'S NAME. It is the same distinction internal/approval draws with
	// its table of tools a blanket allow cannot vouch for, and it is drawn
	// once here per capability rather than per tool: a search that was not
	// wanted costs a moment, a message that was not wanted has been read by
	// somebody else by the time anyone notices.
	//
	// It decides the default state and nothing else. See [defaultState].
	Acts bool
}

// capabilitySet is what one service declares: its sentences, in the order they
// are shown, and which of its tools each sentence owns.
type capabilitySet struct {
	capabilities []Capability
	tools        map[string]string
}

var (
	capabilityMu       sync.RWMutex
	capabilityRegistry = map[string]capabilitySet{}
)

// RegisterCapabilities declares what one service can be used for. Like
// [Register] it is meant to be called from a package file's init, and like
// [Register] it panics rather than returning an error: a declaration that failed
// would not fail here but silently later, as a settings panel missing a row
// nobody thought to check.
//
// THE DECLARATION LIVES WITH THE PLUG. A switch over service names in this file
// would put every future vendor's vocabulary in one source and make this package
// a merge point for work that has nothing to do with capabilities — the same
// reason [Register] exists at all.
//
// tools maps a tool name to the ID of the capability that owns it. Several tools
// may share one capability; a tool may belong to at most one.
func RegisterCapabilities(service string, capabilities []Capability, tools map[string]string) {
	service = normalize(service)
	if service == "" {
		panic("connect: register capabilities with empty service id")
	}
	set := capabilitySet{
		capabilities: make([]Capability, 0, len(capabilities)),
		tools:        make(map[string]string, len(tools)),
	}
	declared := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		capability.ID = normalize(capability.ID)
		if capability.ID == "" {
			panic("connect: " + service + " declares a capability with no id")
		}
		if declared[capability.ID] {
			panic("connect: " + service + " declares the capability " + capability.ID + " twice")
		}
		declared[capability.ID] = true
		set.capabilities = append(set.capabilities, capability)
	}
	for tool, owner := range tools {
		tool, owner = normalize(tool), normalize(owner)
		if tool == "" {
			panic("connect: " + service + " maps a capability to an unnamed tool")
		}
		// A tool pointing at a capability nobody declared would read as a
		// tool with no capability at all, which is exactly the reading that
		// must never happen by accident: see [Manager.ToolCapability].
		if !declared[owner] {
			panic("connect: " + service + " maps the tool " + tool + " to the undeclared capability " + owner)
		}
		set.tools[tool] = owner
	}

	capabilityMu.Lock()
	defer capabilityMu.Unlock()
	if _, taken := capabilityRegistry[service]; taken {
		panic("connect: " + service + " declares its capabilities twice")
	}
	capabilityRegistry[service] = set
}

// RegisterGenericCapabilities declares the read/act pair for a service that has
// no sentences of its own to say.
//
// It is the door for the services that arrive as DATA rather than as code — a
// catalog of key-based connections, where nobody has written a file per vendor
// and nobody is going to. Those services still divide the same way every service
// divides: what only looks, and what leaves the machine in the person's name. So
// they get the same two rows and the same three states, and the panel cannot
// tell them apart from a plug that declared four.
func RegisterGenericCapabilities(service string, tools map[string]string) {
	RegisterCapabilities(service, genericCapabilities(), tools)
}

// genericCapabilities is the read/act pair, phrased WITHOUT a service name
// because the caller has one and this does not: the panel renders these rows
// under the service's own heading, where "read what is in this account" is a
// whole sentence and "read what is in your Notion account" would be a stutter.
func genericCapabilities() []Capability {
	return []Capability{
		{ID: "read", Phrase: "read what is in this account", Acts: false},
		{ID: "act", Phrase: "act in this account in your name", Acts: true},
	}
}

// Capabilities lists what one service can be used for, in a stable order.
//
// THE ORDER IS THE DECLARATION'S ORDER, not the alphabet's. It is the opposite
// choice from [sortPlugs] and for the same underlying reason: plugs arrive from
// separate files in init order, which reshuffles when somebody renames a source
// file, while a service's capabilities are one list written in one place by
// somebody who put reading before sending on purpose.
//
// A service nobody declared capabilities for answers nothing — THE EMPTINESS
// LAW: a panel that renders no rows is honest, one that invents a row is not.
func (m *Manager) Capabilities(service string) []Capability {
	capabilityMu.RLock()
	defer capabilityMu.RUnlock()
	set, ok := capabilityRegistry[normalize(service)]
	if !ok {
		return nil
	}
	// A copy, so that a panel sorting or trimming what it was handed cannot
	// reorder the declaration for everybody else.
	return append([]Capability(nil), set.capabilities...)
}

// CapabilityState answers what a person has said about one capability, falling
// back to what this build did before anybody said anything.
//
// A CAPABILITY THIS BUILD DOES NOT HAVE IS OFF. An id nobody declared cannot be
// listed, cannot be set, and the only safe reading of it is that nothing runs
// under it.
func (m *Manager) CapabilityState(service, capability string) CapabilityState {
	service, capability = normalize(service), normalize(capability)
	declared, ok := lookupCapability(service, capability)
	if !ok {
		return StateOff
	}
	if state, stored := m.policy().get(service, capability); stored {
		return state
	}
	return defaultState(declared)
}

// SetCapabilityState remembers what a person said.
//
// Setting a capability back to its default FORGETS it rather than writing the
// default down, which is the emptiness law applied to disk: see
// [capabilityStore].
func (m *Manager) SetCapabilityState(service, capability string, state CapabilityState) error {
	if !state.valid() {
		return fmt.Errorf("unknown answer %q (want yes, ask or off)", state)
	}
	service, capability = normalize(service), normalize(capability)
	declared, ok := lookupCapability(service, capability)
	if !ok {
		return fmt.Errorf("%s has nothing called %q it can be used for", service, capability)
	}
	if state == defaultState(declared) {
		return m.policy().clear(service, capability)
	}
	return m.policy().set(service, capability, state)
}

// ToolCapability answers which capability owns one tool.
//
// NO CAPABILITY IS NOT THE SAME AS AN OFF ONE. An empty answer means no sentence
// in this build covers that tool, so the tool is judged exactly as it is today —
// by internal/approval and nothing else. A wiring that read the empty answer as
// a capability and asked for its state would be told off, and would quietly
// strip the belt of every tool no plug had declared.
func (m *Manager) ToolCapability(service, tool string) string {
	capabilityMu.RLock()
	defer capabilityMu.RUnlock()
	set, ok := capabilityRegistry[normalize(service)]
	if !ok {
		return ""
	}
	return set.tools[normalize(tool)]
}

// policy names the file this manager keeps its answers in.
//
// It is derived from where the connections themselves live rather than held as a
// field, because a manager is built from a profile directory it does not keep.
// THIS IS THE ONE PLACE THAT DERIVATION HAPPENS, so that a manager which one day
// remembers its own directory changes this line and nothing else.
func (m *Manager) policy() *capabilityStore {
	return capabilityStoreAt(filepath.Dir(m.store.path))
}

// lookupCapability finds one declared capability. Both arguments are already
// normalized by every caller.
func lookupCapability(service, capability string) (Capability, bool) {
	capabilityMu.RLock()
	defer capabilityMu.RUnlock()
	set, ok := capabilityRegistry[service]
	if !ok {
		return Capability{}, false
	}
	for _, declared := range set.capabilities {
		if declared.ID == capability {
			return declared, true
		}
	}
	return Capability{}, false
}

// normalize is the one reading of every id this file takes — a service, a
// capability, a tool. Slugs are lower case and carry no spaces, so a caller that
// hands over "Google" or " gmail_send " means the thing that was declared rather
// than a thing nobody has.
func normalize(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
