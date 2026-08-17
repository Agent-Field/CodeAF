package subharness

import (
	"fmt"
	"strings"
	"time"
)

// THE PREVIEW CARD: the one rendering of a harness, and the thing a person
// actually approves.
//
// A sub-harness is built in conversation, which means the shape arrives as JSON
// a model wrote. Nobody approves JSON. What somebody can approve is a numbered
// list of steps with the branches, the rounds and the checks written where they
// happen — because the question being asked is not "is this valid", it is "is
// this what I meant", and that question is answered by reading the order of
// events.
//
//	triage-flake · v2 · chase a flaky test to a fix
//
//	1  start     trigger        watch · bench/nightly.log
//	2  name      agent.loop     name the test that failed · read, grep · 8 turns
//	3  pick      branch         when contains flaky
//	   a → rerun
//	   b → explain
//	4  rerun     tool.call      bash · go test -run TestFoo -count 20
//	5  tries     loop.until     until the suite is green · up to 3
//	6  land      human.gate     land the fix?
//
//	tools  read · grep · bash        verify  loop        dynamism  branch (cap 4)
//
// THE STEPS ARE THE RUN'S OWN ORDER, not the file's. A program is a DAG with its
// edges written down (registry.go), and the walk that runs it is a topological
// one (run.go) — so the card is numbered in that same order and a person reading
// it top to bottom has read the run. The ARMS of a branch and the LANES of a
// split are drawn under the node that opens them, labelled, because a choice is
// the one thing in a program somebody has to be able to point at: "it went down
// b" is a sentence about a run, and it needs a b on the card to be about.
//
// ONE RENDERER, EVERY SURFACE. The chat tool prints these lines into a tool
// result, the TUI panel draws the same lines into a block, and a test asserts
// against them. A second rendering would be a second thing that could disagree
// with the program it claims to describe — and the approval is given against
// what was drawn, not against what was stored.

// cardDetailCap bounds one step's detail. A brief is paragraphs; a card row is a
// row, and the whole brief is one `show` away.
const cardDetailCap = 72

// Card is the harness as a person reads it, as one block of text.
func Card(h Harness) string { return strings.Join(CardLines(h), "\n") }

// CardLines is the card, one string per line, unstyled. Nothing here is padded
// to a width: the surfaces that care about width own their own fitting, and a
// renderer that guessed at one would be guessing for the narrowest reader.
func CardLines(h Harness) []string {
	h = h.Normalize()
	lines := []string{cardHead(h), ""}
	lines = append(lines, stepLines(h.Program)...)
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
	parts := []string{"(unnamed)"}
	if name := strings.TrimSpace(h.Id.Name); name != "" {
		parts[0] = name
	}
	if h.Id.Version > 0 {
		parts = append(parts, fmt.Sprintf("v%d", h.Id.Version))
	} else {
		// A draft has no version because nothing has accepted it yet. Saying so
		// is the difference between a card somebody is being asked to approve
		// and a card of something already registered.
		parts = append(parts, "draft")
	}
	if note := strings.TrimSpace(h.Id.Desc); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, " · ")
}

// cardFoot is the bounds: what it may reach, how hard it checks, how much shape
// it may grow. These three are what a person is really approving — the steps say
// what it does today, and the foot says what it is ALLOWED to do.
func cardFoot(h Harness) string {
	tools := "no tools"
	if len(h.Whitelist) > 0 {
		tools = strings.Join(h.Whitelist, " · ")
	}
	dynamism := h.Dyn.Ladder
	if h.Dyn.Cap > 0 {
		dynamism += fmt.Sprintf(" (cap %d)", h.Dyn.Cap)
	}
	return fmt.Sprintf("tools  %s        verify  %s        dynamism  %s",
		tools, h.Verify.Ladder, dynamism)
}

// stepLines renders the program in the order it runs.
//
// A cycle is drawn in file order rather than refused: the card is what a person
// reads to find out a shape is wrong, so it has to survive the shapes Validate
// is about to reject.
func stepLines(p Program) []string {
	order, err := topo(p)
	if err != nil {
		order = make([]string, len(p.Nodes))
		for at, node := range p.Nodes {
			order[at] = node.Id
		}
	}
	var lines []string
	for at, id := range order {
		node, found := p.Node(id)
		if !found {
			continue
		}
		row := fmt.Sprintf("%-3d %-12s %-14s %s", at+1, node.Id, node.Kind, stepDetail(node))
		lines = append(lines, strings.TrimRight(row, " "))
		lines = append(lines, armLines(p, node)...)
	}
	return lines
}

// armLines draws the successors a node CHOOSES between, and nothing else. An
// ordinary node's successor is simply the next row; a branch's and a split's are
// the choice and the fan, and both are facts about this node rather than about
// what comes after it.
func armLines(p Program, node Node) []string {
	successors := p.Successors(node.Id)
	if len(successors) == 0 {
		return nil
	}
	var lines []string
	switch node.Kind {
	case KindBranch:
		for at, next := range successors {
			lines = append(lines, fmt.Sprintf("    %s → %s", letter(at), next))
		}
	case KindParallelSplit:
		for _, next := range successors {
			lines = append(lines, fmt.Sprintf("    ‖ → %s", next))
		}
	}
	return lines
}

