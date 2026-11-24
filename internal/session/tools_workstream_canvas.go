package session

// THE CANVAS, FOR AN ORGANIZATION SERVER'S AGENT.
//
// On a laptop a conversation draws a canvas with the `canvas` command in its
// shell, guided by the canvas skill the desktop app installs. The server's
// agent has no shell, so it had no canvas at all, and every organization
// canvas had to be drawn by hand. `ws_canvas` is that same command, one call
// per command line: the words after `canvas` in command, what it reads from
// stdin in input, and a file it names in files. The server runs the very
// same `canvas` program against the organization's canvases, after deciding
// which commands and which canvases the person being answered may reach.
//
//   - THE GUIDE IS THE PERSONAL ONE, WORD FOR WORD. prompts/canvas-guide.md is
//     the desktop app's own skill (codeaf-desk docs/canvas/skill/SKILL.md),
//     with one preamble that says how its shell commands become calls here.
//     An organization's canvases are held to the same bar as a person's own
//     because the model reads the same teaching.
//
//   - THE GUIDE IS READ BEFORE THE FIRST WRITE. A canvas drawn without it is
//     the wireframe the guide exists to prevent, so the first command that
//     would change a canvas in a conversation answers the guide instead and
//     runs nothing. Reading (list, show, the tokens, the marks) needs no guide.
//
//   - A SERVER WITHOUT CANVASES GIVES NO TOOL. The verb is on the belt only
//     when the door implements [WorkstreamCanvasDoor] and says it keeps
//     canvases; absent, never present and failing (the design law).

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// WSCanvasTool is the tool's name, spelled once.
const WSCanvasTool = "ws_canvas"

//go:embed prompts/canvas-guide.md
var canvasGuideSkill string

// CanvasCall is one `canvas` command for the server to run: Args are the
// words after `canvas`, Input what it reads from stdin, Files the files it
// names, by name, each a text file's words.
type CanvasCall struct {
	Args  []string
	Input string
	Files map[string]string
}

// WorkstreamCanvasDoor is the door onto the workstream's canvases. Canvas runs
// one command as the person the conversation answers and answers what the
// model reads; an error is said to the model and nothing changed.
type WorkstreamCanvasDoor interface {
	CanvasesKept() bool
	Canvas(ctx context.Context, call CanvasCall) (string, error)
}

// canvasDoor is the conversation's canvas door, or nil when its workstream
// door keeps no canvases.
func (c Config) canvasDoor() WorkstreamCanvasDoor {
	door, ok := c.Workstream.(WorkstreamCanvasDoor)
	if !ok || !door.CanvasesKept() {
		return nil
	}
	return door
}

// canvasFileName is a name a call's file may have: plain, no folder.
var canvasFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}$`)

// The most a call may carry: its stdin and its files together.
const canvasCallMost = 2 << 20

// canvasReadVerbs are the commands that change nothing, which need no guide.
var canvasReadVerbs = map[string]bool{"list": true, "show": true, "marks": true, "writers": true, "help": true}

// canvasGuidePreamble is how the guide's shell becomes this tool, and what
// the server cannot do that a laptop can.
const canvasGuidePreamble = "# Drawing a canvas on the organization's server\n\n" +
	"You have no shell here. Every `canvas …` command the guide below spells is ONE " + WSCanvasTool + " call: " +
	"put the words after `canvas` in command, quoted as in a shell (`frame add CV --kind slide --name \"The problem\"`); " +
	"what the command reads from stdin (`--html -`, `--tree -`, `--json -`) in input; and each file it names " +
	"(`--html home.html`, `asset add CV logo.svg`, `--csv rows.csv`) in files, by that plain name, as the file's text.\n\n" +
	"- Only this workstream's canvases are reachable. `list` lists them. `new --title T` makes one in the workstream's Files, where everyone in it sees it; in a task's run it is also the task's Result.\n" +
	"- There is no app on the server to draw a picture, so `look` is not available. Check a frame by reading it back with `show ID --frame F`, and check sizes, words and overlaps from what it says.\n" +
	"- Not available here: look, render, export, trash, copy, members, invite, item, rpc, `lease --wait`, `--root`, `--out` and `--worker`.\n" +
	"- Never reply with a workaf:// link. Name the canvas by its title and say it is in the workstream's Files.\n" +
	"- Ignore the guide's lines about your shell, PATH, WORKAF_CANVAS_ROOT, your working folder and `canvas look`.\n\n" +
	"The guide, as written for a laptop:\n\n"

