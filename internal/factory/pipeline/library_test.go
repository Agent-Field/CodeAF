package pipeline

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const libraryText = `pipeline:
  docs: [write, docs-check, proof]
  quick: [write, proof]
steps:
  docs-check:
    prompt: build the docs and read what changed
  write:
    prompt: write it the library's way
`

func mustLibrary(t *testing.T, text string) File {
	t.Helper()
	lib, err := ParseLibrary(text)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func stageNames(stages []factory.Stage) string {
	names := make([]string, len(stages))
	for i, st := range stages {
		names[i] = st.Name
	}
	return strings.Join(names, ",")
}

// THE LIBRARY IS KEPT AS WHAT WAS WRITTEN: the encoder writes the two pipelines
// and the two steps the text wrote, and none of the shipped steps it names but
// leaves alone (proof here).
func TestLibraryRoundTripsThroughEncodeWithOnlyWhatWasWritten(t *testing.T) {
	data, refused := CheckLibrary(libraryText)
	if refused != "" {
		t.Fatal(refused)
	}
	want := `pipeline:
    docs:
        - write
        - docs-check
        - proof
    quick:
        - write
        - proof
steps:
    docs-check:
        prompt: build the docs and read what changed
    write:
        prompt: write it the library's way
`
	if string(data) != want {
		t.Fatalf("got\n%s\nwant\n%s", data, want)
	}
	again, refused := CheckLibrary(string(data))
	if refused != "" || string(again) != want {
		t.Fatalf("not a fixed point: %q %s", refused, again)
	}
	for _, shipped := range []string{"plan", "review", "bisect", "fixed", "rules", "proof:"} {
		if strings.Contains(string(data), shipped) {
			t.Fatalf("the library wrote %q, which nobody wrote:\n%s", shipped, data)
		}
	}
	if empty, refused := CheckLibrary("  \n"); refused != "" || empty != nil {
		t.Fatalf("an empty library is %q %q", empty, refused)
	}
	if empty, refused := CheckLibrary("pipeline: {}\n"); refused != "" || empty != nil {
		t.Fatalf("a library that writes nothing is %q %q", empty, refused)
	}
}

func TestLibraryRefusesWhatIsNotAPipelineOrAStep(t *testing.T) {
	for text, want := range map[string]string{
		"start:\n  labels: [go]\n":                                                            "start belongs to a repository's file",
		"pipeline:\n  pr: [read, proof]\n":                                                    "PR jobs",
		"pipeline:\n  ci: [bisect, proof]\n":                                                  "CI jobs",
		"pipeline:\n  docs: [write]\nrules: [x]\n":                                            "rules belongs to a repository's file",
		"pipeline:\n  docs: [write]\nsteps: [x]\n":                                            "steps",
		"pipeline:\n  docs: [abcdefghijklmnop]\nsteps:\n  abcdefghijklmnop:\n    prompt: p\n": "would be skipped",
		"- just a list\n":                                                                     "a map of pipeline and steps",
	} {
		if _, refused := CheckLibrary(text); !strings.Contains(refused, want) {
			t.Errorf("%q refused %q, want %q", text, refused, want)
		}
	}
}

// RESOLUTION ORDER: the repository's file (or the factory's copy of it), then
// the library, then the built-in steps; and a library pipeline runs the
// library's words, never a repository step of the same name.
func TestResolutionOrderIsRepoThenLibraryThenBuiltIn(t *testing.T) {
	lib := mustLibrary(t, strings.Replace(libraryText, "  quick: [write, proof]\n", "  quick: [write, proof]\n  issue: [write, proof]\n", 1))
	repo, err := Parse(`pipeline:
  quick: [plan, write, proof]
steps:
  write:
    prompt: write it the repository's way
`)
	if err != nil {
		t.Fatal(err)
	}
	f := WithLibrary(repo, lib)

	quick, ok := f.Resolve("quick")
	if !ok || quick.From != FromRepoFile || joined(quick.Steps) != "plan,write,proof" || !reflect.DeepEqual(quick.Over, []string{FromLibrary}) {
		t.Fatalf("a repository pipeline of the same name wins: %+v", quick)
	}
	docs, ok := f.Resolve("docs")
	if !ok || docs.From != FromLibrary || stageNames(docs.Stages) != "write,docscheck,proof" || docs.Over != nil {
		t.Fatalf("a library-only pipeline: %+v", docs)
	}
	if docs.Stages[0].Ask != "write it the library's way" {
		t.Fatalf("a library pipeline ran the repository's step: %q", docs.Stages[0].Ask)
	}
	if quick.Stages[1].Ask != "write it the repository's way" {
		t.Fatalf("a repository pipeline ran the library's step: %q", quick.Stages[1].Ask)
	}
	// The repository's file does not list issue, so the library's issue wins
	// over the built-in one.
	issue, ok := f.Resolve("issue")
	if !ok || issue.From != FromLibrary || joined(issue.Steps) != "write,proof" || !reflect.DeepEqual(issue.Over, []string{FromBuiltIn}) {
		t.Fatalf("issue %+v", issue)
	}
	if _, ok := f.Resolve("nowhere"); ok {
		t.Fatal("a name no layer has resolved")
	}

	// THE FACTORY'S COPY stands where the repository's file does.
	copied, _ := CopyFile(factory.PipelineCopy{Repo: "o/r", Text: "pipeline:\n  docs: [plan, proof]\n", By: "op", At: time.Now()})
	cf := WithLibrary(copied, lib)
	if r, _ := cf.Resolve("docs"); r.From != FromFactoryCopy || joined(r.Steps) != "plan,proof" || !reflect.DeepEqual(r.Over, []string{FromLibrary}) {
		t.Fatalf("copy %+v", r)
	}

	// The recipe every other door reads resolves the same way.
	r := f.Recipe()
	if stageNames(r.Named("docs")) != "write,docscheck,proof" || stageNames(r.Named("quick")) != "plan,write,proof" || stageNames(r.Stages) != "write,proof" {
		t.Fatalf("recipe %v / %v / %v", stageNames(r.Named("docs")), stageNames(r.Named("quick")), stageNames(r.Stages))
	}
	if !reflect.DeepEqual(f.Named("docs"), r.Named("docs")) {
		t.Fatal("Named and the recipe disagree")
	}

	// A repository that lists its own issue pipeline keeps it.
	own := WithLibrary(reread(t, "pipeline:\n  issue: [plan, write, proof]\n"), lib)
	if r, _ := own.Resolve("issue"); r.From != FromRepoFile || joined(r.Steps) != "plan,write,proof" {
		t.Fatalf("own issue %+v", r)
	}
}

// EVERY PIPELINE A JOB CAN RUN is listed once, from all the layers, issue first.
func TestPipelinesListsEveryChoiceWithWhereItCameFrom(t *testing.T) {
	repo := reread(t, "pipeline:\n  quick: [plan, proof]\n")
	got := WithLibrary(repo, mustLibrary(t, libraryText)).Pipelines()
	var lines []string
	for _, c := range got {
		lines = append(lines, c.Name+"="+c.From)
	}
	if g := strings.Join(lines, " "); g != "issue=built_in docs=library quick=repo" {
		t.Fatalf("%s", g)
	}
}

// NO LIBRARY, NOTHING CHANGES: a read with an empty library is the read
// without one, recipe for recipe.
func TestAnEmptyLibraryChangesNothing(t *testing.T) {
	for _, base := range []File{Defaults(), reread(t, editBase)} {
		with := WithLibrary(base, mustLibrary(t, ""))
		if !reflect.DeepEqual(with.Recipe(), base.Recipe()) {
			t.Fatal("an empty library changed the recipe")
		}
		if _, ok := with.Library(); ok {
			t.Fatal("an empty library was kept")
		}
	}
	r, ok := Defaults().Resolve("issue")
	if !ok || r.From != FromBuiltIn || !reflect.DeepEqual(r.Stages, factory.DefaultRecipe().For(factory.KindIssue)) {
		t.Fatalf("%+v", r)
	}
}

// THE LIBRARY IS EDITED WITH THE SAME VERBS, and only those that touch its
// two keys.
func TestLibraryEditedMakesAPipelineAndRefusesRepositoryVerbs(t *testing.T) {
	ed, err := LibraryEdited("", []EditOp{addPipeline("docs", "write", "proof"), add("docs", "docs-check", "write", "check the docs")})
	if err != nil {
		t.Fatal(err)
	}
	lib := mustLibrary(t, ed.Text)
	if joined(lib.Pipeline["docs"]) != "write,docs-check,proof" || strings.Contains(ed.Text, "issue") {
		t.Fatalf("%s", ed.Text)
	}
	if _, err := LibraryEdited(ed.Text, []EditOp{{Op: OpSetBudget, Value: f(3)}}); err == nil {
		t.Fatal("a budget was set on the library")
	}
	gone, err := LibraryEdited(ed.Text, []EditOp{removePipeline("docs")})
	if err != nil || gone.Text != "" && !strings.HasPrefix(gone.Text, "steps:") {
		t.Fatalf("%v %q", err, gone.Text)
	}
}
