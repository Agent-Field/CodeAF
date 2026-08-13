package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A result is cut on line boundaries, at the end that matters for the tool that
// produced it, and the notice says exactly what to run next.
//
// The old cut was a byte count through the middle of the text with "[N bytes
// elided]" in the gap. Every part of that was a small tax the model paid twice:
// the two ends were both half-lines, so anything structured — a stack frame, a
// JSON object, a path — arrived unusable; and the pointer at the end named a
// file with a made-up example command, so a model that wanted the rest had to
// invent the offsets itself and usually re-ran the command instead.
func TestATruncatedResultKeepsWholeLinesAndSaysHowToReadTheRest(t *testing.T) {
	const carry, keep = 4096, 1024
	body := ""
	for line := 1; len(body) < 8*carry; line++ {
		body += fmt.Sprintf("line %d: the quick brown fox jumps over the lazy dog\n", line)
	}
	total := countLines(body)

	command, cut := boundResult(body, carry, keep, shapeCommand, spillRef{path: ".obs/1-1.txt", bytes: len(body)})
	if !cut {
		t.Fatal("output far past both caps was not cut")
	}
	read, _ := boundResult(body, carry, keep, shapeRead, spillRef{path: ".obs/1-1.txt", bytes: len(body)})

	// Whole lines, both ways. Nothing that reaches the model ends or begins in
	// the middle of one.
	for name, text := range map[string]string{"command": command, "read": read} {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "line ") && !strings.HasSuffix(line, "lazy dog") {
				t.Errorf("%s: %q is half a line", name, line)
			}
		}
	}

	// A command's END survives, because that is where its verdict is; a read's
	// BEGINNING survives, because that is what was asked for.
	last := fmt.Sprintf("line %d:", total)
	if !strings.Contains(command, last) {
		t.Error("command output was cut from the end — the last line, where the error lives, is gone")
	}
	if strings.Contains(command, "line 1:") {
		t.Error("command output kept its head; a tail cut is the whole point")
	}
	if !strings.Contains(read, "line 1:") || strings.Contains(read, last) {
		t.Error("a read was not head-truncated")
	}

	// The exact next command, not an example of one. A read continues at the
	// line after the last one shown; a command's cut end is read from the top of
	// the file the whole output was saved to.
	shown := countLines(strings.TrimSpace(read[:strings.Index(read, "[Showing")]))
	if want := fmt.Sprintf("sed -n '%d,%dp' .obs/1-1.txt", shown+1, shown+maxResultLines); !strings.Contains(read, want) {
		t.Errorf("the read notice does not carry %q:\n%s", want, noticeOf(read))
	}
	if !strings.Contains(command, "sed -n '1,") || !strings.Contains(command, ".obs/1-1.txt") {
		t.Errorf("the command notice does not say how to read the part that was cut:\n%s", noticeOf(command))
	}
	// And it says where in the whole it is, in lines and in bytes.
	if !strings.Contains(command, fmt.Sprintf("of %d, the last", total)) ||
		!strings.Contains(command, fmt.Sprintf("of %d bytes", len(body))) {
		t.Errorf("the notice does not locate the fragment in the whole:\n%s", noticeOf(command))
	}
}

// The line cap is a cap of its own, not a byte cap in disguise: thin output
// well inside the byte budget is still cut, because two thousand lines of it is
// a wall the model will not read and a bill every later turn pays.
func TestTheLineCapBindsOnItsOwn(t *testing.T) {
	body := strings.Repeat("x\n", maxResultLines+500)
	if len(body) >= 1<<20 {
		t.Fatal("the sample is supposed to be thin")
	}
	bounded, cut := boundResult(body, 1<<20, 1<<19, shapeCommand, spillRef{path: ".obs/1-1.txt"})
	if !cut {
		t.Fatalf("%d lines inside the byte cap were carried whole", countLines(body))
	}
	if lines := countLines(bounded); lines > maxResultLines+2 {
		t.Errorf("the cut result still carries %d lines", lines)
	}
	if _, cut := boundResult(strings.Repeat("x\n", maxResultLines), 1<<20, 1<<19, shapeCommand, spillRef{}); cut {
		t.Error("output exactly at the line cap was cut")
	}

	// And through the streaming path, where the line cap has to open the spill
	// file on its own: a thin listing is nowhere near the byte limit, so nothing
	// else would have opened one, and a notice with no file behind it would be
	// telling the model to run the command again.
	tools := NewToolbox(workspace(t), "5", nil)
	thin := tools.Execute(t.Context(), "sh", `{"cmd":"awk 'BEGIN{for(i=1;i<=4000;i++) print i}'"}`)
	if thin.IsError {
		t.Fatalf("command failed: %s", thin.Content)
	}
	if !strings.Contains(thin.Content, obsDir) {
		t.Errorf("a 4,000-line command was cut with nowhere to read the rest: %q", noticeOf(thin.Content))
	}
}

