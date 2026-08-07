package head

import (
	"context"
	"strings"
	"testing"
)

func TestModelWordRecognitionTable(t *testing.T) {
	tests := []struct {
		instruction string
		boost       bool
		name        string
		explicit    bool
		recognized  bool
	}{
		{instruction: "summarize the repo with the better model", boost: true, recognized: true},
		{instruction: "use the boost model for this one", boost: true, recognized: true},
		{instruction: "draft the memo with model 2", boost: true, recognized: true},
		{instruction: "run it with the stronger model", boost: true, recognized: true},
		{instruction: "benchmark the parser with the gemini model", name: "gemini", explicit: true, recognized: true},
		{instruction: "use model google/gemini-3-pro for the writeup", name: "google/gemini-3-pro", explicit: true, recognized: true},
		{instruction: "write the report using opus", name: "opus", recognized: true},
		{instruction: "review the diff with claude-opus-5", name: "claude-opus-5", recognized: true},
		{instruction: "summarize this file", recognized: false},
		{instruction: "handle it with care", recognized: false},
		{instruction: "use the same approach as before", recognized: false},
	}
	for _, test := range tests {
		t.Run(test.instruction, func(t *testing.T) {
			words, recognized := RecognizeModelWords(test.instruction)
			if recognized != test.recognized {
				t.Fatalf("recognized = %t, want %t (%+v)", recognized, test.recognized, words)
			}
			if !recognized {
				return
			}
			if words.Boost != test.boost || words.Explicit != test.explicit {
				t.Fatalf("words = %+v", words)
			}
			if test.name != "" && (len(words.Names) == 0 || words.Names[0] != test.name) {
				t.Fatalf("names = %v, want %q first", words.Names, test.name)
			}
		})
	}
}

func TestQualityWordRecognitionTable(t *testing.T) {
	for instruction, want := range map[string]bool{
		"make the cover art, best quality please":     true,
		"this is the final deliverable, make it good": true,
		"render a production-quality clip":            true,
		"make a quick sketch of the logo":             false,
	} {
		if got := RecognizesQualityIntent(instruction); got != want {
			t.Fatalf("quality(%q) = %t, want %t", instruction, got, want)
		}
	}
}

func compilerWithChoice(t *testing.T, choice WorkModelChoice) (*Compiler, *fakeClient, *[]ModelWords) {
	t.Helper()
	client := &fakeClient{responses: []string{
		`{"goal":"Benchmark the parser.","deliverable":"a benchmark table","budget":"$0.40","assumptions":["Use the current checkout"]}`,
	}}
	var seen []ModelWords
	compiler := NewCompiler(client).WithModelResolver(func(words ModelWords) WorkModelChoice {
		seen = append(seen, words)
		return choice
	})
	return compiler, client, &seen
}

