package consentui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// mode is which of the dialog's four faces is drawn. They are modes rather than
// separate panes because they answer ONE question and share its queue: a scope
// list that could be reached with the answer behind it dismissed would be a
// grant with nothing to grant.
type mode uint8

const (
	// modeAnswer is the fast path: prompt, consequence, answers, letters.
	modeAnswer mode = iota
	// modeScope lists the exact patterns an option would whitelist, before it
	// is confirmed (10.4.18).
	modeScope
	// modeScopeEdit edits one of those patterns. It is reachable only from
	// modeScope and only in fullscreen — the escalation is the point.
	modeScopeEdit
	// modeSteer collects the typed redirect a rejection opens (10.4.16).
	modeSteer
)

// Options configures a dialog.
type Options struct {
	// Styler paints. Nil renders plain, which is what the golden harness and
	// every test in this package use.
	Styler *tokens.Styler

	// OnAnswer receives one settled answer. The dialog has already advanced its
	// queue by the time this runs, so a handler may push, remove or inspect
	// freely. Journaling is the handler's job and never this package's — see
	// [Result] for the exact doors.
	OnAnswer func(Result) tea.Cmd

	// OnClose fires when the dialog has nothing left to show: the queue drained,
	// or esc closed it. The wiring drops the overlay plane here.
	OnClose func() tea.Cmd

	// Invalidate marks the frame stale. It is Shell.Invalidate, passed in rather
	// than reached for, because a pane is never given the shell (pane.go).
	Invalidate func()

	// Linear is the accessible rendering (10.1.5). It drops the caret and the
	// selection band for a leading marker, so a screen reader hears the
	// selection instead of being asked to see it.
	Linear bool
}

// Model is the consent dialog. It satisfies tui2.Pane, tui2.PaneKeys and
// tui2.PaneFocus.
//
// The zero value is not usable; build one with [New]. Everything it holds is a
// rendering decision or a draft — the questions themselves are durable rows it
// borrowed, and losing the whole struct loses nothing a restart cannot rebuild
// from the store, except the stashed steering drafts, which is exactly why they
// are stashed rather than posted.
type Model struct {
	style      *tokens.Styler
	onAnswer   func(Result) tea.Cmd
	onClose    func() tea.Cmd
	invalidate func()
	linear     bool

	// queue is the pending questions, oldest first. The head of the queue is
	// the one on screen; the length is the amber count.
	queue []Question
	// drafts keeps typed steering per question seq, so esc can close the dialog
	// without destroying a sentence someone was halfway through (8.2.21).
	drafts map[int64]string

	open    bool
	focused bool

	mode   mode
	sel    int
	detail bool
	full   bool

	// steer holds the redirect being typed for the question on screen.
	steer editor

	// pending is the option waiting on a scope confirmation, and scope is the
	// working copy of its patterns — working, because the fullscreen escalation
	// may edit them and the durable option must not be mutated.
	pending    Option
	hasPending bool
	scope      []string
	scopeSel   int
	scopeEdit  editor
}

// New builds a dialog.
func New(opts Options) *Model {
	return &Model{
		style:      opts.Styler,
		onAnswer:   opts.OnAnswer,
		onClose:    opts.OnClose,
		invalidate: opts.Invalidate,
		linear:     opts.Linear,
		drafts:     make(map[int64]string),
	}
}

// Push enqueues a question and raises the dialog.
//
// A question already in the queue is REPLACED in place rather than appended,
// and a replacement that says the same thing changes nothing at all. Both halves
// matter, because the caller is a poll: it re-reads the open rows every couple
// of seconds and hands them all back. Appending would stack one question into a
// hundred; resetting the view on every pass would yank a user out of the
// steering editor twice a minute; and raising the dialog on every pass would
// make esc a key that closes a dialog for two seconds. So only a question that
// is genuinely NEW — or genuinely different — moves anything.
func (m *Model) Push(q Question) {
	q = q.normalize()
	if q.Seq != 0 {
		for i := range m.queue {
			if m.queue[i].Seq != q.Seq {
				continue
			}
			if sameShape(m.queue[i], q) {
				m.queue[i] = q
				return
			}
			if i == 0 {
				// The row changed under the reader. Whatever they had typed is
				// still theirs, so it goes to the stash on the way through and
				// comes back out of it on the other side.
				m.stashDraft()
			}
			m.queue[i] = q
			if i == 0 {
				m.resetForCurrent()
			}
			m.touch()
			return
		}
	}
	m.queue = append(m.queue, q)
	if len(m.queue) == 1 {
		m.resetForCurrent()
	}
	m.open = true
	m.touch()
}

