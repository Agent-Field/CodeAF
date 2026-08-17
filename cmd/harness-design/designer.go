package main

// STAGE 1 AND STAGE 1.5: the designer's brief, the critic that reads what it
// wrote, and the loop that holds both to the law.
//
// The whole question this rig asks is whether a model handed a plain goal can
// ARCHITECT a sub-harness — pick a topology, pick a verification rung it can
// actually keep, pick a dynamism budget it actually needs — rather than fill in a
// template somebody else already shaped. The brief that asks that question is no
// longer in this file. It is a document, internal/subharness/prompts/designer.md,
// because it is a META-GUIDE: it teaches a derivation procedure in paragraphs, and
// a paragraph held in a Fprintf loses the shape of its own argument to quoting.
//
// The parts that are DERIVED are still derived — every cap, both ladders and the
// tool belt are read out of internal/subharness and filled into the guide's
// placeholders, and a placeholder nobody filled is a load-time error rather than a
// stray brace in a prompt. A guide that has drifted from the package it describes
// still cannot reach a model.
//
// STAGE 1.5 is the second pass. A first draft is written by a model that has just
// finished reading three thousand words of law and is, predictably, still in the
// mood that produced them: it over-builds, it leaves conditions as sentences that
// cost a model call each, and it draws independent work in a line. So the draft is
// handed back to the same guide wearing PART FOUR — a critic with the guide's own
// steps turned into checklists for SPEED, COST and QUALITY.
//
// THE CRITIC DOES NOT HAND BACK A PAGE. It hands back a PATCH — the ops in
// internal/subharness/ops.go — which is applied to the original parsed harness.
// The reason is measured rather than aesthetic: a critic asked to re-emit four
// thousand bytes retypes them, and a retyped page comes back with "whishpeR.cpp"
// where the draft said whisper.cpp. That is a page made worse by the pass that was
// supposed to improve it, in the one place nobody reviewed. A patch cannot make
// that mistake, because text the critic did not name is never in its reply at all.
//
// The delta printed afterwards is the ops list itself. Nothing is diffed and
// nothing is inferred: with a patch, what the critic did and what it says it did
// are the same object.
//
// The other half of holding a model to JSON is [jsonReply]: the reply climbs
// subharness.Salvage's ladder, and a reply that still will not parse buys ONE
// repair turn — its own text and the parser's exact complaint — before a full
// retry is counted. A design refused for typographic quotes is a good design lost
// to punctuation, and the whole guide is re-read to fix a delimiter.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/subharness/prompts"
)

// design is what the model must produce: a harness page, plus the two things a
// page has nowhere to put.
//
// CUES AND JUSTIFICATION LIVE OUTSIDE THE PAGE, and that is a finding rather than
// a convenience. `subharness.Entry` (detection) carries Name, Description and
// Cues; `subharness.Harness` (the page) carries Id{name,desc} and no cues at all,
// and Decode refuses unknown fields — so a designer that wrote its trigger
// vocabulary into the page would produce a page that cannot be read back. The
// envelope is the seam until the package grows one.
type design struct {
	Cues          []string        `json:"cues"`
	Justification string          `json:"justification"`
	Harness       json.RawMessage `json:"harness"`
}

// finding is one thing the critic found, and which of the three passes found it.
// The pass is carried as a field rather than as three arrays because it is a
// label on a sentence, and a critic that has to fill three arrays fills all three.
type finding struct {
	Pass string `json:"pass"`
	Text string `json:"text"`
}

// revision is stage 1.5's envelope: what the critic found, the patch it wrote,
// and its own count of what the two versions cost.
//
// THERE IS NO `harness` FIELD AND NO `changed` FIELD, and both absences are the
// point. The ops ARE what changed — a separate account of it would be a second,
// unverifiable story about the same work — and the page is never re-emitted, so
// there is nothing for a transcription slip to damage.
//
// `cues` and `justification` are OPTIONAL for the same reason: text the critic
// does not intend to change must not appear in its reply, so an omitted field
// means "the draft's, unchanged" rather than "empty".
type revision struct {
	Findings []finding       `json:"findings"`
	Ops      []subharness.Op `json:"ops"`
	Calls    struct {
		Draft   int `json:"draft"`
		Revised int `json:"revised"`
	} `json:"calls"`
	Cues          []string `json:"cues,omitempty"`
	Justification string   `json:"justification,omitempty"`
}

