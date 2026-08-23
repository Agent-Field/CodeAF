package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ONE ENV, TWO CONSUMERS — and this file is the wound being closed.
//
// The tree already had two drifting things called Env: internal/subharness's
// (exec_model.go, an interface over the five DAG node kinds) and the session's
// implementation of the same interface over its own belt. The seam between them
// is written up at cmd/aforge/chatv3_harness.go as a known wound, in the two
// absences it produced — a gate with nobody to ask, and a model the conversation
// could not move. PRD §5 says plainly: do not mint a third. So this is the one,
// and it is the one for both consumers at once.
//
//   - It is the HOST API handed to a JavaScript bundle. goja has no filesystem,
//     no network and no clock unless the host hands them in, so the six methods
//     below are the ENTIRE capability surface of a bundle. There is no seventh
//     way to spend, reach out, or ask.
//   - It is the INTERFACE handed to a Go runner. A subharness compiled into this
//     binary gets the same six doors and no more, which is what makes "the person
//     cannot tell which is which" a structural fact rather than a promise.
//
// EVERY TOKEN AND EVERY DOLLAR FLOWS THROUGH ai() AND tool(). That is what makes
// the ledger complete without the ledger having to be everywhere: a program
// cannot spend any other way, so summing the journal's entries is summing the
// run. And every call here is JOURNALED BEFORE ITS ANSWER RETURNS (journal.go) —
// an implementation that answered first and wrote afterwards would leave a run
// killed mid-call with no record of the call that killed it.
//
// The bodies behind these doors are the runtime lane's. [UnwiredEnv] below is
// what a caller that has not wired one yet hands over, and it is honest about
// having nothing behind it rather than pretending to work.
type Env interface {
	// AI is one model call. promptRef NAMES A PROMPT ASSET IN THE BUNDLE and is
	// never an inline string: prompts are files under prompts/ so they can be
	// diffed, reviewed, revised into a new version, and — later — carry their
	// own accepted-output cache per call site. A program that could inline its
	// prompt would put the one thing worth improving somewhere nothing can
	// improve it.
	AI(ctx context.Context, promptRef string, input any, opts AIOptions) (Answer, error)

	// Tool calls one belt tool, filtered by the manifest's whitelist, through
	// the same consent doors any tool call goes through. A tool not on the
	// whitelist is refused here; a tool not on the session's belt is absent, and
	// the two are told apart in the error because they are different facts about
	// what the person can fix.
	Tool(ctx context.Context, name string, args map[string]any) (ToolResult, error)

	// Ask puts a question to the person and waits for the answer. WHAT IT MEANS
	// WITH NOBODY THERE IS DECLARED PER GATE, never assumed: a headless run
	// either takes the answer the manifest defaulted or stops incomplete and
	// says which question it stopped at. The old bridge's auto-approving gate
	// (internal/subharness/exec_model.go) is the anti-pattern this signature
	// exists to make unnecessary — it was kept honest only by writing "nobody was
	// there to ask, so it carried on" into the trail afterwards.
	Ask(ctx context.Context, question string, opts AskOptions) (AskAnswer, error)

	// Remember keeps one note in this subharness's OWN memory — its file in its
	// own bundle, not the session's. A program that has learned that this
	// company's brief always arrives as a PDF has learned something about its
	// domain and nothing about this conversation.
	Remember(ctx context.Context, note string) error

	// Recall reads that memory back. An empty query is everything it has kept.
	Recall(ctx context.Context, query string) ([]Note, error)

	// Log raises one visible progress row into the run's journal, in the
	// program's own words. IT IS THE ONE DOOR TO A PERSON'S ATTENTION that costs
	// nothing, and the vocabulary law reaches it: work is running, finishing,
	// done, incomplete, or needs your look.
	Log(ctx context.Context, status string) error
}

// AIOptions is what one model call may ask for beyond its prompt and its input.
type AIOptions struct {
	// Schema constrains the answer. When it is set the answer comes back in
	// [Answer.JSON] as well as in [Answer.Text], and a model that could not
	// produce the shape is an error rather than a text answer quietly standing
	// in for a structured one.
	Schema Schema
	// Effort is the reasoning knob, in the provider package's own vocabulary
	// rather than a second spelling of it. The zero value is [provider.EffortNone]
	// — send nothing and let the model use its own default — which is not the
	// same request as [provider.EffortOff].
	Effort provider.Effort
}

