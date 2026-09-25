package exec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// attributionStrings are the bytes that are the feature. They are pinned here
// rather than described, because a reworded trailer does not attribute and a
// footer that lost a utm parameter cannot be counted — either would pass a test
// that only looked for the word "attribution".
var attributionStrings = []string{
	"Co-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>",
	"Drafted with [CodeAF](https://agentfield.ai/github/codeaf?utm_source=github&utm_medium=pull_request&utm_campaign=drafted_with) · reviewed and owned by the author",
	"Drafted with [CodeAF](https://agentfield.ai/github/codeaf?utm_source=github&utm_medium=issue&utm_campaign=drafted_with) · reviewed and owned by the author",
	// THE COMMENT LINE IS PINNED THE HARDEST OF THE FOUR, because every part of
	// it is doing a job that is easy to edit away: `<sub>` is what makes it muted
	// rather than a shout in somebody's thread, the lowercase `drafted` is what
	// keeps it a remark rather than a heading, `utm_medium=comment` is what makes
	// a comment countable apart from a body, and there is deliberately no
	// "reviewed and owned by the author" tail.
	"<sub>drafted with [CodeAF](https://agentfield.ai/github/codeaf?utm_source=github&utm_medium=comment&utm_campaign=drafted_with)</sub>",
}

func TestAttributionConstantsAreTheExactStrings(t *testing.T) {
	for index, want := range attributionStrings {
		got := []string{AttributionTrailer, AttributionPullFooter, AttributionIssueFooter, AttributionCommentFooter}[index]
		if got != want {
			t.Fatalf("constant %d = %q, want %q", index, got, want)
		}
	}
	if AttributionSeparator != "—" {
		t.Fatalf("separator = %q, want an em dash", AttributionSeparator)
	}
	if AttributionAssistedBy != "Assisted-by: CodeAF" {
		t.Fatalf("assisted-by = %q, want %q", AttributionAssistedBy, "Assisted-by: CodeAF")
	}
}

// THE BARE NAME IS THE MODEL AND NOTHING ABOUT WHO SERVED IT. Every id here is
// one the catalog or the router hands codeaf. The provider or company comes
// off, and so does a routing suffix; the model's own version or date, and a
// local model's size tag, stay.
func TestBareModelNameKeepsOnlyTheModel(t *testing.T) {
	for _, row := range []struct{ id, want string }{
		{"deepseek/deepseek-v4-flash", "deepseek-v4-flash"},
		{"qwen/qwen3-coder", "qwen3-coder"},
		{"z-ai/glm-5.3", "glm-5.3"},
		{"moonshotai/kimi-k3", "kimi-k3"},
		{"minimax/minimax-m2.7", "minimax-m2.7"},
		{"mistralai/mistral-nemo", "mistral-nemo"},
		// The model's own version or date is the model, not routing.
		{"deepseek/deepseek-v4-flash-0731", "deepseek-v4-flash-0731"},
		{"deepseek/deepseek-v4-flash-20260731", "deepseek-v4-flash-20260731"},
		{"qwen/qwen3.5-vl-32b-instruct", "qwen3.5-vl-32b-instruct"},
		{"moonshotai/kimi-k2-thinking", "kimi-k2-thinking"},
		// OpenRouter's alias marker is the router's, and so is the prefix; the
		// pointer itself is what the person picked.
		{"~deepseek/deepseek-v4-flash-latest", "deepseek-v4-flash-latest"},
		// Routing suffixes: how the request was routed, or how hard to think.
		{"qwen/qwen3-coder:free", "qwen3-coder"},
		{"z-ai/glm-5.3:nitro", "glm-5.3"},
		{"inclusionai/ling-3.0-tiny:free", "ling-3.0-tiny"},
		{"nvidia/nemotron-3.5-lightning:free", "nemotron-3.5-lightning"},
		{"moonshotai/kimi-k3:high", "kimi-k3"},
		{"deepseek/deepseek-v4-flash-latest:high", "deepseek-v4-flash-latest"},
		{"qwen/qwen3-coder:free:nitro", "qwen3-coder"},
		// A size tag is which weights ran, and a suffix this build was never
		// taught is kept rather than guessed at.
		{"ollama/qwen3:32b", "qwen3:32b"},
		{"ollama/llama3.2", "llama3.2"},
		// Already bare, padded, or nothing at all.
		{"deepseek-v4-flash", "deepseek-v4-flash"},
		{"  z-ai/glm-5.3  ", "glm-5.3"},
		{"", ""},
		{"qwen/", ""},
	} {
		if got := BareModelName(row.id); got != row.want {
			t.Errorf("BareModelName(%q) = %q, want %q", row.id, got, row.want)
		}
	}
}

