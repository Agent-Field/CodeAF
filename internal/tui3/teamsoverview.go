package tui3

import (
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// Preview text is bounded and cached so a team's overview never retains whole
// conversations on each beat. The frame only sees these cached readings.
const teamsPreviewBytes = 64 << 10
const teamsPreviewMessages = 2
const teamsManagerRows = 14
const teamsManagerMinRows = 8
const teamsInteractionRows = 6

type teamsPreviewMessage struct {
	author      string
	role, text  string
	interrupted bool
	clipped     bool
}

type teamsPreview struct {
	unavailable bool
	missing     bool
	text        string
	messages    [teamsPreviewMessages]teamsPreviewMessage
	count       int
	size        int64
	modified    time.Time
	messageAt   time.Time
}

func teamsReadPreview(file string, previous teamsPreview) teamsPreview {
	f, err := os.Open(file)
	if err != nil {
		return teamsPreview{missing: errors.Is(err, os.ErrNotExist), unavailable: true}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return teamsPreview{unavailable: true}
	}
	if info.Size() == previous.size && info.ModTime().Equal(previous.modified) {
		return previous
	}
	preview := teamsPreview{size: info.Size(), modified: info.ModTime()}
	if err := teamsReadExchange(f, info.Size(), &preview); err != nil {
		return teamsPreview{unavailable: true}
	}
	return preview
}

// Use the same delivery parse as Chats so wrappers intended only for the
// model never appear in the preview and each teammate keeps their own words.
func teamsPreviewMessageOf(e session.DisplayEntry) []teamsPreviewMessage {
	if e.Role == "aside" && !e.Interrupted && asideShapeOf(e) == asideTeam {
		var out []teamsPreviewMessage
		for _, card := range teamCardsOf(e, "") {
			author := strings.TrimSpace(card.from) + " to " + strings.TrimSpace(card.to)
			if card.tag != "" {
				author += " (" + card.tag + ")"
			}
			out = append(out, teamsPreviewMessage{role: "team", author: author, text: card.text})
		}
		return out
	}
	role := e.Role
	if e.Interrupted {
		role = "assistant"
	}
	if role == "user" && e.Steer != nil {
		role = "correction"
	}
	if (role != "assistant" && role != "user" && role != "correction") || strings.TrimSpace(e.Text) == "" {
		return nil
	}
	return []teamsPreviewMessage{{role: role, text: e.Text, interrupted: e.Interrupted}}
}

// The overview's log read must never execute commands from past traffic or
// consume the cursor used by the live delivery loop. Concurrent readings are
// merged by entry id so a slow overview response cannot erase a newer reply.
func (a *app) teamsTakeInteractions(id string, entries []teamstore.Entry) bool {
	if a.traffic.rows == nil {
		a.traffic.rows = map[string][]teamstore.Entry{}
	}
	byID := map[string]teamstore.Entry{}
	for _, e := range a.traffic.rows[id] {
		byID[e.ID] = e
	}
	for _, e := range entries {
		byID[e.ID] = e
	}
	list := make([]teamstore.Entry, 0, len(byID))
	for _, e := range byID {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	if len(list) > trafficKeep {
		list = list[len(list)-trafficKeep:]
	}
	previous := a.traffic.rows[id]
	if len(previous) == len(list) {
		same := true
		for i := range list {
			if previous[i].ID != list[i].ID {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}
	a.traffic.rows[id] = list
	a.traffic.version++
	return true
}

func (a *app) teamsOverviewHeader(d *teamsDraw, t team, width, y int) string {
	if t.Closed() {
		return a.teamsHeader(d, t, width, y)
	}
	type control struct {
		word string
		act  teamsAct
	}
	right := []control{{"Settings", teamsActSettings}}
	if !t.Root {
		right = append(right, control{"Disband", teamsActClose})
	}
	rightWidth := 0
	for _, c := range right {
		rightWidth += ansi.StringWidth(c.word) + 2
	}
	rightWidth += len(right) - 1
	if rightWidth+3 > width {
		right = right[:1]
		rightWidth = 10
	}
	if rightWidth+3 > width {
		right = nil
		rightWidth = 0
	}
	rightX := width - rightWidth - 1
	leftRoom := max(rightX-2, 1)
	left := " "
	if spend := a.teamsSpendWords(t); spend != "" {
		left += "  " + a.pal.dim(spend)
	}
	if wrap := a.teamWrapWords(t, a.now()); wrap != "" {
		left += "  " + a.pal.dim(wrap)
	}
	left = fit(left, leftRoom)
	if !t.Closed() {
		for _, c := range []control{{"+ Add chat", teamsActAddMember}, {"+ Add subteam", teamsActAddSubteam}, {"Choose AI manager", teamsActChooseManager}} {
			if t.Root && c.act == teamsActAddSubteam {
				continue
			}
			x := ansi.StringWidth(left) + 1
			if x+ansi.StringWidth(c.word)+2 > leftRoom {
				continue
			}
			button, _ := d.button(c.word, teamsTarget{act: c.act, id: t.ID, x0: x, y: y, hint: c.word, pane: true}, a.pal.muted)
			left += " " + button
		}
	}
	var buttons []string
	x := rightX
	for _, c := range right {
		hint := "Team settings and spending controls" + hintSegment + "s"
		if c.act == teamsActClose {
			hint = "Disband this team and its subteams; current work continues" + hintSegment + "c"
		}
		button, cells := d.button(c.word, teamsTarget{act: c.act, id: t.ID, x0: x, y: y, hint: hint, pane: true}, a.pal.muted)
		buttons = append(buttons, button)
		x += cells + 1
	}
	if len(right) == 0 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", max(rightX-ansi.StringWidth(left), 0)) + strings.Join(buttons, " ") + " "
}

// The manager leads with a full-width excerpt. Recent interactions separate
// it from the compact member grid, and every preview uses the same chat door.
func (a *app) teamsMemberCards(d *teamsDraw, t team, width, y int) []string {
	if width < 12 {
		return nil
	}
	crew := a.teamsCrew(t)
	columns := 1
	if width >= 60 {
		columns = 2
	}
	if width >= 100 {
		columns = 3
	}
	w := (width - (columns-1)*2) / columns
	var out []string
	if len(crew) > 0 && crew[0].manager {
		out = append(out, a.teamsManagerCard(d, t, crew[0], width, y)...)
		out = append(out, "")
		crew = crew[1:]
	}
	out = append(out, a.teamsInteractionTable(d, t, width, y+len(out))...)
	out = append(out, "")
	for first := 0; first < len(crew); first += columns {
		var cards [][]string
		for column := 0; column < columns && first+column < len(crew); column++ {
			r := crew[first+column]
			x := column * (w + 2)
			role := ""
			if r.manager {
				role = a.teamManagerMark() + " Manager"
			}
			labelWidth := w - 4
			if !t.Root && !t.Closed() {
				labelWidth -= 3
			}
			words := a.teamsCardMetadata(r, labelWidth)
			words = append(words, a.teamsConversationPreview(r.key, w-4, 5-len(words))...)
			for len(words) < 5 {
				words = append(words, "")
			}
			var lines []wallCardLine
			for row, word := range words {
				contentWidth := w - 4
				if row == 0 && !t.Root && !t.Closed() {
					contentWidth -= 3
				}
				line := teamsPad(word, contentWidth)
				if !a.tp.previews[r.key].missing {
					line = d.row(word, contentWidth, teamsTarget{act: teamsActMember, id: t.ID, arg: r.key,
						x0: x + 2, y: y + len(out) + 1 + row, hint: a.teamsCrewHint(r), pane: true}, false)
				}
				if row == 0 && !t.Root && !t.Closed() {
					const actionCells = 3
					if !a.tp.previews[r.key].missing {
						d.targets[len(d.targets)-1].x1 = x + w - 2 - actionCells
					}
					action, _ := d.button(a.linearMark(tabCloseASCII, tabCloseASCII), teamsTarget{act: teamsActRemoveMember, id: t.ID, arg: r.key, x0: x + w - 2 - actionCells, y: y + len(out) + 1, hint: "Remove from this team; conversation and work continue", pane: true}, a.pal.muted)
					line += action
				}
				lines = append(lines, wallCardLine{s: line})
			}
			cards = append(cards, a.teamsConversationCard(d, t, r, role, lines, x, y+len(out), w))
		}
		for row := 0; row < 7; row++ {
			var line []string
			for _, card := range cards {
				line = append(line, card[row])
			}
			out = append(out, strings.Join(line, "  "))
		}
		out = append(out, "")
	}
	return out
}

// Interaction pages keep six body rows, reducing only when the terminal
// cannot fit the table and its fixed header, footer and border.
func (a *app) teamsInteractionHeight() int {
	height := min(max(a.height/3, 4), teamsInteractionRows)
	available := a.height - placeHeadRows - placeFootRowsFor(pageTeams, a.height)
	if teamsRailCols(a.width) == 0 {
		available -= min(len(a.teamsRailRows()), max(min(available/3, 5), 1)) + 1
	}
	height = min(height, max(available-4, 1))
	return height
}

// Live words take precedence over the saved excerpt without borrowing another
// conversation's front entries. The bounded exchange keeps the prompt and the
// beginning of its reply rather than a disconnected tail.
func (a *app) teamsManagerMessages(key string) []teamsPreviewMessage {
	p := a.tp.previews[key]
	if key == a.frontTabKey() && len(a.entries) > 0 {
		start := 0
		for i := len(a.entries) - 1; i >= 0; i-- {
			if a.entries[i].kind == entryUser || a.entries[i].kind == entrySteer {
				start = i
				break
			}
		}
		var messages []teamsPreviewMessage
		for _, e := range a.entries[start:] {
			switch e.kind {
			case entryTeam:
				messages = append(messages, teamsPreviewMessageOf(session.DisplayEntry{Role: "aside", Text: e.text, Team: e.team})...)
			case entryUser:
				messages = append(messages, teamsPreviewMessage{role: "user", text: e.text})
			case entryAssistant:
				messages = append(messages, teamsPreviewMessage{role: "assistant", text: e.text, interrupted: e.cut})
			case entrySteer:
				if e.steer != nil && strings.TrimSpace(e.steer.words) != "" {
					messages = append(messages, teamsPreviewMessage{role: "correction", text: e.steer.words})
				}
			}
		}
		return teamsLatestExchange(messages)
	}
	messages := append([]teamsPreviewMessage(nil), p.messages[:p.count]...)
	if len(messages) == 0 && p.text != "" {
		messages = append(messages, teamsPreviewMessage{role: "assistant", text: p.text})
	}
	return teamsLatestExchange(messages)
}

// The shorter card keeps room for a prompt and response while making the
// overview's manager section about sixty percent of its former height.
func (a *app) teamsManagerHeight() int {
	return max(teamsManagerMinRows, min(a.height*3/10, teamsManagerRows), (a.teamsInteractionHeight()+4)*3/5)
}

func (a *app) teamsManagerCard(d *teamsDraw, t team, r teamsCrewRow, width, y int) []string {
	inner := width - 4
	height := a.teamsManagerHeight()
	labelWidth := inner
	if !t.Closed() {
		labelWidth -= 3
	}
	words := a.teamsCardMetadata(r, labelWidth)
	words = append(words, a.teamsConversationPreview(r.key, inner, height-2-len(words))...)
	latestRow := len(words) - 1
	for len(words) < height-2 {
		words = append(words, "")
	}
	var lines []wallCardLine
	for row, word := range words {
		contentWidth := inner
		if row == 0 && !t.Closed() {
			contentWidth -= 3
		}
		line := teamsPad(word, contentWidth)
		if !a.tp.previews[r.key].missing {
			opt := ""
			if row == latestRow {
				opt = "preview"
			}
			line = d.row(word, contentWidth, teamsTarget{act: teamsActMember, id: t.ID, arg: r.key, opt: opt, x0: 2, y: y + 1 + row, hint: a.teamsCrewHint(r), pane: true}, false)
		}
		if row == 0 && !t.Closed() {
			button, _ := d.button(a.linearMark(tabCloseASCII, tabCloseASCII), teamsTarget{act: teamsActRemoveMember, id: t.ID, arg: r.key, x0: width - 5, y: y + 1, hint: "Assign another manager before removing this one", pane: true}, a.pal.muted)
			line += button
		}
		lines = append(lines, wallCardLine{s: line})
	}
	return a.teamsConversationCard(d, t, r, a.teamManagerMark()+" Manager", lines, 0, y, width)
}

type teamsTableRect struct{ x, y, w, h int }

func (r teamsTableRect) contains(x, y int) bool {
	return r.w > 0 && x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

func (a *app) teamsInteractionsScroll(delta int) {
	if a.tp.interactionOffsets == nil {
		a.tp.interactionOffsets = map[string]int{}
	}
	a.tp.interactionOffsets[a.tp.sel] = min(max(a.tp.interactionOffsets[a.tp.sel]+delta, 0), a.tp.tableOver)
	a.touch()
}

// Paging moves by one full viewport in either direction and retains a short
// final page, rather than sliding the final rows back over the previous page.
func (a *app) teamsInteractionsPage(delta int) {
	if a.tp.tablePageSize <= 0 {
		return
	}
	off := a.tp.interactionOffsets[a.tp.sel]
	next := (off/a.tp.tablePageSize + 1) * a.tp.tablePageSize
	if delta < 0 {
		next = max((off-1)/a.tp.tablePageSize, 0) * a.tp.tablePageSize
	}
	a.teamsInteractionsScroll(next - off)
}

// Old logs without stable keys are linked only when their alias is unambiguous.
func teamsInteractionMember(t team, who string) string {
	if who == teamstore.FromManager {
		if t.FormerManager != "" {
			return ""
		}
		return t.Manager
	}
	key := ""
	for _, m := range append(append([]teamstore.Member(nil), t.Members...), t.FormerMembers...) {
		if m.Handle != who {
			continue
		}
		if key != "" && key != m.Key {
			return ""
		}
		key = m.Key
	}
	return key
}

func teamsInteractionIdentity(t team, e teamstore.Entry, sender bool) string {
	if sender {
		if e.FromKey != "" {
			return e.FromKey
		}
		return teamsInteractionMember(t, e.From)
	}
	if e.ToKey != "" {
		return e.ToKey
	}
	return teamsInteractionMember(t, e.To)
}

type teamsInteractionLine struct {
	text    string
	targets []teamsTarget
}

// The interaction table has one border and a pinned header. Its viewport is
// independent of the page, including when expanded exchanges add more rows.
func (a *app) teamsInteractionTable(d *teamsDraw, t team, width, y int) []string {
	if width < 20 {
		return nil
	}
	inner := width - 4
	var rows []teamsInteractionLine
	for _, th := range a.trafficThreads(t) {
		e := th.Root
		if e.Kind == teamstore.KindEvent || e.Kind == teamstore.KindClose || e.Kind == teamstore.KindReopen {
			continue
		}
		key := trafficOpenKey(t.ID, e.ID)
		expanded := a.traffic.open[key]
		mark := a.icon(tokens.GCollapsed)
		if expanded {
			mark = a.icon(tokens.GExpanded)
		}
		who := e.From
		if who == teamstore.FromManager {
			who = "Manager"
		} else {
			who = "@" + who
		}
		to := e.To
		if to == teamstore.ToManager {
			to = "Manager"
		} else if to != teamstore.ToEveryone && to != teamstore.ToRoom && to != teamstore.ToYou && to != teamstore.ToSeveral {
			to = "@" + to
		}
		participants := who + " to " + to
		labelWidth := min(26, max(inner/3, 8))
		meta := ""
		replies := 0
		for _, reply := range replyLines(th) {
			if reply.said {
				replies++
			}
		}
		if replies > 0 {
			meta = strconv.Itoa(replies) + " replies"
			if replies == 1 {
				meta = "1 reply"
			}
		}
		if age := sinceAt(e.At, a.now()); age != "" {
			meta += " " + age
		}
		metaWidth := 17
		if inner < 60 {
			metaWidth = 0
		}
		messageWidth := max(inner-labelWidth-metaWidth-4, 1)
		preview := strings.Join(strings.Fields(e.Text), " ")
		line := teamsPad(mark, 2) + teamsPad(participants, labelWidth) + " " + teamsPad(preview, messageWidth)
		if metaWidth > 0 {
			line += " " + a.pal.dim(teamsPad(strings.TrimSpace(meta), metaWidth))
		}
		root := teamsTarget{act: teamsActInteractionToggle, id: t.ID, arg: e.ID, x0: 0, x1: 2, hint: "Expand or collapse this exchange", pane: true}
		member := teamsInteractionIdentity(t, e, true)
		if !a.trafficHeld(member) && a.tp.previews[member].missing {
			member = ""
		}
		jump := teamsTarget{act: teamsActInteractionJump, id: t.ID, arg: member, opt: e.ID, x0: 2, x1: inner, hint: "Go to this interaction in Chats", pane: true}
		targets := []teamsTarget{root}
		if member != "" {
			targets = append(targets, jump)
		}
		toStart := 2 + ansi.StringWidth(who+" to ")
		if recipient := teamsInteractionIdentity(t, e, false); recipient != "" && (a.trafficHeld(recipient) || !a.tp.previews[recipient].missing) && toStart < 2+labelWidth {
			toTarget := jump
			toTarget.arg, toTarget.x0, toTarget.x1 = recipient, toStart, min(toStart+ansi.StringWidth(to), 2+labelWidth)
			if len(targets) > 1 {
				targets[1].x0 = 2 + labelWidth + 1
			}
			fromTarget := jump
			fromTarget.x1 = min(2+ansi.StringWidth(who), 2+labelWidth)
			if member != "" {
				targets = append(targets, fromTarget)
			}
			targets = append(targets, toTarget)
		}
		rows = append(rows, teamsInteractionLine{text: line, targets: targets})
		if expanded {
			for _, text := range wrap(e.Text, inner-2) {
				var hits []teamsTarget
				if member != "" {
					full := jump
					full.x0 = 0
					hits = []teamsTarget{full}
				}
				rows = append(rows, teamsInteractionLine{text: "  " + a.pal.muted(text), targets: hits})
			}
			for _, reply := range replyLines(th) {
				text := reply.note
				entry := reply.last
				if reply.said {
					text = reply.entry.Text
					entry = reply.entry
				}
				text = "@" + reply.who + "  " + reply.state + "  " + text
				replyTarget := teamsTarget{act: teamsActInteractionJump, id: t.ID, arg: teamsInteractionIdentity(t, entry, true), opt: entry.ID,
					x0: 0, x1: inner, hint: "Go to this reply in Chats", pane: true}
				for _, line := range wrap(text, inner-2) {
					var hits []teamsTarget
					if replyTarget.arg != "" && (a.trafficHeld(replyTarget.arg) || !a.tp.previews[replyTarget.arg].missing) {
						hits = []teamsTarget{replyTarget}
					}
					rows = append(rows, teamsInteractionLine{text: "  " + a.pal.dim(line), targets: hits})
				}
			}
		}
	}
	height := a.teamsInteractionHeight()
	a.tp.tablePageSize = height
	a.tp.tableOver = max((len(rows)-1)/height, 0) * height
	if a.tp.interactionOffsets == nil {
		a.tp.interactionOffsets = map[string]int{}
	}
	off := min(max(a.tp.interactionOffsets[t.ID], 0), a.tp.tableOver)
	a.tp.interactionOffsets[t.ID] = off
	var lines []wallCardLine
	labelWidth := min(26, max(inner/3, 8))
	metaWidth := 17
	if inner < 60 {
		metaWidth = 0
	}
	messageWidth := max(inner-labelWidth-metaWidth-4, 1)
	header := "  " + teamsPad("From / to", labelWidth) + " " + teamsPad("Message / reply", messageWidth)
	if metaWidth > 0 {
		header += " " + teamsPad("Replies / age", metaWidth)
	}
	lines = append(lines, wallCardLine{s: a.pal.dim(header)})
	for i := 0; i < height; i++ {
		text := ""
		if off+i < len(rows) {
			r := rows[off+i]
			text = r.text
			for _, target := range r.targets {
				if hot, cur := d.lit(target.ref()); cur || hot {
					text = a.pal.cursor(teamsPad(text, inner), inner)
					break
				}
			}
			for _, target := range r.targets {
				target.x0 += 2
				target.x1 += 2
				target.y = y + 2 + i
				d.targets = append(d.targets, target)
			}
		} else if len(rows) == 0 && i == 0 {
			text = a.pal.dim("Messages between members appear here")
		}
		lines = append(lines, wallCardLine{s: text})
	}
	prevInk, nextInk := a.pal.muted, a.pal.muted
	prevHint, nextHint := "Show the previous page of interactions", "Show the next page of interactions"
	if off == 0 {
		prevInk, prevHint = a.pal.dim, "First page of interactions"
	}
	if off >= a.tp.tableOver {
		nextInk, nextHint = a.pal.dim, "Last page of interactions"
	}
	footerY := y + height + 2
	previous, previousW := d.button("Prev", teamsTarget{act: teamsActInteractionUp, id: t.ID, x0: 2, y: footerY, hint: prevHint, pane: true}, prevInk)
	next, _ := d.button("Next", teamsTarget{act: teamsActInteractionDown, id: t.ID, x0: 2 + previousW + 1, y: footerY, hint: nextHint, pane: true}, nextInk)
	footer := previous + " " + next
	if len(rows) > height {
		span := strconv.Itoa(off+1) + " to " + strconv.Itoa(min(off+height, len(rows))) + " of " + strconv.Itoa(len(rows))
		// Both directions stay visible on narrow cards. The range moves
		// below them rather than cutting off a button to make room.
		if ansi.StringWidth(footer)+2+ansi.StringWidth(span) > inner {
			lines = append(lines, wallCardLine{s: footer})
			footer = a.pal.dim(fit(span, inner))
		} else {
			footer += "  " + a.pal.dim(span)
		}
	}
	lines = append(lines, wallCardLine{s: footer})
	a.tp.table = teamsTableRect{x: 0, y: y, w: width, h: len(lines) + 2}
	return wallCardBuild(a.pal, "Recent interactions", lines, 0, y, width, 1, 0).rows
}

// A missing local manager can be replaced; a remote path is never tested on
// this machine and therefore never guessed to be missing.
func (a *app) teamsManagerMissing(t team) bool {
	return t.Manager != "" && a.tp.previews[t.Manager].missing
}
