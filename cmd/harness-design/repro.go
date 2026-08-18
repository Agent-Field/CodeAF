package main

// THE REPRODUCTION RIG: the same design stage, run N times, with the refusals
// counted instead of the architecture read.
//
// It exists because of a failure this binary could not see. A person's chat kept
// failing to build a harness on a slow reasoning model, and the rig above was
// green — because the rig was measuring nothing. The shared guide had grown a
// `derivation` key, this file's envelope had not, and `strict` refuses unknown
// fields: every reply the guide asked for was thrown out here as "not the
// envelope". A rig that agrees with the guide by accident is not an instrument.
//
// TWO THINGS ARE DIFFERENT FROM THE STAGES ABOVE, and both are about measuring
// the surface that actually ships:
//
//   - THE BELT. `-belt chat` renders the guide against internal/exec/bare's
//     tools — read, bash, edit, write, grep, find, ls — which is what a
//     conversation hands a harness (internal/session's harnessMachinery). The
//     rig's own belt is three tools that cannot fail, which is right for
//     measuring architecture and wrong for reproducing a chat.
//   - THE SAMPLE. One design says nothing: the refusals here are stochastic, and
//     the same goal on the same model lands first try and then burns all three
//     attempts. `-repro N` runs the design stage N times and prints how many
//     attempts each one cost and what refused it, so a change to the guide can be
//     argued from a rate rather than from an anecdote.
//
// Usage:
//
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -repro 5 -belt chat \
//	  -model deepseek/deepseek-v4-pro \
//	  -goal "research helper: given a topic, fetch 3 sources and summarize with citations"

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// chatBelt is the tool belt a CONVERSATION hands a harness, in this rig's own
// shape. The `about` line is clipped the way the session clips it, so the guide
// a repro renders is the guide a chat renders.
//
// Nothing here runs. A repro is design-only by construction — stage 3 would be
// executing pi's real read, write and bash against the machine it is measuring
// on — so the run funcs refuse rather than pretend, and a future caller that
// wires them into an Env is told so at the first call instead of the first
// silent wrong answer.
func chatBelt(cwd string) []toolSpec {
	tools := bare.AllTools(cwd)
	belt := make([]toolSpec, 0, len(tools))
	for _, tool := range tools {
		belt = append(belt, toolSpec{
			name:  tool.Name,
			about: clip(firstLine(tool.Description), chatToolAbout),
			run: func(string) (string, error) {
				return "", fmt.Errorf("the chat belt is rendered for the guide here, never run: a repro is design-only")
			},
		})
	}
	return belt
}

// chatToolAbout matches internal/session's harnessToolAbout. The two are the
// same number for the same reason — a wire description is a paragraph written
// for a model deciding whether to CALL the tool, and a designer needs a name and
// a sentence — and they are written twice only because neither package may
// import the other's rig.
const chatToolAbout = 160

func firstLine(text string) string {
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return strings.TrimSpace(text)
}

// chosenBelt is the belt this run was started with, and it is a package var for
// one reason: `check` is reached from two call sites that have never carried a
// belt, and a lint that kept using the rig's three tools while the GUIDE was
// rendered against the chat's seven would refuse every design it asked for. One
// flag, one belt, both halves. Nothing writes it after [beltNamed].
var chosenBelt []toolSpec

// beltInUse is the lint's belt: whatever [beltNamed] settled on, and the rig's
// own when nothing did — a test that calls `check` directly has no flags.
func beltInUse() []toolSpec {
	if chosenBelt != nil {
		return chosenBelt
	}
	return availableTools
}

// beltNamed is the belt one word chooses, or an error naming the two that exist.
func beltNamed(word, cwd string) ([]toolSpec, error) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "", "rig":
		chosenBelt = availableTools
	case "chat", "session":
		chosenBelt = chatBelt(cwd)
	default:
		return nil, fmt.Errorf("there is no belt called %q — it is rig or chat", word)
	}
	return chosenBelt, nil
}

// sameBelt reports whether two belts are the same set of tools, by name. It is
// how the run gate below tells "the rig's own" from anything else without the
// caller having to remember which word it typed.
func sameBelt(a, b []toolSpec) bool {
	if len(a) != len(b) {
		return false
	}
	for at := range a {
		if a[at].name != b[at].name {
			return false
		}
	}
	return true
}

// trial is what one design attempt-ladder came to.
type trial struct {
	attempts int
	took     time.Duration
	// refusals is what each spent attempt was refused for, oldest first. A trial
	// that landed first try has none.
	refusals []string
	err      error
	nodes    int
	rung     string
	// lastRaw is the reply behind the last refusal, kept for a trial that never
	// came right: a tally says WHICH law was broken and only the text says how.
	lastRaw string
}