// design is the revision seen as one, against the draft it patched: whatever the
// critic did not restate is the draft's own. The harness is carried separately
// because a patched page is not a page the critic wrote.
func (r revision) design(draft design) design {
	out := design{Cues: draft.Cues, Justification: draft.Justification}
	if len(r.Cues) > 0 {
		out.Cues = r.Cues
	}
	if strings.TrimSpace(r.Justification) != "" {
		out.Justification = r.Justification
	}
	return out
}

func (r revision) findings() int { return len(r.Findings) }

// pass is the findings one of the guide's three passes turned up, for printing.
type pass struct {
	Name  string
	Found []string
}

// byPass groups the findings for printing, keeping the guide's own three passes
// in the guide's own order and putting anything else at the end under its own
// label rather than dropping it. A pass with nothing under it is KEPT, because
// "speed: nothing found" is a review result and a missing line is not.
func (r revision) byPass() []pass {
	order := []string{"speed", "cost", "quality"}
	found := map[string][]string{}
	for _, one := range r.Findings {
		name := strings.ToLower(strings.TrimSpace(one.Pass))
		if name == "" {
			name = "unlabelled"
		}
		if !contains(order, name) {
			order = append(order, name)
		}
		found[name] = append(found[name], one.Text)
	}
	out := make([]pass, 0, len(order))
	for _, name := range order {
		out = append(out, pass{Name: name, Found: found[name]})
	}
	return out
}

func contains(words []string, word string) bool {
	for _, one := range words {
		if one == word {
			return true
		}
	}
	return false
}

// designerSystem is the brief: the meta-guide, with this build's machinery filled
// into it. It is one string built once so the whole of what the model was told can
// be printed beside what it produced.
func designerSystem(tools []toolSpec) (string, error) {
	guide, err := prompts.Render(prompts.Designer, machinery(tools))
	if err != nil {
		return "", err
	}
	return guide + asciiRule, nil
}

// asciiRule is the one line of output contract this rig adds to the guide it was
// handed. It lives here rather than in the guide because the guide is a document
// about DESIGN and this is a fact about the transport: a page whose delimiters
// came out as typographic quotes is a good design that will not parse, and the
// cheapest place to fix that is before it is written.
//
// It draws the line where the salvage ladder draws it — syntax is ASCII, prose is
// the writer's — so a designer is never told to flatten an em-dash out of a brief
// in order to be read.
const asciiRule = "\n" + `
One rule about the characters, not the design: JSON DELIMITERS AND SYNTAX ARE
ASCII. The quotes around every key and every string value are " (U+0022) — never
“ ” ‘ ’ — and so are the braces, brackets, colons and commas. Prose may use any
character INSIDE a string value: an em-dash in a brief is content and stays.
`

// reviewSystem is stage 1.5's brief. It is the WHOLE designer guide plus PART
// FOUR, because a critic that cannot see the law it is judging against would be
// reviewing its recollection of it — and because the checklists in PART FOUR are
// the guide's own steps, which only mean anything beside the steps.
func reviewSystem(tools []toolSpec) (string, error) {
	guide, err := designerSystem(tools)
	if err != nil {
		return "", err
	}
	return guide + "\n" + prompts.Reviewer, nil
}

// machinery is every value the guide leaves a hole for. It is the one place this
// binary's numbers meet that document, and prompts.Render refuses a hole nobody
// filled and a value nothing reads — so this map and that guide cannot drift apart
// without the next run saying so.
func machinery(tools []toolSpec) map[string]string {
	belt := make([]string, 0, len(tools))
	for _, tool := range tools {
		belt = append(belt, fmt.Sprintf("%-6s %s", tool.name, tool.about))
	}
	return map[string]string{
		"max_turns":      strconv.Itoa(subharness.MaxTurns),
		"max_rounds":     strconv.Itoa(subharness.MaxRounds),
		"default_rounds": strconv.Itoa(subharness.DefaultRounds),
		"max_width":      strconv.Itoa(subharness.MaxWidth),
		"max_nodes":      strconv.Itoa(subharness.MaxNodes),
		"max_id_bytes":   strconv.Itoa(subharness.MaxIdBytes),
		"max_dyn_cap":    strconv.Itoa(subharness.MaxDynCap),
		"verify_ladder":  strings.Join(subharness.VerifyLadder(), " < "),
		"dyn_ladder":     strings.Join(subharness.DynLadder(), " < "),
		"tools":          strings.Join(belt, "\n"),
	}
}

