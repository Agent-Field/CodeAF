package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// MENTIONS: "@" ALSO NAMES A TEAM OR A CONVERSATION.
//
// files.go is the list, and taskmention.go is the task half of it. This file
// is the other two sections of that same list: teams, then conversations, then
// the tasks and the files that were already there.
//
// THE CATALOG IS MEMORY. Teams are the ones this window already loaded
// ([app.wall.teams]). Conversations are the ones open in this window, then the
// recent list the door already handed the surface. Nothing here opens a file
// on a frame. The recent list is read once, inside a command, because that
// read can touch the disk.
//
// A PREFIX NARROWS THE LIST TO ONE SECTION. "@team:", "@chat:" and "@file:"
// are the three, and the same three words sit on the list's first row, each
// a press that types its prefix. Typing still filters every visible section
// at once.
//
// WHAT A CHOICE BECOMES. A team is "●" and its slug, drawn in the team's
// colour. A conversation is "@" and its handle, or a short slug of its title
// when it has no handle. The token is text. The digest the model reads is
// built on the engine (internal/session's mention.go), so a window over
// --host never has to open the other transcript.

const (
	scopeTeam = "team"
	scopeChat = "chat"
	scopeFile = "file"
	// mentionRows is how many teams, and how many conversations, one list draws.
	// It is the file list's own screenful.
	mentionRows = 8
	// mentionRecentCap is how many recent conversations the snapshot keeps.
	mentionRecentCap = 24
)

// mentionTeam is one team as the list and the link pass need it.
type mentionTeam struct {
	id, name, slug string
	hue            teamHueSpec
	members        int
}

// mentionChat is one conversation as the list and the link pass need it.
type mentionChat struct {
	key, file, where string
	title, handle    string
	slug             string
	open             bool
	note             string
}

// mentionScope splits an @ query into a section prefix and the needle. No
// prefix answers "" and the query unchanged.
func mentionScope(query string) (string, string) {
	low := strings.ToLower(query)
	for _, scope := range []string{scopeTeam, scopeChat, scopeFile} {
		prefix := scope + ":"
		if strings.HasPrefix(low, prefix) {
			return scope, query[len(prefix):]
		}
	}
	return "", query
}

// mentionSlug is a title as one @ token: the same spelling a task mention uses,
// so a person can type what they can see.
func mentionSlug(title string) string {
	slug := session.TaskSlug(title)
	if slug == "" {
		return "chat"
	}
	return slug
}

// rankMentions keeps the teams and conversations the needle matches. An
// argument list gets none of them: "/image" takes a path.
func (c *completion) rankMentions(needle string) {
	c.teamHits, c.chatHits = c.teamHits[:0], c.chatHits[:0]
	if c.arg {
		return
	}
	if c.scope == "" || c.scope == scopeTeam {
		for _, team := range c.teams {
			if _, ok := pathScore(team.name+" "+team.slug, needle); ok {
				c.teamHits = append(c.teamHits, team)
				if len(c.teamHits) >= mentionRows {
					break
				}
			}
		}
	}
	if c.scope == "" || c.scope == scopeChat {
		for _, chat := range c.chats {
			hay := chat.title + " " + chat.handle + " " + chat.slug
			if _, ok := pathScore(hay, needle); ok {
				c.chatHits = append(c.chatHits, chat)
				if len(c.chatHits) >= mentionRows {
					break
				}
			}
		}
	}
}

// layoutMentions appends the team section and the conversation section.
func (c *completion) layoutMentions(lines []compLine) []compLine {
	if len(c.teamHits) > 0 {
		rule := deadLine()
		rule.header = "teams"
		lines = append(lines, rule)
		for at := range c.teamHits {
			line := deadLine()
			line.team = at
			lines = append(lines, line)
		}
	}
	if len(c.chatHits) > 0 {
		rule := deadLine()
		rule.header = "conversations"
		lines = append(lines, rule)
		for at := range c.chatHits {
			line := deadLine()
			line.chat = at
			lines = append(lines, line)
		}
	}
	return lines
}

