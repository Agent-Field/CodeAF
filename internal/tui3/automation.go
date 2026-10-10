package tui3

// automation.go is how an automation touches a conversation
// (docs/design/automations/DESIGN.md): the card a proposal raises, the one dim
// line a change to an automation writes, and the one dim line a finished run
// writes into the conversation that made it.
//
// THE CARD SHOWS THE REAL THING. A person approves exactly what will run: the
// schedule in words and exactly, the first run, what it says or does, what a
// watch looks at and the condition it is held to, where work runs, and what one
// run may take and spend. The answers are not drawn here; they are the
// question, built once by the engine (session's [AutomationQuestion]) and drawn
// by the block like every other question.
//
// THE LINES ARE THE SURFACE'S OWN. A run's line is never written into the
// transcript and never wakes the model: it is news for the person, read off the
// store by this window ([fromTranscript] steps over the entry).

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The words a settled card reads. They are the answers' own, so the row a person
// reads back and the row they answered from are one account.
const (
	autoSavedWord   = "saved"
	autoNotSaved    = "not saved"
	autoChangedWord = "you asked for something different"
	autoEndedWord   = "ended · nothing was saved"
	autoChangeWord  = "Change…"
)

// automationCard is one card, or one line of news, in the transcript.
type automationCard struct {
	id     uint64
	notice session.AutomationNotice
	// verdict and answer are what a settled card says it came to.
	verdict, answer string
	// line is set on a line of news rather than a card: the whole row.
	line  string
	glyph tokens.GlyphID
	// head replaces the card's first line, for a card this window raised about
	// a line the person typed rather than one a model proposed
	// (automationscmd.go).
	head string
}

func (c *automationCard) settled() bool { return c.verdict != "" }
func (c *automationCard) news() bool    { return c.line != "" }

// proposeAutomation draws the card (session.EventAutomationProposal) and raises
// its question. An older unanswered card settles as ended: a question that can
// no longer be answered must stop looking like one.
func (a *app) proposeAutomation(ev session.Event) {
	notice := ev.Automation
	if notice == nil || notice.ID == 0 {
		return
	}
	if a.auto != nil && !a.auto.settled() {
		a.auto.verdict = autoEndedWord
	}
	card := &automationCard{id: notice.ID, notice: *notice}
	a.auto = card
	a.closeLive()
	a.closeLists()
	a.closeSettings()
	a.entries = append(a.entries, entry{kind: entryAutomation, turn: a.turn, auto: card})
	a.raiseQuestion(a.automationShown(card))
	a.follow()
	a.touch()
}

// automationShown is the card's question as the block holds it, with the one
// thing the engine cannot carry: the card in the transcript keeping what was
// decided.
func (a *app) automationShown(card *automationCard) questionShown {
	question := session.AutomationQuestion(card.notice)
	question.Asked = a.now()
	id := card.id
	return questionShown{
		question: question,
		answered: func(answer session.Answer) session.Answer {
			a.automationAnswered(id, answer)
			return answer
		},
	}
}

// automationAnswered writes what was decided onto the card. It decides
// nothing: the answer is already on its way to the engine.
func (a *app) automationAnswered(id uint64, answer session.Answer) {
	card := a.auto
	if card == nil || card.id != id || card.settled() {
		return
	}
	key := answer.FirstKey()
	switch {
	case key == "" && strings.TrimSpace(answer.Words()) != "":
		card.verdict, card.answer = autoChangedWord, autoChangeWord
	case key == session.AutomationSaveKey || key == session.AutomationRunNowKey:
		card.verdict, card.answer = autoSavedWord, automationOptionLabel(card.notice, key)
	default:
		card.verdict = autoNotSaved
	}
	a.input.reset()
	a.endRecall()
	a.closeLists()
	a.touch()
}

// automationOptionLabel is the label the card drew for one answer's key, or ""
// for a key it did not offer.
func automationOptionLabel(notice session.AutomationNotice, key string) string {
	for _, option := range notice.Options {
		if option.Key == key {
			return option.Label
		}
	}
	return ""
}

// automationUpdate draws one dim line about an automation a call in this turn
// changed (session.EventAutomationUpdate).
func (a *app) automationUpdate(ev session.Event) {
	notice := ev.Automation
	if notice == nil || strings.TrimSpace(notice.Update) == "" {
		return
	}
	glyph := tokens.GQueued
	switch notice.Update {
	case "paused", "deleted":
		glyph = tokens.GStopped
	case "running":
		glyph = tokens.GWorking
	}
	line := strings.TrimSpace(notice.Automation.Title) + " · " + notice.Update
	if text := strings.TrimSpace(notice.Text); text != "" {
		line += " · " + text
	}
	a.automationNews(line, glyph)
}

// automationRan draws one finished run in the conversation that made its
// automation: what it came to, and the one sentence it carried.
func (a *app) automationRan(item automation.Automation, run automation.Run) {
	a.automationNews(automationRunWords(item, run), automationRunGlyph(run.Outcome))
}

// automationNews appends one line of news to the transcript.
func (a *app) automationNews(line string, glyph tokens.GlyphID) {
	a.closeLive()
	a.entries = append(a.entries, entry{kind: entryAutomation, turn: a.turn, auto: &automationCard{line: line, glyph: glyph}})
	a.follow()
	a.touch()
}

// automationRunWords is one run as a person reads it on a line:
// `weekly update · done · drafted it`, `ci · couldn't check · no API key`.
func automationRunWords(item automation.Automation, run automation.Run) string {
	parts := []string{strings.TrimSpace(item.Title), run.Outcome.Word()}
	if late := run.Late(); late > 0 {
		parts = append(parts, "late "+automationLate(late))
	}
	if line := strings.TrimSpace(run.Line); line != "" && !(run.Outcome == automation.OutcomeDone && item.Kind() == automation.KindReminder && line == item.Title) {
		parts = append(parts, line)
	}
	return strings.Join(parts, " · ")
}