// attempt is what one turn of the model cost, told separately from what it
// produced. A run that is measuring whether a model can architect a harness has
// to be able to say "this reply needed sanitising and a repair turn" — otherwise
// a model quietly emitting typographic quotes looks exactly like a model that
// never had a problem.
type attempt struct {
	raw      string // the last text the model produced, for the retry history
	rung     string // the salvage rung that made it JSON
	repaired bool   // a repair turn was spent before it parsed
	// What the turn cost, the repair turn included. The design stages read the
	// client's running ledger instead; the adaptive run cannot, because it has
	// several nodes in flight and a delta would price the wrong call.
	spent  float64
	tokens int
}

// cost is the phrase a stage line adds when the reply was not clean. A clean
// reply says nothing, so the log stays quiet until there is something to say.
func (a attempt) cost() string {
	var said []string
	if a.rung != "" && a.rung != subharness.SalvageStrict {
		said = append(said, "salvaged at "+a.rung)
	}
	if a.repaired {
		said = append(said, "after a repair turn")
	}
	if len(said) == 0 {
		return ""
	}
	return " · " + strings.Join(said, " ")
}

// jsonReply is one model turn whose reply has to be JSON, with the ONE repair
// turn this pipeline allows before a full retry is counted.
//
// The order matters and it is cheapest-first. Salvage costs nothing and fixes the
// fence, the prose, the smart quotes and the trailing comma. What it cannot fix is
// a reply that was cut off mid-object or one whose structure is genuinely wrong,
// and for those a REPAIR turn is still an order of magnitude cheaper than a
// retry: the model is shown its own output and the parser's exact complaint, and
// asked for the corrected JSON alone — no guide, no goal, no re-derivation. Only
// when that fails does the caller spend a real attempt.
func jsonReply(ctx context.Context, chat *chatClient, history []message, maxTokens int, temperature float64) (subharness.Salvaged, attempt, error) {
	out, err := chat.complete(ctx, chatRequest{
		Messages:    history,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	})
	if err != nil {
		return subharness.Salvaged{}, attempt{}, err
	}
	at := attempt{raw: out.Text, spent: out.Cost, tokens: out.Tokens}
	salvaged, err := subharness.SalvageDetail(out.Text)
	if err == nil {
		at.rung = salvaged.Rung
		return salvaged, at, nil
	}

	// THE REPAIR TURN CARRIES NO GUIDE. It is not a second attempt at the design —
	// it is a transcription job, and handing it the twenty-five thousand tokens of
	// law that produced the first reply would invite it to reconsider the design
	// while it is meant to be fixing a delimiter. What it gets is its own text,
	// the parser's exact complaint, and one instruction.
	at.repaired = true
	repair := []message{
		{Role: "system", Content: "You repair malformed JSON and do nothing else. You never change content, never add a field, never drop one, and never explain. Your whole reply is one JSON value."},
		{Role: "user", Content: "This was meant to be one JSON object:\n\n" + out.Text +
			"\n\nIt did not parse. " + err.Error() +
			"\n\nReply with ONLY the corrected JSON — the same content, nothing added, nothing dropped, no prose, no code fence. " +
			"JSON delimiters and syntax are ASCII: every key and string value is wrapped in \" (U+0022). Prose inside a string value stays exactly as it is."},
	}
	// The same budget: the repair is a whole reply, not a fragment, and a repair
	// turn that runs out of room has failed for the reason it was called.
	second, err := chat.complete(ctx, chatRequest{
		Messages:    repair,
		MaxTokens:   maxTokens,
		Temperature: 0,
	})
	at.spent += second.Cost
	at.tokens += second.Tokens
	if err != nil {
		return subharness.Salvaged{}, at, err
	}
	at.raw = second.Text
	salvaged, err = subharness.SalvageDetail(second.Text)
	if err != nil {
		return subharness.Salvaged{}, at, fmt.Errorf("neither the reply nor its repair parsed: %w", err)
	}
	at.rung = salvaged.Rung
	return salvaged, at, nil
}

