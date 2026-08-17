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
// steps turned into checklists for SPEED, COST and QUALITY — which revises it. The
// delta printed afterwards is COMPUTED from the two pages, not taken from the
// critic's account of itself, because a model's report of what it changed is one
// more thing worth verifying.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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

// revision is stage 1.5's envelope: the three critique passes, the critic's own
// call count, what it says it changed, and the whole revised design.
//
// The findings are carried separately from `changed` on purpose. A finding the
// critic named and then did NOT act on is the most interesting line in the report
// — it is either restraint (the finding was real but not worth the change) or a
// critic that wrote a paragraph and did nothing, and the review print puts both in
// front of a person rather than deciding which it was.
type revision struct {
	Speed   []string `json:"speed"`
	Cost    []string `json:"cost"`
	Quality []string `json:"quality"`
	Calls   struct {
		Draft   int `json:"draft"`
		Revised int `json:"revised"`
	} `json:"calls"`
	Changed       []string        `json:"changed"`
	Cues          []string        `json:"cues"`
	Justification string          `json:"justification"`
	Harness       json.RawMessage `json:"harness"`
}

// design is the revision seen as one, so the same decode/validate/lint path serves
// both stages.
func (r revision) design() design {
	return design{Cues: r.Cues, Justification: r.Justification, Harness: r.Harness}
}

func (r revision) findings() int { return len(r.Speed) + len(r.Cost) + len(r.Quality) }

// designerSystem is the brief: the meta-guide, with this build's machinery filled
// into it. It is one string built once so the whole of what the model was told can
// be printed beside what it produced.
func designerSystem(tools []toolSpec) (string, error) {
	return prompts.Render(prompts.Designer, machinery(tools))
}

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

// designOnce asks for one design and returns it decoded, or the error the model is
// going to be shown.
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
	h, err := accept(envelope)
	return envelope, h, raw, err
}

