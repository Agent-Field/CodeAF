package tui3

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── TEAM REFERENCES IN A CHAT ARE DOORS ─────────────────────────────────────
//
// A conversation in a team talks about its team all the time: the manager's
// replies name members (`@security has the branch`), a member's card says who
// wrote to it (`◆ manager → @gravity`), a tool call names who it is for
// (`team_send @milestones`). Every one of those names a conversation a person
// may want to look at, so every one of them is a link, exactly as a task
// reference is (markdown.go's [linkifyTasks]): the same pass over rendered rows,
// the same columns recorded on the row, the same click resolved by column
// before the row's own answer, and the same hover held as (block, ordinal).
//
// WHAT IS A LINK IS DECIDED FROM MEMORY AND NEVER GUESSED. An `@word` is a
// link only when it is the handle of a member of a team this conversation is
// in ([app.wall.teams], loaded at an opening; the frame reads no disk). Any
// other `@word` stays exactly as the model wrote it: a link that opens nothing
// is the surface claiming a door it does not have. A team's name is a link only
// where it is written AS a team, `team test`, `the test team` or `"test"`,
// because a team named `test` is also an ordinary word.
//
// A PRESS ON A MEMBER OPENS IT, and resumes it first when this window does not
// have it open, through the strip's own door ([app.tabGo]). A press on a team
// opens the conversations view shown on that team. The hint line says which,
// with the member's title: `Open @security · santosh dev2 branch… · click`.
//
// THE HOVER IS A GROUND, as every other control on this surface answers the
// pointer, and not the task link's brightening: the owner's bar is that
// everything clickable has a hover ground.
//
// THE ORDINALS ARE THE TASK LINKS' OWN SPACE, offset by [teamLinkOrd]. A block
// numbers its task references from zero across its rows; its team references
// are numbered from teamLinkOrd, so one hover state holds either and neither
// pass can light the other's link.

// teamLinkOrd is the first ordinal a block's team references take.
const teamLinkOrd = 1 << 16

// teamLinkRow reports whether a row of entry e may carry team references: the
// model's prose, a team's quoted card, the surface's own notes, and the rows of
// a team tool's call. Everything else is someone else's text.
func teamLinkRow(e *entry) bool {
	switch e.kind {
	case entryAssistant, entryTeam, entryNote:
		return true
	case entryTool:
		return strings.HasPrefix(e.tool, "team_")
	}
	return false
}

// teamLinkScope is the teams this conversation's handles resolve in: every
// team it is a member of. nil when it is in none, which is almost every
// conversation, and the pass then costs one comparison per row. Frame-safe:
// memory only.
func (a *app) teamLinkScope() []team {
	if !a.wall.loaded || len(a.wall.teams) == 0 {
		return nil
	}
	front := a.frontTabKey()
	var scope []team
	for _, t := range a.wall.teams {
		if teamHolds(t, front) {
			scope = append(scope, t)
		}
	}
	return scope
}

// teamLinkPass inks every team reference on the rows of out, after they were
// laid out and before the indent law moves them. It is the team half of the
// link pass render.go's [app.deckRows] runs, over the same rows.
func (a *app) teamLinkPass(out []row, es []entry) {
	scope := a.teamLinkScope()
	if len(scope) == 0 {
		return
	}
	front := a.frontTabKey()
	block, n := -1, 0
	for i := range out {
		r := &out[i]
		if r.entry < 0 || r.entry >= len(es) || !teamLinkRow(&es[r.entry]) || r.hit == hitPictureOriginal {
			continue
		}
		if r.entry != block {
			block, n = r.entry, 0
		}
		hot := -1
		if at := a.hoveringLink(r.entry); at >= teamLinkOrd {
			hot = at - teamLinkOrd - n
		}
		prose := es[r.entry].kind == entryAssistant || es[r.entry].kind == entryNote
		text, links := linkifyTeams(r.text, a.pal, scope, a.wall.teams, front, prose, hot)
		if len(links) == 0 {
			continue
		}
		for j := range links {
			links[j].ord = teamLinkOrd + n + j
		}
		n += len(links)
		r.text = text
		r.links = append(r.links, links...)
	}
}

// linkifyTeams is the pass over one painted row: its references to scope's
// members and to teams inked, and their columns recorded. self is this
// conversation's key, whose own handle is not a door. prose says the row is
// the model's words, where inline code is code and a fence is source.
func linkifyTeams(text string, pal palette, scope, all []team, self string, prose bool, hot int) (string, []taskLink) {
	if !strings.Contains(text, "@") && !teamNameIn(text, all) {
		return text, nil
	}
	flat, ground := flatten(text)
	if prose && strings.Contains(flat, tokens.GlyphCodeGutter) {
		return text, nil
	}
	refs := append(teamHandleRefs(flat, scope, self), teamNameRefs(flat, all)...)
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].from < refs[j].from })
	kept := refs[:0]
	end := 0
	for _, ref := range refs {
		if ref.from < end {
			continue
		}
		if prose && (grounded(ground, ref.from, ref.to) || masked(flat, ref.from)) {
			continue
		}
		kept = append(kept, ref)
		end = ref.to
	}
	if len(kept) == 0 {
		return text, nil
	}
	return paintLinksWith(text, flat, kept, pal, hot, teamLinkInk, teamLinkHotInk)
}

