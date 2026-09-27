package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A TILE IS THE CONVERSATION, SMALLER ─────────────────────────────────────
//
// A tile used to draw its tail as flattened text: every line one ink, fences
// stripped, a reply and a tool call told apart only by a leading mark. On a
// wall of six conversations that is six walls of grey, and the eye has nothing
// to land on. So a tile is drawn with the conversation's own pieces: the
// person's words under the `›` in their muted blue, the reply through the same
// markdown pass the transcript uses (headings, lists, code highlighted by
// chroma), and each tool call on the rail with its name and its target, a
// shell command highlighted as it is in the chat.
//
// IT DOES NOT CALL [app.renderEntry]. That painter caches rows by the entry's
// position in the conversation in front, and a tile drawn through it would
// write another conversation's rows into those slots. The pieces below are
// the stateless ones renderEntry is built from.
//
// AND IT IS DRAWN ONCE PER READING, not once per frame: the rows are kept on
// the tail against its version and the width they were drawn at, so a frame
// with nothing new costs a slice copy per tile.

// wallRecentCap is how many entries a tail keeps for drawing. A tile is at
// most a screen tall, and a dozen entries fill that many times over.
const wallRecentCap = 12

// wallReplyBytes is the most of one reply a tile renders. The tile shows the
// end of it, and markdown over a whole essay to draw its last paragraph is work
// thrown away on every reading.
const wallReplyBytes = 3000

// wallMiniRows is the tail drawn at the given width, from the cache when the
// reading and the width have not moved.
func (a *app) wallMiniRows(t *wallTail, width int) []string {
	if t == nil || width < 8 || len(t.recent) == 0 {
		return nil
	}
	if t.rows != nil && t.rowsW == width && t.rowsVer == t.ver {
		return t.rows
	}
	t.rows, t.rowsW, t.rowsVer = a.wallQuiet(a.wallDraw(t.recent, width)), width, t.ver
	return t.rows
}

// wallSharpRows is how many of a tile's newest rows keep the chat's full ink.
const wallSharpRows = 3

// wallQuiet puts a tile's body BELOW the chat in the hierarchy. A tile is a
// glance, and six of them drawn at the transcript's full ink are six
// conversations all asking to be read at once. So the fade ladder the long
// lists already use (depthfade.go) is spent on age: the newest rows, which say
// what the agent is doing now, keep their colour, and everything above them
// steps down the three stops toward the top of the tile. Sharp where the
// agent is, quiet where it was.
func (a *app) wallQuiet(rows []string) []string {
	n := len(rows)
	for i := range rows {
		age := n - 1 - i
		switch {
		case age < wallSharpRows:
			continue
		case age < wallSharpRows+4:
			rows[i] = a.pal.fadeRow(rows[i], 0)
		case age < wallSharpRows+10:
			rows[i] = a.pal.fadeRow(rows[i], 1)
		default:
			rows[i] = a.pal.fadeRow(rows[i], 2)
		}
	}
	return rows
}

// wallDoing is what a working conversation is doing, in two or three words,
// read off the newest entry it holds. It says nothing for a conversation at
// rest: the tile's age already tells that story.
func wallDoing(recent []session.DisplayEntry, signal tabSignal) string {
	switch signal {
	case tabNeedsPerson:
		return "waiting on you"
	case tabWorking:
	default:
		return ""
	}
	if len(recent) == 0 {
		return "working"
	}
	last := recent[len(recent)-1]
	switch last.Role {
	case "tool":
		if name, _ := toolWords(last.Tool, ""); name != "" {
			return "running " + name
		}
		return "running a tool"
	case "assistant":
		return "writing"
	}
	return "thinking"
}