func TestBoostWordResolvesToTheBoostSlotAndIsJournaledOnTheBrief(t *testing.T) {
	compiler, _, seen := compilerWithChoice(t, WorkModelChoice{
		Model: "anthropic/claude-opus-5", Requested: "the boost model",
	})
	brief, err := compiler.Compile(context.Background(), "benchmark the parser with the better model", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || !(*seen)[0].Boost {
		t.Fatalf("resolver saw %+v", *seen)
	}
	if brief.WorkModel != "anthropic/claude-opus-5" {
		t.Fatalf("work model = %q", brief.WorkModel)
	}
	if brief.ModelNote != "Running on anthropic/claude-opus-5." {
		t.Fatalf("receipt line = %q", brief.ModelNote)
	}
}

func TestNamedModelResolvesAndOrdinaryAsksCarryNoModelAtAll(t *testing.T) {
	compiler, _, _ := compilerWithChoice(t, WorkModelChoice{
		Model: "google/gemini-3-pro", Requested: "gemini",
	})
	brief, err := compiler.Compile(context.Background(), "benchmark the parser with the gemini model", "")
	if err != nil {
		t.Fatal(err)
	}
	if brief.WorkModel != "google/gemini-3-pro" || brief.ModelNote != "Running on google/gemini-3-pro." {
		t.Fatalf("named resolution = %+v", brief)
	}

	plain := &fakeClient{responses: []string{
		`{"goal":"Benchmark the parser.","deliverable":"a table","budget":"$0.40","assumptions":["Use the current checkout"]}`,
	}}
	resolved := 0
	ordinary, err := NewCompiler(plain).WithModelResolver(func(ModelWords) WorkModelChoice {
		resolved++
		return WorkModelChoice{}
	}).Compile(context.Background(), "benchmark the parser", "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 0 || ordinary.WorkModel != "" || ordinary.ModelNote != "" {
		t.Fatalf("ordinary ask touched model resolution: resolved=%d brief=%+v", resolved, ordinary)
	}
}

func TestAmbiguousModelNameAsksOnceAndNeverCallsTheProvider(t *testing.T) {
	compiler, client, _ := compilerWithChoice(t, WorkModelChoice{
		Requested: "gemini", Candidates: []string{"google/gemini-3-pro", "google/gemini-3-flash"},
	})
	brief, err := compiler.Compile(context.Background(), "benchmark the parser with the gemini model", "")
	if err != nil {
		t.Fatal(err)
	}
	if brief.Question != "Which gemini do you mean?" || len(brief.QuestionOptions) != 2 {
		t.Fatalf("ambiguity question = %+v", brief)
	}
	// The option label is the ask itself, so the answer re-compiles into an
	// exact resolution through the ordinary compiler-question rail.
	if brief.QuestionOptions[0].Label != "use google/gemini-3-pro" ||
		brief.QuestionOptions[0].Value != "google/gemini-3-pro" {
		t.Fatalf("options = %+v", brief.QuestionOptions)
	}
	if _, recognized := RecognizeModelWords(
		"benchmark the parser\n\nAnswer to compiler question: " + brief.QuestionOptions[0].Label); !recognized {
		t.Fatal("the answer does not read back as a model word")
	}
	if client.callCount() != 0 {
		t.Fatalf("provider was called %d times for an ambiguous name", client.callCount())
	}
	if brief.WorkModel != "" {
		t.Fatalf("ambiguous name pinned a model: %q", brief.WorkModel)
	}
}

func TestUnresolvableModelNameIsOneCalmLineAndTheJobStillRuns(t *testing.T) {
	compiler, _, _ := compilerWithChoice(t, WorkModelChoice{Requested: "gemini-9"})
	brief, err := compiler.Compile(context.Background(), "benchmark the parser with the gemini-9 model", "")
	if err != nil {
		t.Fatal(err)
	}
	if brief.Question != "" || brief.WorkModel != "" {
		t.Fatalf("unresolvable name blocked the job: %+v", brief)
	}
	if brief.ModelNote != `I don't have a model matching "gemini-9" — running on the usual one.` {
		t.Fatalf("receipt line = %q", brief.ModelNote)
	}
	if !strings.Contains(brief.Goal, "Benchmark the parser.") {
		t.Fatalf("goal = %q", brief.Goal)
	}

	// A bare word that resolves to nothing was probably never a model word.
	quiet, _, _ := compilerWithChoice(t, WorkModelChoice{Requested: "opus"})
	brief, err = quiet.Compile(context.Background(), "benchmark the parser using opus", "")
	if err != nil {
		t.Fatal(err)
	}
	if brief.ModelNote != "" {
		t.Fatalf("bare unresolved word spoke up: %q", brief.ModelNote)
	}
}

func TestQualityWordsReachTheBriefAsOneSentence(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Produce the cover art.","deliverable":"a cover image","budget":"$0.50","assumptions":["Square crop"]}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(),
		"make the cover art, best quality please", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(brief.Goal, qualityBriefSentence) {
		t.Fatalf("goal = %q", brief.Goal)
	}
	plain := &fakeClient{responses: []string{
		`{"goal":"Produce the cover art.","deliverable":"a cover image","budget":"$0.50","assumptions":["Square crop"]}`,
	}}
	routine, err := NewCompiler(plain).Compile(context.Background(), "make a quick sketch of the logo", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(routine.Goal, "model:") {
		t.Fatalf("routine goal carries a quality sentence: %q", routine.Goal)
	}
}
