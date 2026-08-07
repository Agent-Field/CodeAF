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
	{text: "alt+v talks — your typed draft is never lost", feature: tipVoice},
	{text: "say \"remind me Friday at 3\" — standing goals ask before they act", feature: tipStandingStart},
	{text: "nothing dies at the daily dollar rail — /budget shows or raises it", feature: tipBudget},
	{text: "redirect running work by talking about it — no task id needed"},
	{text: "drag a screenshot into chat — a vision talk model can see it"},
	{text: "models in the header are filtered to what each slot can do"},
	{text: "press a question's number — or type your own answer"},
	{text: "click a standing line to see its history, cost, and firings"},
	{text: "tell aforge to retract a wrong belief — the notebook is editable"},
	{text: "ask how we solved this last time — finished graphs stay searchable"},
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
	return strings.TrimSpace(m.input.Value()) == "" && len(m.attachments) == 0 &&
		!m.hasPendingQuestion() && m.streamMode == streamNone && len(m.streamQueue) == 0 &&
		m.voiceState == voiceIdle && m.liveWorkCount() == 0 && m.activeCardCount() == 0
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

// contextHelpLine keeps at most four actions and follows the current focus
// zone, so the footer describes what the next key will do rather than acting
// as a static command inventory.
func (m *Model) contextHelpLine() string {
	switch {
	case m.focus == focusCards:
		return "↑/↓ select · enter details · esc back · " + keyBindings.graph + " tasks"
	case m.focus == focusQuestions:
		return "↑/↓ select · enter ask inline · esc back · tab focus"
	case m.nodeViewID != "":
		return "type to steer · enter send · c cancel · esc back"
	case m.focus == focusGraph:
		return "↑/↓ select · enter inspect · esc close · " + keyBindings.graph + " hide"
	case m.focus == focusHeader:
		return "←/→ choose · enter models/tasks · esc back · tab focus"
	case m.focus == focusChat:
		return "↑/↓ select · enter open · esc back · tab focus"
	default:
		return "/ commands · " + keyBindings.voice + " voice · v receipts · ? help"
	}
}