// emptyWord is the line under the prefix words when nothing matched.
func (c *completion) emptyWord() string {
	switch c.scope {
	case scopeTeam:
		return "no team matches"
	case scopeChat:
		return "no conversation matches"
	case scopeFile:
		return "no file matches"
	default:
		return "no matches"
	}
}

// teamChoice is the team under the cursor.
func (c *completion) teamChoice() (mentionTeam, bool) {
	at := c.selLine()
	if at < 0 || c.lines[at].team < 0 {
		return mentionTeam{}, false
	}
	return c.teamHits[c.lines[at].team], true
}

// chatChoice is the conversation under the cursor.
func (c *completion) chatChoice() (mentionChat, bool) {
	at := c.selLine()
	if at < 0 || c.lines[at].chat < 0 {
		return mentionChat{}, false
	}
	return c.chatHits[c.lines[at].chat], true
}

func mentionCount(team mentionTeam) string {
	n := team.members
	if n == 1 {
		return "1 conversation"
	}
	if n == 0 {
		return ""
	}
	return itoa(n) + " conversations"
}

func mentionTeamLabel(team mentionTeam, pal palette) string {
	dot := "●"
	if pal.ascii {
		dot = "*"
	}
	if pen := pal.teamInk(team.hue); pen != nil {
		return pen(dot) + " " + team.name
	}
	return dot + " " + team.name
}

func mentionChatLabel(chat mentionChat) string {
	if chat.handle != "" {
		return "@" + chat.handle
	}
	if chat.title != "" {
		return chat.title
	}
	return "@" + chat.slug
}

// mentionToken is what choosing a conversation types after the "@".
func mentionToken(chat mentionChat) string {
	if chat.handle != "" {
		return chat.handle
	}
	if chat.slug != "" {
		return chat.slug
	}
	return mentionSlug(chat.title)
}

// ── the catalogs, from memory ───────────────────────────────────────────────

// fillMentions copies the in-memory catalogs onto the list. It runs on the
// update loop, beside [completion.sync], and never from a frame.
func (a *app) fillMentions() {
	a.comp.teams = a.mentionTeams()
	a.comp.chats = a.mentionChats()
}

func (a *app) mentionTeams() []mentionTeam {
	if !a.wall.loaded || len(a.wall.teams) == 0 {
		return nil
	}
	out := make([]mentionTeam, 0, len(a.wall.teams))
	for _, t := range a.wall.teams {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		out = append(out, mentionTeam{
			id: t.ID, name: name, slug: mentionSlug(name),
			hue: t.HueSpec(), members: len(t.Members),
		})
	}
	return out
}

// mentionChats is the open conversations in this window, then recent ones that
// are not already open. The conversation in front is left off: pointing at the
// chat you are typing in is not a reference.
func (a *app) mentionChats() []mentionChat {
	front := a.frontTabKey()
	var out []mentionChat
	seen := map[string]bool{}
	for _, tab := range a.tabList() {
		if tab.slot || tab.key == "" || tab.key == front || seen[tab.key] {
			continue
		}
		seen[tab.key] = true
		out = append(out, a.mentionFromTab(tab, true))
	}
	for _, chat := range a.comp.recents {
		if chat.key == "" || chat.key == front || seen[chat.key] {
			continue
		}
		seen[chat.key] = true
		chat.open = false
		out = append(out, chat)
	}
	return out
}

func (a *app) mentionFromTab(tab chatTab, open bool) mentionChat {
	title := strings.TrimSpace(tab.full)
	if title == "" {
		title = strings.TrimSpace(tab.word)
	}
	handle := a.mentionHandle(tab.key)
	note := title
	if open {
		note = "open"
		if title != "" && handle != "" {
			note = title
		}
	}
	return mentionChat{
		key: tab.key, file: tab.file, where: tab.where,
		title: title, handle: handle, slug: mentionSlug(title),
		open: open, note: note,
	}
}

// mentionHandle is the handle any team gave this conversation, or "".
func (a *app) mentionHandle(key string) string {
	if key == "" || !a.wall.loaded {
		return ""
	}
	for _, t := range a.wall.teams {
		for _, m := range t.Members {
			if m.Key == key && m.Handle != "" {
				return m.Handle
			}
		}
	}
	return ""
}

