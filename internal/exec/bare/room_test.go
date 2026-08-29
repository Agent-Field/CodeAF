package bare

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE INK RUN OF 2026-08-29, AT THE MOMENT ONE COMMAND TOOK THE LEAF WITH IT.
//
// `bench/deepswe/results/ink-grid-box-layout-…-s8` ran a hundred and nine turns
// against a fifteen-minute envelope, and its last act was `npx ava test/grid.tsx`
// with the leaf's whole remainder to spend. The command was cut when the leaf's
// clock ran out, reported "(no output)" as a clean success, and the loop then
// found a dead context at the next turn boundary and stopped — so the model
// never saw the result of the command it had asked for, the leaf's last words
// were a sentence about a print statement, and the node watchdog two minutes
// above recorded a leaf that had been working for seventeen minutes as one that
// never came back.
//
// The law: a tool call runs inside the room the leaf has left, LESS what it
// takes this leaf to land — and the leaf lands rather than being killed.

// pacedCompleter answers with one tool call per turn, taking a measurable time
// over each answer so the loop has a real pace to reserve against.
type pacedCompleter struct {
	asked int
	// first and rest are how long this completer takes over its first answer
	// and over every one after it. They differ on purpose: the reserve is the
	// WORST call this leaf has seen, so an ordinary call has to fit inside it
	// comfortably — which is the whole reason a landing is affordable at all.
	first time.Duration
	rest  time.Duration
	// command is what every tool call asks for.
	command string
}

func (p *pacedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	p.asked++
	took := p.rest
	if p.asked == 1 {
		took = p.first
	}
	select {
	case <-time.After(took):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// The landing turn is the one made with no tools on the wire; it is
	// answered with words rather than with more work to do.
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		if last.Role == "user" && len(last.Content) > 0 && strings.Contains(last.Content[0].Text, "do not call any tool") {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "I implemented the grid layout in src/grid-layout.ts; the tests are not passing yet."}},
			}}}}, nil
		}
	}
	args, _ := json.Marshal(map[string]any{"command": p.command})
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
		Role:    "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: "running the suite"}},
		ToolCalls: []ai.ToolCall{{
			ID: "call_1", Type: "function",
			Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)},
		}},
	}}}}, nil
}

// A command bigger than the leaf's remaining room is cut, and what it had
// written by then comes back to the model — which is the whole difference
// between a leaf that can scope its next command and a leaf that is dead.
func TestACommandTooBigForTheRoomIsCutAndTheLeafCarriesOn(t *testing.T) {
	completer := &pacedCompleter{
		first: 400 * time.Millisecond, rest: 30 * time.Millisecond,
		// Writes immediately and then outlives anything the leaf can afford.
		command: "echo 'reached 44 checks'; sleep 60",
	}
	loop := &loopState{
		client: completer,
		tools:  Tools(t.TempDir()),
		system: "you are a bare leaf",
		user:   "add CSS grid layout",
	}
	// A whole envelope of two seconds. The reserve is measured from the loop's
	// own first call, so the command is bounded under the remainder and the
	// leaf still has the room its own worst call needs in order to land.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	outcome := loop.run(ctx)
	if outcome == nil {
		t.Fatal("the loop returned nothing")
	}
	if completer.asked < 2 {
		t.Fatalf("the leaf made %d calls; a cut command must not end the turn that asked for it", completer.asked)
	}
	// The cut result reached the model, with the output the command had
	// actually produced and the pace of the machine it produced it on.
	cut := ""
	for _, message := range loop.messages {
		if message.Role == "tool" && len(message.Content) > 0 && strings.HasPrefix(message.Content[0].Text, "cut after ") {
			cut = message.Content[0].Text
		}
	}
	if cut == "" {
		t.Fatalf("no cut command reached the model; the leaf's messages were:\n%s", firstTexts(loop.messages))
	}
	if !strings.Contains(cut, "output so far") {
		t.Fatalf("the cut said %q, want it to hand back what the command had written", cut)
	}
	if !strings.Contains(cut, "reached 44 checks") {
		t.Fatalf("the cut said %q, want the output the command had already produced", cut)
	}
	// AND THE LEAF LANDED. Exhaustion, with words of its own — not a leaf that
	// was killed and whose last sentence was about something else.
	if outcome.Exhausted != exec.StopDeadline {
		t.Fatalf("the leaf ended %q/%q, want a landing marked as exhaustion", outcome.Stop, outcome.Exhausted)
	}
	if !strings.Contains(outcome.Text, "grid-layout.ts") {
		t.Fatalf("the leaf landed saying %q, want its own account of where it got to", outcome.Text)
	}
}

// The reserve is read from the leaf's own measurements and from nothing else.
func TestTheLandingReserveIsWhatThisLeafMeasuredOfItself(t *testing.T) {
	var measured pace
	if got := measured.reserve(); got != 0 {
		t.Fatalf("a leaf that has measured nothing reserves %s, want nothing", got)
	}
	measured.noteCall(3 * time.Second)
	measured.noteCall(time.Second)
	measured.noteFlush(200 * time.Millisecond)
	if got := measured.reserve(); got != 3*time.Second+200*time.Millisecond {
		t.Fatalf("reserve = %s, want the slowest call plus the slowest flush", got)
	}

	// A context with no deadline narrows nothing: everything is affordable and
	// the tool runs under the leaf's own context unchanged.
	if budget, ok := affordable(context.Background(), measured); !ok || budget != 0 {
		t.Fatalf("an unbounded leaf answered (%s, %v), want (0, true)", budget, ok)
	}
	// Room inside the reserve is room for landing and nothing else.
	tight, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if budget, ok := affordable(tight, measured); ok {
		t.Fatalf("a leaf with %s left and a %s reserve was offered %s of work", time.Second, measured.reserve(), budget)
	}
	// Room above it is work, and the reserve is never part of the offer.
	wide, cancelWide := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelWide()
	budget, ok := affordable(wide, measured)
	if !ok {
		t.Fatal("a leaf with seven seconds of spare room was offered no work at all")
	}
	if budget > 10*time.Second-measured.reserve() {
		t.Fatalf("the offer of %s reaches into the landing reserve", budget)
	}
}

// firstTexts renders a message list for a failure message.
func firstTexts(messages []ai.Message) string {
	var out strings.Builder
	for _, message := range messages {
		text := ""
		if len(message.Content) > 0 {
			text = message.Content[0].Text
		}
		out.WriteString("  " + message.Role + ": " + strings.SplitN(text, "\n", 2)[0] + "\n")
	}
	return out.String()
}
