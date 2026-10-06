package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// A card shows the latest human exchange, even when team traffic arrives
// afterwards. Without a human prompt it falls back to the latest delivery or
// assistant update, never inventing a prompt for autonomous work.
func teamsLatestExchange(messages []teamsPreviewMessage) []teamsPreviewMessage {
	start := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].role == "user" || messages[i].role == "correction" {
			start = i
			break
		}
	}
	if start < 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].role == "team" || messages[i].role == "assistant" {
				start = i
				break
			}
		}
	}
	if start < 0 {
		return nil
	}
	first := messages[start]
	limit := teamsPreviewBytes
	if first.role != "assistant" {
		limit /= 4
	}
	first.text, first.clipped = teamsPreviewPrefix(first.text, limit, first.clipped)
	out := []teamsPreviewMessage{first}
	if first.role == "assistant" {
		return out
	}
	remaining := teamsPreviewBytes - len(first.text)
	reply := teamsPreviewMessage{role: "assistant"}
	for _, m := range messages[start+1:] {
		if m.role != "assistant" || strings.TrimSpace(m.text) == "" {
			continue
		}
		text := m.text
		if reply.text != "" {
			text = "\n\n" + text
		}
		prefix, clipped := teamsPreviewPrefix(text, remaining, m.clipped)
		reply.text += prefix
		remaining -= len(prefix)
		reply.clipped = reply.clipped || clipped
		reply.interrupted = reply.interrupted || m.interrupted
		if remaining <= 0 {
			break
		}
	}
	if reply.text != "" {
		out = append(out, reply)
	}
	return out
}

// Byte bounds preserve a UTF-8 prefix, with an explicit clipping fact for the
// renderer. The display adds its own ellipsis after fitting complete rows.
func teamsPreviewPrefix(text string, limit int, clipped bool) (string, bool) {
	if len(text) <= limit {
		return text, clipped
	}
	return strings.ToValidUTF8(text[:max(limit, 0)], ""), true
}

// Metadata occupies one row when it fits, otherwise a second grey model row.
// The title yields space to the alias and model, which identify the recipient.
func (a *app) teamsCardMetadata(r teamsCrewRow, width int) []string {
	alias := r.name()
	model := a.teamsConversationModel(r.key, r.file)
	title := strings.Join(strings.Fields(r.title), " ")
	name := a.pal.ink(ansi.Truncate(alias, width, "..."))
	room := width - ansi.StringWidth(alias)
	modelRows := false
	if model != "" && title != "" && room < 24 {
		modelRows = true
	}
	titleRoom := room
	if model != "" && !modelRows {
		titleRoom -= min(ansi.StringWidth(model)+3, max(room/2, 0))
	}
	if title != "" && title != alias && titleRoom >= 7 {
		name += a.pal.dim(" (" + ansi.Truncate(title, titleRoom-3, "...") + ")")
	}
	if model != "" && !modelRows {
		left := width - ansi.StringWidth(name) - 3
		if left >= 4 {
			name += a.pal.dim(" " + a.teamsDot() + " " + ansi.Truncate(model, left, "..."))
		} else {
			modelRows = true
		}
	}
	rows := []string{name}
	if modelRows {
		rows = append(rows, a.pal.dim(ansi.Truncate(model, width, "...")))
	}
	return rows
}

// Prompt and response use the transcript's own glyph, colours and Markdown
// renderer. The prompt retains a share of the room even for a long reply, and
// both messages clip from their beginning rather than displaying a tail.
func (a *app) teamsConversationPreview(key string, width, budget int) []string {
	if budget <= 0 {
		return nil
	}
	messages := a.teamsManagerMessages(key)
	if len(messages) == 0 {
		word := "Updates appear here"
		p := a.tp.previews[key]
		if p.unavailable || a.hosted() {
			word = "Preview unavailable"
		}
		if p.missing {
			word = "Conversation unavailable"
		}
		return []string{a.pal.dim(word)}
	}
	var out []string
	for i, m := range messages {
		room := budget - len(out)
		if room <= 0 {
			break
		}
		user := m.role == "user" || m.role == "correction"
		var rows []string
		if user {
			for j, part := range wrap(strings.TrimSpace(m.text), max(width-2, 1)) {
				lead := userLead
				if j == 0 {
					lead = a.pal.accent(a.pal.youGlyph())
				}
				rows = append(rows, lead+a.pal.muted(part))
			}
		} else if m.role == "team" {
			rows = append(rows, a.pal.dim(ansi.Truncate(m.author, width, "...")))
			for _, part := range wrap(m.text, width) {
				rows = append(rows, a.pal.ink(part))
			}
		} else {
			rows = trimBlanks(a.renderMarkdown(m.text, width))
			if m.interrupted {
				rows = append(rows, a.pal.dim("interrupted"))
			}
		}
		if i < len(messages)-1 {
			room = max(room/3, 1)
		}
		clipped := len(rows) > room || m.clipped
		rows = rows[:min(len(rows), room)]
		if clipped && len(rows) > 0 {
			ink := a.pal.ink
			if user {
				ink = a.pal.muted
			}
			last := len(rows) - 1
			rows[last] = ansi.Truncate(rows[last], max(width-3, 0), "") + ink("...")
		}
		out = append(out, rows...)
		if i < len(messages)-1 && budget-len(out) > 2 {
			out = append(out, "")
		}
	}
	return out
}

