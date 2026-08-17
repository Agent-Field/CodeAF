package main

// STAGE 1'S HALF: the designer's brief, and the loop that holds it to the law.
//
// The whole question this rig asks is whether a model handed a plain goal can
// ARCHITECT a sub-harness — pick a topology, pick a verification rung it can
// actually keep, pick a dynamism budget it actually needs — rather than fill in
// a template somebody else already shaped. So the prompt below says what the
// machinery IS and what the law is, and it deliberately does not contain a
// worked example of a program: an example is a menu, and a menu is what gets
// copied instead of thought.
//
// The parts that are DERIVED are derived on purpose — the kind list, both
// ladders and every integer cap are read out of internal/subharness at build
// time, so a prompt that has drifted from the package it describes is a
// compile-time impossibility rather than a thing to notice in a bad run.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// design is what the model must produce: a harness page, plus the two things a
// page has nowhere to put.
//
// CUES AND JUSTIFICATION LIVE OUTSIDE THE PAGE, and that is a finding rather
// than a convenience. `subharness.Entry` (detection) carries Name, Description
// and Cues; `subharness.Harness` (the page) carries Id{name,desc} and no cues at
// all, and Decode refuses unknown fields — so a designer that wrote its trigger
// vocabulary into the page would produce a page that cannot be read back. The
// envelope is the seam until the package grows one.
type design struct {
	Cues          []string        `json:"cues"`
	Justification string          `json:"justification"`
	Harness       json.RawMessage `json:"harness"`
}