// The case whole-line truncation cannot serve: one line longer than everything
// that fits — a minified bundle, a one-line JSON dump, a base64 blob. The cut
// happens inside the line, and because no line number can address a byte range,
// the notice hands over the exact bytes-and-all command instead.
func TestAGiantSingleLineGetsTheExactCommandForItsBytes(t *testing.T) {
	const keep = 512
	body := strings.Repeat("j", 40*keep)

	read, cut := boundResult(body, 4*keep, keep, shapeRead, spillRef{path: ".obs/1-1.txt", bytes: len(body)})
	if !cut {
		t.Fatal("a one-line result far past the caps was not cut")
	}
	want := fmt.Sprintf("sed -n '1p' .obs/1-1.txt | cut -b %d-%d", keep+1, 2*keep)
	if !strings.Contains(read, want) {
		t.Errorf("the notice does not say how to read the next bytes of the line (%s):\n%s", want, noticeOf(read))
	}

	// From the command side the end is what was kept, so the command offered is
	// the one that shows the line from its start.
	command, _ := boundResult(body, 4*keep, keep, shapeCommand, spillRef{path: ".obs/1-1.txt", bytes: len(body)})
	if !strings.Contains(command, fmt.Sprintf("sed -n '1p' .obs/1-1.txt | head -c %d", keep)) {
		t.Errorf("the notice does not say how to read the start of the cut line:\n%s", noticeOf(command))
	}
	if !strings.Contains(command, "is longer than the 512 bytes that fit") {
		t.Errorf("the notice does not say why it cut inside a line:\n%s", noticeOf(command))
	}
}

// A truncation that cannot point anywhere says so rather than naming a file
// that is not there. A wrong path in a notice costs a turn to discover.
func TestATruncationWithNowhereToSpillSaysSo(t *testing.T) {
	bounded, _ := boundResult(strings.Repeat("a line\n", 5000), 1024, 512, shapeCommand, spillRef{})
	if strings.Contains(bounded, "sed -n") {
		t.Errorf("a notice with no file behind it still offered a command:\n%s", noticeOf(bounded))
	}
	if !strings.Contains(bounded, "not saved") {
		t.Errorf("a notice with no file behind it does not say so:\n%s", noticeOf(bounded))
	}
}

// End to end, through the tool the whole thing exists for: a command that
// prints more than the leaf can hold leaves the tail in context, the whole
// output on disk, and a command that reads the head back — and running that
// command works.
func TestALoudCommandLeavesItsTailInContextAndTheWholeOnDisk(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "9", nil)
	result := tools.Execute(t.Context(), "sh",
		`{"cmd":"awk 'BEGIN{for(i=1;i<=20000;i++) printf \"line %d of a very loud command\\n\", i}'"}`)
	if result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}
	if len(result.Content) > tools.budgets.spill {
		t.Fatalf("a 20,000-line command left %d bytes in context", len(result.Content))
	}
	if !strings.Contains(result.Content, "line 20000 of a very loud command") {
		t.Error("the end of the output — where a command says how it went — did not survive")
	}
	if strings.Contains(result.Content, "line 1 of a very loud command\n") {
		t.Error("the head was kept; command output is cut from the front")
	}
	notice := noticeOf(result.Content)
	if !strings.Contains(notice, obsDir) {
		t.Fatalf("the notice does not name the file the output went to: %q", notice)
	}
	// The command in the notice is run verbatim, and it produces the lines the
	// context does not have.
	offered := strings.TrimSuffix(notice[strings.Index(notice, "sh: ")+len("sh: "):], "]")
	arguments, err := json.Marshal(map[string]any{"cmd": offered})
	if err != nil {
		t.Fatal(err)
	}
	back := tools.Execute(t.Context(), "sh", string(arguments))
	if back.IsError {
		t.Fatalf("the command the notice offered failed: %s", back.Content)
	}
	// It read the window it said it would: the end of the head range is there,
	// and the tail the context already held is not.
	if !strings.Contains(back.Content, "line 2000 of a very loud command") {
		t.Errorf("%q did not read back the head of the output: %q", offered, clipForTest(back.Content))
	}
	if strings.Contains(back.Content, "line 20000 of a very loud command") {
		t.Errorf("%q read past the window it named", offered)
	}
}

