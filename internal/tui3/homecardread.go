package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── WHAT A CARD READS, AND WHEN ─────────────────────────────────────────────
//
// ARCHITECTURE.md's fourth law: `open` and `tick` may read the disk and `body`
// may not. This file is the seam that makes it true of home's right column, and
// it takes the law one step further than that sentence does:
//
//	A KEY OR A POINTER ON HOME READS NOTHING AND RUNS NOTHING.
//
// Three bands were reading the disk while DRAWING, each behind a cache that made
// the cost look like nothing because it was paid once per card rather than once
// per frame. What that hides is that a card ARRIVES on every arrow key and on
// every hover that moves, so "once per card" is once per keystroke — on the
// update loop, in front of the next key. Measured on this machine, one `↓` onto
// a row home had not drawn yet cost:
//
//	the deliverables index   7-11ms   900KB of JSON, decoded to keep one row's worth
//	the conversation's tail  1-33ms   session.Peek scans the whole transcript
//	the repository            7.7ms   git status on aforge's own worktree, 1s ceiling
//
// The first is gone: that index is one file about the whole machine, so it is
// read with the world and filed by conversation (homeband_deliverables.go). The
// other two are about the row itself and cannot be read in advance for every row
// on the machine — so they are ASKED FOR here and come back as messages, and the
// band draws the last answer it was given. A card with no answer yet draws no
// band, which is the emptiness law rather than a blank.
//
// AND NOTHING IS ASKED FOR TWICE. Each reading has a set of what is in flight,
// so a pointer swept down twenty rows asks about twenty rows once, not twenty
// times each.

// homeNewsMsg and homeLeftOffMsg are those two readings coming back. Each
// carries what it was ABOUT, because several may be in flight at once and an
// answer filed against whatever the cursor had reached would be another row's.
type homeNewsMsg struct {
	key   string
	at    time.Time
	notes []standing.Note
}

type homeLeftOffMsg struct {
	transcript string
	summary    session.Summary
}

// refreshHomeCard asks for every reading the card that is about to be drawn
// needs — the row under the pointer, or under the cursor when no pointer is on
// the column ([homeView.previewLine]).
//
// IT ASKS FOR WHAT THIS CARD DRAWS AND NOT FOR EVERYTHING A CARD COULD DRAW. The
// resting list's card is the switcher's five bands (place_home.go's
// [app.homeSwitchCard]) and carries neither the inbox nor the journal's tail, so
// a person moving down that list pays for one `git status` per project and for
// nothing else at all.
func (a *app) refreshHomeCard(now time.Time) tea.Cmd {
	if !a.at(pageHome) {
		return nil
	}
	line, ok := a.home.previewLine()
	if !ok {
		return nil
	}
	var asked []tea.Cmd
	switch {
	case line.kind == homeSession && line.sw != nil:
		// The switcher's own card: the place line wants the branch and nothing
		// else on it reads anything.
		asked = append(asked, a.refreshRepoOf(strings.TrimSpace(line.row.Workspace), now))
	case line.kind == homeSession:
		asked = append(asked,
			a.refreshRepoOf(strings.TrimSpace(line.row.Workspace), now),
			a.askHomeNews(bandSubject{kind: bandKindSession, row: line.row}, now),
			a.askHomeLeftOff(line.row.Transcript))
	case line.kind == homeProject:
		asked = append(asked, a.askHomeNews(bandSubject{
			kind: bandKindProject, project: line.project,
			dir: homeProjectPath(line.proj), world: a.home.world,
		}, now))
	}
	return tea.Batch(asked...)
}

// askHomeNews asks for the inbox behind the `since you left` band
// (homeband_news.go). It expires on home's own clock, so a firing that landed in
// another window shows up on the next arrival at that card.
func (a *app) askHomeNews(subject bandSubject, now time.Time) tea.Cmd {
	key := subject.id()
	if key == "" {
		return nil
	}
	if cached, ok := a.home.news[key]; ok && now.Sub(cached.at) < homeEvery {
		return nil
	}
	if a.newsAsking == nil {
		a.newsAsking = map[string]bool{}
	}
	if a.newsAsking[key] {
		return nil
	}
	a.newsAsking[key] = true
	return func() tea.Msg {
		return homeNewsMsg{key: key, at: now, notes: newsNotesOf(a, subject)}
	}
}

// tookHomeNews files that answer.
func (a *app) tookHomeNews(msg homeNewsMsg) {
	delete(a.newsAsking, msg.key)
	if a.home.news == nil {
		a.home.news = map[string]homeNewsCache{}
	}
	a.home.news[msg.key] = homeNewsCache{at: msg.at, notes: msg.notes}
	a.touch()
}

// askHomeLeftOff asks for the tail of one conversation's own journal, behind the
// band that says where it got to (homeband_leftoff.go).
//
// IT DOES NOT EXPIRE. A conversation nobody is in does not move, and the one
// this window is holding says where it got to on the screen behind home.
func (a *app) askHomeLeftOff(transcript string) tea.Cmd {
	path := strings.TrimSpace(transcript)
	if path == "" {
		return nil
	}
	if _, ok := a.home.last[path]; ok {
		return nil
	}
	if a.leftOffAsking == nil {
		a.leftOffAsking = map[string]bool{}
	}
	if a.leftOffAsking[path] {
		return nil
	}
	a.leftOffAsking[path] = true
	return func() tea.Msg {
		summary, _ := session.Peek(path)
		return homeLeftOffMsg{transcript: path, summary: summary}
	}
}

// tookHomeLeftOff files that answer.
func (a *app) tookHomeLeftOff(msg homeLeftOffMsg) {
	delete(a.leftOffAsking, msg.transcript)
	if a.home.last == nil {
		a.home.last = map[string]session.Summary{}
	}
	a.home.last[msg.transcript] = msg.summary
	a.touch()
}