// designerSystem is the brief. It is one string built once so the whole of what
// the model was told can be printed beside what it produced.
func designerSystem(tools []toolSpec) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	p("You are a sub-harness ARCHITECT for aforge.")
	p("")
	p("A sub-harness is a durable registry entry: an identity, a PROGRAM that is a DAG")
	p("over a fixed set of node kinds, a tool whitelist, a verification rung, a")
	p("dynamism rung with an integer budget, and tests. The node kinds are machinery")
	p("this binary already has — agent.loop IS the session loop, tool.call IS the tool")
	p("call, human.gate IS the ask a person already answers. You are ARRANGING existing")
	p("machinery. You are not writing code and you cannot add a kind.")
	p("")
	p("── HOW A RUN FLOWS ─────────────────────────────────────────────────────────")
	p("")
	p("The run starts with the person's goal as the state. Every node reads THE")
	p("PREVIOUS STEP'S OUTPUT as its input and leaves its own output as the new state.")
	p("The last node's output is the run's answer, so the last node must be the one")
	p("that produces the thing the person asked for — not a check, not a gate.")
	p("A brief that needs material from two steps back must say so, because it will be")
	p("handed only the step before it.")
	p("")
	p("── THE NODE KINDS AND THEIR FIELDS ─────────────────────────────────────────")
	p("")
	p("Every field value is a STRING on the wire, integers included: \"max_turns\": \"6\".")
	p("An unknown field is an error, not an ignored typo. Fields listed as required")
	p("must be present and non-blank.")
	p("")
	p("agent.loop      a worker's turn, oriented by a brief")
	p("                brief     REQUIRED. What this node is for, in enough words that")
	p("                          somebody who read only this node could do the job.")
	p("                          Carry the goal's own nouns into it.")
	p("                tools     optional, comma-separated, a SUBSET of the whitelist")
	p("                model     optional")
	p("                max_turns optional integer, 1..%d", subharness.MaxTurns)
	p("tool.call       one whitelisted tool with fixed arguments")
	p("                tool      REQUIRED, must be on the whitelist")
	p("                args      optional; the literal {{input}} means the previous")
	p("                          step's output")
	p("verify          a check at a rung of the ladder")
	p("                ladder    optional; empty means the harness's own rung. It may")
	p("                          name a LOWER rung, never a higher one.")
	p("                check     what is being checked, as a sentence a judge can act on")
	p("human.gate      a stop until a person answers")
	p("                ask       REQUIRED, the question")
	p("branch          one successor, chosen at runtime")
	p("                when      REQUIRED, a condition (see below)")
	p("loop.until      the same node again until a condition holds")
	p("                until      REQUIRED, a condition")
	p("                max_rounds optional integer, 1..%d (default %d)", subharness.MaxRounds, subharness.DefaultRounds)
	p("parallel.split  the point where independent lanes open")
	p("                width     REQUIRED integer, 1..%d", subharness.MaxWidth)
	p("                over      optional, what the lanes are over")
	p("parallel.join   the lanes gathered back into one thread")
	p("                mode      optional: all (default, a barrier) | any | first")
	p("subharness.call another registered harness")
	p("                name      REQUIRED, another harness's name")
	p("                version   optional integer; 0/absent means its head")
	p("trigger         what starts a run. Declarative: it does nothing itself.")
	p("                source    REQUIRED: hosted | idle | watch | source.command")
	p("                command   REQUIRED when source is source.command")
	p("                spec      optional, what a watch watches or how long idle is")
	p("                args      optional, a hosted command's allowed-argument list")
	p("")
	p("── THE DAG LAW ─────────────────────────────────────────────────────────────")
	p("")
	p("- Node ids and the harness name are slugs: ^[a-z0-9][a-z0-9_-]*$, at most %d bytes.", subharness.MaxIdBytes)
	p("- Edges are [\"from\",\"to\"] pairs of ids. No self-edge, no duplicate edge.")
	p("- EXACTLY ONE ENTRY: exactly one node with nothing leading to it.")
	p("- No cycles. A loop.until is how a program repeats, NOT a back edge.")
	p("- Every node must be reachable from the entry.")
	p("- At most %d nodes, and far fewer than that is usually the right answer.", subharness.MaxNodes)
	p("- A branch's FIRST successor is the arm taken when `when` holds and the SECOND")
	p("  is the else. A branch with one successor takes it either way, so a branch that")
	p("  means \"otherwise carry on\" must DRAW the otherwise. Three answers means two")
	p("  branches.")
	p("- A parallel.split's successors are its lanes; they converge on a parallel.join.")
	p("  Lanes must be genuinely independent — a lane that needs another lane's output")
	p("  is a sequence somebody drew sideways.")
	p("")
	p("── CONDITIONS (branch `when`, loop.until `until`) ──────────────────────────")
	p("")
	p("A small language decides what it can, deterministically, about ONE thing: what")
	p("the step before produced and whether it succeeded.")
	p("  always | never | ok | failed | empty | nonempty")
	p("  contains <text> | equals <text> | matches <regexp>   (rest of line, verbatim)")
	p("A condition may ALSO be a plain sentence (\"the summary cites every claim\"), which")
	p("is handed to the environment's judgement. Prefer the language when the question")
	p("really is about the last output; use a sentence when it genuinely is not.")
	p("")
	p("── THE TWO LADDERS ─────────────────────────────────────────────────────────")
	p("")
	p("verify.ladder, weakest first: %s", strings.Join(subharness.VerifyLadder(), " < "))
	p("  accept       believe the output")
	p("  schema       it has the shape that was asked for")
	p("  invariants   stated properties hold")
	p("  loop         it was checked and re-worked until the check passed")
	p("  report       it carries its own evidence")
	p("  rederive     the answer was reached a second, independent way and agreed")
	p("  adversarial  something tried to break it and failed")
	p("  human        a person signed it off")
	p("Rules that are ENFORCED: a rung above accept needs at least one verify node in")
	p("the program; the `human` rung needs a human.gate; no verify node may name a rung")
	p("above the harness's own. A rung is a PROMISE about what the output went through,")
	p("so claim the rung the program can actually keep.")
	p("")
	p("dyn.ladder, least autonomous first: %s", strings.Join(subharness.DynLadder(), " < "))
	p("A kind cannot be held below the rung it needs:")
	p("  fixed      agent.loop, tool.call, verify, human.gate, trigger")
	p("  branch     branch, loop.until           — the run picks a path")
	p("  width      parallel.split, parallel.join — the run picks how many")
	p("  recursive  subharness.call              — the run enters another harness")
	p("meta is for a program whose briefs rewrite themselves; selfmod for one that may")
	p("save a new version of itself. Neither unlocks a kind.")
	p("dyn.cap is the WHOLE-RUN budget for deciding: one unit per loop round past the")
	p("first, one per minted lane of width. A `fixed` harness MUST NOT carry a cap (omit")
	p("it or write 0). Anything above fixed MUST carry one, 1..%d. Size it by counting", subharness.MaxDynCap)
	p("what the program can actually spend — a cap of 20 on a program with one two-round")
	p("loop is a number nobody chose.")
	p("")
	p("── THE TOOLS THAT EXIST HERE ───────────────────────────────────────────────")
	p("")
	p("The whitelist may contain ONLY these, spelled exactly. There is no web, no")
	p("filesystem, no shell:")
	for _, tool := range tools {
		p("  %-6s %s", tool.name, tool.about)
	}
	p("An empty whitelist grants nothing, which is the honest answer for a goal whose")
	p("work is all reasoning. Do not whitelist a tool no node uses.")
	p("IF THE GOAL WANTS SOMETHING YOU CANNOT REACH — sources on the live web, a repo to")
	p("read — the harness must do the best honest thing with what it has, and its")
	p("verification rung must not promise what no node here could check.")
	p("")
	p("── DETECTION: name, desc, cues ─────────────────────────────────────────────")
	p("")
	p("A harness is not reached by a slash command. It is reached because somebody said")
	p("what they wanted and a pure, deterministic matcher scored the sentence against")
	p("this entry. So:")
	p("  id.name  the slug it is called")
	p("  id.desc  ONE sentence, in the words a PERSON would use for this, not the words")
	p("           the program uses for itself")
	p("  cues     the trigger vocabulary: 3-8 words and short phrases somebody would")
	p("           actually type when they want this. A multi-word phrase is worth more")
	p("           than a lone word; a lone word that is common English is a false")
	p("           positive waiting to happen.")
	p("A harness described as \"does stuff\" with no cues is never offered.")
	p("")
	p("── HOW TO THINK ────────────────────────────────────────────────────────────")
	p("")
	p("PATTERNS ARE OUTPUTS OF THINKING, NOT A MENU. Do not reach for a shape because it")
	p("is a shape you know. Derive the topology from THIS goal: what does the work")
	p("actually decompose into, what genuinely cannot start until something else has")
	p("finished, what is genuinely independent, and what would somebody be right to")
	p("distrust about the answer.")
	p("")
	p("- DECOMPOSE GRANULARLY. One node, one job. A single agent.loop with a brief that")
	p("  says \"do the whole thing\" is not a harness, it is a prompt.")
	p("- CONTEXTUAL FIDELITY. Every brief carries the goal's own nouns and constraints.")
	p("  A brief that would read the same for a different goal is too vague to run.")
	p("- PARALLEL WHEN INDEPENDENT, sequential when not. Independent lanes are the one")
	p("  reason to spend width.")
	p("- MATCH THE WEIGHT TO THE GOAL. A three-line answer with an adversarial rung and")
	p("  a human gate on it is over-engineered, and over-engineering is a defect: it")
	p("  costs money and it lies about what the answer went through. A contested")
	p("  judgement with one node and `accept` on it is under-engineered.")
	p("- A verify node must have something to verify. Put it after the work, not before.")
	p("")
	p("── OUTPUT ──────────────────────────────────────────────────────────────────")
	p("")
	p("Reply with ONE JSON object and NOTHING else — no prose, no code fence:")
	p("")
	p("{")
	p("  \"cues\": [\"...\"],")
	p("  \"justification\": \"why THIS topology, why THIS verify rung, why THIS dyn budget — a short paragraph, no restating of the goal\",")
	p("  \"harness\": {")
	p("    \"id\": {\"name\": \"slug\", \"desc\": \"one sentence\", \"author\": \"designer\"},")
	p("    \"program\": {")
	p("      \"nodes\": [{\"id\": \"slug\", \"kind\": \"agent.loop\", \"fields\": {\"brief\": \"...\"}}],")
	p("      \"edges\": [[\"from\", \"to\"]]")
	p("    },")
	p("    \"whitelist\": [],")
	p("    \"verify\": {\"ladder\": \"accept\"},")
	p("    \"dyn\": {\"ladder\": \"fixed\"},")
	p("    \"tests\": [{\"name\": \"slug-ish name\", \"input\": \"...\", \"expect\": \"...\"}]")
	p("  }")
	p("}")
	p("")
	p("Omit id.version — the store mints it. No other keys anywhere, at any depth.")
	return b.String()
}