// reviewOnce is stage 1.5's turn: one critique-and-revision, decoded, validated
// and linted on exactly the terms the draft was. A revision that does not hold up
// is refused the same way a draft is, and for the same reason — a critic that
// hands back a broken page has not improved anything.
func reviewOnce(ctx context.Context, chat *chatClient, history []message, maxTokens int, temperature float64) (revision, subharness.Harness, string, error) {
	out, err := chat.complete(ctx, chatRequest{
		Messages:    history,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	})
	if err != nil {
		return revision{}, subharness.Harness{}, "", err
	}
	raw := out.Text
	var envelope revision
	decoder := json.NewDecoder(strings.NewReader(stripFence(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return revision{}, subharness.Harness{}, raw, fmt.Errorf("your reply is not the review envelope: %w. Reply with ONE JSON object with exactly the keys speed, cost, quality, calls, changed, cues, justification, harness", err)
	}
	if len(envelope.Harness) == 0 {
		return envelope, subharness.Harness{}, raw, fmt.Errorf("the review carries no harness: the WHOLE page goes in `harness` every time, changed or not")
	}
	h, err := accept(envelope.design())
	return envelope, h, raw, err
}

// accept is the gauntlet an envelope passes at either stage: the page decoded by
// the package that owns the format with unknown fields refused, validated, then
// linted for the law this rig has that Validate does not.
func accept(envelope design) (subharness.Harness, error) {
	if len(envelope.Harness) == 0 {
		return subharness.Harness{}, fmt.Errorf("the envelope has no harness in it")
	}
	h, err := subharness.Decode(envelope.Harness)
	if err != nil {
		return subharness.Harness{}, err
	}
	if err := subharness.Validate(h); err != nil {
		return h, err
	}
	return h, lint(h, envelope, availableTools)
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

// ── the computed delta ──────────────────────────────────────────────────────

// delta is what actually changed between the draft and the revision, read off the
// two pages. Nothing here asks the critic what it did: `changed` is its account,
// this is the record, and the review print shows them together because the gap
// between them is the measure of whether the second pass is worth its money.
type delta struct {
	AddedNodes   []string
	RemovedNodes []string
	Rekinded     []string
	Rebriefed    []string
	AddedEdges   []string
	RemovedEdges []string
	Verify       string
	Dyn          string
	Whitelist    string
	Calls        string
	Identity     string
}

func (d delta) empty() bool {
	return len(d.AddedNodes) == 0 && len(d.RemovedNodes) == 0 && len(d.Rekinded) == 0 &&
		len(d.Rebriefed) == 0 && len(d.AddedEdges) == 0 && len(d.RemovedEdges) == 0 &&
		d.Verify == "" && d.Dyn == "" && d.Whitelist == "" && d.Calls == "" && d.Identity == ""
}

// diff reads one page against the other.
func diff(before, after subharness.Harness) delta {
	var d delta

	from := nodesById(before)
	to := nodesById(after)
	for id, node := range to {
		was, existed := from[id]
		switch {
		case !existed:
			d.AddedNodes = append(d.AddedNodes, fmt.Sprintf("%s (%s)", id, node.Kind))
		case was.Kind != node.Kind:
			d.Rekinded = append(d.Rekinded, fmt.Sprintf("%s: %s → %s", id, was.Kind, node.Kind))
		default:
			if changed := fieldsChanged(was, node); len(changed) > 0 {
				d.Rebriefed = append(d.Rebriefed, fmt.Sprintf("%s: %s", id, strings.Join(changed, ", ")))
			}
		}
	}
	for id, node := range from {
		if _, kept := to[id]; !kept {
			d.RemovedNodes = append(d.RemovedNodes, fmt.Sprintf("%s (%s)", id, node.Kind))
		}
	}

	was, now := edgeSet(before), edgeSet(after)
	for edge := range now {
		if !was[edge] {
			d.AddedEdges = append(d.AddedEdges, edge)
		}
	}
	for edge := range was {
		if !now[edge] {
			d.RemovedEdges = append(d.RemovedEdges, edge)
		}
	}

	if before.Verify.Ladder != after.Verify.Ladder {
		d.Verify = fmt.Sprintf("%s → %s (%s)", rungOr(before.Verify.Ladder), rungOr(after.Verify.Ladder),
			direction(subharness.VerifyRung(after.Verify.Ladder)-subharness.VerifyRung(before.Verify.Ladder)))
	}
	if before.Dyn.Ladder != after.Dyn.Ladder || before.Dyn.Cap != after.Dyn.Cap {
		d.Dyn = fmt.Sprintf("%s cap %d → %s cap %d", rungOr(before.Dyn.Ladder), before.Dyn.Cap, rungOr(after.Dyn.Ladder), after.Dyn.Cap)
	}
	if a, b := strings.Join(before.Whitelist, ","), strings.Join(after.Whitelist, ","); a != b {
		d.Whitelist = fmt.Sprintf("[%s] → [%s]", a, b)
	}
	if a, b := estimateCalls(before), estimateCalls(after); a != b {
		d.Calls = fmt.Sprintf("%d → %d model calls at most", a, b)
	}
	if before.Id.Name != after.Id.Name || before.Id.Desc != after.Id.Desc {
		d.Identity = fmt.Sprintf("%s — %q → %s — %q", before.Id.Name, before.Id.Desc, after.Id.Name, after.Id.Desc)
	}

	sort.Strings(d.AddedNodes)
	sort.Strings(d.RemovedNodes)
	sort.Strings(d.Rekinded)
	sort.Strings(d.Rebriefed)
	sort.Strings(d.AddedEdges)
	sort.Strings(d.RemovedEdges)
	return d
}

func nodesById(h subharness.Harness) map[string]subharness.Node {
	out := make(map[string]subharness.Node, len(h.Program.Nodes))
	for _, node := range h.Program.Nodes {
		out[node.Id] = node
	}
	return out
}

func edgeSet(h subharness.Harness) map[string]bool {
	out := map[string]bool{}
	for _, edge := range h.Program.Edges {
		out[edge.From()+"→"+edge.To()] = true
	}
	return out
}

// fieldsChanged names the fields that differ, and says HOW for the one field
// whose length is the cost — a brief is paid for on every turn of the node that
// holds it, so "brief 340→180 bytes" is the finding and the new text is not.
func fieldsChanged(before, after subharness.Node) []string {
	names := map[string]bool{}
	for name := range before.Fields {
		names[name] = true
	}
	for name := range after.Fields {
		names[name] = true
	}
	var out []string
	for name := range names {
		was, now := before.Fields.Get(name), after.Fields.Get(name)
		if was == now {
			continue
		}
		switch {
		case was == "":
			out = append(out, fmt.Sprintf("+%s=%q", name, clip(oneLine(now), 60)))
		case now == "":
			out = append(out, fmt.Sprintf("-%s", name))
		case len(was) > 80 || len(now) > 80:
			out = append(out, fmt.Sprintf("%s reworded %d→%d bytes", name, len(was), len(now)))
		default:
			out = append(out, fmt.Sprintf("%s %q→%q", name, clip(oneLine(was), 40), clip(oneLine(now), 40)))
		}
	}
	sort.Strings(out)
	return out
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

func rungOr(word string) string { return firstOr(word, "(none)") }

func direction(by int) string {
	switch {
	case by > 0:
		return "raised"
	case by < 0:
		return "lowered"
	default:
		return "changed"
	}
}

// stripFence takes the JSON out of a reply that arrived wearing a code fence. The
// instruction says not to use one; a model that does anyway has not made a design
// error, and spending a retry on punctuation would measure the wrong thing.
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