// Answer is what one model call produced.
type Answer struct {
	Text string
	// JSON is the structured answer, filled only when [AIOptions.Schema] asked
	// for one. Nil is "nobody asked", never "the model refused" — a refusal is
	// an error.
	JSON json.RawMessage
	// Spend is what this one call cost, in the ledger's own figures. It is
	// returned rather than only journaled so a runner can reason about its own
	// budget without reading back what it just wrote.
	Spend Spend
}

// ToolResult is what one belt tool produced. It is text plus, where the tool
// speaks JSON, the same answer structured — the belt's tools mostly answer in
// prose, and a result type that demanded JSON of all of them would make every
// caller invent a wrapper.
type ToolResult struct {
	Text string
	JSON json.RawMessage
	// Spend is what the call cost where a tool spends model tokens of its own.
	// Most tools spend nothing and leave it zero.
	Spend Spend
}

// AskOptions is what a question may offer beyond a blank line.
type AskOptions struct {
	// Options are the answers this question offers, if it offers a set. Empty is
	// a free-text question. A surface draws these as chips; a headless run
	// matches a manifest default against them.
	Options []string
	// Default is what an unattended run answers with. EMPTY IS NOT A YES: a
	// question with no default, asked where nobody is, stops the run incomplete
	// at that question rather than choosing for the person.
	Default string
}

// AskAnswer is what came back.
//
// THE THIRD ANSWER IS THE POINT, and it is carried over from the gate the old
// system got right (internal/subharness's GateAnswer): a person watching a
// program they wrote go slightly wrong does not want to kill it and does not
// want to wave it through — they want to take it from here. So Taking over ends
// the run where it stands, with its journal complete, and what they typed
// becomes the run's answer for whoever picks it up.
type AskAnswer struct {
	// Text is what they said — the chosen option, or the words they typed.
	Text string
	// TakingOver means the person is continuing by hand. The run ends as
	// incomplete with their note as its report, whatever Text says.
	TakingOver bool
	// Unanswered means nobody was there and no default was declared. A program
	// that reads this stops; it does not guess.
	Unanswered bool
}

// Note is one thing a subharness has remembered about its own domain.
type Note struct {
	Text string
	// At is when it was kept, as an RFC 3339 string rather than a time, because
	// this is what a bundle's memory.md carries and a program reads it as text.
	At string
}

// ErrNotWired is what every door of [UnwiredEnv] answers. It is a typed error so
// a runner can tell "this build has nothing behind that door" apart from "the
// call was made and failed", which are opposite facts about whose problem it is.
var ErrNotWired = errors.New("nothing is wired behind this door yet")

// NotWired names which door was unwired. Its message is developer-facing and
// says so plainly — a person never sees one of these, because a capability that
// cannot work is absent from the surface rather than present and failing.
type NotWired struct{ Call string }

func (n *NotWired) Error() string {
	return fmt.Sprintf("nothing is wired behind %s() yet", n.Call)
}

func (n *NotWired) Unwrap() error { return ErrNotWired }

// UnwiredEnv is an Env with nothing behind it. Every door answers [NotWired] for
// its own name.
//
// IT IS SCAFFOLDING AND IT SAYS SO. The lanes building the runtime, the store
// and the surfaces all compile against [Env] before any of them has a working
// host, and a shared honest stub is what lets them do that without three private
// fakes that drift. It is NOT the "absent, not broken" pattern reaching a
// person: a door that hands a subharness one of these has no subharnesses to
// offer, so nothing about them is drawn at all.
type UnwiredEnv struct{}

func (UnwiredEnv) AI(context.Context, string, any, AIOptions) (Answer, error) {
	return Answer{}, &NotWired{Call: CallAI}
}

func (UnwiredEnv) Tool(context.Context, string, map[string]any) (ToolResult, error) {
	return ToolResult{}, &NotWired{Call: CallTool}
}

func (UnwiredEnv) Ask(context.Context, string, AskOptions) (AskAnswer, error) {
	return AskAnswer{}, &NotWired{Call: CallAsk}
}

func (UnwiredEnv) Remember(context.Context, string) error { return &NotWired{Call: CallRemember} }

func (UnwiredEnv) Recall(context.Context, string) ([]Note, error) {
	return nil, &NotWired{Call: CallRecall}
}

func (UnwiredEnv) Log(context.Context, string) error { return &NotWired{Call: CallLog} }
