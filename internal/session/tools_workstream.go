package session

// THE ORGANIZATION SERVER'S WORKSTREAM AGENT, AND ITS ONE TOOL: `workstream_pages`.
//
// When an organization runs its workstreams' agents on its own server
// (WAVE2.md §4.3, internal/factory/serve's workstream_agent.go), the server
// answers each message with an ordinary conversation of this package, run with
// the organization's key, whose belt carries exactly one tool: this one, onto
// the workstream as the server keeps it ([Config.Workstream]).
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - THE SERVER'S AGENT TOUCHES NOTHING BUT ITS WORKSTREAM. No shell, no files,
//     no repository and no machine: it runs on the organization's server, which
//     is nobody's laptop, so a door that set it loose there would be a door onto
//     every other organization item on that disk. Its belt is this tool alone
//     ([Agent.belt] returns early), and every other verb is ABSENT rather than
//     present and refusing.
//
//   - IT WRITES AS THE PERSON WHO ASKED. The door checks their rights again on
//     every write; a person whose edit was taken away mid-answer cannot write
//     through the agent, and the tool says so in words.
//
//   - A PAGE SOMEONE IS EDITING IS NEVER WRITTEN OVER. The door keeps the
//     agent's text as a version in that page's history instead, and the line it
//     answers says which version, so the model can tell the person.
//
//   - A DECISION IS PROPOSED, NEVER KEPT. A person keeps it.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// WorkstreamDoor is the server agent's door onto its one workstream. Every
// method may touch the server's store; the tool calls them from its own
// goroutine.
type WorkstreamDoor interface {
	// Workstream is the workstream as it is now, its pages' words included.
	Workstream(ctx context.Context) (factory.OrgWorkstream, error)
	// WritePage writes body as page's words, or makes a new page titled title
	// when page is "". It answers one line saying what happened.
	WritePage(ctx context.Context, page, title, body string) (string, error)
	// Decide proposes a decision. It answers one line.
	Decide(ctx context.Context, text string) (string, error)
}

// WorkstreamPagesTool is the tool's name, spelled once.
const WorkstreamPagesTool = "workstream_pages"

// The asks the tool answers.
const (
	wsAskRead   = "read"
	wsAskPage   = "page"
	wsAskWrite  = "write"
	wsAskNew    = "new"
	wsAskDecide = "decide"
)

// workstreamPageCut is how much of one page `read` quotes; `page` quotes it whole.
const workstreamPageCut = 1500

const workstreamPagesDescription = "Read and write the organization workstream you are this conversation's agent for. " +
	"ask: read (its name, purpose, stage, go-aheads, budget, every page with the start of its words, decisions, sources and links), " +
	"page (one page whole; set page to its id), write (replace one page's words with body, in Markdown; set page), " +
	"new (a new page with title and body), or decide (propose one decision, its words in text; a person keeps it). " +
	"You write as the person whose message you are answering. A page someone has open for editing is not written over: " +
	"your text is kept as a version in its history and the answer says which; tell the person so they can restore it."

func workstreamPagesSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"ask":{"type":"string","enum":["` + strings.Join([]string{wsAskRead, wsAskPage, wsAskWrite, wsAskNew, wsAskDecide}, `","`) + `"],"description":"What to do."},` +
		`"page":{"type":"string","description":"With page or write: the page's id (pg_…), as read lists it."},` +
		`"title":{"type":"string","description":"With new: the new page's title."},` +
		`"body":{"type":"string","description":"With write or new: the page's words, in Markdown, whole."},` +
		`"text":{"type":"string","description":"With decide: the decision, in one or two sentences."}` +
		`},"required":["ask"],"additionalProperties":false}`
}

func init() { glossField[WorkstreamPagesTool] = "ask" }

// mayWorkstream says this conversation is a workstream's server agent.
func (c Config) mayWorkstream() bool { return c.Workstream != nil }

// workstreamTool reads and writes the workstream through [Config.Workstream].
func (a *Agent) workstreamTool() bare.Tool {
	return bare.Tool{
		Name:        WorkstreamPagesTool,
		Description: workstreamPagesDescription,
		Schema:      json.RawMessage(workstreamPagesSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Ask   string `json:"ask"`
				Page  string `json:"page"`
				Title string `json:"title"`
				Body  string `json:"body"`
				Text  string `json:"text"`
			}
			if err := decodeToolArguments(args, &p); err != nil {
				return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
			}
			door := a.config.Workstream
			page := strings.TrimSpace(p.Page)
			switch strings.ToLower(strings.TrimSpace(p.Ask)) {
			case wsAskRead:
				ws, err := door.Workstream(ctx)
				if err != nil {
					return "the workstream could not be read: " + oneLine(err.Error()), true, nil
				}
				return WorkstreamAnswer(ws), false, nil
			case wsAskPage:
				ws, err := door.Workstream(ctx)
				if err != nil {
					return "the workstream could not be read: " + oneLine(err.Error()), true, nil
				}
				for _, pg := range ws.Pages {
					if pg.ID == page {
						return "# " + pg.Title + " (" + pg.ID + ")\n\n" + pg.BodyMD, false, nil
					}
				}
				return "this workstream has no page " + strconvQuote(page) + "; ask read lists its pages", true, nil
			case wsAskWrite:
				if page == "" {
					return rejectToolArguments(ctx, "Invalid arguments: write needs page, the page's id; use new for a new page."), true, nil
				}
				line, err := door.WritePage(ctx, page, "", p.Body)
				if err != nil {
					return "Nothing changed: " + oneLine(err.Error()), true, nil
				}
				return line, false, nil
			case wsAskNew:
				if strings.TrimSpace(p.Title) == "" {
					return rejectToolArguments(ctx, "Invalid arguments: new needs title."), true, nil
				}
				line, err := door.WritePage(ctx, "", p.Title, p.Body)
				if err != nil {
					return "Nothing changed: " + oneLine(err.Error()), true, nil
				}
				return line, false, nil
			case wsAskDecide:
				line, err := door.Decide(ctx, p.Text)
				if err != nil {
					return "Nothing changed: " + oneLine(err.Error()), true, nil
				}
				return line, false, nil
			}
			return rejectToolArguments(ctx, "Invalid arguments: ask is read, page, write, new or decide."), true, nil
		},
	}
}

func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }

// WorkstreamAnswer is `read`'s answer: the workstream in plain words, each
// page cut to its start.
func WorkstreamAnswer(ws factory.OrgWorkstream) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nstage: %s\n", ws.Name, ws.Stage)
	if ws.BudgetUSD != nil {
		fmt.Fprintf(&b, "budget: $%.2f\n", *ws.BudgetUSD)
	}
	if strings.TrimSpace(ws.Purpose) != "" {
		fmt.Fprintf(&b, "\n## Purpose\n\n%s\n", strings.TrimSpace(ws.Purpose))
	}
	b.WriteString("\n## Pages\n")
	if len(ws.Pages) == 0 {
		b.WriteString("\n(no pages yet)\n")
	}
	for _, p := range ws.Pages {
		body := strings.TrimSpace(p.BodyMD)
		cut := ""
		if len(body) > workstreamPageCut {
			body, cut = body[:workstreamPageCut], fmt.Sprintf("\n… (%d more bytes; ask page)", len(p.BodyMD)-workstreamPageCut)
		}
		fmt.Fprintf(&b, "\n### %s (%s, %s)\n\n%s%s\n", p.Title, p.ID, p.Kind, body, cut)
	}
	if len(ws.Decisions) > 0 {
		b.WriteString("\n## Decisions\n\n")
		for _, d := range ws.Decisions {
			fmt.Fprintf(&b, "- [%s] %s (%s)\n", d.State, oneLine(d.Text), d.By)
		}
	}
	if len(ws.Sources) > 0 {
		b.WriteString("\n## Sources\n\n")
		for _, s := range ws.Sources {
			fmt.Fprintf(&b, "- %s %s %s\n", s.Kind, s.Title, s.Ref)
		}
	}
	if len(ws.Links) > 0 {
		fmt.Fprintf(&b, "\n## Linked workstreams\n\n%s\n", strings.Join(ws.Links, ", "))
	}
	return b.String()
}