// designOnce asks for one design and returns it decoded, or the error the model
// is going to be shown.
func designOnce(ctx context.Context, chat *chatClient, history []message, maxTokens int, temperature float64) (design, subharness.Harness, string, error) {
	out, err := chat.complete(ctx, chatRequest{
		Messages:    history,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	})
	if err != nil {
		return design{}, subharness.Harness{}, "", err
	}
	raw := out.Text
	var envelope design
	decoder := json.NewDecoder(strings.NewReader(stripFence(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return design{}, subharness.Harness{}, raw, fmt.Errorf("your reply is not the envelope: %w. Reply with ONE JSON object with exactly the keys cues, justification, harness", err)
	}
	if len(envelope.Harness) == 0 {
		return envelope, subharness.Harness{}, raw, fmt.Errorf("the envelope has no harness in it")
	}
	// The page itself is decoded by the package that owns the format, unknown
	// fields refused, so a field the model invented is a retry rather than a
	// silently dropped instruction.
	h, err := subharness.Decode(envelope.Harness)
	if err != nil {
		return envelope, subharness.Harness{}, raw, err
	}
	if err := subharness.Validate(h); err != nil {
		return envelope, h, raw, err
	}
	if err := lint(h, envelope, availableTools); err != nil {
		return envelope, h, raw, err
	}
	return envelope, h, raw, nil
}

// lint is the law this rig has that Validate does not, and every line of it is a
// gap worth reporting rather than a preference.
//
//   - The WHITELIST is free strings to Validate: a page may whitelist a tool
//     nothing on this machine has, and the failure surfaces as a dead tool.call
//     in the middle of a run instead of at save time.
//   - A `verify` node's `check` is optional to the kind, so a program can hold a
//     check with nothing written in it — a rung claimed by an empty box.
//   - CUES are not part of the page at all (see [design]), so nothing in the
//     package can refuse an entry that could never be reached.
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
	if strings.TrimSpace(d.Justification) == "" {
		problems = append(problems, "the justification is empty")
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(problems, "; "))
}

// stripFence takes the JSON out of a reply that arrived wearing a code fence.
// The instruction says not to use one; a model that does anyway has not made a
// design error, and spending a retry on punctuation would measure the wrong
// thing.
func stripFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		text = text[at+1:]
	}
	if at := strings.LastIndex(text, "```"); at >= 0 {
		text = text[:at]
	}
	return strings.TrimSpace(text)
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