// sameShape reports whether two readings of one question would draw the same
// dialog. It compares what is on screen rather than every field: a row whose
// session moved (a resurfaced question, 9.7) is the same question to look at,
// and re-drawing it would be a flicker with no fact behind it.
func sameShape(a, b Question) bool {
	if a.Prompt != b.Prompt || a.Consequence != b.Consequence ||
		a.DetailTitle != b.DetailTitle || a.Default != b.Default ||
		a.Class != b.Class || len(a.Options) != len(b.Options) || len(a.Detail) != len(b.Detail) {
		return false
	}
	for i := range a.Options {
		if a.Options[i].Label != b.Options[i].Label ||
			a.Options[i].Hint != b.Options[i].Hint ||
			a.Options[i].Rejecting != b.Options[i].Rejecting ||
			!sameStrings(a.Options[i].Scope, b.Options[i].Scope) {
			return false
		}
	}
	for i := range a.Detail {
		if a.Detail[i] != b.Detail[i] {
			return false
		}
	}
	return true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Remove drops a question the dialog no longer owns — answered in another
// window, expired, or cancelled (10.4.20: a question answered anywhere closes
// everywhere). Any draft typed against it goes with it, because a redirect for
// a settled question is a redirect with nothing to redirect.
func (m *Model) Remove(seq int64) {
	for i := range m.queue {
		if m.queue[i].Seq != seq {
			continue
		}
		m.queue = append(m.queue[:i], m.queue[i+1:]...)
		delete(m.drafts, seq)
		if i == 0 {
			m.resetForCurrent()
		}
		if len(m.queue) == 0 {
			m.open = false
		}
		m.touch()
		return
	}
}

// Pending is how many questions are waiting, including the one on screen. It is
// the number the amber `?` badge carries, here and in the footer (5.16).
func (m *Model) Pending() int { return len(m.queue) }

// Open reports whether the overlay should be raised.
func (m *Model) Open() bool { return m.open && len(m.queue) > 0 }

// Reopen raises a dialog that esc closed, with the queue and every stashed
// draft intact. It is what the `?` affordance calls.
func (m *Model) Reopen() {
	if len(m.queue) == 0 || m.open {
		return
	}
	m.open = true
	m.resetForCurrent()
	m.touch()
}

// Current is the question on screen.
func (m *Model) Current() (Question, bool) {
	if len(m.queue) == 0 {
		return Question{}, false
	}
	return m.queue[0], true
}

// Draft is the steering text stashed against one question. It exists so the
// esc law is testable from outside and so a caller can carry a draft across a
// rebuild of the dialog.
func (m *Model) Draft(seq int64) string {
	if seq == currentSeq(m) {
		return m.steer.String()
	}
	return m.drafts[seq]
}

// Focus implements tui2.PaneFocus.
func (m *Model) Focus(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	m.touch()
}

// resetForCurrent points every piece of view state at the head of the queue.
// The stashed draft is restored here rather than at push time, because a
// question can reach the head of the queue long after it was pushed.
func (m *Model) resetForCurrent() {
	m.mode = modeAnswer
	m.detail = false
	m.hasPending = false
	m.pending = Option{}
	m.scope = nil
	m.scopeSel = 0
	m.scopeEdit.reset()
	m.steer.reset()
	q, ok := m.Current()
	if !ok {
		m.sel = 0
		return
	}
	m.sel = 0
	if q.Default >= 1 && q.Default <= len(q.Options) {
		m.sel = q.Default - 1
	}
	if draft := m.drafts[q.Seq]; draft != "" {
		m.steer.set(draft)
	}
}

// stashDraft is the esc law in one place (8.2.21): typed text is moved aside,
// never destroyed.
func (m *Model) stashDraft() {
	q, ok := m.Current()
	if !ok {
		return
	}
	if text := m.steer.String(); text != "" {
		m.drafts[q.Seq] = text
		return
	}
	delete(m.drafts, q.Seq)
}

// dismiss closes the dialog without answering anything.
func (m *Model) dismiss() tea.Cmd {
	m.stashDraft()
	m.open = false
	m.touch()
	return m.closed()
}

// advance settles the head of the queue and moves to the next question.
func (m *Model) advance(result Result) tea.Cmd {
	if len(m.queue) > 0 {
		delete(m.drafts, m.queue[0].Seq)
		m.queue = m.queue[1:]
	}
	m.resetForCurrent()
	if len(m.queue) == 0 {
		m.open = false
	}
	m.touch()

	var cmds []tea.Cmd
	if m.onAnswer != nil {
		if cmd := m.onAnswer(result); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if !m.open {
		if cmd := m.closed(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return batch(cmds)
}

func (m *Model) closed() tea.Cmd {
	if m.onClose == nil {
		return nil
	}
	return m.onClose()
}

// touch tells the shell a fact moved. A pane that changed for a reason the
// shell could not observe owes it this (pane.go).
func (m *Model) touch() {
	if m.invalidate != nil {
		m.invalidate()
	}
}

// result assembles the answer for one option. The detail rides out whatever the
// answer was (10.4.17), and the scope is the CONFIRMED list rather than the
// offered one, because the fullscreen escalation may have edited it.
func (m *Model) result(q Question, option Option, scope []string, steering string) Result {
	out := Result{
		QuestionSeq: q.Seq,
		SessionID:   q.SessionID,
		Category:    q.Category,
		Option:      option,
		Body:        option.Label,
		Steering:    steering,
		Rejected:    option.Rejecting,
		DetailTitle: q.DetailTitle,
	}
	if len(scope) > 0 {
		out.Scope = append([]string(nil), scope...)
	}
	if len(q.Detail) > 0 {
		out.Detail = append([]DetailLine(nil), q.Detail...)
	}
	return out
}

// ForcedFullscreen states 10.4.17's size threshold, read off the tokens table
// (10.5.24) rather than invented here. Below either number a floating dialog
// has nothing left to float over, so `f` stops being a choice.
func ForcedFullscreen(width, height int) bool {
	return width < tokens.DialogFullscreenBelowWidth || height < tokens.DialogFullscreenBelowHeight
}

func currentSeq(m *Model) int64 {
	if len(m.queue) == 0 {
		return 0
	}
	return m.queue[0].Seq
}

func batch(cmds []tea.Cmd) tea.Cmd {
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}
