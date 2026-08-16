package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
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

// seededModelResolver is the surface's catalog reading reduced to what this
// rail depends on: an exact slug is never ambiguous, and any other word
// returns every model it could mean, best first.
func seededModelResolver(models ...string) ModelResolver {
	return func(words ModelWords) WorkModelChoice {
		for _, name := range words.Names {
			var partial []string
			for _, model := range models {
				base := model[strings.LastIndex(model, "/")+1:]
				switch {
				case model == name || base == name:
					return WorkModelChoice{Model: model, Requested: name}
				case strings.Contains(model, name):
					partial = append(partial, model)
				}
			}
			if len(partial) == 1 {
				return WorkModelChoice{Model: partial[0], Requested: name}
			}
			if len(partial) > 1 {
				return WorkModelChoice{Candidates: partial, Requested: name}
			}
		}
		requested := ""
		if len(words.Names) > 0 {
			requested = words.Names[0]
		}
		return WorkModelChoice{Requested: requested}
	}
}

const kimiAsk = "okay use kimi 3 model to look at our aforge and ideate various " +
	"=featrures we can build on top of it after underdtand the philosophy dont buikd anything yet"

func kimiCompiler(t *testing.T) *Compiler {
	t.Helper()
	client := &fakeClient{responses: []string{
		`{"goal":"Study aforge and ideate features.","deliverable":"an idea list","budget":"$0.40","assumptions":["Read the repo first"]}`,
	}}
	return NewCompiler(client).WithModelResolver(seededModelResolver(
		"moonshotai/kimi-k2", "moonshotai/kimi-k2-thinking", "google/gemini-3-pro"))
}

func TestAmbiguousModelWordAsksOnceThenEveryAnswerSettlesIt(t *testing.T) {
	asked, err := kimiCompiler(t).Compile(context.Background(), kimiAsk, "")
	if err != nil {
		t.Fatal(err)
	}
	if asked.Question != "Which kimi do you mean?" || len(asked.QuestionOptions) != 2 {
		t.Fatalf("first compile = %+v", asked)
	}
	if asked.QuestionOptions[0].Value != "moonshotai/kimi-k2" ||
		asked.QuestionOptions[1].Value != "moonshotai/kimi-k2-thinking" {
		t.Fatalf("options do not carry exact slugs: %+v", asked.QuestionOptions)
	}

	for _, test := range []struct {
		name   string
		answer string
		model  string
		note   string
	}{
		{
			name: "option value", answer: asked.QuestionOptions[1].Value,
			model: "moonshotai/kimi-k2-thinking", note: "Running on moonshotai/kimi-k2-thinking.",
		},
		{
			name: "option label", answer: asked.QuestionOptions[0].Label,
			model: "moonshotai/kimi-k2", note: "Running on moonshotai/kimi-k2.",
		},
		{
			// Free text that still fits both models proceeds on the best one
			// rather than asking the settled question a second time.
			name: "free text", answer: "kimi 3", model: "moonshotai/kimi-k2",
			note: `Went with moonshotai/kimi-k2 — say "use moonshotai/kimi-k2-thinking" to switch.`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			brief, err := kimiCompiler(t).Compile(context.Background(),
				SpliceCompilerAnswer(kimiAsk, test.answer), "")
			if err != nil {
				t.Fatal(err)
			}
			if brief.Question != "" {
				t.Fatalf("an answered question was asked again: %q", brief.Question)
			}
			if brief.WorkModel != test.model || brief.ModelNote != test.note {
				t.Fatalf("answer %q pinned %q with note %q", test.answer, brief.WorkModel, brief.ModelNote)
			}
		})
	}
}

func TestStackedAnswersNeverReopenTheModelQuestion(t *testing.T) {
	// The transcript that started this: every answer re-spliced the original
	// words, and the original words asked again. The last answer is the live
	// one and the ask is spent whatever it says.
	instruction := SpliceCompilerAnswer(SpliceCompilerAnswer(kimiAsk, "kimi 3"), "1")
	brief, err := kimiCompiler(t).Compile(context.Background(), instruction, "")
	if err != nil {
		t.Fatal(err)
	}
	if brief.Question != "" {
		t.Fatalf("stacked answers asked again: %q", brief.Question)
	}
	if brief.WorkModel != "moonshotai/kimi-k2" {
		t.Fatalf("stacked answers pinned %q", brief.WorkModel)
	}
	if answers := answeredCompilerQuestions(instruction); len(answers) != 2 ||
		answers[0] != "kimi 3" || answers[1] != "1" {
		t.Fatalf("answers read back as %v", answers)
	}
}

func TestCompilerAnswerFromOptionPrefersTheExactValueOnlyWhenTheLabelWrapsIt(t *testing.T) {
	for _, test := range []struct {
		option store.QuestionOption
		want   string
	}{
		{option: store.QuestionOption{Label: "use moonshotai/kimi-k2", Value: "moonshotai/kimi-k2"},
			want: "moonshotai/kimi-k2"},
		// A value that is an internal code leaves the human label alone.
		{option: store.QuestionOption{Label: "London", Value: "uk"}, want: "London"},
		{option: store.QuestionOption{Label: "yes, stand this up", Value: "ratify"}, want: "yes, stand this up"},
		{option: store.QuestionOption{Value: "moonshotai/kimi-k2"}, want: "moonshotai/kimi-k2"},
	} {
		if got := compilerAnswerFromOption(test.option); got != test.want {
			t.Fatalf("answer for %+v = %q, want %q", test.option, got, test.want)
		}
	}
}

// TestModelChoiceRoundTripsThroughTheRealAnswerRailExactlyOnce walks the
// transcript that started this: the ask, the choose question, the user's "1",
// and the work. The rail must journal one question and then get on with it.
func TestModelChoiceRoundTripsThroughTheRealAnswerRailExactlyOnce(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"goal":"Study aforge and ideate features.","deliverable":"an idea list","budget":"$0.40","assumptions":["Read the repo first"]}`,
	}}
	compiler := NewCompiler(client).WithModelResolver(seededModelResolver(
		"moonshotai/kimi-k2", "moonshotai/kimi-k2-thinking", "google/gemini-3-pro"))
	reconciler := resident.New(graph,
		func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
			brief, err := compiler.Compile(ctx, instruction, graphContext)
			if err != nil {
				return resident.Compiled{}, err
			}
			return resident.Compiled{
				Goal: brief.Goal, Assumptions: brief.Assumptions, Question: brief.Question,
				QuestionOptions: brief.QuestionOptions, WorkModel: brief.WorkModel,
				ModelNote: brief.ModelNote,
			}, nil
		}, nil)

	original, err := graph.RequestCommand(store.Command{
		SessionID: "kimi", Kind: store.CommandSplice, Instruction: kimiAsk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	question := waitForAgentReply(t, graph, "kimi", original.Seq)
	if len(question.Options) != 2 || question.Options[0].Value != "moonshotai/kimi-k2" {
		t.Fatalf("question options = %+v", question.Options)
	}

	user, err := graph.PostMessage(store.Message{SessionID: "kimi", Role: store.RoleUser, Body: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	messages, err := graph.Messages("kimi", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	questions := 0
	for _, message := range messages {
		if message.Role == store.RoleAgent && len(message.Options) > 0 {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("the rail asked %d times:\n%+v", questions, messages)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	pinned := ""
	for _, node := range nodes {
		if node.Parent == store.RootID && node.Provenance.WorkModel != "" {
			pinned = node.Provenance.WorkModel
		}
	}
	if pinned != "moonshotai/kimi-k2" {
		t.Fatalf("the answered choice did not reach the work: %q", pinned)
	}
}