// reproMode runs the design stage over and over and prints the tally. It never
// reviews, never saves and never runs: the question is whether a page can be
// GOT AT ALL, and every stage past that one is a different question.
func reproMode(ctx context.Context, chat *chatClient, designer string, goals []struct{ key, note, text string }, trials, tokens int, temperature float64, retries int) int {
	failed := 0
	for _, goal := range goals {
		head(fmt.Sprintf("repro · %s · %d trials", goal.key, trials), goal.text)
		results := make([]trial, 0, trials)
		for at := 1; at <= trials; at++ {
			if ctx.Err() != nil {
				break
			}
			one := oneTrial(ctx, chat, designer, goal.text, tokens, temperature, retries)
			results = append(results, one)
			switch {
			case one.err != nil:
				fmt.Printf("trial %2d  FAILED after %d attempts · %s\n          %v\n", at, one.attempts, one.took.Round(time.Millisecond), one.err)
				failed++
			default:
				fmt.Printf("trial %2d  landed on attempt %d/%d · %s · %d nodes · %s\n", at, one.attempts, retries+1, one.took.Round(time.Millisecond), one.nodes, one.rung)
			}
			for _, refusal := range one.refusals {
				fmt.Printf("          refused: %s\n", clip(refusal, 200))
			}
			if one.err != nil && one.lastRaw != "" {
				fmt.Printf("\n          the last reply, verbatim:\n%s\n\n", indent(clip(one.lastRaw, 4000), "          "))
			}
		}
		printTally(results, retries)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// oneTrial is [rig.oneGoal]'s stage 1, with the printing taken out and the
// refusals kept. The history is threaded exactly as it is there and in
// internal/session, because a repro of a retry ladder that repairs differently
// is a repro of something else.
func oneTrial(ctx context.Context, chat *chatClient, designer, goal string, tokens int, temperature float64, retries int) trial {
	history := []message{
		{Role: "system", Content: designer},
		{Role: "user", Content: "THE GOAL:\n\n" + goal + "\n\nDesign the sub-harness for it."},
	}
	began := time.Now()
	out := trial{}
	for tries := 0; tries <= retries; tries++ {
		out.attempts = tries + 1
		_, harness, at, err := designOnce(ctx, chat, history, tokens, temperature)
		if err == nil {
			out.took = time.Since(began)
			out.nodes = len(harness.Program.Nodes)
			out.rung = harness.Verify.Ladder + "/" + harness.Dyn.Ladder
			return out
		}
		out.refusals = append(out.refusals, err.Error())
		out.lastRaw = at.raw
		if ctx.Err() != nil {
			out.err, out.took = ctx.Err(), time.Since(began)
			return out
		}
		if tries == retries {
			out.err, out.took = fmt.Errorf("no valid design in %d attempts: %w", tries+1, err), time.Since(began)
			return out
		}
		// The refused page goes back with the refusal, but only when there IS one:
		// a model that spent its whole budget thinking answered with nothing, and
		// an empty assistant turn is a message with no content in it. The shipped
		// path guards it the same way (internal/session's designPage), and a repro
		// that threaded the history differently would be reproducing something
		// else.
		if strings.TrimSpace(at.raw) != "" {
			history = append(history, message{Role: "assistant", Content: at.raw})
		}
		history = append(history,
			message{Role: "user", Content: "That harness was REFUSED:\n\n" + err.Error() +
				"\n\nFix exactly that and reply with the whole envelope again — one JSON object, no prose."})
	}
	out.took = time.Since(began)
	return out
}

// printTally is the whole point of running more than one: the landing rate, the
// attempts it cost, and the refusals ranked by how often they were what stood in
// the way.
func printTally(results []trial, retries int) {
	if len(results) == 0 {
		return
	}
	landed, spent := 0, 0
	counts := map[string]int{}
	for _, one := range results {
		spent += one.attempts
		if one.err == nil {
			landed++
		}
		for _, refusal := range one.refusals {
			counts[refusalClass(refusal)]++
		}
	}
	section("the tally")
	fmt.Printf("landed  %d/%d\n", landed, len(results))
	fmt.Printf("cost    %d attempts for %d designs (the ladder allows %d each)\n", spent, len(results), retries+1)
	if len(counts) == 0 {
		fmt.Println("refused nothing")
		return
	}
	classes := make([]string, 0, len(counts))
	for class := range counts {
		classes = append(classes, class)
	}
	sort.Slice(classes, func(i, j int) bool {
		if counts[classes[i]] != counts[classes[j]] {
			return counts[classes[i]] > counts[classes[j]]
		}
		return classes[i] < classes[j]
	})
	for _, class := range classes {
		fmt.Printf("refused %3d × %s\n", counts[class], class)
	}
}

// refusalClass strips the parts of a refusal that name THIS design — the harness
// slug, the node ids — so that the same law broken twice counts twice rather
// than reading as two different problems.
func refusalClass(refusal string) string {
	// Every Validate and CheckDerivation refusal is prefixed `subharness: <name>:`.
	if rest, found := strings.CutPrefix(refusal, "subharness: "); found {
		if at := strings.Index(rest, ": "); at >= 0 {
			rest = rest[at+2:]
		}
		refusal = rest
	}
	// The transport's own retries prefix their diagnosis; the diagnosis is what
	// classifies.
	if rest, found := strings.CutPrefix(refusal, "after 3 attempts: "); found {
		refusal = rest
	}
	for _, known := range []string{
		"spent its whole budget thinking",
		"with no verify node in the program",
		"with no human.gate in the program",
		"is derived as independent, but the program runs one into the other",
		"which is not a node in the program",
		"is derived twice",
		"with no `why`",
		"ran out of completion budget",
		"neither the reply nor its repair parsed",
		"your reply is not the envelope",
		"the whitelist names",
		"the whitelist grants",
		"fewer than two cues",
		"the justification is",
		"is a verify with no `check`",
		"cannot be reached from",
		"is not one of",
		"dyn cap",
	} {
		if strings.Contains(refusal, known) {
			return known
		}
	}
	return clip(refusal, 90)
}
