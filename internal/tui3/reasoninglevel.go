package tui3

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE REASONING LEVEL IS A FACT THIS SURFACE HOLDS, NOT A QUESTION IT ASKS
// WHILE DRAWING.
//
// ── THE DEFECT ──────────────────────────────────────────────────────────────
//
// The top bar spells the level onto the model segment (topbar.go's
// [app.topBarRightFit]), the picker spells it onto every row it lists
// (palette.go), and the settings sheet spells it onto its own. All three read
// it through [app.reasoningFor], and until this file existed that method asked
// THE AGENT, on the draw path, once per reader per frame.
//
// At home that is a map read under the session's mutex and costs nothing worth
// a name. Over `--host` the agent is a handle on another machine
// (internal/remote's Agent.ReasoningFor) and the same line is a ROUND TRIP DOWN
// AN SSH PIPE, with a ten-second deadline on it. Measured over a loopback
// client before this file was written:
//
//	100 frames                        100 far calls — one per frame
//	200 pointer motions over the foot  200 far calls — one per MOTION EVENT,
//	                                   because a hover below the conversation
//	                                   rebuilds the chrome to find out what row
//	                                   it is on (view.go's [app.chromeAt])
//	one frame with /model open          13 far calls — one per visible row
//
// A pointer crossing the bottom of the window sends one motion per cell, and
// every one of them put a synchronous round trip in front of the update loop.
// That is the whole of "even hover seems to slow everything down": the loop was
// not slow at painting, it was waiting on the network once per cell, and every
// key and click behind it in the queue waited too.
//
// ── THE RULE ────────────────────────────────────────────────────────────────
//
// NOTHING ON THE DRAW PATH TOUCHES THE AGENT. [app.reasoningFor] answers from
// [app.levels] and from nowhere else. A level this surface has not been told
// about yet is queued ([app.wantLevel]) and answered as no level at all, which
// is what the emptiness law already draws as nothing; the frame clock asks the
// agent off the loop ([app.levelKick]) and the answer lands as a message, one
// frame later, exactly as the far machine's file facts do (remotefiles.go,
// whose shape this follows deliberately).
//
// AND THE MOMENTS THAT CANNOT AFFORD TO BE A FRAME LATE ARE SEEDED DIRECTLY,
// because each is already a place where the surface talks to the agent and a
// person is waiting: the session opening ([newApp]), a model switch
// ([app.switchModel]), a conversation being attached ([app.attachConversation]),
// and the first ctrl+t against a row nobody has asked about ([app.cycleReasoning],
// whose cycle is relative and so cannot start from "not told yet"). So the status
// row names the level on its FIRST frame and not its second.
//
// A LEVEL THIS SURFACE SET IS NEVER ASKED ABOUT AT ALL. ctrl+t writes through
// ([app.cycleReasoning]), so the picker row under the cursor changes on the very
// next frame at home and over a connection alike — the one thing a cache in
// front of a knob must not get wrong.
//
// ── AND THE WHOLE TABLE ARRIVES AT ONCE WHERE THE ENGINE CAN SEND IT ────────
//
// The one-at-a-time asks above are the FALLBACK. An agent that can hand over
// every level it holds in one answer ([levelSource]) is asked for all of them at
// the moments this surface already talks to it — the session opening, a
// conversation being attached, a model switch, and every turn end — and the
// table is COMPLETE from the first frame rather than a frame behind the first
// row that needed a level. Nothing is queued, because nothing is unknown.
//
// Over a connection that answer is free: internal/remote's Agent reads it out of
// the replica the engine keeps fresh by push (replica.go), so the table is fed
// rather than filled and this surface never asks the far machine about a level
// at all. At home it is one copy of a small map under the session's own lock,
// once a turn.
//
// ── WHAT IT CAN BE WRONG ABOUT, SAID OUT LOUD ───────────────────────────────
//
// Between two of those moments the table is a photograph. A level dialled on
// ANOTHER window attached to the same hosted session reaches this one at the end
// of the next turn — the engine states its facts there — and not the instant it
// changes. That is the whole of the bound. An agent with no bulk door keeps the
// older bound: a level it was never asked about is asked for once and then kept
// for the life of the agent.
//
// The three doors that change a level HERE — this surface's own ctrl+t,
// `--reasoning` at launch (which the door sets before [newApp] reads it), and a
// model switch — are all seeded or written through above, so nothing a person
// does on this screen is ever a frame late.

// levelSource is an agent that can hand over every level it holds in one
// answer. It is asserted rather than added to [Agent] on [effortDialer]'s terms:
// a method on that interface is a method thirty test doubles have to grow, and a
// session that cannot answer it simply falls back to the one-at-a-time asks
// above — which is the whole reason those are kept.
type levelSource interface {
	ReasoningLevels() map[string]string
}

// levelsSeed replaces the table with everything the agent holds, and reports
// whether it could. It is called from the moments this surface already speaks to
// the agent and never from a draw.
//
// IT REPLACES RATHER THAN MERGES, because the answer is the whole truth: a level
// this table holds and the agent does not is a level somebody cycled back to off
// on another window, and merging would keep it standing for ever.
func (a *app) levelsSeed() bool {
	if a.agent == nil {
		return false
	}
	source, ok := a.agent.(levelSource)
	if !ok {
		return false
	}
	held := source.ReasoningLevels()
	clear(a.levels)
	clear(a.levelWanted)
	a.levelWant = nil
	for id, level := range held {
		a.keepLevel(id, level)
	}
	return true
}