// mentionRecentsMsg is the recent list, read off the loop.
type mentionRecentsMsg struct{ rows []Session }

// loadMentionRecents reads the door's recent list once. The door's function
// may open a directory, so it runs inside the command and not on the loop.
func (a *app) loadMentionRecents() tea.Cmd {
	if a.comp.recentsHeld || a.recentSessions == nil {
		return nil
	}
	a.comp.recentsHeld = true
	read := a.recentSessions
	return func() tea.Msg {
		list := read()
		if len(list) > mentionRecentCap {
			list = list[:mentionRecentCap]
		}
		return mentionRecentsMsg{rows: list}
	}
}

func (a *app) mentionRecentsLoaded(rows []Session) {
	a.comp.recentsHeld = true
	a.comp.recents = a.comp.recents[:0]
	seen := map[string]bool{}
	for _, row := range rows {
		file := strings.TrimSpace(row.File)
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		title := strings.TrimSpace(row.Title)
		if title == "" {
			title = strings.TrimSpace(row.Opening)
		}
		if title == "" {
			continue
		}
		a.comp.recents = append(a.comp.recents, mentionChat{
			key: file, file: file, title: title,
			handle: a.mentionHandle(file), slug: mentionSlug(title),
			note: title,
		})
	}
	if a.comp.open {
		a.fillMentions()
		a.comp.rank()
	}
	a.touch()
}

// ── choosing ────────────────────────────────────────────────────────────────

// completeTeam types "●" and the team's slug where the @ token was. The "@"
// comes out: the bullet is the mark, and a second mark in front of it would
// be two names for one thing.
func (a *app) completeTeam(team mentionTeam) {
	slug := team.slug
	if slug == "" {
		slug = mentionSlug(team.name)
	}
	token := "●" + slug
	e := &a.input
	head := append([]rune(nil), e.value[:a.comp.at]...)
	tail := append([]rune(nil), e.value[e.cursor:]...)
	e.value = append(append(head, []rune(token)...), tail...)
	e.cursor = a.comp.at + len([]rune(token))
	a.comp.done = ""
	a.comp.close()
	a.touch()
}

// completeChat types "@" and the handle, or the title's slug when the
// conversation has no handle.
func (a *app) completeChat(chat mentionChat) {
	token := mentionToken(chat)
	if token == "" {
		a.comp.close()
		return
	}
	e := &a.input
	head := append([]rune(nil), e.value[:a.comp.at+1]...)
	tail := append([]rune(nil), e.value[e.cursor:]...)
	e.value = append(append(head, []rune(token)...), tail...)
	e.cursor = a.comp.at + 1 + len([]rune(token))
	a.comp.done = token
	a.comp.close()
	a.touch()
}

// ── the prefix words ────────────────────────────────────────────────────────

// mentionHeadWords are the three prefixes, drawn as words on the list's first
// row. The order is the order of the sections.
var mentionHeadWords = []string{scopeTeam, scopeChat, scopeFile}

// mentionHeadLine paints those words. The active prefix is accent. The word
// under the pointer wears the cursor ground.
func mentionHeadLine(pal palette, scope, hot string, width int) string {
	var b strings.Builder
	b.WriteString("  ")
	for i, word := range mentionHeadWords {
		if i > 0 {
			b.WriteString("  ")
		}
		painted := pal.dim(word)
		if word == scope {
			painted = pal.accent(word)
		}
		if word == hot {
			painted = pal.cursor(word, 0)
		}
		b.WriteString(painted)
	}
	return b.String()
}

// mentionHeadAt reports which prefix word a column of the header row is on.
// The row is "  team  chat  file", and the column is the frame's.
func mentionHeadAt(x int) (string, bool) {
	at := 2
	for _, word := range mentionHeadWords {
		if x >= at && x < at+len(word) {
			return word, true
		}
		at += len(word) + 2
	}
	return "", false
}