// designOnce asks for one design and returns it decoded, or the error the model is
// going to be shown.
func designOnce(ctx context.Context, chat *chatClient, history []message, maxTokens int, temperature float64) (design, subharness.Harness, attempt, error) {
	salvaged, at, err := jsonReply(ctx, chat, history, maxTokens, temperature)
	if err != nil {
		return design{}, subharness.Harness{}, at, err
	}
	var envelope design
	if err := strict(salvaged.JSON, &envelope); err != nil {
		return design{}, subharness.Harness{}, at, fmt.Errorf("your reply is not the envelope: %w. Reply with ONE JSON object with exactly the keys cues, justification, harness", err)
	}
	h, err := accept(envelope)
	return envelope, h, at, err
}

// reviewOnce is stage 1.5's turn: one critique, one patch, applied to the DRAFT
// the critic was reading and held to exactly the law the draft passed.
//
// The op results come back beside the harness because a skipped op is a finding
// about the review itself, and this rig prints it rather than quietly landing
// eleven of twelve edits.
func reviewOnce(ctx context.Context, chat *chatClient, history []message, maxTokens int, temperature float64, draft design, draftHarness subharness.Harness) (revision, subharness.Harness, []subharness.OpResult, attempt, error) {
	salvaged, at, err := jsonReply(ctx, chat, history, maxTokens, temperature)
	if err != nil {
		return revision{}, subharness.Harness{}, nil, at, err
	}
	var envelope revision
	if err := strict(salvaged.JSON, &envelope); err != nil {
		return revision{}, subharness.Harness{}, nil, at, fmt.Errorf("your reply is not the review envelope: %w. Reply with ONE JSON object with exactly the keys findings, ops, calls, and optionally cues and justification", err)
	}
	revised, results, err := subharness.ApplyReport(draftHarness, envelope.Ops)
	if err == nil {
		err = check(revised, envelope.design(draft))
	} else {
		err = fmt.Errorf("your ops produced a page that is refused: %w", err)
	}
	// A skipped op is not fatal on its own, but if the patch is going back for
	// another turn the critic should be told which of its ops did nothing —
	// otherwise it fixes the validation error and sends the same dead op again.
	if err != nil {
		if skipped := skippedOps(results); skipped != "" {
			err = fmt.Errorf("%w\n\nAlso, these ops did nothing:\n%s", err, skipped)
		}
	}
	return envelope, revised, results, at, err
}

// skippedOps names the ops that did nothing, in the words the package refused
// them with, for a critic that is getting another turn.
func skippedOps(results []subharness.OpResult) string {
	var lines []string
	for _, result := range results {
		if !result.Applied() {
			lines = append(lines, fmt.Sprintf("- %s: %v", result.Op, result.Err))
		}
	}
	return strings.Join(lines, "\n")
}

