package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// An observer ends on a broken connection. Reconnect obtains another atomic
// history/current-turn boundary, rather than treating an old ownership cursor
// as permission to suppress a new live tail.
func (a *app) refreshHostedReplay() tea.Cmd {
	if a.hostReplayLoading {
		return nil
	}
	door, ok := a.agent.(attachReplayer)
	if !ok {
		return nil
	}
	if a.hostCalls > 0 {
		a.hostReplayWaiting = true
		return nil
	}
	a.hostReplayWaiting = false
	a.hostReplayLoading = true
	return a.offLoop(func() func(bool) tea.Cmd {
		entries, events, stop := door.AttachReplay()
		return func(here bool) tea.Cmd {
			if !here {
				if stop != nil {
					stop()
				}
				return nil
			}
			if a.streamStop != nil {
				a.streamStop()
				a.streamStop = nil
			}
			a.gen++
			a.stream = nil
			// THE SENTENCES THIS WINDOW SAID ABOUT ITSELF ARE NOT IN THE HOST'S
			// RECORD, so the rebuild below would take them away: a refused move's
			// `could not move this chat here` was read and then gone, because a
			// road swapped under it. They are lifted out now and put back below.
			kept := a.takeLocalNotes()
			// The new observer starts at the current turn's beginning. Its
			// earlier visible prefix is replaced, not retained as "in the gap".
			a.entries = nil
			a.recordRows = 0
			a.turn = 0
			abandonLive(a.entries, &a.live)
			abandonLive(a.entries, &a.think)
			a.echoAt, a.sel = -1, -1
			a.unfolded = map[int]bool{}
			a.rows, a.rowsWidth = nil, 0
			a.hudStale = true
			a.dropHover()
			a.replayList(entries)
			a.putLocalNotes(kept)
			var joined tea.Cmd
			if events != nil {
				joined = a.adoptTurn(events, stop)
			} else {
				a.state = stateIdle
			}
			a.touch()
			return tea.Batch(joined, a.finishHostedReplay())
		}
	})
}

func (a *app) finishHostedReplay() tea.Cmd {
	a.hostReplayLoading = false
	pending := a.hostReplayPending
	a.hostReplayPending = nil
	var cmds []tea.Cmd
	for _, msg := range pending {
		if msg.gen == a.convGen {
			if a.hostReplayLoading {
				a.hostReplayPending = append(a.hostReplayPending, msg)
			} else if msg.turn.Replay {
				cmds = append(cmds, a.refreshHostedReplay())
			} else {
				cmds = append(cmds, a.admitFollowing(msg.turn))
			}
		}
	}
	if !a.hostReplayLoading {
		if a.deferredModelRename != nil {
			a.ensureAvailableModel()
		}
		deferred := a.hostDeferred
		a.hostDeferred = nil
		for _, start := range deferred {
			cmds = append(cmds, start())
		}
		cmds = append(cmds, a.startFollow())
	}
	return tea.Batch(cmds...)
}

// A snapshot and a locally initiated turn cannot own the surface at the same
// time. Calls already sent settle before refresh begins; new calls wait until
// its atomic boundary has been folded. The composer itself remains responsive.
type hostCall struct{ generation int }

func (a *app) deferHosted(start func() tea.Cmd) bool {
	if !a.hostReplayLoading && !a.hostReplayWaiting {
		return false
	}
	a.hostDeferred = append(a.hostDeferred, start)
	return true
}

func (a *app) hostCallStarted() *hostCall {
	if _, ok := a.agent.(interface{ ReplayCovers(session.Event) bool }); !ok {
		return nil
	}
	a.hostCalls++
	return &hostCall{generation: a.convGen}
}

func (a *app) hostCallSettled(call *hostCall) tea.Cmd {
	if call == nil || call.generation != a.convGen {
		return nil
	}
	if a.hostCalls > 0 {
		a.hostCalls--
	}
	if a.hostCalls == 0 && a.hostReplayWaiting {
		return a.refreshHostedReplay()
	}
	return nil
}

