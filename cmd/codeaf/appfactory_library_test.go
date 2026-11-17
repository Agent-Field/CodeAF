package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// THE LIBRARY IS READ, PREVIEWED AND WRITTEN THROUGH THE APP, END TO END
// against the real factory handler: empty at first, a preview of
// add_pipeline answers the text a write keeps, the write keeps it, a stale
// version writes nothing, and every read the factory makes afterwards
// resolves the library's pipeline beside the repository's file.
func TestFactoryLibraryIsReadPreviewedAndWrittenThroughTheApp(t *testing.T) {
	w, id, st := spendingWire(t)
	for _, word := range []string{appwire.FeatureFactoryLibrary, appwire.FeatureFactoryPipelineEditPipelines} {
		if !strings.Contains(w.hello, `"`+word+`"`) {
			t.Fatalf("hello does not announce %s", word)
		}
	}
	var lib appwire.FactoryLibrary
	w.must("factory.library", map[string]any{"factory": id}, &lib)
	if lib.Text != "" || lib.Version != 1 || !lib.Can.Change || len(lib.Pipelines) != 0 {
		t.Fatalf("a new factory's library %+v", lib)
	}
	var preview appwire.FactoryLibraryPreview
	w.must("factory.library_preview", map[string]any{"factory": id, "base_text": "", "edits": []map[string]any{
		{"op": "add_pipeline", "name": "docs", "steps": []string{"write", "proof"}},
		{"op": "add_step", "pipeline": "docs", "name": "docs-check", "after": "write", "prompt": "build the docs"},
	}}, &preview)
	if !preview.Changed || preview.Structure == nil || len(preview.Structure.Pipelines) != 1 || preview.Structure.Pipelines[0].Pipeline != "docs" {
		t.Fatalf("preview %+v", preview)
	}
	want := "pipeline:\n    docs:\n        - write\n        - docs-check\n        - proof\nsteps:\n    docs-check:\n        prompt: build the docs\n"
	if preview.Text != want {
		t.Fatalf("preview text\n%s\nwant\n%s", preview.Text, want)
	}
	// A repository's own verbs are not the library's.
	if _, code, _ := w.call("factory.library_preview", map[string]any{"factory": id, "base_text": "", "edits": []map[string]any{{"op": "set_budget", "value": 3}}}); code != appwire.CodeBadParams {
		t.Fatalf("set_budget on the library answered %q", code)
	}
	w.must("factory.library_set", map[string]any{"factory": id, "text": preview.Text, "expect": map[string]any{"version": 1}}, &lib)
	if lib.Text != want || lib.Version != 2 || len(lib.Pipelines) != 1 || lib.Pipelines[0].Name != "docs" || lib.Pipelines[0].From != "library" {
		t.Fatalf("saved %+v", lib)
	}
	if _, code, _ := w.call("factory.library_set", map[string]any{"factory": id, "text": "", "expect": map[string]any{"version": 1}}); code != appwire.CodeStale {
		t.Fatalf("a stale write answered %q", code)
	}
	if _, code, _ := w.call("factory.library_set", map[string]any{"factory": id, "expect": map[string]any{"version": 2}}); code != appwire.CodeBadParams {
		t.Fatalf("a write with no text answered %q", code)
	}
	// Every read the factory makes now has the library beside it.
	f, err := withFactoryHouseRules(st, pipeline.Defaults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res, ok := f.Resolve("docs"); !ok || res.From != pipeline.FromLibrary || len(res.Stages) != 3 {
		t.Fatalf("docs resolves %+v %v", res, ok)
	}
	own, err := pipeline.Parse("pipeline:\n  docs: [plan, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	if f, _ = withFactoryHouseRules(st, own, nil); f.Pipelines()[1].From != pipeline.FromRepoFile {
		t.Fatalf("the repository's docs did not win: %+v", f.Pipelines())
	}
}
