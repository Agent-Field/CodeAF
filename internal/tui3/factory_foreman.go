package tui3

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE FOREMAN: `m`, THE FLOOR'S OWN CONVERSATION ─────────────────────────
//
// `m` anywhere on the floor (a row, the item page, an empty floor) opens the
// foreman: the one conversation the floor keeps for the judgment about what to
// take first ([factory.Seam.Foreman], cmd/codeaf's factory_foreman.go). It
// opens EXACTLY AS `T` OPENS AN ITEM'S CONVERSATION ([app.factoryTalk]): the
// door is asked off the loop in the ordered line, the floor is read in the
// same ask, the conversation opens the way the Chats open one, and the first
// `esc` on its empty box with nothing running is the floor again, on the same
// row. The way back is the same one-shot field, so the two cannot disagree
// about what `esc` means.
//
// THE FOREMAN PROPOSES BY MARKING. What it marks is kept by the store, so the
// floor's next read carries it ([factory.Snapshot.Marked]) and the row draws
// the accent lead a `space` mark draws. `L` launches them; nothing else does.
//
// WITH NO FOREMAN DOOR THERE IS NO KEY: the hint does not name `m foreman`,
// and `m` does nothing.

// foremanHintWord is the hint's clause for the key.
const foremanHintWord = "m foreman"

// factoryForeman asks the foreman's conversation off the loop and opens it.
func (a *app) factoryForeman() tea.Cmd {
	seam := a.factory
	return a.offLoop(func() func(bool) tea.Cmd {
		chat, err := seam.Foreman(context.Background())
		where := ""
		if err == nil {
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
			// THE WAY BACK IS `T`'s OWN ([app.factoryTalkBack]).
			a.fp.act.talk = a.convKey(chat)
			cmd := a.openConversationRow(session.SessionRow{Transcript: chat, ProjectDir: where})
			if a.pageShowing() {
				a.fp.act.talk = ""
			}
			a.touch()
			return tea.Batch(cmd, a.factoryTeamsRead())
		}
	})
}

// factoryForemanKey is `m` on the floor: the foreman, when the seam has one.
// It answers false otherwise, so the key falls through.
func (a *app) factoryForemanKey() (tea.Cmd, bool) {
	if !a.factory.Has("foreman") {
		return nil, false
	}
	a.pageMsg = ""
	return a.factoryForeman(), true
}

// factoryForemanHint is the hint's clause for `m`, or nothing when there is
// no foreman to open.
func (a *app) factoryForemanHint() []string {
	if !a.factory.Has("foreman") {
		return nil
	}
	return []string{foremanHintWord}
}

// factoryFoldStoreMarks reads the floor's own marks off a fresh snapshot,
// next, against the one the page held, prev. It is called before next is
// folded in.
//
// THE NEWER WORD WINS. A mark the store took on or off since the last read
// (the foreman marked it, or unmarked it) replaces the person's own `space`
// toggle on that item, so the foreman's proposal is drawn even on a row the
// person once toggled; a mark the store did not change leaves the toggle as
// it was. Each marked id is also written onto its item, so a seam that keeps
// marks but does not fold them draws the same lead.
func (a *app) factoryFoldStoreMarks(prev factory.Snapshot, next *factory.Snapshot) {
	if next == nil {
		return
	}
	factory.FoldMarks(next, next.Marked)
	was := map[int]bool{}
	for _, id := range prev.Marked {
		was[id] = true
	}
	now := map[int]bool{}
	for _, id := range next.Marked {
		now[id] = true
		if !was[id] {
			delete(a.fp.marked, id)
		}
	}
	for id := range was {
		if !now[id] {
			delete(a.fp.marked, id)
		}
	}
}