func (a *app) hostStreamCovered(ch <-chan session.Event) func() bool {
	if agent, ok := a.agent.(interface {
		ReplayCoversStream(<-chan session.Event) func() bool
	}); ok {
		return agent.ReplayCoversStream(ch)
	}
	return nil
}

// ── THE SENTENCES A LIVE VIEW SAID ABOUT ITSELF ─────────────────────────────

// heldNote is one local note lifted out of the entry list across a snapshot
// refresh, with where it stood: the entry it followed, and how many entries of
// the host's record came before it.
type heldNote struct {
	note    entry
	after   entryKey
	hasPrev bool
	before  int
}

// entryKey is what names a drawn entry well enough to find it again after the
// list was rebuilt from the record: the same kind, the same words, the same
// tool. The record and the live stream draw one conversation, so a finished
// entry has the same key whichever of them drew it.
type entryKey struct {
	kind entryKind
	text string
	tool string
}

func keyOfEntry(e entry) entryKey { return entryKey{kind: e.kind, text: e.text, tool: e.tool} }

// takeLocalNotes lifts the notes this window wrote about itself out of the
// drawn entries, in order, before a live view's snapshot refresh rebuilds them
// from the host's record. It answers nothing for any window that is not a live
// view: a conversation held over ssh keeps the refresh it always had.
func (a *app) takeLocalNotes() []heldNote {
	if a.liveFront() == nil {
		return nil
	}
	var (
		held    []heldNote
		prev    entryKey
		hasPrev bool
		before  int
	)
	for _, e := range a.entries {
		if e.kind == entryNote && e.local {
			held = append(held, heldNote{note: e, after: prev, hasPrev: hasPrev, before: before})
			continue
		}
		prev, hasPrev = keyOfEntry(e), true
		before++
	}
	return held
}

// putLocalNotes puts the lifted notes back into the rebuilt entries, each right
// after the entry it followed and never before one it came after, so a sentence
// the person has read stays where they read it and the host's own news that
// arrived during the refresh lands below it.
//
// A NOTE THE RECORD ALREADY SAYS IS NOT PUT BACK, because the record is the
// authority: the same words twice would be the surface repeating itself. An
// entry that cannot be found again (the one it followed was part of a running
// turn, which the stream redraws from its first event) is placed by how many
// entries of the record came before it, and failing that at the end.
func (a *app) putLocalNotes(held []heldNote) {
	if len(held) == 0 {
		return
	}
	base := a.entries
	said := make(map[string]bool, len(held))
	for _, e := range base {
		if e.kind == entryNote {
			said[e.text] = true
		}
	}
	slots := make([]int, 0, len(held))
	kept := make([]heldNote, 0, len(held))
	floor := 0
	for _, h := range held {
		if said[h.note.text] {
			continue
		}
		at := min(h.before, len(base))
		if !h.hasPrev {
			at = 0
		} else {
			for i := len(base) - 1; i >= 0; i-- {
				if base[i].kind != entryNote && keyOfEntry(base[i]) == h.after {
					at = i + 1
					break
				}
			}
		}
		at = max(at, floor)
		floor = at
		slots = append(slots, at)
		kept = append(kept, h)
	}
	if len(kept) == 0 {
		return
	}
	merged := make([]entry, 0, len(base)+len(kept))
	next := 0
	for i := 0; i <= len(base); i++ {
		for next < len(kept) && slots[next] == i {
			note := kept[next].note
			if i > 0 {
				note.turn = base[i-1].turn
			}
			merged = append(merged, note)
			next++
		}
		if i < len(base) {
			merged = append(merged, base[i])
		}
	}
	a.entries = merged
	// EVERYTHING ON SCREEN NOW CAME FROM THE RECORD OR WAS LIFTED OUT OF IT, so
	// nothing is "said since the replay" ([app.recordRows]): a notes row sitting
	// among the record's would otherwise make the rows after it look like the gap.
	a.recordRows = len(merged)
}
