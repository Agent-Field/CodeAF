package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A TILE SAYS WHAT ITS CONVERSATION SPENT ─────────────────────────────────
//
// The wall promised what each conversation spent and drew nothing. A tile now
// ends its state line in the figure that conversation's own status line shows:
// for the conversation in front, [app.spendShown] itself; for one this window
// holds behind it, the same rule ([spendOf]) over that conversation's books and
// its tree on the ledger. Nothing spent, or nothing known, draws nothing.
//
// THE FRAME READS ONLY THE CACHE. The books are asked in the command that
// already reads the transcript ([app.wallReadCmd]); the ledger is read by one
// command of its own, at most every [wallTreeEvery] while the wall is up and a
// held tile exists, through the two doors [app.treeLines] uses (the seam, or
// the file through a cache the wall owns), and summed per conversation with
// [session.UsageTree]. A frame never asks an agent, reads a file or crosses a
// wire (framedisk_law_test.go).

// wallTreeEvery is the least time between two ledger readings for the held
// tiles. A task's spend moves on the ledger as it is made, and a tile is a
// glance: five seconds behind is still the right figure to decide on.
const wallTreeEvery = 5 * time.Second

// wallTreeMsg is one ledger reading: each held conversation's tree, by key.
type wallTreeMsg struct {
	trees map[string]float64
}

// wallSpent is tile key's figure, from memory only.
func (a *app) wallSpent(key string, tail *wallTail) float64 {
	if key == a.frontTabKey() {
		return a.spendShown()
	}
	if tail == nil {
		return 0
	}
	return spendOf(tail.books, tail.tree)
}

// wallTreeCmd reads the ledger once, off the loop, for every conversation this
// window holds behind the front, and is nil when there is nothing to read or a
// reading is out or was made less than [wallTreeEvery] ago.
func (a *app) wallTreeCmd() tea.Cmd {
	if !a.wall.on || a.wall.treeAsking || a.shared {
		return nil
	}
	now := a.now()
	if !a.wall.treeAt.IsZero() && now.Sub(a.wall.treeAt) < wallTreeEvery {
		return nil
	}
	ids := map[string]string{}
	for key, held := range a.behind {
		if held == nil || held.conv.Agent == nil {
			continue
		}
		if id := sessionIDOf(held.conv.SessionFile); id != "" {
			ids[key] = id
		}
	}
	ledger, path := a.ledger, strings.TrimSpace(a.usageLedger)
	if len(ids) == 0 || (ledger == nil && path == "") {
		return nil
	}
	if a.wall.treeCache == nil {
		a.wall.treeCache = &session.UsageCache{}
	}
	cache, from := a.wall.treeCache, session.LastDays(now, spendWindowDays).From
	a.wall.treeAsking, a.wall.treeAt = true, now
	return func() tea.Msg {
		var lines []session.UsageLine
		known := true
		if ledger != nil {
			lines, _, known = ledger(from)
		} else {
			cache.Path = path
			// A torn last line costs that line and never the figure, as it does
			// for the status line ([app.treeLines]).
			lines, _ = cache.Read(time.Time{})
		}
		trees := map[string]float64{}
		if known {
			for key, id := range ids {
				trees[key] = session.UsageTree(lines, id).Folded()
			}
		}
		return wallTreeMsg{trees: trees}
	}
}

// wallTakeTree folds one ledger reading into the tiles' cache, on the loop. A
// figure only ever grows, as the status line's does.
func (a *app) wallTakeTree(msg wallTreeMsg) {
	a.wall.treeAsking = false
	for key, tree := range msg.trees {
		tail := a.wall.tails[key]
		if tail == nil {
			continue
		}
		if tree > tail.tree {
			tail.tree = tree
			a.touch()
		}
	}
}
