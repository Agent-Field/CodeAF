package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// pressureCompleter answers every turn with one tool call and a fixed reported
// prompt size, which is what a leaf whose transcript never shrinks looks like
// from the meter's side. It never finishes on its own: the bounds are the thing
// under test, so nothing else may end the loop.
type pressureCompleter struct {
	promptTokens int
	calls        int
	landed       int // the turn on which the landing order first appeared, one-based
}

func (p *pressureCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message,
	_ ...ai.Option) (*ai.Response, error) {
	p.calls++
	if p.landed == 0 {
		for _, message := range messages {
			if message.Role == "user" && strings.Contains(contentOf(message), "budget for this task is spent") {
				p.landed = p.calls
				break
			}
		}
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role:      "assistant",
			Content:   text("still working"),
			ToolCalls: []ai.ToolCall{call(fmt.Sprintf("c%d", p.calls), "no_such_tool", `{}`)},
		}, FinishReason: "tool_calls"}},
		Usage: &ai.Usage{PromptTokens: p.promptTokens, CompletionTokens: 1_000},
	}, nil
}

// The third bound, measured — and measured again, on the run that retired it.
//
// It was written to catch what neither grant can see: the sum of what went on
// the wire, the quantity a loop moves when nothing ever leaves its transcript.
// The quantity is real. Bounding it was not the answer, because Σ over turns of
// the prompt is turns × mean-context for ANY transcript that only grows — ink
// s9 of 2026-08-29 stopped at a duplication factor of 8.9× and the audited
// runaway this was built for ran 11.2×, and no detector lives in a gap of
// 1.26×. All three of that run's leaves were landed here, at turns 13, 12 and
// 9 of a 200-turn grant, with a third of their money unspent.
//
// SO IT LANDS NOBODY. It crosses, it is still crossed, and it reaches the leaf
// as the wrap-up warning — where firing early costs a sentence instead of the
// leaf's work. See PERF.md, "A leaf's bounds".
func TestCumulativePressureWarnsTheLeafRatherThanLandingIt(t *testing.T) {
	const window = 200_000
	ceiling := ctxbudget.For(window).ReuseCeiling()
	const perTurn = 30_000
	// The first turn on which the running total reaches the ceiling.
	crossing := (ceiling + perTurn - 1) / perTurn

	client := &pressureCompleter{promptTokens: perTurn}
	// A grant enormous enough that neither the cost ceiling nor the raw ceiling
	// can be what fires: this test is about the bound that reads the model.
	linear := NewLinear(client, workspace(t), nil, 200, 100_000_000, time.Minute).
		WithContextLength(window)

	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "go round in circles"})
	if err != nil {
		t.Fatal(err)
	}
	// The leaf runs to its TURN cap, not to the reuse ceiling: with a grant it
	// cannot spend, the only bound left that may land it is the backstop.
	if outcome.Stop == StopBudget || outcome.Exhausted == StopBudget {
		t.Fatalf("stop=%q exhausted=%q — the reuse bound landed a leaf inside its grant",
			outcome.Stop, outcome.Exhausted)
	}
	// It was crossed, several times over, and the leaf kept working.
	if sent := outcome.contextPressure(); sent < ceiling {
		t.Fatalf("the run sent %d prompt tokens against a %d ceiling; the fixture no longer crosses it",
			sent, ceiling)
	}
	if outcome.Turns <= crossing+landingTurns {
		t.Fatalf("the leaf ran %d turns and stopped at the old bound's %d — it is still landing work",
			outcome.Turns, crossing+landingTurns)
	}
	// AND THE WARNING REACHED IT. The evidence is demoted, not discarded.
	if used := budgetUsed(outcome, 100_000_000, window); used <= wrapUpAt {
		t.Fatalf("budgetUsed = %.2f: the reuse pressure never raised the wrap-up warning past %.2f",
			used, wrapUpAt)
	}
	// The ledger is what the bound was read off, and it has to sum to the row
	// the journal already wrote.
	if got := outcome.contextPressure(); got != outcome.Usage.PromptTokens {
		t.Fatalf("the per-turn ledger sums to %d sent tokens, and the node row says %d",
			got, outcome.Usage.PromptTokens)
	}
	if len(outcome.PerTurn) != outcome.Turns {
		t.Fatalf("the ledger holds %d rows for %d turns", len(outcome.PerTurn), outcome.Turns)
	}
}

