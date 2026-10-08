package tui3

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── TALK IT THROUGH: `T`, THE ITEM'S OWN CONVERSATION ───────────────────────
//
// `T` on a floor row or on the item page opens the item's own conversation:
// the one its seam made on the first press and keeps on the item after
// ([factory.Seam.Talk]). The door is asked OFF THE LOOP, in the ordered line
// like every verb, the floor is read in the same ask so the row learns it has
// a conversation, and then the conversation is opened THE WAY THE CHATS OPEN
// ONE ([app.openConversationRow]): the conversation this window was in goes on
// running behind it, and the person lands in the chat.
//
// THE WAY BACK IS `esc`. While the conversation `T` opened is the one in
// front, `esc` on its empty box with nothing running closes it into the floor
// again, on the same row and on the item page when that is where `T` was
// pressed, because the floor's cursor and its page are kept while the floor is
// not showing. It is taken once: the way back is spent by taking it, so a
// conversation reopened from the Chats is an ordinary conversation, and `esc`
// there means what it means everywhere. A running turn is still stopped by
// `esc` first, exactly as everywhere.
//
// WITH NO TALK DOOR THERE IS NO KEY: the hint does not name `T talk` and the
// key does nothing (a capability that cannot work is absent, not broken).

// factoryTalk asks the item's conversation off the loop and opens it.
func (a *app) factoryTalk(it factory.Item) tea.Cmd {
	seam, id := a.factory, it.ID
	return a.offLoop(func() func(bool) tea.Cmd {
		chat, err := seam.Talk(context.Background(), id)
		where := ""
		if err == nil {
			// THE CONVERSATION'S FOLDER IS READ HERE, off the loop: the open
			// is asked about the folder the conversation works in, which its
			// session folder's meta says.
			if meta, merr := session.LoadMeta(filepath.Dir(chat)); merr == nil {
				where = strings.TrimSpace(meta.Workspace)
			}
		}
		var snap factory.Snapshot
		var lerr error
		if seam.Load != nil {
			snap, lerr = seam.Load()
		}
		return func(bool) tea.Cmd {
			switch {
			case lerr != nil:
				a.fp.err = lerr
			case seam.Load != nil:
				a.factoryFold(snap)
			}
			if err != nil {
				a.pageMsg = strings.TrimSpace(err.Error())
				a.touch()
				return nil
			}
			a.fp.act.talk = a.convKey(chat)
			cmd := a.openConversationRow(session.SessionRow{Transcript: chat, ProjectDir: where})
			if a.pageShowing() {
				// The open refused, and said why on the floor's note line: there
				// is no conversation in front to come back from.
				a.fp.act.talk = ""
			}
			a.touch()
			return tea.Batch(cmd, a.factoryTeamsRead())
		}
	})
}

// factoryTalkBack is `esc` in the conversation `T` opened: back to the floor
// when the box is empty and nothing is running, once. It answers false for
// every other `esc`, which then means what it always meant.
func (a *app) factoryTalkBack() (tea.Cmd, bool) {
	talk := a.fp.act.talk
	if talk == "" || a.pageShowing() || a.state == stateWorking || a.asking() {
		return nil, false
	}
	if a.convKey(a.file) != talk || strings.TrimSpace(a.input.String()) != "" {
		return nil, false
	}
	a.fp.act.talk = ""
	a.touch()
	return a.showPage(pageFactory), true
}

// ── the fold ────────────────────────────────────────────────────────────────

// factoryTeamWord is the one team every item's own team sits under (cmd/codeaf's
// factory_talk.go makes it, once per home, by this name).
const factoryTeamWord = "factory"

// teamFoldedHere says whether the open-team walk stops at team t rather than
// listing what is under it: THE `factory` TEAM IS FOLDED, so a hundred items
// that were each talked through are one row on the teams rail and in the
// Chats' team menu, not a hundred. It unfolds while the person is standing in
// it — the team selected on the rail, or the team the Chats are showing is it
// or one of its items' teams — so the row a person opened is never hidden
// under them.
func (a *app) teamFoldedHere(t team) bool {
	if t.Name != factoryTeamWord || t.Root {
		return false
	}
	for _, at := range []string{a.tp.sel, a.teamViews.id} {
		if at == "" {
			continue
		}
		if at == t.ID {
			return false
		}
		for _, up := range a.teamTree().Ancestors(at) {
			if up.ID == t.ID {
				return false
			}
		}
	}
	return true
}

// factoryTeamsRead reads the teams file again beside the loop, after a `T`:
// the first press wrote the item's team (and the `factory` team) from outside
// this window, and the rail and the team menu should hold it now rather than on
// the next turn of a Traffic clock that may not be running with no teams yet.
// It is the clock's own read and fold ([app.trafficTake]'s guard): what is read
// is taken only while nothing was edited here since the read began.
func (a *app) factoryTeamsRead() tea.Cmd {
	if a.teamsOff() {
		return nil
	}
	seam := a.teamsSeam()
	if seam.ReadSince == nil {
		return nil
	}
	// A WINDOW THAT HAS NOT LOADED ITS TEAMS YET still reads, from nothing: a
	// seam over a connection answers its first load from what it last held,
	// and this read is what it then holds.
	loaded := a.wall.loaded
	stamp := ""
	if loaded {
		stamp = a.traffic.stamp
	}
	reserved, edits := teamReservedHues(a.pal), a.traffic.edits
	return a.besideLine(func() func(bool) tea.Cmd {
		fresh, at, same, err := seam.ReadSince(stamp, reserved)
		return func(bool) tea.Cmd {
			if err != nil || same || fresh == nil || !loaded || !a.wall.loaded {
				return nil
			}
			if edits != a.traffic.edits || a.traffic.wrote != a.traffic.edits {
				return nil
			}
			a.teamAdopt(teamsClone(fresh))
			a.traffic.stamp = at
			a.touch()
			return nil
		}
	})
}