// Several edits, one call, all matched against the file the model read. The
// alternative — five calls — is five round-trips and five results in a
// transcript that is re-sent every turn afterwards.
func TestEditAppliesSeveralBlocksAgainstTheOriginalFile(t *testing.T) {
	space := workspace(t)
	path := filepath.Join(space.Root(), "prog.go")
	original := "alpha one\nbeta two\ngamma three\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := NewToolbox(space, "1", nil)

	result := tools.Execute(t.Context(), "edit",
		`{"path":"prog.go","edits":[{"old":"alpha one","new":"gamma three"},{"old":"gamma three","new":"delta four"}]}`)
	if result.IsError {
		t.Fatalf("a two-block edit failed: %s", result.Content)
	}
	// The second edit matched the ORIGINAL gamma, not the one the first edit
	// wrote. Matching against the running file would have made these two edits
	// ambiguous or wrong, and the model has no way to reason about a file it has
	// never seen.
	body, _ := os.ReadFile(path)
	if string(body) != "gamma three\nbeta two\ndelta four\n" {
		t.Fatalf("edits were applied against a moving file: %q", body)
	}
	// One sentence back. The diff is the expensive thing to return here — it is
	// the size of the change and it is re-sent on every remaining turn — and the
	// model already knows what it asked for.
	if result.Content != "Successfully replaced 2 block(s) in prog.go." {
		t.Errorf("the result is not the one sentence: %q", result.Content)
	}
	for _, leaked := range []string{"alpha", "delta four", "@@", "---", "+++"} {
		if strings.Contains(result.Content, leaked) {
			t.Errorf("the result echoes file content (%q) back into the transcript: %q", leaked, result.Content)
		}
	}
}

// The two refusals that keep a batch honest, and the single edit still working
// exactly as it did.
func TestEditRefusesAmbiguityAndOverlapAcrossABatch(t *testing.T) {
	space := workspace(t)
	path := filepath.Join(space.Root(), "doc.md")
	original := "alpha\nbeta\nalpha\ngamma delta\n"
	write := func() {
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write()
	tools := NewToolbox(space, "1", nil)

	for name, item := range map[string]struct{ args, want string }{
		"a block that matches twice": {
			`{"path":"doc.md","edits":[{"old":"beta","new":"b"},{"old":"alpha","new":"a"}]}`,
			"Found 2 occurrences of edit 2's text in doc.md",
		},
		"a block that matches nothing": {
			`{"path":"doc.md","edits":[{"old":"beta","new":"b"},{"old":"epsilon","new":"e"}]}`,
			"edit 2's text is not in doc.md",
		},
		"two blocks over the same text": {
			`{"path":"doc.md","edits":[{"old":"gamma delta","new":"g"},{"old":"delta","new":"d"}]}`,
			"edits 1 and 2 overlap",
		},
	} {
		t.Run(name, func(t *testing.T) {
			result := tools.Execute(t.Context(), "edit", item.args)
			if !result.IsError || !strings.Contains(result.Content, item.want) {
				t.Fatalf("want an error saying %q, got %+v", item.want, result)
			}
			// Nothing is written unless every edit resolves: a file half-edited
			// by a call that then failed is broken in a way the model cannot see.
			if body, _ := os.ReadFile(path); string(body) != original {
				t.Fatalf("the file was changed by a refused batch: %q", body)
			}
		})
	}

	// The single-edit form is untouched, including its one-sentence answer.
	if result := tools.Execute(t.Context(), "edit", `{"path":"doc.md","old":"gamma delta","new":"g"}`); result.IsError ||
		result.Content != "Successfully replaced 1 block(s) in doc.md." {
		t.Fatalf("the single-edit form changed: %+v", result)
	}
}

// lengthStopped is a model that hits its output ceiling holding tool calls —
// the case that used to run them.
type lengthStopped struct {
	calls []ai.ToolCall
	turns int
	seen  []ai.Message
}

func (l *lengthStopped) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	l.turns++
	l.seen = append([]ai.Message{}, messages...)
	message := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "writing the file now"}}}
	reason := "stop"
	if l.turns == 1 {
		message.ToolCalls, reason = l.calls, "length"
	} else {
		message.Content = []ai.ContentPart{{Type: "text", Text: "done"}}
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: message, FinishReason: reason}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

