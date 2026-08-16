package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
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

// groundSchema types the one property the prompt above already states as
// decidable: "If a point does not contain the actual names, numbers, or values,
// it is not settled." A free-text line cannot carry that property, so nothing
// could check it and a tautology — "the vendors are the vendors in scope" —
// entered the graph looking exactly like a binding. Splitting the point into the
// variable and the values it is bound to makes the property structural: an empty
// values array IS an unsettled point, and it is refused rather than rendered.
var groundSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "settled":  { "type": "array", "items": {
      "type": "object",
      "properties": {
        "variable": { "type": "string" },
        "values":   { "type": "array", "items": { "type": "string" } }
      },
      "required": ["variable", "values"],
      "additionalProperties": false
    } },
    "open":     { "type": "array", "items": { "type": "string" } },
    "evidence": { "type": "string" }
  },
  "required": ["settled", "open", "evidence"],
  "additionalProperties": false
}`)

// Settlement is one bound scope variable: the thing the goal left free, and the
// actual names, numbers or values it is now bound to.
//
// The pair is the whole point. The prompt has always demanded that a settled
// point contain real values and has always been free to return a sentence that
// merely sounds like one; the enumeration and the thing enumerated were fused
// into one string, so no reader downstream could tell "the three cities are
// Berlin, Lisbon and Warsaw" from "the cities are the ones in scope". Separated,
// the difference is a slice length.
type Settlement struct {
	// Variable names what the goal left free — "the three cities", "the vendors
	// compared", "the period covered".
	Variable string `json:"variable"`
	// Values are what it is bound to, by name. Empty means nothing was bound,
	// which is not a settlement however it is worded.
	Values []string `json:"values"`
}

// Bound reports a settlement that actually settles something.
func (s Settlement) Bound() bool {
	return strings.TrimSpace(s.Variable) != "" && len(s.Values) > 0
}

// Line is the settlement as one line of a prompt.
//
// It is byte-identical to what the untyped list rendered for a well-formed
// point: the variable, a colon, the values in the order they were bound. A
// settlement decoded from a legacy graph carries its whole sentence in Variable
// and no values, and renders as that sentence unchanged — which is what keeps
// every plan document written before this schema existed rendering the bytes it
// always did.
func (s Settlement) Line() string {
	variable := strings.TrimSpace(s.Variable)
	if len(s.Values) == 0 {
		return variable
	}
	values := strings.Join(s.Values, ", ")
	if variable == "" {
		return values
	}
	return variable + ": " + values
}

// UnmarshalJSON accepts the object this schema now emits and the bare string
// every plan document written before it carries. A graph on disk is a durable
// artifact that is loaded and re-planned long after it was written, so the older
// spelling decodes into the same type rather than failing the load.
func (s *Settlement) UnmarshalJSON(data []byte) error {
	var line string
	if err := json.Unmarshal(data, &line); err == nil {
		s.Variable, s.Values = trim(line), nil
		return nil
	}
	var decoded struct {
		Variable string   `json:"variable"`
		Values   []string `json:"values"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	s.Variable, s.Values = trim(decoded.Variable), cleanStrings(decoded.Values)
	return nil
}

// Grounding is everything the ground pass binds: the goal's scope variables and
// the standard of evidence the goal warrants. They travel together because they
// are the same kind of decision — settled once, upstream, and inherited by every
// call after it rather than re-answered per subtree.
type Grounding struct {
	Settled  []Settlement
	Open     []string
	Evidence string
}

// cleanSettlements drops the points that bind nothing.
//
// A point with no values is the failure this schema was introduced to name: the
// prompt asks for a decision already made, and a variable returned without
// values is the ambiguity handed back. It is not repairable here and it must not
// reach a fan-out, so it is dropped and the count of what was dropped is
// returned for the caller to decide what to do about.
func cleanSettlements(values []Settlement) ([]Settlement, int) {
	kept := make([]Settlement, 0, len(values))
	unbound := 0
	for _, value := range values {
		settlement := Settlement{Variable: trim(value.Variable), Values: cleanStrings(value.Values)}
		if !settlement.Bound() {
			unbound++
			continue
		}
		kept = append(kept, settlement)
	}
	return kept, unbound
}