// A leaf that stays under the bound never hears about it. The same run shape,
// with turns small enough that the ceiling is out of reach, must finish on the
// turn cap with nothing exhausted — a governor that fires on an honest leaf is
// worse than no governor.
func TestCumulativePressureLeavesAnOrdinaryLeafAlone(t *testing.T) {
	const window = 200_000
	const turns = 12
	// Comfortably inside the ceiling across the whole run.
	perTurn := ctxbudget.For(window).ReuseCeiling() / (turns * 2)

	client := &pressureCompleter{promptTokens: perTurn}
	linear := NewLinear(client, workspace(t), nil, turns, 100_000_000, time.Minute).
		WithContextLength(window)

	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "do the work"})
	if err != nil {
		t.Fatal(err)
	}
	if client.landed != 0 {
		t.Fatalf("an ordinary leaf was told to land on turn %d, having sent %d of %d permitted tokens",
			client.landed, outcome.Usage.PromptTokens, ctxbudget.For(window).ReuseCeiling())
	}
	if outcome.Exhausted != "" {
		t.Fatalf("exhausted=%q on a leaf that ran out of nothing", outcome.Exhausted)
	}
}

// An unknown window is not governed. The catalog is offline, the slug is
// unrecognised, the row was cached before the field was kept — all three look
// identical here, and a cumulative bound derived from a guessed window would
// land honest leaves on evidence nobody has.
func TestCumulativePressureIsInertWhenTheWindowIsUnknown(t *testing.T) {
	if got := reuseCeiling(0); got != 0 {
		t.Fatalf("an unknown window produced a %d-token bound", got)
	}
	if pressureReached(1<<30, reuseCeiling(0)) {
		t.Fatal("the bound fired against an unknown window")
	}

	client := &pressureCompleter{promptTokens: 200_000}
	// No WithContextLength: nobody could say what this model holds.
	linear := NewLinear(client, workspace(t), nil, 10, 100_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "go round in circles"})
	if err != nil {
		t.Fatal(err)
	}
	if client.landed != 0 {
		t.Fatalf("a leaf on an unknown window was landed on turn %d", client.landed)
	}
	if outcome.Stop != StopTurnCap {
		t.Fatalf("stop=%q, want the turn cap — nothing else may have ended it", outcome.Stop)
	}
}

// The per-turn ledger is evidence, and its whole job is to sum to the row that
// was already being written. Tool spend belongs to the turn that caused it: a
// reader who has to remember which costs were left out has been handed a second
// accounting rather than a finer one.
func TestTheTurnLedgerSumsToTheNodeRow(t *testing.T) {
	outcome := &Outcome{}
	for turn := 1; turn <= 3; turn++ {
		outcome.Turns = turn
		outcome.meterTurn(&ai.Response{Usage: &ai.Usage{
			PromptTokens: 1_000 * turn, CompletionTokens: 100, PromptTokensDetails: &ai.PromptTokensDetails{
				CachedTokens: 500 * turn,
			},
		}})
		outcome.meterTool(Usage{Calls: 1, PromptTokens: 7, CompletionTokens: 3, Cost: 0.5})
	}
	var rows Usage
	sent := 0
	for _, row := range outcome.PerTurn {
		rows.merge(row.Usage)
		sent += row.Sent
	}
	if rows != outcome.Usage {
		t.Fatalf("the rows sum to %+v and the node row says %+v", rows, outcome.Usage)
	}
	// Sent is the turn's OWN call, so a tool that runs a model of its own must
	// not move it: transcript growth is what the bound is about.
	if want := 1_000 + 2_000 + 3_000; sent != want {
		t.Fatalf("cumulative sent = %d, want %d — tool spend moved the pressure meter", sent, want)
	}
	if got := outcome.contextPressure(); got != sent {
		t.Fatalf("contextPressure = %d, want %d", got, sent)
	}
}

// A tool bill with no turn to attribute it to still reaches the node total.
func TestToolSpendBeforeAnyTurnStillReachesTheNodeRow(t *testing.T) {
	outcome := &Outcome{}
	outcome.meterTool(Usage{Calls: 1, PromptTokens: 40, Cost: 0.25})
	if outcome.Usage.PromptTokens != 40 || outcome.Usage.Cost != 0.25 {
		t.Fatalf("a bill nobody could place was dropped: %+v", outcome.Usage)
	}
	if len(outcome.PerTurn) != 0 {
		t.Fatalf("a turn row was invented for it: %+v", outcome.PerTurn)
	}
}

// foldLeaf builds a transcript of the given shape: one user brief, then a run of
// assistant turns each carrying a tool call and its answer.
func foldLeaf(assistantBytes, toolBytes, turns int) []ai.Message {
	messages := []ai.Message{{Role: "user", Content: text("do the thing")}}
	for turn := 1; turn <= turns; turn++ {
		id := fmt.Sprintf("t%d", turn)
		messages = append(messages,
			ai.Message{Role: "assistant",
				Content:   text(fmt.Sprintf("turn %d: ", turn) + strings.Repeat("r", assistantBytes)),
				ToolCalls: []ai.ToolCall{call(id, "sh", `{"cmd":"ls"}`)}},
			ai.Message{Role: "tool", ToolCallID: id, Content: text(strings.Repeat("o", toolBytes))},
		)
	}
	return messages
}