// A reply cut off at the output limit while holding tool calls is a reply whose
// arguments may be half-written. Truncated JSON can still parse — a string that
// happens to close, an object that happens to balance — so "it validated" is no
// evidence at all, and a write with half its text or an edit with half its old
// is a workspace the model believes something untrue about. None of them run.
func TestALengthStoppedBatchIsFailedWithoutRunning(t *testing.T) {
	space := workspace(t)
	client := &lengthStopped{calls: []ai.ToolCall{
		call("c1", "write", `{"path":"deliverable.md","text":"the first half of a long doc"}`),
		call("c2", "sh", `{"cmd":"echo ran > ran.txt"}`),
	}}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "write the doc"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"deliverable.md", "ran.txt"} {
		if _, err := os.Stat(filepath.Join(space.Root(), name)); err == nil {
			t.Errorf("%s was written by a call from a truncated reply", name)
		}
	}
	// Every call is answered — the protocol requires it — and every answer is
	// the same sentence: nothing ran, ask again with complete arguments.
	answered := map[string]string{}
	for _, message := range client.lastSeen() {
		if message.Role == "tool" {
			answered[message.ToolCallID] = message.Content[0].Text
		}
	}
	if len(answered) != 2 {
		t.Fatalf("a two-call batch got %d tool results", len(answered))
	}
	for id, body := range answered {
		if !strings.Contains(body, "NOT executed") || !strings.Contains(body, "Re-issue the call") {
			t.Errorf("%s was answered with %q, which does not tell the model to re-issue it", id, body)
		}
	}
}

func (l *lengthStopped) lastSeen() []ai.Message { return l.seen }

// The tools contribute their own guidance to the system message, so a leaf
// reads instructions for the tools it is holding and no others.
func TestTheSystemMessageCarriesTheGuidanceOfTheToolsInHand(t *testing.T) {
	tools := NewToolbox(workspace(t), "1", nil)
	lines := tools.Guidelines()
	if len(lines) == 0 {
		t.Fatal("a leaf with the core toolbox contributed no guidance at all")
	}
	system := (&Linear{}).system(Task{}, lines)
	for _, line := range lines {
		if !strings.Contains(system, line) {
			t.Errorf("the assembled system message is missing %q", line)
		}
	}
	// Keyed to the toolbox actually built: every line names a tool this leaf is
	// holding, so nothing in the block is an instruction about a tool that is
	// not in the list.
	held := map[string]bool{}
	for _, definition := range tools.Definitions() {
		held[definition.Function.Name] = true
	}
	for _, line := range lines {
		name, _, _ := strings.Cut(line, ":")
		if !held[name] {
			t.Errorf("the leaf is reading guidance for %q, which it is not holding", name)
		}
	}
	// And a leaf that was handed no tools reads no tool guidance, which is the
	// property the monolithic prompt could not express.
	if bare := (&Linear{}).system(Task{}, nil); strings.Contains(bare, "in the words of the tools themselves") {
		t.Error("a leaf with no tools still carries the tool-guidance block")
	}
}

// noticeOf pulls the bracketed truncation notice out of a bounded result.
func noticeOf(text string) string {
	start := strings.LastIndex(text, "[Showing")
	if at := strings.LastIndex(text, "[Line "); at > start {
		start = at
	}
	if start < 0 {
		return ""
	}
	if end := strings.Index(text[start:], "]"); end >= 0 {
		return text[start : start+end+1]
	}
	return text[start:]
}