// mentionHeadPress is a click on one of those words. It types that prefix, or
// takes it back off when it was already the one in force.
func (a *app) mentionHeadPress(x, y int) (tea.Cmd, bool) {
	if !a.comp.open || a.comp.arg || a.comp.top != 0 {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay || mark.index != 0 {
		return nil, false
	}
	word, ok := mentionHeadAt(x)
	if !ok {
		return nil, false
	}
	a.applyMentionScope(word)
	return a.edited(), true
}

// applyMentionScope rewrites the @ token's prefix and leaves the needle.
func (a *app) applyMentionScope(scope string) {
	e := &a.input
	if a.comp.at < 0 || a.comp.at >= len(e.value) {
		return
	}
	_, needle := mentionScope(a.comp.query)
	next := scope + ":"
	if a.comp.scope == scope {
		next = ""
	}
	repl := []rune(next + needle)
	head := append([]rune(nil), e.value[:a.comp.at+1]...)
	tail := append([]rune(nil), e.value[e.cursor:]...)
	e.value = append(append(head, repl...), tail...)
	e.cursor = a.comp.at + 1 + len(repl)
	a.comp.done = ""
}

// mentionHeadHint is the one line under the box while the pointer is on a
// prefix word.
func (a *app) mentionHeadHint() string {
	if a.hot.kind != hoverOverlay || !a.comp.open {
		return ""
	}
	switch a.hot.key {
	case scopeTeam:
		return "only teams" + hintSegment + "click"
	case scopeChat:
		return "only conversations" + hintSegment + "click"
	case scopeFile:
		return "only files" + hintSegment + "click"
	default:
		return ""
	}
}

// paintDraftMentions colours a "●slug" in the box with its team's colour. The
// runes stay the runes, so the caret's column does not move.
func (a *app) paintDraftMentions(block []string) []string {
	if len(a.comp.teams) == 0 && len(a.wall.teams) == 0 {
		return block
	}
	teams := a.comp.teams
	if len(teams) == 0 {
		teams = a.mentionTeams()
	}
	for i, line := range block {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "●") {
			continue
		}
		for _, team := range teams {
			token := "●" + team.slug
			if !strings.Contains(line, token) {
				continue
			}
			pen := a.pal.accent
			if ink := a.pal.teamInk(team.hue); ink != nil {
				pen = ink
			}
			line = strings.Replace(line, token, pen(token), 1)
		}
		block[i] = line
	}
	return block
}

// mentionLinkOrd is the first ordinal a block's mention references take. It
// sits above the team references ([teamLinkOrd]) so one hover holds either.
const mentionLinkOrd = 1 << 17

// mentionLinkPass inks "●slug" and "@handle" on the person's own messages, and
// on the same rows a team reference already rides. A conversation in no team
// is still a door: the catalog is every open tab, every recent row this list
// has loaded, and every team member.
func (a *app) mentionLinkPass(out []row, es []entry) {
	teams := a.mentionTeams()
	chats := a.mentionLinkChats()
	if len(teams) == 0 && len(chats) == 0 {
		return
	}
	block, n := -1, 0
	for i := range out {
		r := &out[i]
		if r.entry < 0 || r.entry >= len(es) || !mentionLinkRow(&es[r.entry]) || r.hit == hitPictureOriginal {
			continue
		}
		if r.entry != block {
			block, n = r.entry, 0
		}
		hot := -1
		if at := a.hoveringLink(r.entry); at >= mentionLinkOrd {
			hot = at - mentionLinkOrd - n
		}
		rowChats := chats
		if es[r.entry].kind != entryUser {
			rowChats = nil
		}
		text, links := linkifyMentions(r.text, a.pal, teams, rowChats, hot)
		if len(links) == 0 {
			continue
		}
		for j := range links {
			links[j].ord = mentionLinkOrd + n + j
		}
		n += len(links)
		r.text = text
		r.links = append(r.links, links...)
	}
}

func mentionLinkRow(e *entry) bool {
	if e.kind == entryUser {
		return true
	}
	return teamLinkRow(e)
}