// THE TRAILER BLOCK IS TWO EXACT LINES, and with no model to name it is the
// bare line — never an empty `()`.
func TestTheTrailerBlockIsTwoExactLinesWithOrWithoutTheModel(t *testing.T) {
	const coAuthor = "Co-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>"
	for _, row := range []struct{ model, want string }{
		{"deepseek/deepseek-v4-flash", "Assisted-by: CodeAF (deepseek-v4-flash)\n" + coAuthor},
		{"qwen/qwen3-coder:free", "Assisted-by: CodeAF (qwen3-coder)\n" + coAuthor},
		{"", "Assisted-by: CodeAF\n" + coAuthor},
		{"qwen/", "Assisted-by: CodeAF\n" + coAuthor},
	} {
		if got := AttributionTrailers(row.model); got != row.want {
			t.Errorf("AttributionTrailers(%q) = %q, want %q", row.model, got, row.want)
		}
	}
	signed := SignCommitMessage("task: write the report\n\n\n", "z-ai/glm-5.3")
	if want := "task: write the report\n\nAssisted-by: CodeAF (glm-5.3)\n" + coAuthor; signed != want {
		t.Fatalf("signed message = %q, want %q", signed, want)
	}
}

// THE SETTINGS ROW'S HINT IS THE TWO LINES THIS PACKAGE WRITES. internal/config
// cannot import this package, so it spells them; this holds its spelling to the
// one that reaches a commit.
func TestTheModelNameRowsHintIsTheTwoLines(t *testing.T) {
	want := "On, commits say `" + AssistedBy("<model>") + "`; off, `" + AttributionAssistedBy + "`."
	if config.AttributionModelHint != want {
		t.Fatalf("the attribution.model hint = %q, want %q", config.AttributionModelHint, want)
	}
}

// THE LAW IS IN EVERY CONTRACT, because signing has no off. What the surface
// hands the loop decides only whether the `Assisted-by` line names a model.
func TestAttributionLawIsInEveryContract(t *testing.T) {
	named := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute).
		WithAssistedBy("qwen/qwen3-coder").system(Task{NodeID: 1, Brief: "work"}, nil)
	for _, want := range attributionStrings {
		if !strings.Contains(named, want) {
			t.Fatalf("the contract is missing %q", want)
		}
	}
	if want := "`Assisted-by: CodeAF (qwen3-coder)` and `" + AttributionTrailer + "` as its last two lines"; !strings.Contains(named, want) {
		t.Fatalf("the contract does not spell both trailer lines in order: want %q", want)
	}
	for _, want := range []string{"CONTRIBUTING", "commit subject", "README"} {
		if !strings.Contains(named, want) {
			t.Fatalf("the contract does not say where attribution must not go: %q", want)
		}
	}
	// AND THE BOUND ON THE COMMENT LINE, which is the whole difference between
	// provenance and advertising: the first comment in a thread carries it and
	// no later one does.
	for _, want := range []string{"ONCE per thread", "one-liner", "dictated"} {
		if !strings.Contains(named, want) {
			t.Fatalf("the contract does not bound the comment line: %q", want)
		}
	}

	// A LOOP HANDED NO MODEL STILL SIGNS, with the bare line.
	unnamed := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute).
		system(Task{NodeID: 1, Brief: "work"}, nil)
	if want := "`Assisted-by: CodeAF` and `" + AttributionTrailer + "`"; !strings.Contains(unnamed, want) {
		t.Fatalf("a loop handed no model does not carry the bare line: want %q", want)
	}
	for _, page := range []string{named, unnamed} {
		for _, unwanted := range []string{AttributionAssistedBySlot, "CodeAF ()"} {
			if strings.Contains(page, unwanted) {
				t.Fatalf("the contract carries %q", unwanted)
			}
		}
	}
}

// The law is unconditional: a reflex micro-leaf and a contracted job carry it
// too, because nothing detects in advance whether a job will touch git.
func TestAttributionRidesEveryShapeOfLeafToTheModel(t *testing.T) {
	for _, task := range []Task{
		{NodeID: 1, Brief: "work"},
		{NodeID: 2, Brief: "work", Reflex: true},
		{NodeID: 3, Brief: "work", Contract: "read the diff first"},
	} {
		client := &scriptedCompleter{}
		linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute).WithAssistedBy("deepseek/deepseek-v4-flash")
		if _, err := linear.Run(context.Background(), task); err != nil {
			t.Fatal(err)
		}
		if len(client.seen) == 0 {
			t.Fatal("the model was never called")
		}
		system := client.seen[0][0].Content[0].Text
		for _, want := range append(attributionStrings, "Assisted-by: CodeAF (deepseek-v4-flash)") {
			if !strings.Contains(system, want) {
				t.Fatalf("node %d never saw %q", task.NodeID, want)
			}
		}
	}
}