// strict is how every envelope is read: by the same decoder that reads a page
// from disk, with unknown fields refused. A key nobody asked for is a model
// answering a different question, and a rig that ignored it would be measuring
// the answer to that one.
func strict(data []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

// accept is the gauntlet the DESIGN stage's envelope passes: the page decoded by
// the package that owns the format with unknown fields refused, then checked.
func accept(envelope design) (subharness.Harness, error) {
	if len(envelope.Harness) == 0 {
		return subharness.Harness{}, fmt.Errorf("the envelope has no harness in it")
	}
	h, err := subharness.Decode(envelope.Harness)
	if err != nil {
		return subharness.Harness{}, err
	}
	return h, check(h, envelope)
}

// check is the law both stages are held to: Validate, then the lint this rig has
// that Validate does not.
func check(h subharness.Harness, d design) error {
	if err := subharness.Validate(h); err != nil {
		return err
	}
	return lint(h, d, availableTools)
}

// lint is the law this rig has that Validate does not, and every line of it is a
// gap worth reporting rather than a preference.
//
//   - The WHITELIST is free strings to Validate: a page may whitelist a tool
//     nothing on this machine has, and the failure surfaces as a dead tool.call in
//     the middle of a run instead of at save time.
//   - A `verify` node's `check` is optional to the kind, so a program can hold a
//     check with nothing written in it — a rung claimed by an empty box.
//   - CUES are not part of the page at all (see [design]), so nothing in the
//     package can refuse an entry that could never be reached.
//   - The JUSTIFICATION is not part of the page either, and the guide asks it for
//     five specific things. Nothing can check that it gave all five, but a
//     justification of two sentences did not.
func lint(h subharness.Harness, d design, tools []toolSpec) error {
	known := map[string]bool{}
	for _, tool := range tools {
		known[tool.name] = true
	}
	var problems []string
	for _, tool := range h.Whitelist {
		if !known[tool] {
			problems = append(problems, fmt.Sprintf("the whitelist names %q, which does not exist here (the tools are %s)", tool, toolNames(tools)))
		}
	}
	used := map[string]bool{}
	for _, node := range h.Program.Nodes {
		switch node.Kind {
		case subharness.KindToolCall:
			used[node.Fields.Get("tool")] = true
		case subharness.KindAgentLoop:
			for _, tool := range strings.Split(node.Fields.Get("tools"), ",") {
				if tool = strings.TrimSpace(tool); tool != "" {
					used[tool] = true
				}
			}
		case subharness.KindVerify:
			if node.Fields.Get("check") == "" {
				problems = append(problems, fmt.Sprintf("node %q is a verify with no `check`: a rung is a promise and this one says nothing about what is being checked", node.Id))
			}
		case subharness.KindSubharnessCall:
			problems = append(problems, fmt.Sprintf("node %q calls the harness %q, which is not registered on this machine — this rig runs ONE harness, so do the work here", node.Id, node.Fields.Get("name")))
		}
	}
	for _, tool := range h.Whitelist {
		if known[tool] && !used[tool] {
			problems = append(problems, fmt.Sprintf("the whitelist grants %q and no node uses it", tool))
		}
	}
	if strings.TrimSpace(h.Id.Desc) == "" {
		problems = append(problems, "id.desc is empty, so the offer card would have nothing to say and detection would have nothing to match")
	}
	if len(d.Cues) < 2 {
		problems = append(problems, "fewer than two cues: the entry would be unreachable by anything but its own name")
	}
	switch justification := strings.TrimSpace(d.Justification); {
	case justification == "":
		problems = append(problems, "the justification is empty")
	case len(justification) < 400:
		problems = append(problems, fmt.Sprintf("the justification is %d bytes: it cannot contain all five things PART THREE asks for — the topology, the verify rungs, the dynamism rung and its budget, which pairs of jobs are independent and which are genuinely dependent, and the estimated model calls as a number", len(justification)))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(problems, "; "))
}

// estimateCalls is what this page costs a run, counted the way the guide asks the
// designer to count it: one per agent.loop (times the turns it may take), one per
// verify, one per condition that the condition language will NOT decide, times the
// rounds a loop.until may re-read it. A tool.call, a gate, a split and a join cost
// nothing — no model is asked anything.
//
// It is an upper bound and says so wherever it is printed. A worker that answers
// without reaching for a tool spends one turn of the several it was allowed, and
// nothing static can know which.
func estimateCalls(h subharness.Harness) int {
	total := 0
	for _, node := range h.Program.Nodes {
		switch node.Kind {
		case subharness.KindAgentLoop:
			turns := node.Fields.Int("max_turns", 1)
			if turns < 1 {
				turns = 1
			}
			// A node with no tools has no reason to take a second turn: the loop in
			// execmodel.go returns the moment a turn arrives without a tool call.
			if node.Fields.Get("tools") == "" {
				turns = 1
			}
			total += turns
		case subharness.KindVerify:
			total++
		case subharness.KindBranch:
			if subharness.ValidCondition(node.Fields.Get("when")) != nil {
				total++
			}
		case subharness.KindLoopUntil:
			if subharness.ValidCondition(node.Fields.Get("until")) != nil {
				total += node.Fields.Int("max_rounds", subharness.DefaultRounds)
			}
		}
	}
	return total
}

// entryOf is the detection half of a page, derived. It is a function here rather
// than a method there because the page has no cues to give it — which is the
// finding [design] records.
func entryOf(h subharness.Harness, cues []string) subharness.Entry {
	return subharness.Entry{
		Name:        h.Id.Name,
		Description: h.Id.Desc,
		Cues:        cues,
		Revision:    h.Id.Version,
	}
}