// automationRunGlyph is the task-state mark an outcome wears.
func automationRunGlyph(outcome automation.Outcome) tokens.GlyphID {
	switch outcome {
	case automation.OutcomeDone:
		return tokens.GSettled
	case automation.OutcomeYourCall, automation.OutcomeUnchecked:
		return tokens.GNeedsHuman
	case automation.OutcomeStopped:
		return tokens.GStopped
	case automation.OutcomeIncomplete:
		return tokens.GFailed
	}
	return tokens.GQueued
}

func automationLate(late time.Duration) string {
	hours := late.Hours()
	switch {
	case hours >= 48:
		return fmt.Sprintf("%dd", int(hours/24))
	case hours >= 1:
		return fmt.Sprintf("%dh", int(hours))
	}
	return fmt.Sprintf("%dm", int(hours*60))
}

// AutomationCardRows draws one card, or one line of news.
func AutomationCardRows(a *app, card *automationCard, width int, sel bool) []string {
	if a == nil || card == nil || width < 4 {
		return nil
	}
	if card.news() {
		return []string{a.pal.dim(fit(a.icon(card.glyph)+" "+card.line, width))}
	}
	paint := a.pal.ask
	if card.settled() {
		paint = a.pal.dim
	}
	rule := a.blockRule()
	head := taskHeadCorner + " " + a.icon(tokens.GNeedsHuman) + " "
	foot := taskFootCorner
	if a.pal.ascii {
		head = taskCornerASCII + " " + a.icon(tokens.GNeedsHuman) + " "
		foot = taskCornerASCII
	}
	said := session.AutomationHead(card.notice.Automation)
	if card.head != "" {
		said = card.head
	}
	name := fit(said, width-ansi.StringWidth(head)-3)
	top := paint(head)
	if card.settled() {
		top += a.pal.muted(name)
	} else {
		top += a.pal.askBold(name)
	}
	if fill := width - ansi.StringWidth(head) - ansi.StringWidth(name) - 1; fill > 0 {
		top += paint(" " + strings.Repeat(rule, fill))
	}
	if card.settled() {
		word := card.verdict
		if card.answer != "" {
			word = card.answer + " · " + card.verdict
		}
		return []string{top, paint(foot+" ") + a.pal.dim(fit(strings.TrimSpace(card.notice.Automation.Title)+" · "+word, width-ansi.StringWidth(foot)-1))}
	}
	stem := a.pal.ask(a.blockStem())
	room := width - ansi.StringWidth(a.blockStem())
	out := []string{top}
	for _, line := range wrap(strings.TrimSpace(card.notice.Automation.Title), room) {
		out = append(out, stem+a.pal.ink(line))
	}
	for _, band := range automationBands(card.notice) {
		for _, line := range wrap(band, room) {
			out = append(out, stem+a.pal.dim(line))
		}
	}
	if fill := width - ansi.StringWidth(foot); fill > 0 {
		out = append(out, paint(foot+strings.Repeat(rule, fill)))
	} else {
		out = append(out, paint(foot))
	}
	return out
}

// automationBands are the card's facts, one per line, each said only when
// there is something to say (the emptiness law).
func automationBands(notice session.AutomationNotice) []string {
	item := notice.Automation
	var bands []string
	if words := strings.TrimSpace(item.Words); words != "" && words != strings.TrimSpace(item.Title) {
		bands = append(bands, "“"+words+"”")
	}
	when := strings.TrimSpace(notice.WhenWords)
	if !notice.Next.IsZero() {
		if when != "" {
			when += " · "
		}
		when += "first run " + notice.Next.Local().Format("Mon 2 Jan 15:04")
	}
	if when != "" {
		bands = append(bands, when)
	}
	if exact := strings.TrimSpace(notice.Exact); exact != "" && item.Schedule.Repeats() {
		bands = append(bands, exact)
	}
	if look := item.Look; look != nil {
		switch {
		case strings.TrimSpace(look.Command) != "":
			bands = append(bands, "looks · "+look.Command)
		case strings.TrimSpace(look.Files) != "":
			bands = append(bands, "looks · the files matching "+look.Files)
		default:
			target := "looks · " + look.Tool
			if look.Service != "" {
				target += " (" + look.Service + ")"
			}
			if args := strings.TrimSpace(string(look.Args)); args != "" && args != "{}" {
				target += " " + args
			}
			bands = append(bands, target)
		}
		until := "until · " + look.Condition
		if look.Once {
			until += " · tells you once, then stops"
		}
		bands = append(bands, until)
	}
	if say := strings.TrimSpace(item.Action.Say); say != "" {
		bands = append(bands, "says · "+say)
	}
	if do := strings.TrimSpace(item.Action.Do); do != "" {
		bands = append(bands, "does · "+do)
		if item.Worktree {
			bands = append(bands, "in a separate worktree, kept for review")
		} else {
			bands = append(bands, "in your checkout")
		}
	}
	if item.Kind() != automation.KindReminder {
		limits := item.Limits.Effective()
		line := fmt.Sprintf("up to %s and $%.2f a run", automationLimitWords(limits.Time), limits.USD)
		if cost := strings.TrimSpace(notice.CostWords); cost != "" {
			line += " · " + cost
		}
		bands = append(bands, line)
	}
	return bands
}

func automationLimitWords(d time.Duration) string {
	minutes := int(d.Minutes())
	switch {
	case minutes >= 60 && minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	case minutes >= 60:
		return fmt.Sprintf("%dh%dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}