// mentionLinkChats is every conversation a sent token might name: open tabs,
// the recent snapshot, and every team member, including ones in no team only
// as a tab or a recent row.
func (a *app) mentionLinkChats() []mentionChat {
	seen := map[string]bool{}
	var out []mentionChat
	add := func(chat mentionChat) {
		if chat.key == "" || seen[chat.key] {
			return
		}
		seen[chat.key] = true
		out = append(out, chat)
	}
	for _, chat := range a.mentionChats() {
		add(chat)
	}
	for _, chat := range a.comp.recents {
		add(chat)
	}
	if a.wall.loaded {
		for _, t := range a.wall.teams {
			for _, m := range t.Members {
				add(mentionChat{
					key: m.Key, file: m.File, where: m.Where,
					title: m.Word, handle: m.Handle, slug: mentionSlug(m.Word),
					note: m.Word,
				})
			}
		}
	}
	return out
}

func linkifyMentions(text string, pal palette, teams []mentionTeam, chats []mentionChat, hot int) (string, []taskLink) {
	if !strings.Contains(text, "●") && !strings.Contains(text, "@") {
		return text, nil
	}
	flat, _ := flatten(text)
	refs := mentionTextRefs(flat, teams, chats)
	if len(refs) == 0 {
		return text, nil
	}
	return paintLinksWith(text, flat, refs, pal, hot, teamLinkInk, teamLinkHotInk)
}

func mentionTextRefs(flat string, teams []mentionTeam, chats []mentionChat) []taskRef {
	var out []taskRef
	const bullet = "●"
	for i := 0; i < len(flat); i++ {
		if strings.HasPrefix(flat[i:], bullet) && (i == 0 || !wordByte(flat[i-1])) {
			j := i + len(bullet)
			for j < len(flat) && (wordByte(flat[j]) || flat[j] == '-') {
				j++
			}
			slug := strings.ToLower(flat[i+len(bullet) : j])
			for _, team := range teams {
				if team.slug == slug {
					hue := team.hue
					out = append(out, taskRef{
						from: i, to: j, team: team.id,
						paint: func(pal palette, s string) string {
							if pen := pal.teamInk(hue); pen != nil {
								return pal.underline(pen(s))
							}
							return teamLinkInk(pal, s)
						},
					})
					break
				}
			}
			i = j - 1
			continue
		}
		if flat[i] != '@' || (i > 0 && (wordByte(flat[i-1]) || flat[i-1] == '.' || flat[i-1] == '@')) {
			continue
		}
		j := i + 1
		for j < len(flat) && (wordByte(flat[j]) || flat[j] == '-' || flat[j] == '/') {
			j++
		}
		token := flat[i+1 : j]
		if strings.Contains(token, "/") || token == "" {
			i = j - 1
			continue
		}
		low := strings.ToLower(token)
		if scope, rest := mentionScope(low); scope != "" {
			low = strings.ToLower(rest)
		}
		for _, chat := range chats {
			if low == "" {
				break
			}
			if strings.EqualFold(chat.handle, low) || chat.slug == low {
				out = append(out, taskRef{from: i, to: j, member: chat.key, title: chat.title})
				break
			}
		}
		i = j - 1
	}
	return out
}

func (a *app) mentionChatPress(key string) tea.Cmd {
	if key == "" || key == a.frontTabKey() {
		return nil
	}
	for _, tab := range a.tabList() {
		if tab.key == key {
			return a.tabGo(tab)
		}
	}
	for _, chat := range a.mentionLinkChats() {
		if chat.key != key {
			continue
		}
		word := chat.title
		if strings.TrimSpace(word) == "" {
			word = "@" + mentionToken(chat)
		}
		return a.tabGo(chatTab{key: chat.key, file: chat.file, where: chat.where, word: word, full: chat.title})
	}
	return nil
}

func (a *app) mentionChatHint(key string) string {
	for _, chat := range a.mentionLinkChats() {
		if chat.key != key {
			continue
		}
		name := "@" + mentionToken(chat)
		verb := "Resume"
		if tabsHold(a.tabList(), key) {
			verb = "Open"
		}
		words := verb + " " + name
		if title := strings.TrimSpace(chat.title); title != "" && title != name {
			words += hintSegment + title
		}
		return words + hintSegment + "click"
	}
	return ""
}
