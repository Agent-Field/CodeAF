package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE LIVE SET ────────────────────────────────────────────────────────────
//
// The task strip is gone (ISSUE-126): what is running is the top bar's crumb
// and the status row's state word now, and the phone tier's deck carries the
// live set in its own rows. What survives the strip is the live set itself —
// the nodes, the order, the parent seam they hang from — because the deck and
// the roster read the same facts, and a fact two surfaces draw is a fact that
// belongs to neither of them.

// stripKey is a node's own key in the alphabet [taskNode.ParentID] speaks.
func stripKey(node *taskNode) string { return itoa(int(node.id)) }

// nodesByKey indexes every node this session has admitted by that key. It is
// ONE index because three surfaces climb the same seam — the roster's forest
// ([app.railKin]), the crumb's walk up the parents (topbar.go's
// [app.crumbSegments]) and the one-level climb esc makes (room.go's
// [app.roomStepUp]) — and a map each was three walks of [app.taskOrder] per
// frame that could disagree about which nodes exist.
func (a *app) nodesByKey() map[string]*taskNode {
	byKey := make(map[string]*taskNode, len(a.taskOrder))
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil {
			byKey[stripKey(node)] = node
		}
	}
	return byKey
}

// ── THE PARENT SEAM ─────────────────────────────────────────────────────────
//
// These two are the whole of what the roster's forest reads (task.go's
// [app.railKin]). The parent is filled from the engine's own updates
// (session's TaskNotice.Parent), and TWO KINDS OF WORK FILL IT: an adaptive
// run, which takes one row with a row under it for every node it cuts
// (session's orchestrate.go), and a TASK THAT SPLIT ITS OWN BRIEF, whose pieces
// are registered under it (session's task.go). Neither is anything to this
// package: a family is a family. A session that has run neither answers ""
// and false to both, which is the flat row, unchanged — and that is what keeps
// the roster honest about a session that has only ever run one thing at a
// time.

func (n *taskNode) ParentID() string { return n.parent }

// Paused reports whether this task is HELD rather than working: an adaptive run
// stopped at its fuel gate, waiting for a person to top it up or finish it
// (session's EventOrchestratePause). It is not a state the engine moves a node
// through, which is why it is a fact of its own — a paused node is still
// running as far as the run is concerned, and it is not moving as far as a
// person is concerned, and the second reading is the one a roster owes them.
func (n *taskNode) Paused() bool { return n.paused }

// glyphPaused marks work held at a gate. It is the transport bar every device a
// person owns pauses with, and it is deliberately NOT the queued circle: a
// queued node has not started and this one has, and the difference is the whole
// of what the gate is asking about.
const (
	glyphPaused      = "⏸"
	glyphPausedASCII = "="
)

// stripOrder is the order the live set comes in, and it is not the roster's.
// The column leads with what is asking for a decision because a person reads it
// top to bottom looking for work to do. The live set leads with what is RUNNING
// because it is a presence: the first entry is the thing a person is waiting
// on. What is parked and what is done are not on it — the live set is the live
// set, and the roster is where a session's history lives.
var stripOrder = [...]railGroup{railRunning, railAttention, railIdle}

// stripNodes is the live set, in the order it is drawn.
func (a *app) stripNodes() []*taskNode {
	members := a.railMembers()
	out := make([]*taskNode, 0, len(a.taskOrder))
	for _, g := range stripOrder {
		out = append(out, members[g]...)
	}
	return out
}

// stripPausedGlyph is work stopped at a gate, in the hue the roster gives every
// other state that is waiting on a person to say something (task.go's
// [app.railGlyph] paints the unverified question the same way).
func (a *app) stripPausedGlyph() string {
	return a.pal.warn(a.linearMark(glyphPaused, glyphPausedASCII))
}

// liveShowing reports whether the live set is drawn anywhere the paint clock
// has to serve. The strip that once asked this is gone; what asks it now is the
// phone tier's deck (statusdeck.go), which draws the live set as its own rows
// and turns the same spinner on them, and a running sub-harness, whose panel
// is alive for minutes at a time. A wide frame with the roster closed shows the
// live set nowhere — the top bar's glyph is the open room's, not the session's
// — and a paint clock with nothing to repaint is an hour of nothing.
func (a *app) liveShowing() bool {
	width, _ := a.size()
	// A RUNNING SUB-HARNESS RAISES THE CLOCK even where the roster is standing
	// (harnesspanel.go). It is alive for minutes at a time and the run happens
	// inside one tool call, so the transcript shows a single row that has not
	// come back yet, and the roster cannot carry it either: its rows are the
	// session's task nodes, and a harness run is not one.
	if _, running := a.runningHarness(); running {
		return true
	}
	// THE DECK IS THE ONE SURFACE LEFT THAT DRAWS THE LIVE SET, and it is a
	// phone-tier surface: below [hudTight] the frame is a deck, and the deck's
	// task rows turn the same spinner the strip's chips once did.
	if layoutTier(width) != tierPhone {
		return false
	}
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil && node.state == session.TaskRunning {
			return true
		}
	}
	return false
}