// teamLinkInk is what a team reference wears: the task link's own ink, the
// accent underlined, because both are the same kind of door.
func teamLinkInk(pal palette, s string) string { return taskLinkInk(pal, s) }

// teamLinkHotInk is a team reference under the pointer: ink on the cursor
// ground, still underlined.
func teamLinkHotInk(pal palette, s string) string {
	return pal.cursor(pal.underline(pal.ink(s)), 0)
}

// teamNameIn is the cheap reject for names: whether any team's name appears
// in the painted row at all, ignoring case.
func teamNameIn(text string, all []team) bool {
	for _, t := range all {
		if name := strings.TrimSpace(t.Name); name != "" && indexFoldAny(text, name, 0) >= 0 {
			return true
		}
	}
	return false
}

// teamHandleRefs is every `@handle` in s that names a member of scope other
// than self. An `@` inside a word (an address, `a@b`) opens nothing.
func teamHandleRefs(s string, scope []team, self string) []taskRef {
	var out []taskRef
	for i := 0; i < len(s); i++ {
		if s[i] != '@' || (i > 0 && (wordByte(s[i-1]) || s[i-1] == '.' || s[i-1] == '@')) {
			continue
		}
		j := i + 1
		for j < len(s) && (wordByte(s[j]) || s[j] == '-') {
			j++
		}
		for j > i+1 && s[j-1] == '-' {
			j--
		}
		if j == i+1 {
			continue
		}
		handle := strings.ToLower(s[i+1 : j])
		for _, t := range scope {
			if m, ok := t.ByHandle(handle); ok && m.Key != self {
				out = append(out, taskRef{from: i, to: j, member: m.Key, team: t.ID})
				break
			}
		}
		i = j - 1
	}
	return out
}

// teamNameRefs is every place in s a team's name is written as a team: after
// the word `team`, before it, or in quotes.
func teamNameRefs(s string, all []team) []taskRef {
	var out []taskRef
	for _, t := range all {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		for at := 0; ; {
			i := indexFoldAny(s, name, at)
			if i < 0 {
				break
			}
			j := i + len(name)
			at = j
			if (i > 0 && wordByte(s[i-1])) || (j < len(s) && wordByte(s[j])) {
				continue
			}
			if teamNamedAsTeam(s, i, j) {
				out = append(out, taskRef{from: i, to: j, team: t.ID})
			}
		}
	}
	return out
}

// teamNamedAsTeam reports whether s[i:j] is written as a team's name.
func teamNamedAsTeam(s string, i, j int) bool {
	if i > 0 && j < len(s) && s[i-1] == '"' && s[j] == '"' {
		return true
	}
	before := strings.TrimRight(strings.TrimRight(s[:i], `"`), " ")
	if len(before) >= 4 && strings.EqualFold(before[len(before)-4:], "team") &&
		(len(before) == 4 || !wordByte(before[len(before)-5])) {
		return true
	}
	after := strings.TrimLeft(strings.TrimLeft(s[j:], `"`), " ")
	if len(after) < len(s[j:]) && len(after) >= 4 && strings.EqualFold(after[:4], "team") &&
		(len(after) == 4 || !wordByte(after[4])) {
		return true
	}
	return false
}

// indexFoldAny is [indexFold] for a needle in any case.
func indexFoldAny(s, needle string, from int) int {
	for i := from; i+len(needle) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

// teamLinkPress is a press on a team reference: the member opened, resumed
// first when this window does not have it open, or the team shown on the
// conversations view.
func (a *app) teamLinkPress(link taskLink) tea.Cmd {
	t, ok := a.teamByID(link.team)
	if !ok {
		return nil
	}
	if link.member == "" {
		a.wall.activeID = t.ID
		a.chatTabBar = tabBar{}
		return a.openWall()
	}
	m, ok := t.Member(link.member)
	if !ok || m.Key == a.frontTabKey() {
		return nil
	}
	for _, tab := range a.tabList() {
		if tab.key == m.Key {
			return a.tabGo(tab)
		}
	}
	word := m.Word
	if strings.TrimSpace(word) == "" && m.Handle != "" {
		word = "@" + m.Handle
	}
	return a.tabGo(chatTab{key: m.Key, file: m.File, where: m.Where, word: word, full: word})
}

// teamLinkHint is what the hint line says with the pointer on a team
// reference, "" when it is not on one.
func (a *app) teamLinkHint() string {
	if a.hot.kind != hoverLink || a.hot.index < teamLinkOrd || a.hot.key == "" {
		return ""
	}
	id, key, _ := strings.Cut(a.hot.key, "\x00")
	if id == "" && key != "" {
		return a.mentionChatHint(key)
	}
	t, ok := a.teamByID(id)
	if !ok {
		return ""
	}
	if key == "" {
		return "Show " + t.Name + " on the conversations view" + hintSegment + wallMembersWord(len(t.Members)) + hintSegment + "click"
	}
	m, ok := t.Member(key)
	if !ok {
		return ""
	}
	return a.teamMemberHint(m)
}

// teamLinkHintTitle is the most cells of a member's title the hint line spends.
const teamLinkHintTitle = 28

// teamLinkKey is a team reference's identity for the hover, from which the
// hint is read again off memory: the team, and the member when there is one.
func teamLinkKey(link taskLink) string {
	if link.team == "" && link.member == "" {
		return ""
	}
	return link.team + "\x00" + link.member
}
