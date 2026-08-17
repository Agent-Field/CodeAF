package subharness

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// THE PREVIEW CARD: the one rendering of a harness, and the thing a person
// actually approves.
//
// A sub-harness is built in conversation, which means the shape arrives as JSON
// a model wrote. Nobody approves JSON. What somebody can approve is a numbered
// list of steps with the branches, the rounds and the checks written INLINE
// where they happen — because the question being asked is not "is this valid",
// it is "is this what I meant", and that question is answered by reading the
// order of events.
//
//	triage-flake · v2 · chase a flaky test to a fix
//
//	1  trigger      watch · bench/nightly.log
//	2  agent.loop   name the test that failed and why · read, grep · 8 turns
//	3  branch
//	   a  contains flaky
//	      1  tool.call   bash · go test -run TestFoo -count 20
//	   else
//	      1  agent.loop  say why it is not flaky
//	4  loop.until   until ok · up to 3
//	   1  agent.loop  fix the test
//	   2  verify      loop · go test ./...
//	5  human.gate   land the fix? · intervene
//
//	tools  read · grep · bash        verify  loop        dynamism  branch (cap 4)
//
// ONE RENDERER, EVERY SURFACE. The chat tool prints these lines into a tool
// result, the TUI panel draws the same lines into a block, and a test asserts
// against them. A second rendering would be a second thing that could disagree
// with the program it claims to describe — and the approval is given against
// what was drawn, not against what was stored.

// cardDetailCap bounds one step's detail. A brief is paragraphs; a card row is a
// row, and the whole brief is one `show` away.
const cardDetailCap = 72

// Card is the harness as a person reads it, with the trailing newline a tool
// result wants and a block does not (use [CardLines] for the block).
func Card(h Harness) string { return strings.Join(CardLines(h), "\n") }

// CardLines is the card, one string per line, unstyled. Nothing here is padded
// to a width: the surfaces that care about width own their own fitting, and a
// renderer that guessed at one would be guessing for the narrowest reader.
func CardLines(h Harness) []string {
	lines := []string{cardHead(h), ""}
	lines = append(lines, stepLines(h.Program, "", 0)...)
	lines = append(lines, "", cardFoot(h))
	if len(h.Tests) > 0 {
		names := make([]string, 0, len(h.Tests))
		for _, test := range h.Tests {
			names = append(names, test.Name)
		}
		lines = append(lines, "tests  "+strings.Join(names, " · "))
	}
	return lines
}

// cardHead is the identity line: what it is called, which version, and what it
// is for.
func cardHead(h Harness) string {
	parts := []string{}
	if name := strings.TrimSpace(h.Name); name != "" {
		parts = append(parts, name)
	} else {
		parts = append(parts, "(unnamed)")
	}
	if h.Version > 0 {
		parts = append(parts, fmt.Sprintf("v%d", h.Version))
	} else {
		// A draft has no version because nothing has accepted it yet. Saying so
		// is the difference between a card somebody is being asked to approve
		// and a card of something already registered.
		parts = append(parts, "draft")
	}
	if note := strings.TrimSpace(h.Description); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, " · ")
}

// cardFoot is the bounds: what it may reach, how hard it checks, how much shape
// it may grow. These three are what a person is really approving — the steps say
// what it does today, and the foot says what it is ALLOWED to do.
func cardFoot(h Harness) string {
	tools := "no tools"
	if len(h.Tools) > 0 {
		tools = strings.Join(h.Tools, " · ")
	}
	dynamism := string(h.Dynamism.Or(DynFixed))
	if h.Cap > 0 {
		dynamism += fmt.Sprintf(" (cap %d)", h.Cap)
	}
	return fmt.Sprintf("tools  %s        verify  %s        dynamism  %s",
		tools, h.Verify.Or(RungAccept), dynamism)
}

// stepLines renders one sequence at one indent. label is what precedes the
// number — empty at the top level, so the top level reads "1", "2", "3".
func stepLines(steps []Node, label string, depth int) []string {
	var lines []string
	pad := strings.Repeat("   ", depth)
	for at, node := range steps {
		number := fmt.Sprintf("%d", at+1)
		if label != "" {
			number = label
		}
		head := fmt.Sprintf("%s%-3s %-14s %s", pad, number, node.Kind, stepDetail(node))
		lines = append(lines, strings.TrimRight(head, " "))
		lines = append(lines, nestedLines(node, depth+1)...)
	}
	return lines
}

// nestedLines draws what a node carries under it: the cases of a branch, the
// body of a loop, the lanes of a split. THE ARMS ARE LABELLED AND THE BODIES ARE
// NOT — an arm is a choice a person has to be able to point at ("it went down
// b"), and a body is simply the next thing that happens.
func nestedLines(node Node, depth int) []string {
	var lines []string
	pad := strings.Repeat("   ", depth)
	for at, one := range node.Cases {
		lines = append(lines, fmt.Sprintf("%s%-3s %s", pad, letter(at), one.When))
		lines = append(lines, stepLines(one.Steps, "", depth+1)...)
	}
	if len(node.Else) > 0 {
		lines = append(lines, pad+"else")
		lines = append(lines, stepLines(node.Else, "", depth+1)...)
	}
	lines = append(lines, stepLines(node.Steps, "", depth)...)
	for _, lane := range node.Lanes {
		lines = append(lines, fmt.Sprintf("%s%-3s %s", pad, "‖", lane.Name))
		lines = append(lines, stepLines(lane.Steps, "", depth+1)...)
	}
	return lines
}