// levelBatchMax is how many model ids one background ask carries.
//
// It is a bound on WORK and not on correctness: there is no bulk question on
// the wire — [session.Agent.ReasoningFor] answers about one model — so a batch
// is that many round trips on one goroutine, and a picker scrolled through a
// three-hundred-model catalog must not turn into three hundred of them at once
// in front of the fetches somebody is actually waiting on. Sixteen covers every
// row a list can show at once, which is all a frame can have asked for.
const levelBatchMax = 16

// levelWantMax bounds the queue, on [remoteFactsMax]'s reasoning: a surface
// that has queued that many ids has drawn far more rows than any list shows,
// and the tail is asked on the frames after the head comes back.
const levelWantMax = 512

// levelsMsg is one background ask coming back. It carries the AGENT it asked so
// that an answer about a conversation that has since been replaced is dropped
// rather than written down — /new and /resume swap the agent under this
// surface, and a level from the old one is a fact about somebody else's
// session.
type levelsMsg struct {
	agent   Agent
	learned map[string]string
}

// reasoningFor is the level held for a model id, and it is the ONE reader the
// draw path has. "" is both "no level" and "not told yet", which are the same
// thing on the screen — see the header for why that is the honest answer rather
// than a swallowed one.
func (a *app) reasoningFor(id string) string {
	if a.agent == nil || id == "" {
		return ""
	}
	// THE ID IS FOLDED THE WAY THE SESSION FOLDS IT ([session.ReasoningKey]), and
	// this table is keyed the same way, because a level set from a picker row and
	// looked up by a `/model <slug>` typed in another case is ONE level. Two
	// spellings of the folding rule is a level that goes missing on the surface
	// while the engine still holds it.
	key := session.ReasoningKey(id)
	if level, known := a.levels[key]; known {
		return level
	}
	a.wantLevel(key)
	return ""
}

// learnLevel asks the agent about one model NOW and writes the answer down. It
// is the seam for the three moments a frame of lateness would show — see the
// header — and it is called from keystrokes and from the boot, never from a
// draw.
func (a *app) learnLevel(id string) {
	if a.agent == nil || id == "" {
		return
	}
	// THE WHOLE TABLE IF IT CAN BE HAD, because it costs the same as one answer
	// where the agent can give it and leaves nothing for the queue to discover.
	if a.levelsSeed() {
		return
	}
	a.keepLevel(id, a.agent.ReasoningFor(id))
}

// keepLevel writes one level down, keeping the table bounded and taking the id
// out of the queue it may have been sitting in.
func (a *app) keepLevel(id, level string) {
	key := session.ReasoningKey(id)
	if key == "" {
		return
	}
	if a.levels == nil {
		a.levels = make(map[string]string, 16)
	}
	if len(a.levels) >= levelWantMax {
		clear(a.levels)
	}
	a.levels[key] = level
	delete(a.levelWanted, key)
}

// forgetLevels drops everything held, which is what an agent being REPLACED
// means: the map lives on the session (internal/session's agent.go), so /new
// starts empty and a resumed conversation has its own.
func (a *app) forgetLevels() {
	clear(a.levels)
	clear(a.levelWanted)
	a.levelWant = nil
}

// wantLevel puts one id in the next background ask, once.
func (a *app) wantLevel(id string) {
	if a.levelWanted == nil {
		a.levelWanted = make(map[string]bool, 16)
	}
	if a.levelWanted[id] || len(a.levelWant) >= levelWantMax {
		return
	}
	a.levelWanted[id] = true
	a.levelWant = append(a.levelWant, id)
}

// levelsWaiting reports whether anything is still owed an answer, so the frame
// clock goes on turning until it lands ([app.paint]). Without it a level
// discovered on the last frame of a burst would sit unasked until something
// unrelated repainted the row.
func (a *app) levelsWaiting() bool {
	return a.levelAsking || len(a.levelWant) > 0
}

// levelKick sends the next background ask, or nothing. It is called from the
// frame clock and from nowhere else, which is what makes it a debounce: one ask
// in flight at any moment, however many rows the render pass queued.
func (a *app) levelKick() tea.Cmd {
	if a.agent == nil || a.levelAsking || len(a.levelWant) == 0 {
		return nil
	}
	n := min(levelBatchMax, len(a.levelWant))
	batch := append([]string(nil), a.levelWant[:n]...)
	a.levelWant = append(a.levelWant[:0], a.levelWant[n:]...)
	a.levelAsking = true
	agent := a.agent
	return func() tea.Msg {
		learned := make(map[string]string, len(batch))
		for _, id := range batch {
			learned[id] = agent.ReasoningFor(id)
		}
		return levelsMsg{agent: agent, learned: learned}
	}
}

// levelsBack writes one answer into the table and repaints the rows it touched.
func (a *app) levelsBack(msg levelsMsg) tea.Cmd {
	a.levelAsking = false
	// AN ANSWER ABOUT A CONVERSATION THAT IS NO LONGER OPEN IS DROPPED. The ids
	// stay out of the queue, which is right: whatever asked for them is asking
	// again on the next frame it draws, against the agent that is standing now.
	if msg.agent != a.agent {
		return nil
	}
	for id, level := range msg.learned {
		a.keepLevel(id, level)
	}
	a.touch()
	return a.wake()
}