// The decayer reaches the leaf's own prose, and only after it has spent
// everything it can on spent raw material.
//
// The rule it replaces said assistant messages are compressed state and are
// never touched, and that reasoning is right about the recent ones and was
// assumed forever. Measured, assistant output was 45% of context growth while no
// governor in the harness could reach a byte of it.
func TestFoldRetiresAgedAssistantTurnsAndKeepsTheLiveEdge(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "9", nil)
	fade := newDecayer(map[string]string{}, tools.decaySpill)
	const turns = 10
	messages := foldLeaf(8<<10, 64, turns)
	originals := make([]string, len(messages))
	for index := range messages {
		originals[index] = contentOf(messages[index])
	}
	budget := 16 << 10

	// Tool output is tiny here, so decay has nothing worth retiring and the
	// transcript is over budget on the leaf's own prose alone.
	if decayed := fade.decay(messages, budget); decayed != 0 {
		t.Fatalf("decay stubbed %d results; this transcript's bulk is assistant text", decayed)
	}
	folded := fade.fold(messages, budget)
	if folded != turns-foldKeepAssistantTurns {
		t.Fatalf("folded %d turns, want the %d older than the %d-turn live edge",
			folded, turns-foldKeepAssistantTurns, foldKeepAssistantTurns)
	}

	seen := 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "assistant" {
			continue
		}
		body := contentOf(messages[index])
		seen++
		if seen <= foldKeepAssistantTurns {
			if body != originals[index] {
				t.Fatalf("assistant turn %d back from the edge was folded; the model's live working "+
					"memory and its deliverable are what the keep protects", seen)
			}
			continue
		}
		if !strings.Contains(body, "folded") {
			t.Fatalf("an aged assistant turn survived: %q", clipForTest(body))
		}
		// A fold that does not say where is a deletion with a receipt.
		if !strings.Contains(body, obsDir) {
			t.Fatalf("the fold stub names no durable address: %q", body)
		}
		path := body[strings.Index(body, obsDir):]
		path = path[:strings.IndexAny(path, " ")]
		on, err := os.ReadFile(filepath.Join(space.Root(), path))
		if err != nil || string(on) != originals[index] {
			t.Fatalf("the text the stub points at is %d bytes, err=%v", len(on), err)
		}
		// Pairing is absolute: only the words go.
		if len(messages[index].ToolCalls) != 1 {
			t.Fatal("a folded assistant message lost its tool_calls; that is a provider rejection")
		}
	}

	// The deliverable is the newest assistant text, and it must still be
	// readable as such after a fold — this is what land() hands over.
	if got := lastAssistantText(messages); !strings.HasPrefix(got, fmt.Sprintf("turn %d: ", turns)) {
		t.Fatalf("the newest assistant text reads %q after folding", clipForTest(got))
	}

	// Write-once, like every other stub here: a second pass changes nothing.
	before := contentOf(messages[1])
	if again := fade.fold(messages, budget); again != 0 {
		t.Fatalf("a second pass folded %d more; folding is write-once", again)
	}
	if contentOf(messages[1]) != before {
		t.Fatal("a stub was rewritten on a later pass")
	}
}

// The order is the policy. Fold is asked after decay, sees what decay left, and
// on the ordinary leaf — whose bulk is tool output — does nothing at all. That
// is what makes this additive: every run whose results fit after decay behaves
// exactly as it did before assistant text was reachable.
func TestFoldDoesNothingWhileRetiringToolOutputIsEnough(t *testing.T) {
	tools := NewToolbox(workspace(t), "8", nil)
	fade := newDecayer(map[string]string{}, tools.decaySpill)
	messages := foldLeaf(200, 16<<10, 8)
	budget := 24 << 10

	fade.decay(messages, budget)
	if folded := fade.fold(messages, budget); folded != 0 {
		t.Fatalf("folded %d assistant turns on a leaf whose results decay already brought inside "+
			"the working set", folded)
	}
	for index, message := range messages {
		if message.Role != "assistant" {
			continue
		}
		if !strings.HasPrefix(contentOf(message), fmt.Sprintf("turn %d: ", index/2+1)) {
			t.Fatalf("assistant message %d was touched: %q", index, clipForTest(contentOf(message)))
		}
	}
}

// Bytes with no durable address are never pointed at — the same rule the
// observation pointers hold to, arriving from the other side. A leaf with no
// filesystem carries its own prose again rather than being handed a tombstone.
func TestFoldNeverLeavesAPointerItCannotHonour(t *testing.T) {
	fade := newDecayer(map[string]string{}, nil)
	messages := foldLeaf(8<<10, 64, 10)
	before := contentOf(messages[1])
	if folded := fade.fold(messages, 16<<10); folded != 0 {
		t.Fatalf("folded %d turns with nowhere to put their text", folded)
	}
	if contentOf(messages[1]) != before {
		t.Fatal("an assistant message was replaced by a stub pointing nowhere")
	}
}
