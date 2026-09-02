package tui3

import "github.com/Agent-Field/aforge-v2/internal/history"

// THE UP ARROW: what you typed before, newest first.
//
// The list is built ONCE per walk, at the moment ↑ is first pressed, and it is
// built in two passes: everything typed in THIS directory, newest first, and
// then everything typed anywhere else. That order is the whole design — a
// prompt is about a project, so the project's own prompts are the ones a person
// is reaching for, and the rest are still there underneath rather than lost
// because the walk was scoped.
//
// The live draft is stashed on the way in and restored on the way out. A recall
// that overwrote a half-written sentence would make ↑ a key nobody could press
// without first saving their own work somewhere else.

// recallDepth is how far back a walk reaches on each pass. Two hundred prompts
// is further than any ↑ walk anybody has ever done, and it bounds the copy.
const recallDepth = 200

// History is the slice of *history.Store this surface uses. It is an interface
// for the same reason [Agent] is: the surface is driven in a test without a
// file, and a store that is nil is simply a surface with no recall.
type History interface {
	// Append records one submitted message and the directory it was typed in.
	Append(text, cwd string)
	// RecentFor is the newest entries typed in one directory.
	RecentFor(cwd string, limit int) []history.Entry
	// Recent is the newest entries typed anywhere.
	Recent(limit int) []history.Entry
}

// recall is one walk through the history: where it is, what it is walking, and
// the draft it is holding for the person.
type recall struct {
	active bool
	list   []string
	// at indexes list; -1 is the live draft, 0 the newest entry.
	at     int
	draft  []rune
	cursor int
}

func (a *app) recalling() bool { return a.hist.active }

// recallBack is ↑. It reports whether it took the key: a surface with no
// history takes nothing and the arrow keeps its older meanings.
func (a *app) recallBack() bool {
	if a.history == nil {
		return false
	}
	if !a.hist.active {
		list := a.recallList()
		if len(list) == 0 {
			return false
		}
		a.hist = recall{
			active: true,
			list:   list,
			at:     -1,
			draft:  append([]rune(nil), a.input.value...),
			cursor: a.input.cursor,
		}
	}
	if a.hist.at+1 >= len(a.hist.list) {
		// The oldest entry stays under the caret rather than the walk falling
		// off the end into an empty box.
		return true
	}
	a.hist.at++
	a.input.setText(a.hist.list[a.hist.at])
	a.closeLists()
	a.touch()
	return true
}

// recallForward is ↓: back toward the live draft, which is where the walk ends.
func (a *app) recallForward() bool {
	if !a.hist.active {
		return false
	}
	if a.hist.at <= 0 {
		a.recallCancel()
		return true
	}
	a.hist.at--
	a.input.setText(a.hist.list[a.hist.at])
	a.closeLists()
	a.touch()
	return true
}

// recallCancel is esc, and the end of a walk that went forward past the newest
// entry: the person's own draft comes back exactly as it was.
func (a *app) recallCancel() {
	if !a.hist.active {
		return
	}
	a.input.value = append(a.input.value[:0], a.hist.draft...)
	a.input.cursor = a.hist.cursor
	if a.input.cursor > len(a.input.value) {
		a.input.cursor = len(a.input.value)
	}
	a.hist = recall{}
	a.closeLists()
	a.touch()
}

// endRecall drops the walk WITHOUT restoring the draft — the recalled line has
// just been sent, so the draft it was holding is a sentence about a message
// that is now in the transcript.
func (a *app) endRecall() { a.hist = recall{} }

// recallList is the walk's list: this directory first, then everywhere, with
// duplicates dropped so that a prompt typed in both places is one step and not
// two.
func (a *app) recallList() []string {
	seen := map[string]bool{}
	out := make([]string, 0, recallDepth)
	add := func(entries []history.Entry) {
		for _, entry := range entries {
			if entry.Text == "" || seen[entry.Text] {
				continue
			}
			seen[entry.Text] = true
			out = append(out, entry.Text)
		}
	}
	add(a.history.RecentFor(a.workspace, recallDepth))
	add(a.history.Recent(recallDepth))
	return out
}

// remember records one submitted message. It never blocks — internal/history
// queues and drains on its own goroutine — and it is deliberately not called
// for slash commands: /quit is not a sentence anybody wants back.
func (a *app) remember(text string) {
	if a.history == nil {
		return
	}
	a.history.Append(text, a.workspace)
}
