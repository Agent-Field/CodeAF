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
//     every other organization item on that disk. Its belt is this tool, the
//     workstream's own tools beside it, and the web's two READING hands
//     (web_search, web_fetch, tools_search.go) so research can be answered
//     ([Agent.belt] returns early); every other verb is ABSENT rather than
//     present and refusing.
//
//   - IT WRITES AS THE PERSON WHO ASKED. The door checks their rights again on
//     every write; a person whose edit was taken away mid-answer cannot write
//     through the agent, and the tool says so in words.
//
//   - A PAGE SOMEONE IS EDITING IS NEVER WRITTEN OVER. The door keeps the
//     agent's text as a suggestion for someone to take instead, and the line
//     it answers says so, so the model can tell the person.
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

// workstreamPageReader records task inputs only when page words were shown.
// General workstream reads used by other tools do not imply reading its pages.
type workstreamPageReader interface {
	ReadPages(context.Context, []string)
}

// WorkstreamPagesTool is the tool's name, spelled once.
const WorkstreamPagesTool = "workstream_pages"

// The asks the tool answers.
const (
	wsAskRead  = "read"
	wsAskPage  = "page"
	wsAskWrite = "write"
	wsAskNew   = "new"
	// wsAskDecide is no longer offered (solo-ios-app #4): a proposed decision is one no screen can keep, since
	// Decisions shows only the decisions that recorded themselves. A call admitted before the upgrade still runs.
	wsAskDecide = "decide"
)

// workstreamPageCut is how much of one page `read` quotes; `page` quotes it whole.
const workstreamPageCut = 1500

const workstreamPagesDescription = "Read and write the organization workstream you are this conversation's agent for. " +
	"ask: read (its name, purpose, budget, every page with the start of its words, decisions, sources and links), " +
	"page (one page whole; set page to its id), write (replace one page's words with body, in Markdown; set page), " +
	"or new (a new page with title and body). Decisions are written down by themselves when an Ask is answered: there is nothing to propose and no Keep. " +
	"You write as the person whose message you are answering. A page someone has open for editing is not written over: " +
	"your text waits as a suggestion for someone to take, and the answer says so; tell the person."

func workstreamPagesSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"ask":{"type":"string","enum":["` + strings.Join([]string{wsAskRead, wsAskPage, wsAskWrite, wsAskNew}, `","`) + `"],"description":"What to do."},` +
		`"page":{"type":"string","description":"With page or write: the page's id (pg_…), as read lists it."},` +
		`"title":{"type":"string","description":"With new: the new page's title."},` +
		`"body":{"type":"string","description":"With write or new: the page's words, in Markdown, whole."}` +
		`},"required":["ask"],"additionalProperties":false}`
}

func init() { glossField[WorkstreamPagesTool] = "ask" }

// mayWorkstream says this conversation is a workstream's server agent.
func (c Config) mayWorkstream() bool { return c.Workstream != nil }

// AllWorkDoor reads the person's accessible workstreams without widening writes.
type AllWorkDoor interface {
	AllWorkAvailable() bool
	AllWork(context.Context) (string, error)
}

// workstreamTool reads and writes the workstream through [Config.Workstream].
func (a *Agent) workstreamTool() bare.Tool {
	scope, scoped := a.config.Workstream.(AllWorkDoor)
	scoped = scoped && scope.AllWorkAvailable()
	description, schema := workstreamPagesDescription, workstreamPagesSchemaJSON()
	if scoped {
		description += " In My space, ask all_work reads all workstreams you can access and their tasks. It is read-only; writes still go to My space."
		schema = strings.Replace(schema, `"enum":["read"`, `"enum":["all_work","read"`, 1)
	}
	return bare.Tool{
		Name:        WorkstreamPagesTool,
		Description: description,
		Schema:      json.RawMessage(schema),
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
			case "all_work":
				if !scoped {
					return "All my work is available from Afi in My space.", true, nil
				}
				text, err := scope.AllWork(ctx)
				if err != nil {
					return "Could not read all work: " + oneLine(err.Error()), true, nil
				}
				return text, false, nil
			case wsAskRead:
				ws, err := door.Workstream(ctx)
				if err != nil {
					return "the workstream could not be read: " + oneLine(err.Error()), true, nil
				}
				if reads, ok := door.(workstreamPageReader); ok {
					ids := make([]string, 0, len(ws.Pages))
					for _, pg := range ws.Pages {
						ids = append(ids, pg.ID)
					}
					reads.ReadPages(ctx, ids)
				}
				return WorkstreamAnswer(ws), false, nil
			case wsAskPage:
				ws, err := door.Workstream(ctx)
				if err != nil {
					return "the workstream could not be read: " + oneLine(err.Error()), true, nil
				}
				for _, pg := range ws.Pages {
					if pg.ID == page {
						if reads, ok := door.(workstreamPageReader); ok {
							reads.ReadPages(ctx, []string{page})
						}
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
	// The stage is NOT said: it left the surface in Workstreams v5, and a
	// model that reads it says it ("at stage shaping") to a person.
	fmt.Fprintf(&b, "# %s\n", ws.Name)
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
		fmt.Fprintf(&b, "\n### %s (page id %s, for your tools only)\n\n%s%s\n", p.Title, p.ID, body, cut)
	}
	if len(ws.Decisions) > 0 {
		b.WriteString("\n## Decisions\n\n")
		for _, d := range ws.Decisions {
			state := map[string]string{"kept": "kept", "proposed": "waiting for a person to keep it", "superseded": "replaced by a later one"}[d.State]
			fmt.Fprintf(&b, "- %s (%s, %s)\n", oneLine(d.Text), state, d.By)
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