// Questions sit on the top edge so their frame and short heading remain
// visible without consuming the prompt-and-answer preview. Permission choices
// continue to use the existing actionable decision cards.
func (a *app) teamsCardQuestion(t team, r teamsCrewRow) string {
	if r.asking {
		if r.key == a.frontTabKey() {
			if q, ok := a.questionHead(); ok && q.local == nil && strings.TrimSpace(q.question.Head) != "" {
				return q.question.Head
			}
		}
		if reason := a.tp.world[r.file].Reason(); reason != "" {
			return reason
		}
		return "waiting on you"
	}
	if r.manager {
		for _, p := range a.tp.packets {
			if p.Waiting() && p.Team == teamstore.Person && p.Origin == t.ID {
				return p.Question
			}
		}
	}
	return ""
}

// Work uses the shared time-based spinner, so a quiet polling frame and a
// faster stream frame agree on its phase. Reduced motion keeps one still mark.
func (a *app) teamsWorkMark() string {
	if a.wallReduced() {
		return glyphRunASCII
	}
	return tokens.Spinner(wallSpin(a.now()))
}

// Live facts share the top edge with the role and question so they never
// displace the model or the prompt inside a compact member card.
func (a *app) teamsCardFacts(r teamsCrewRow, mark string) string {
	var facts []string
	if r.word == "working" && !r.asking {
		facts = append(facts, "working "+mark)
	} else if r.word == "failed" {
		facts = append(facts, r.word)
	}
	if r.independent {
		facts = append(facts, "independent")
	}
	if a.unreadChats[r.key] {
		facts = append(facts, "unread")
	}
	return strings.Join(facts, " ")
}

func (a *app) teamsCardHeading(t team, r teamsCrewRow, title string, width int) (string, func(string) string, bool) {
	border := a.pal.muted
	question := a.teamsCardQuestion(t, r)
	// A manager's pending team decision takes precedence over the working
	// signal, just as a conversation question does for ordinary members.
	if question != "" {
		r.asking = true
	}
	mark := a.teamsWorkMark()
	if fact := a.teamsCardFacts(r, mark); fact != "" {
		if r.word == "working" && !r.asking {
			// The work mark is the point of this heading. Narrow cards give
			// up role text and trailing facts before giving up the spinner.
			budget := max(width-5, 0)
			workWidth := ansi.StringWidth("working " + mark)
			if budget < workWidth {
				title = ""
				fact = ansi.Truncate("working", max(budget-2, 0), "") + " " + mark
			} else {
				factRoom := min(max(budget-ansi.StringWidth(title)-2, workWidth), ansi.StringWidth(fact))
				title = fit(title, max(budget-factRoom-2, 0))
				fact = ansi.Truncate(fact, factRoom, "")
			}
		}
		if title != "" {
			title += "  "
		}
		title += a.teamsCrewInk(r.word)(fact)
	}
	title = ansi.Truncate(title, max(width-5, 0), "...")
	if question != "" {
		border = a.pal.ask
		if title != "" {
			title += "  "
		}
		title = ansi.Truncate(title, max(width-9, 0), "...")
		title += a.pal.ask(ansi.Truncate("? "+strings.Join(strings.Fields(question), " "), max(width-5-ansi.StringWidth(title), 1), "..."))
	}
	return title, border, r.word == "working" && !r.asking && strings.Contains(ansi.Strip(title), mark)
}

// Card headings have their own visibility facts because the preview targets
// below them can remain visible after the spinner has scrolled away.
type teamsCardSpot struct {
	team, key, title string
	y, width         int
}

func (a *app) teamsConversationCard(d *teamsDraw, t team, r teamsCrewRow, title string, lines []wallCardLine, x, y, width int) []string {
	if r.key != "" {
		d.cards = append(d.cards, teamsCardSpot{team: t.ID, key: r.key, title: title, y: y, width: width})
	}
	heading, border, _ := a.teamsCardHeading(t, r, title, width)
	return wallCardBuildWithBorder(a.pal, heading, lines, x, y, width, 1, 0, border).rows
}

// Only visible conversation cards keep the shared clock turning. State comes
// from the same held-tab or world reading as the card, with no disk reads or
// agent calls on the paint path. Reduced motion keeps its static work mark.
func (a *app) teamsSpinning() bool {
	if !a.at(pageTeams) || a.wallReduced() {
		return false
	}
	for _, card := range a.tp.cards {
		if card.y < a.tp.paneOffset || card.y >= a.tp.paneOffset+a.tp.paneRoom {
			continue
		}
		t, ok := a.teamByID(card.team)
		if !ok {
			continue
		}
		m, ok := t.Member(card.key)
		if !ok {
			continue
		}
		state := a.teamsMember(m)
		if state.word != "running" || state.asking {
			continue
		}
		r := teamsCrewRow{key: m.Key, file: m.File, manager: m.Key == t.Manager, word: "working"}
		_, _, working := a.teamsCardHeading(t, r, card.title, card.width)
		if working {
			return true
		}
	}
	return false
}
