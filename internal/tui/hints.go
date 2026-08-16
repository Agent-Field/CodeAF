package tui

import (
	"strings"
	"time"
)

const (
	tipIdleAfter = 20 * time.Second
	tipSpacing   = 60 * time.Second
)

type tipFeature uint8

const (
	tipGeneral tipFeature = iota
	tipVoice
	tipStandingStart
	tipBudget
)

type curatedTip struct {
	text    string
	feature tipFeature
}

// Curated from docs/FEATURES.md. Priority is intentionally fixed in v1: it is
// predictable, free, session-local, and leaves personalization to the future
// journal-aware pipeline described in that document.
var curatedTips = []curatedTip{
	{text: "ctrl+v (or alt+v) talks — your typed draft is never lost", feature: tipVoice},
	{text: "say \"remind me Friday at 3\" — standing goals ask before they act", feature: tipStandingStart},
	{text: "nothing dies at the daily limit — /budget shows or raises it", feature: tipBudget},
	{text: "change work already running just by talking about it"},
	{text: "drag a screenshot into chat — a vision talk model can see it"},
	{text: "models in the header are filtered to what each slot can do"},
	{text: "press a question's number — or type your own answer"},
	{text: "click a standing rule to see its history, cost, and every run"},
	{text: "tell aforge to forget something it learned wrong — just say so"},
	{text: "ask how we solved this last time — finished work stays searchable"},
	{text: "settled cards stay where you asked — not at the end of a log"},
	{text: "▸ ▾ ⋯ ⟨×⟩ and ⌄ are clickable — every action also has a key"},
	{text: "closing the terminal never strands work — reopen and it resumes"},
	{text: "ask any task for a diagram, voiceover, jingle, or clip"},
}

func (m *Model) noteKeypress() {
	m.tipActivityAt = m.standingTime()
	m.tipCurrent = -1
}

func (m *Model) idleTipLine(now time.Time) string {
	if !m.tipSurfaceIdle() {
		m.tipActivityAt = now
		m.tipCurrent = -1
		return ""
	}
	if m.tipActivityAt.IsZero() || now.Before(m.tipActivityAt) {
		m.tipActivityAt = now
		return ""
	}
	if now.Sub(m.tipActivityAt) < tipIdleAfter {
		return ""
	}
	if m.tipCurrent >= 0 && m.tipCurrent < len(curatedTips) {
		if now.Sub(m.tipLastShownAt) < tipSpacing {
			return curatedTips[m.tipCurrent].text
		}
		m.tipCurrent = -1
	}
	if !m.tipLastShownAt.IsZero() && now.Sub(m.tipLastShownAt) < tipSpacing {
		return ""
	}
	for index, tip := range curatedTips {
		if m.tipSeen[index] || m.tipSuppressed(tip.feature) {
			continue
		}
		m.tipSeen[index] = true
		m.tipCurrent = index
		m.tipLastShownAt = now
		return tip.text
	}
	return ""
}

func (m *Model) tipSurfaceIdle() bool {
	// An open notebook, the help overlay, or /history results are active
	// surfaces: the user is reading, so the footer stays quiet rather than
	// pitching features underneath them.
	return strings.TrimSpace(m.input.Value()) == "" && len(m.attachments) == 0 &&
		!m.hasPendingQuestion() && m.streamMode == streamNone && len(m.streamQueue) == 0 &&
		m.voiceState == voiceIdle && m.liveWorkCount() == 0 && m.activeCardCount() == 0 &&
		!m.notebookOpen && !m.historyVisible && m.palette != paletteHelp
}

func (m *Model) tipSuppressed(feature tipFeature) bool {
	switch feature {
	case tipVoice:
		return m.voiceUsed
	case tipStandingStart:
		return m.hasStandingHistory()
	case tipBudget:
		return m.budgetUsed
	default:
		return false
	}
}

// questionSelectable reports whether the arrows and enter will answer a visible
// option question rather than move the surface — the composer holding focus
// over an empty draft, with choices on screen. It is the one state the footer
// announces ahead of the focus zone: a question whose choices nothing says how
// to take is how a live choice ends up reading as dead.
func (m *Model) questionSelectable() bool {
	return m.nodeViewID == "" && m.palette == paletteNone && m.inputFocused &&
		m.input.Value() == "" && m.questionCardWithOptions() != nil
}

// nodeActionHints names the verb that is true of the task being read, and no
// other. The line used to say "c cancel" over every task there is, which is a
// promise on a job that finished an hour ago and a wrong word on one the user
// stopped themselves: a cancelled node's forward door is restart, and a done
// node has no door at all. Same grammar, same four slots, same control ink —
// only the middle term follows the state.
func (m *Model) nodeActionHints() string {
	verb := ""
	switch {
	case nodeRestartable(m.inspectedNode.Status):
		verb = " · r restart"
	case !terminalStatus(m.inspectedNode.Status):
		verb = " · c stop"
	}
	return "↑/↓ read" + verb + " · tab steers · esc back"
}

// contextHelpLine keeps at most four actions and follows the current focus
// zone, so the footer describes what the next key will do rather than acting
// as a static command inventory.
func (m *Model) contextHelpLine() string {
	switch {
	case m.questionSelectable():
		return "↑/↓ choose · enter answer · 1–9 pick · or type your own"
	case m.focus == focusCards:
		return "↑/↓ select · enter details · esc back · ctrl+t tasks"
	case m.focus == focusQuestions:
		return "↑/↓ select · enter ask inline · esc back · tab focus"
	case m.nodeViewID != "" && m.inputFocused:
		return "type to steer · enter send · tab reads · esc back"
	case m.nodeViewID != "":
		return m.nodeActionHints()
	case m.focus == focusGraph:
		return "↑/↓ select · enter inspect · esc close · ctrl+t hide"
	case m.focus == focusSelf && m.selfFilterable():
		return "↑/↓ select · type to filter · esc back · tab focus"
	case m.focus == focusSelf:
		return "↑/↓ select · enter opens · esc back · tab focus"
	case m.focus == focusHeader:
		return "←/→ choose · enter models/tasks/help · esc back · tab focus"
	case m.focus == focusChat:
		return "↑/↓ select · enter open · esc back · tab focus"
	case m.turnInFlight():
		// The one key nobody guesses, said only while it does something.
		return "esc stops this reply · ↑ what you sent · ? help"
	default:
		return "/ commands · ctrl+v voice · v receipts · ? help"
	}
}
