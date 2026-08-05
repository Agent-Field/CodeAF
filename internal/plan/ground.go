package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// groundPrompt settles what a goal leaves unsaid, before anything is split up.
//
// This exists because of a specific failure. Given "a market entry report for
// three European cities", the planner produced twelve nodes covering Berlin,
// Paris and Madrid — and three more covering Amsterdam. Nobody was wrong. The
// goal never said which three cities, so each independently-expanded subtree
// bound that variable for itself, and the results did not fit together.
//
// It is not a context-size problem and no amount of extra context fixes it. It
// is a free variable with no single definition, which is the same shape as a
// partition key chosen per-mapper, or a value used before it is defined. The
// answer everywhere else is the same: bind it once, upstream, and give every
// consumer the bound value.
//
// The evidence standard is the same shape of variable and is settled the same
// way. A goal asking for "a short written report comparing three vector
// databases" once became a fifteen-node benchmarking project with VM setup
// scripts, because nothing said how much support a claim in this work needs and
// each subtree answered generously. How hard the evidence has to be earned is a
// scope decision, not a finding: it is read off the goal's own words, stated
// once, and inherited, so no subtree can escalate it alone.
//
// The distinction the prompt turns on is the one that matters. Some unknowns
// need to be *consistent* — which cities, which vendors, what period, for whom.
// Those can be settled by fiat, and must be, or agents diverge. Other unknowns
// need to be *correct* — which tool wins, what the numbers say. Those are the
// output of work and cannot be chosen in advance; they have to become real
// dependencies. Settling the first kind upfront and the second kind never is
// what keeps parallel work coherent without making it serial.
const groundPrompt = `You settle what a goal leaves unsaid, before the work is split up.

"Compare the top project management tools" does not say which tools. "A report on
three European cities" does not say which three. "Support the common formats"
does not say which formats. Left open, every agent working on the goal picks its
own answer and the results do not fit together — one profiles Berlin while
another profiles Amsterdam, and neither is wrong.

So decide, once, here. The test is what kind of question it is.

SETTLE it when it decides what to LOOK AT: which specific things are in scope,
how many, over what period, for whom, in what form. Choosing what to examine is
scoping — you pick, and then the work examines what you picked. "Which cities to
study", "which vendors to compare", "which years to cover" all settle here, by
name and by number, even when the goal never said. Where the goal is already
specific, restate it exactly as given. A vague settlement is no settlement.

Leave it OPEN only when it is the ANSWER the work produces: which option wins,
what the numbers turn out to be, what the evidence shows.

The two are easy to confuse and the difference decides everything. "Which cities
to study" is chosen; "which city is best" is found. "Which tools to compare" is
chosen; "which tool to adopt" is found. "Which formats to support" is chosen;
"which format performs best" is found. When a goal says "three European cities"
or "the top project management tools", that is a scoping question wearing the
clothes of a finding — name them. Leaving it open does not make the work more
rigorous, it just means every agent picks its own three and the results do not
compose.

Write every settled point as a decision already made, never as an instruction to
make one. "The three cities are Berlin, Lisbon and Warsaw" is a settlement.
"Choose three European cities" is not — it hands back the very ambiguity you
were asked to remove, and every agent will resolve it differently again. If a
point does not contain the actual names, numbers, or values, it is not settled,
and it does not belong in the list.

Keep both lists short and one line each. At most six settled points. If the goal
is already fully specific, settle nothing and say so with an empty list.

One more thing is settled here, separately: the evidence this goal warrants.

Work can be supported by reading sources and citing them, by running something
and measuring it, or by building something and demonstrating that it works. Each
costs far more than the one before it, and the goal's own words say which it is
asking for. "A short written report comparing three options" warrants reading
and citing — building infrastructure to benchmark them is a different and much
larger goal that nobody asked for. "Show that the new path is faster" warrants
running and measuring. "Ship a working importer" warrants building and
demonstrating.

Write it as one line, as a decision already made: what counts as adequate
support for a claim in this work. It is the ceiling as well as the floor — no
part of the work may quietly buy stronger evidence than this, and none may
settle for weaker. Read the goal, not your ambition for it: the cheapest
standard that actually satisfies what was asked is the correct one.`

var groundSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "settled":  { "type": "array", "items": { "type": "string" } },
    "open":     { "type": "array", "items": { "type": "string" } },
    "evidence": { "type": "string" }
  },
  "required": ["settled", "open", "evidence"],
  "additionalProperties": false
}`)

// Grounding is everything the ground pass binds: the goal's scope variables and
// the standard of evidence the goal warrants. They travel together because they
// are the same kind of decision — settled once, upstream, and inherited by every
// call after it rather than re-answered per subtree.
type Grounding struct {
	Settled  []string
	Open     []string
	Evidence string
}

// Ground resolves the goal's free variables. It runs concurrently with the
// spine — both need only the goal — so it costs no wall clock, and its output
// joins the prefix every later call already shares, so it costs no cache either.
func Ground(ctx context.Context, client Completer, goal string) (Grounding, *ai.Usage, error) {
	messages := []ai.Message{
		systemMessage(groundPrompt),
		userMessage("Goal:\n" + strings.TrimSpace(goal)),
	}
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithSchema(groundSchema))
	if err != nil {
		return Grounding{}, nil, fmt.Errorf("ground: %w", err)
	}
	var decoded struct {
		Settled  []string `json:"settled"`
		Open     []string `json:"open"`
		Evidence string   `json:"evidence"`
	}
	if err := decodeJSON(response.Text(), &decoded); err != nil {
		return Grounding{}, usageOf(response), annotate(fmt.Errorf("ground: %w", err), response)
	}
	return Grounding{
		Settled:  cleanStrings(decoded.Settled),
		Open:     cleanStrings(decoded.Open),
		Evidence: trim(decoded.Evidence),
	}, usageOf(response), nil
}

// context is the frozen preamble every planning call shares: the goal, what has
// been settled about it, and what is still open.
//
// Rendering it in one place is not tidiness. This block is the byte-stable
// prefix behind every fan-out, sizing, binding and briefing call, so a stray
// difference in how one caller assembles it would cost every cache hit behind
// it — and, worse, would let two calls work from subtly different premises,
// which is the failure this whole file exists to prevent.
func (g *Graph) context() string {
	var block strings.Builder
	block.WriteString("Goal:\n")
	block.WriteString(g.Goal)
	if len(g.Settled) > 0 {
		block.WriteString("\n\nSettled for this goal. Use these exactly as written. Never substitute\nyour own choice for one of these, and never leave one of them vague:\n")
		for _, item := range g.Settled {
			fmt.Fprintf(&block, "  - %s\n", item)
		}
	}
	if len(g.Open) > 0 {
		block.WriteString("\nDecided by the work itself, not known yet. Anything that needs one of these\nmust wait for whatever produces it — it cannot assume or invent an answer:\n")
		for _, item := range g.Open {
			fmt.Fprintf(&block, "  - %s\n", item)
		}
	}
	if g.Evidence != "" {
		block.WriteString("\nThe evidence this goal warrants. It is the ceiling as well as the floor: no\npart of the work may buy stronger evidence than this, and none may settle for\nweaker:\n")
		fmt.Fprintf(&block, "  - %s\n", g.Evidence)
	}
	return block.String()
}