// wallDraw lays the entries out oldest first, with the chat's own spacing: a
// blank row between turns, none inside a run of tool calls.
func (a *app) wallDraw(entries []session.DisplayEntry, width int) []string {
	var out []string
	prevTool := false
	for i, e := range entries {
		tool := e.Role == "tool"
		var rows []string
		switch e.Role {
		case "user":
			rows = a.wallUserRows(e.Text, width)
		case "assistant":
			rows = a.wallReplyRows(e.Text, width)
		case "tool":
			last := i == len(entries)-1 || entries[i+1].Role != "tool"
			rows = a.wallToolRows(e, last, width)
		case "aside":
			// AN ASIDE IS DRAWN AS THE CONVERSATION DRAWS IT (teamcard.go's
			// [asideShapeOf]): a team delivery as its lines, a team wake with
			// nothing in it not at all, anything else as its first line.
			switch asideShapeOf(e) {
			case asideTeam:
				rows = a.wallTeamRows(e, width)
			case asideLine:
				if line := strings.TrimSpace(firstLine(e.Text)); line != "" {
					rows = []string{a.pal.italic(a.pal.dim(ansi.Truncate(line, width, "…")))}
				}
			}
		default:
			if line := strings.TrimSpace(firstLine(e.Text)); line != "" {
				rows = []string{a.pal.italic(a.pal.dim(ansi.Truncate(line, width, "…")))}
			}
		}
		if len(rows) == 0 {
			continue
		}
		if len(out) > 0 && !(tool && prevTool) {
			out = append(out, "")
		}
		out = append(out, rows...)
		prevTool = tool
	}
	return out
}

// wallUserRows is the person's message as the transcript draws it: the accent
// `›`, the words in muted, continuations hung under the text. A long message
// shows its first three rows; the reply under it is what a tile is for.
func (a *app) wallUserRows(text string, width int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	body := wrap(text, width-2)
	const most = 3
	if len(body) > most {
		body = append(body[:most-1], ansi.Truncate(body[most-1], width-3, "")+"…")
	}
	rows := make([]string, 0, len(body))
	for i, line := range body {
		lead := "  "
		if i == 0 {
			lead = a.pal.accent("› ")
		}
		rows = append(rows, lead+a.pal.muted(line))
	}
	return rows
}

// wallReplyRows is the reply through the transcript's own markdown pass. Only
// its end is rendered, cut at a paragraph so a heading or a list is not split
// mid-row, and a fence the cut landed inside is reopened so the code under it
// is still drawn as code.
func (a *app) wallReplyRows(text string, width int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) > wallReplyBytes {
		cut := len(text) - wallReplyBytes
		if at := strings.Index(text[cut:], "\n\n"); at >= 0 && at < wallReplyBytes/2 {
			cut += at + 2
		}
		head, tail := text[:cut], text[cut:]
		if strings.Count(head, "```")%2 == 1 {
			tail = "```\n" + tail
		}
		text = tail
	}
	// No path linking: resolving a code span as a path asks the workspace, and
	// this is drawn for conversations that are not the one in front.
	return renderMarkdownWithCode(a.styler(), text, width, nil)
}

// wallTeamRows is a team delivery as the conversation's card draws it
// (teamcard.go): each line headed by who said it to whom, its words under a
// bar, fit to the tile. It is the card's words without the card's memory of the
// conversation in front, which a tile of another conversation must not read.
func (a *app) wallTeamRows(e session.DisplayEntry, width int) []string {
	pal := a.pal
	arrow, bar := a.linearMark("→", "->"), a.linearMark("│", "|")
	var out []string
	for _, c := range teamCardsOf(e, a.teamManagerMark()) {
		head := pal.muted(c.from) + pal.dim(" "+arrow+" ") + pal.muted(c.to)
		if c.tag != "" {
			head += "  " + pal.dim(c.tag)
		}
		out = append(out, ansi.Truncate(head, width, "…"))
		if text := strings.TrimSpace(firstLine(c.text)); text != "" {
			out = append(out, pal.dim(bar+" ")+pal.ink(ansi.Truncate(text, max(width-2, 1), "…")))
		}
	}
	return out
}

// wallToolRows is one call on the rail: its name in muted, its target after
// it, a shell command highlighted exactly as the transcript highlights it.
func (a *app) wallToolRows(e session.DisplayEntry, last bool, width int) []string {
	hint := e.Hint
	if strings.TrimSpace(hint) == "" {
		hint = e.Text
	}
	name, target := toolWords(e.Tool, hint)
	if name == "" && target == "" {
		return nil
	}
	rail := a.pal.dim(a.pal.rail(last))
	room := width - ansi.StringWidth(a.pal.rail(last)) - ansi.StringWidth(name) - 1
	line := rail + a.pal.muted(name)
	if target != "" && target != name && room > 4 {
		fit := ansi.Truncate(target, room, "…")
		if e.Tool == "bash" {
			line += " " + a.pal.shell(fit)
		} else {
			line += " " + a.pal.dim(fit)
		}
	}
	return []string{line}
}