// canvasGuide is the guide the tool answers.
func canvasGuide() string { return canvasGuidePreamble + canvasGuideSkill }

const wsCanvasDescription = "Make and change the workstream's canvases — slides, a deck, social cards, posters, a one-page plan, a landing page, a diagram, a board — " +
	"by running one `canvas` command per call. Anything visual a person asks for goes on a canvas, never into a document. " +
	"Before your first change in a conversation, call it with command `guide` and follow the guide: it is how a canvas is made well here. " +
	"command is the words after `canvas` (for example `new --title \"Launch cards\"`, `show cv_…`, `frame add cv_… --kind design --name \"Card one\"`); " +
	"input is what the command reads from stdin; files are the files it names, by plain name. Only this workstream's canvases are reachable."

func wsCanvasSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"command":{"type":"string","description":"The words after canvas, quoted as in a shell; or guide."},` +
		`"input":{"type":"string","description":"What the command reads from stdin (--html -, --tree -, --json -)."},` +
		`"files":{"type":"object","additionalProperties":{"type":"string"},"description":"Files the command names, by plain name (home.html, logo.svg), each the file's text."}` +
		`},"required":["command"],"additionalProperties":false}`
}

func init() { glossField[WSCanvasTool] = "command" }

// wsCanvasTool is the tool onto door. guided says whether this conversation
// has read the guide; the first change marks it.
func (a *Agent) wsCanvasTool(door WorkstreamCanvasDoor) bare.Tool {
	return bare.Tool{
		Name: WSCanvasTool, Description: wsCanvasDescription, Schema: json.RawMessage(wsCanvasSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Command string            `json:"command"`
				Input   string            `json:"input"`
				Files   map[string]string `json:"files"`
			}
			if err := decodeToolArguments(args, &p); err != nil {
				return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
			}
			words, err := splitCanvasCommand(p.Command)
			if err != nil {
				return "Nothing ran: " + err.Error(), true, nil
			}
			if len(words) > 0 && words[0] == "canvas" {
				words = words[1:]
			}
			if len(words) == 0 {
				return "Nothing ran: command is the words after canvas, such as `list` or `new --title \"Launch cards\"`, or `guide`.", true, nil
			}
			if words[0] == "guide" {
				a.canvasGuided.Store(true)
				return canvasGuide(), false, nil
			}
			if !canvasReadVerbs[words[0]] && !canvasReadsOnly(words) && !a.canvasGuided.Swap(true) {
				return "Nothing ran yet. Read this guide first, then run your command again.\n\n" + canvasGuide(), false, nil
			}
			size := len(p.Input)
			for name, text := range p.Files {
				if !canvasFileName.MatchString(name) {
					return "Nothing ran: a file's name is plain, such as home.html or logo.svg, with no folder; " + name + " is not.", true, nil
				}
				size += len(text)
			}
			if size > canvasCallMost {
				return "Nothing ran: one call carries at most 2 MB of input and files.", true, nil
			}
			line, err := door.Canvas(ctx, CanvasCall{Args: words, Input: p.Input, Files: p.Files})
			if err != nil {
				return "Nothing changed: " + oneLine(err.Error()), true, nil
			}
			return line, false, nil
		},
	}
}

// canvasReadsOnly says a two-word command only reads (token list, asset
// list, rows list).
func canvasReadsOnly(words []string) bool {
	return len(words) > 1 && words[1] == "list" && (words[0] == "token" || words[0] == "asset" || words[0] == "rows")
}

// splitCanvasCommand splits a command line the way a shell would for these
// commands: words on spaces, '…' kept whole, "…" kept whole with \" and \\,
// and a backslash outside quotes taking the next character. Nothing expands.
func splitCanvasCommand(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	in := false
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, in = true, true
		case r == '\'' || r == '"':
			quote, in = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("a quote in command is not closed")
	}
	if in {
		words = append(words, cur.String())
	}
	return words, nil
}
