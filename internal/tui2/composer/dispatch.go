package composer

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The `@` grammar's send half (5.18): what leaves the composer when the draft
// carries a mention, and what the two send chords mean.
//
// The composer's authority stops at the edge of the rectangle it draws. It
// knows the user addressed a target, whether that target was settled, and
// whether they asked to follow. It does NOT know what a dispatch does — the
// dispatch echo row, the receipt vocabulary (8.2.11), the journal, the routing
// of a settled target's message to the main head as referenced context — all of
// that is the wiring's, and none of it is guessed at here. This file's whole
// job is to hand the wiring an unambiguous fact.

// followKey is the power chord (5.18): enter sends and stays, ctrl+enter sends
// and follows into the addressed task's room. It is a constant rather than an
// [Options] field because, unlike SendKey, it is not something the shell's
// capability negotiation has an opinion about — ctrl+enter is spellable on
// every terminal this shell supports, including tmux (10.1.2's constraint is
// about shift+enter and key releases).
//
// It is bound ONLY while the draft carries a mention. Before then the chord is
// not relevant, so it stays an unbound no-op exactly as it was — which is also
// what makes the dispatch chip's appearance the honest signal 5.22 asks for:
// the key becomes real at the same moment its hint does.
const followKey = "ctrl+enter"

// Dispatch is what an addressed send hands [Options.OnDispatch].
type Dispatch struct {
	// TargetID is [Target.ID] of the addressed target — the first mention in
	// the draft (see [Model.firstMention]).
	TargetID string
	// Settled reports that the addressed target's thread is closed. 5.18 is
	// blunt about what that has to mean: a settled target never receives direct
	// injection, because a dead thread has no one to absorb it, and the message
	// goes to the main head with that task as referenced context instead. The
	// composer marks it; the wiring routes it.
	Settled bool
	// Follow reports that the send was made with ctrl+enter — "send and follow"
	// rather than "send and stay". A send must never teleport the composer's
	// context on its own (5.18: spatial stability outranks convenience), so
	// this is only ever true when the user explicitly asked.
	Follow bool
	// Text is the trimmed draft, mention token included. The token is left in
	// the text on purpose: the addressed thread's own transcript should read
	// the way the user wrote it, and stripping the word would make the sent
	// message and the echo row disagree about what was said.
	Text string
}

// submit is the send path for both chords. OnDispatch fires only for a draft
// that carries a mention AND a wiring that asked for dispatches; every other
// send is OnSubmit, unchanged, including an addressed send under a nil
// OnDispatch — a composer that dropped those on the floor would be losing user
// speech to a half-adopted option.
//
// The gate on an empty trimmed draft, the recall ring and the clear are exactly
// where they were: bracketed-paste-never-sends and blank-enter-never-sends
// still share this one door.
func (m *Model) submit(follow bool) tea.Cmd {
	trimmed := strings.TrimSpace(string(m.value))
	if trimmed == "" {
		return nil
	}
	if mn, ok := m.firstMention(); ok && m.onDispatch != nil {
		m.onDispatch(Dispatch{
			TargetID: mn.target.ID,
			Settled:  mn.target.Settled,
			Follow:   follow,
			Text:     trimmed,
		})
	} else if m.onSubmit != nil {
		m.onSubmit(trimmed)
	}
	m.remember(trimmed)
	m.reset()
	return nil
}