// stepDetail is the right-hand side of one row: what this particular node is
// about, in the fields its kind actually uses.
func stepDetail(node Node) string {
	f := node.Fields
	switch node.Kind {
	case KindAgentLoop:
		parts := []string{clipDetail(firstLine(f.Get("brief")))}
		if tools := splitList(f.Get("tools")); len(tools) > 0 {
			parts = append(parts, strings.Join(tools, ", "))
		}
		if model := f.Get("model"); model != "" {
			parts = append(parts, model)
		}
		if turns := f.Int("max_turns", 0); turns > 0 {
			parts = append(parts, fmt.Sprintf("%d turns", turns))
		}
		return join(parts)

	case KindToolCall:
		return join([]string{f.Get("tool"), clipDetail(firstLine(f.Get("args")))})

	case KindBranch:
		return join([]string{"when " + clipDetail(firstLine(f.Get("when")))})

	case KindLoopUntil:
		return join([]string{
			"until " + clipDetail(firstLine(f.Get("until"))),
			fmt.Sprintf("up to %d", f.Int("max_rounds", DefaultRounds)),
		})

	case KindParallelSplit:
		parts := []string{fmt.Sprintf("width %d", f.Int("width", 1))}
		if over := f.Get("over"); over != "" {
			parts = append(parts, "over "+clipDetail(over))
		}
		return join(parts)

	case KindParallelJoin:
		return join([]string{"join " + joinMode(node)})

	case KindHumanGate:
		return join([]string{clipDetail(firstLine(f.Get("ask")))})

	case KindVerify:
		rung := f.Get("ladder")
		if rung == "" {
			// The harness's own rung, which the card said in its foot. Saying
			// "the same as above" beside every check would be a column of it.
			rung = "harness rung"
		}
		return join([]string{rung, clipDetail(firstLine(f.Get("check")))})

	case KindSubharnessCall:
		at := "current"
		if version := f.Int("version", 0); version > 0 {
			at = fmt.Sprintf("v%d", version)
		}
		return join([]string{f.Get("name"), at})

	case KindTrigger:
		parts := []string{f.Get("source")}
		if spec := f.Get("spec"); spec != "" {
			parts = append(parts, clipDetail(spec))
		}
		if command := f.Get("command"); command != "" {
			parts = append(parts, clipDetail(command))
		}
		if args := splitList(f.Get("args")); len(args) > 0 {
			parts = append(parts, strings.Join(args, ", "))
		}
		return join(parts)
	}
	return ""
}

// ── the run, read back ──────────────────────────────────────────────────────

// RunLines is one trace as a person reads it: the path the run actually took,
// one row per step, with the rounds and the failure where they landed.
//
// It is the card's twin and it is deliberately the same shape — id on the left,
// kind beside it, detail on the right — because the two are read against each
// other. The question a person opens a trace with is "where did this differ from
// the card", and two renderings with different columns would make them find that
// out by eye.
func RunLines(t Trace) []string {
	head := t.Id.Name
	if head == "" {
		head = "(unnamed)"
	}
	head += fmt.Sprintf(" · v%d · %s", t.Id.Version, t.status())
	if t.Elapsed > 0 {
		head += " · " + t.Elapsed.Round(time.Millisecond).String()
	}
	lines := []string{head, ""}
	for _, step := range t.Trail {
		mark := "✓"
		if step.Err != "" {
			mark = "✗"
		}
		row := fmt.Sprintf("%s %-3d %-12s %-14s", mark, step.Step, step.Id, step.Kind)
		detail := []string{}
		if step.Err != "" {
			detail = append(detail, step.Err)
		} else if step.Out != "" {
			detail = append(detail, clipDetail(firstLine(step.Out)))
		}
		if step.Elapsed > 0 {
			detail = append(detail, step.Elapsed.Round(time.Millisecond).String())
		}
		lines = append(lines, strings.TrimRight(row+" "+strings.Join(detail, " · "), " "))
	}
	if t.Spent > 0 {
		lines = append(lines, "", fmt.Sprintf("spent  %d of the dynamism budget", t.Spent))
	}
	return lines
}

// RunCard is [RunLines] as one block of text.
func RunCard(t Trace) string { return strings.Join(RunLines(t), "\n") }

// letter is an arm's label on the card and in an error: a, b, c…
func letter(at int) string {
	if at < 0 || at > 25 {
		return fmt.Sprintf("case%d", at+1)
	}
	return string(rune('a' + at))
}

// join renders a row's parts and drops the empties: a row that says "bash · " is
// a row with a shrug on the end of it.
func join(parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
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