// stepDetail is the right-hand side of one row: what this particular node is
// about, in the fields its kind actually uses.
func stepDetail(node Node) string {
	switch node.Kind {
	case KindAgentLoop:
		parts := []string{clipDetail(firstLine(node.Prompt))}
		if len(node.Tools) > 0 {
			parts = append(parts, strings.Join(node.Tools, ", "))
		}
		if node.Model != "" {
			parts = append(parts, node.Model)
		}
		if node.MaxTurns > 0 {
			parts = append(parts, fmt.Sprintf("%d turns", node.MaxTurns))
		}
		return join(parts, node.Note)

	case KindToolCall:
		parts := []string{node.Tool}
		if args := argWords(node.Args); args != "" {
			parts = append(parts, clipDetail(args))
		}
		return join(parts, node.Note)

	case KindBranch:
		return join(nil, node.Note)

	case KindLoopUntil:
		return join([]string{"until " + node.Until, fmt.Sprintf("up to %d", roundsOf(node))}, node.Note)

	case KindParallelSplit:
		return join([]string{fmt.Sprintf("%d lanes", len(node.Lanes)), "join " + string(node.Join.Or())}, node.Note)

	case KindHumanGate:
		parts := []string{clipDetail(firstLine(node.Prompt))}
		if node.Escalate {
			// The word is the escalation's own (exec.go's [GateAnswer]): this
			// gate offers a person the run itself, not only a yes and a no.
			parts = append(parts, "intervene")
		}
		return join(parts, node.Note)

	case KindVerify:
		parts := []string{string(node.Rung.Or(RungAccept))}
		if check := strings.TrimSpace(node.Check); check != "" {
			parts = append(parts, clipDetail(firstLine(check)))
		}
		return join(parts, node.Note)

	case KindSubharnessCall:
		at := "current"
		if node.CallVersion > 0 {
			at = fmt.Sprintf("v%d", node.CallVersion)
		}
		return join([]string{node.Call, at}, node.Note)

	case KindTrigger:
		parts := []string{string(node.On)}
		if spec := strings.TrimSpace(node.Spec); spec != "" {
			parts = append(parts, clipDetail(spec))
		}
		return join(parts, node.Note)
	}
	return join(nil, node.Note)
}

// ── the run, read back ──────────────────────────────────────────────────────

// RunLines is one trace DAG as a person reads it: the path the run actually
// took, one row per node, with the rounds and the lanes it went through.
//
// It is the card's twin and it is deliberately the same shape — kind on the
// left, detail on the right — because the two are read against each other. The
// question a person opens a trace with is "where did this differ from the card",
// and two renderings with different columns would make them find that out by
// eye.
func RunLines(run Run) []string {
	head := fmt.Sprintf("%s · v%d · %s", run.Harness, run.Version, run.Status)
	if elapsed := run.Elapsed(); elapsed > 0 {
		head += fmt.Sprintf(" · %s", elapsed.Round(time.Millisecond))
	}
	lines := []string{head, ""}
	for _, node := range run.Nodes {
		mark := "✓"
		if !node.OK {
			mark = "✗"
		}
		row := fmt.Sprintf("%s %-16s %-14s", mark, node.ID, node.Kind)
		detail := []string{}
		if node.Lane != "" {
			detail = append(detail, "‖ "+node.Lane)
		}
		if node.Round > 0 {
			detail = append(detail, fmt.Sprintf("round %d", node.Round))
		}
		if node.Answer != "" {
			detail = append(detail, node.Answer)
		}
		if node.Note != "" {
			detail = append(detail, node.Note)
		}
		if node.Error != "" {
			detail = append(detail, node.Error)
		}
		if len(detail) == 0 && node.Output != "" {
			detail = append(detail, clipDetail(firstLine(node.Output)))
		}
		lines = append(lines, strings.TrimRight(row+" "+strings.Join(detail, " · "), " "))
	}
	if run.Error != "" {
		lines = append(lines, "", run.Error)
	}
	return lines
}

// RunCard is [RunLines] as one block of text.
func RunCard(run Run) string { return strings.Join(RunLines(run), "\n") }

func roundsOf(node Node) int {
	if node.Max > 0 {
		return node.Max
	}
	return DefaultRounds
}

// join renders a row's parts with the node's own note last, and drops the
// empties: a row that says "bash · " is a row with a shrug on the end of it.
func join(parts []string, note string) string {
	if note = strings.TrimSpace(note); note != "" {
		parts = append(parts, note)
	}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

// argWords renders a tool call's arguments in a stable order, because a card
// that shuffled its own rows between two draws would be a card nobody could
// diff.
func argWords(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	words := make([]string, 0, len(keys))
	for _, key := range keys {
		words = append(words, fmt.Sprintf("%s=%v", key, args[key]))
	}
	return strings.Join(words, " ")
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return text
}

// clipDetail bounds one detail on a rune boundary.
func clipDetail(text string) string {
	if len(text) <= cardDetailCap {
		return text
	}
	cut := cardDetailCap - len("…")
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut] + "…"
}