// SettledLines renders a settlement list the way every prompt in this package
// reads it. It is the one conversion, so a caller that wants the settled points
// as text cannot spell them differently from the preamble every planning call
// shares.
func SettledLines(settled []Settlement) []string {
	lines := make([]string, 0, len(settled))
	for _, settlement := range settled {
		if line := settlement.Line(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// Enumerated reports that some variable was bound to more than one value —
// that the material now carries an enumeration the work is expected to follow.
// It is the reference the fan-out's acceptance check is keyed on: a stage split
// under an enumeration owes one unit per part, and a stage split under none owes
// nothing of the kind.
func Enumerated(settled []Settlement) bool {
	for _, settlement := range settled {
		if len(settlement.Values) > 1 {
			return true
		}
	}
	return false
}

// Ground resolves the goal's free variables. It runs concurrently with the
// spine — both need only the goal — so it costs no wall clock, and its output
// joins the prefix every later call already shares, so it costs no cache either.
func Ground(ctx context.Context, client Completer, goal string) (Grounding, Usage, error) {
	return GroundWith(ctx, client, goal, "", nil, nil)
}

// GroundWith resolves the goal with the workspace it stands on and optional
// folded history. Keeping Ground as the bare wrapper is the compatibility
// boundary: callers with neither send exactly the same prompt bytes they did
// before either existed.
//
// The terrain matters more here than anywhere else in the package. This pass is
// the one that binds a goal's free variables by fiat — which three cities, which
// formats, how many of them — and it was doing so without ever being shown that
// the answer was sitting in the workspace under four file names. A grounding
// that settles "the responses" as three regions when four are on disk is the
// exact failure terrain exists to prevent, and it happens before any graph
// exists, so the terrain is handed in directly rather than read off one.
// The usage is the pass's rather than one response's, because a reply that
// bound nothing buys one free retry and both calls are the plan's to pay for.
func GroundWith(ctx context.Context, client Completer, goal, terrain string, asked []string, recall []store.RecallHit) (Grounding, Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanGround)
	user := goalBlock(strings.TrimSpace(goal), terrain, asked)
	if remembered := store.FormatRecall(recall, 8<<10); remembered != "" {
		user += "\n\n" + remembered
	}
	messages := []ai.Message{
		systemMessage(groundPrompt),
		userMessage(user),
	}
	var decoded groundReply
	var usage Usage
	response, err := structured(ctx, client, messages, groundSchema, &decoded)
	usage.Add(usageOf(response))
	if err != nil {
		return Grounding{}, usage, fmt.Errorf("ground: %w", err)
	}
	settled, unbound := cleanSettlements(decoded.Settled)
	if unbound > 0 {
		// One free retry, on the same bytes. This is the empty-reply retry beside
		// it (see structured) applied to the other failure this pass can have: a
		// reply that parsed and settled nothing. The prompt is not changed and no
		// rule is added — the same question is asked once more, because a point
		// returned without values is a miss rather than a position.
		var again groundReply
		retry, retryErr := structured(ctx, client, messages, groundSchema, &again)
		usage.Add(usageOf(retry))
		if retryErr == nil {
			if retried, stillUnbound := cleanSettlements(again.Settled); stillUnbound < unbound {
				decoded, settled, unbound = again, retried, stillUnbound
			}
		}
	}
	// Grounding does have a wrong answer a checker can name, and this is it. The
	// prompt states the property — a point without actual names, numbers or
	// values is not settled — and the schema above makes it structural, so a
	// reply carrying a variable nobody bound is reported as the semantic failure
	// it is rather than counted as a verified pass. An empty settled list remains
	// a legitimate reading of a goal with no free variables; a list of points that
	// bind nothing is not.
	if unbound > 0 {
		provider.Report(ctx, provider.VerdictSemanticFailure)
	} else {
		provider.Report(ctx, provider.VerdictVerifiedSuccess)
	}
	return Grounding{
		Settled:  settled,
		Open:     cleanStrings(decoded.Open),
		Evidence: trim(decoded.Evidence),
	}, usage, nil
}

// groundReply is the ground pass's wire shape, named because it is decoded
// twice: once, and again on the one free retry a reply that bound nothing buys.
type groundReply struct {
	Settled  []Settlement `json:"settled"`
	Open     []string     `json:"open"`
	Evidence string       `json:"evidence"`
}

// context is the frozen preamble every planning call shares: the goal, the
// workspace it stands on, what has been settled about it, and what is still
// open.
//
// Rendering it in one place is not tidiness. This block is the byte-stable
// prefix behind every fan-out, sizing, binding and briefing call, so a stray
// difference in how one caller assembles it would cost every cache hit behind
// it — and, worse, would let two calls work from subtly different premises,
// which is the failure this whole file exists to prevent.
func (g *Graph) context() string {
	var block strings.Builder
	block.WriteString(goalBlock(g.Goal, g.Terrain, g.Asked))
	if len(g.Settled) > 0 {
		block.WriteString("\n\nSettled for this goal. Use these exactly as written. Never substitute\nyour own choice for one of these, and never leave one of them vague:\n")
		for _, item := range SettledLines(g.Settled) {
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

// goalBlock is how the ask is put to a call that has no graph to render a
// preamble from — grounding, the spine, and the ensemble judgment, all three of
// which run before a graph exists or is worth reading.
//
// It exists so there is exactly one wording. Those three passes decide what the
// goal means, what its stages are, and whether it wants a panel at all, and the
// whole point of terrain is that they stop deciding those things blind. Had each
// assembled its own version of the block, the four calls would have described the
// same workspace four ways, and the prefix every later pass shares would have
// matched none of them.
func goalBlock(goal, terrain string, asked []string) string {
	return "Goal:\n" + goal + terrainBlock(terrain) + askedBlock(asked)
}

// askedBlock renders the separable requests the ask was read as containing, in
// the person's own words.
//
// It is evidence, not a layout. What the call that read the whole ask can say
// is where the person drew their own lines; what it cannot say is what those
// lines mean for the shape of the work — whether each really stands alone, and
// whether one of them is written over what the others produce. Laying the lines
// out flat as if that second question were already answered is precisely what
// went wrong when a second road existed: an assembling request admitted beside
// its own inputs ran against nothing and invented the material it was there to
// read. So the reading arrives here, where every pass that decides shape can
// see it, and none of them is told what to do with it.
//
// The words are verbatim. They are the closest thing this process holds to what
// the person actually said, and a request restated in the planner's voice
// before a worker ever sees it has been paraphrased twice.
//
// Fewer than two requests is the ordinary ask — one thing comes back, however
// large — and writes nothing at all, down to the newline.
func askedBlock(asked []string) string {
	kept := cleanStrings(asked)
	if len(kept) < 2 {
		return ""
	}
	var block strings.Builder
	block.WriteString("\n\nThe ask was read as several separate requests. In the person's own words,\nin the order they were spoken:\n")
	for index, request := range kept {
		fmt.Fprintf(&block, "  %d. %s\n", index+1, request)
	}
	block.WriteString("\nThat is where the person drew their own lines, and it is not a decision about\nthe shape of the plan. Whether each of them really stands alone, whether any\nof them is written over what the others produce and must therefore wait for\nthem, and how the work divides, are yours to read.")
	return block.String()
}

// terrainBlock renders the workspace, or nothing at all.
//
// The nothing is the load-bearing half. An empty terrain writes zero bytes, so a
// run with no workspace — which is most of them — sends the prompt bytes it sent
// before terrain existed, down to the newline.
//
// It sits between the goal and whatever follows because that is the order a
// reader needs: what was asked for, then what is actually lying around, then the
// decisions taken over the two. The lines are indented and otherwise verbatim;
// they were rendered deterministically in one place and this is not the place to
// reinterpret them.
func terrainBlock(terrain string) string {
	if terrain == "" {
		return ""
	}
	var block strings.Builder
	block.WriteString("\n\nThe workspace this run stands on (rendered from the material itself; it may\nbe incomplete, and it is what was there when planning began):\n")
	for index, line := range strings.Split(terrain, "\n") {
		if index > 0 {
			block.WriteString("\n")
		}
		block.WriteString("  " + line)
	}
	return block.String()
}
