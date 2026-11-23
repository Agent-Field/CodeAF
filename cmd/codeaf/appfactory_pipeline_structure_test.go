package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── add_step, remove_step and move_step through the engine ──────────────────
//
// editTestFile's issue pipeline is [plan approve write proof]. E5 follows the
// issue-only contract in a71691760d (0-factory-issues-only.md).

func addEdit(kind, name, after, prompt string) appwire.FactoryPipelineEdit {
	return appwire.FactoryPipelineEdit{Op: "add_step", Pipeline: kind, Name: name, After: str(after), Prompt: str(prompt)}
}

func moveEdit(kind, name, after string) appwire.FactoryPipelineEdit {
	return appwire.FactoryPipelineEdit{Op: "move_step", Pipeline: kind, Name: name, After: str(after)}
}

func TestPipelineEditStructureIsAnnounced(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	if !strings.Contains(r.hello, appwire.FeatureFactoryPipelineEditStructure) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineEditStructure, r.hello)
	}
	var caps appwire.FactoryCapabilities
	r.must("factory.capabilities", struct{}{}, &caps)
	if !caps.PipelineEditStructure || !caps.PipelineEdit || !caps.PipelineEditRuntime || caps.Writes {
		t.Fatalf("%+v", caps)
	}
}

// A structural preview is pure: it asks who holds the connection and nothing
// else, reads no file and writes nothing anywhere.
func TestStructuralPreviewReadsNothingAndWritesNothing(t *testing.T) {
	w := newGuardWire(t)
	got, err := w.previewAt(prBaseSHA, editTestFile, addEdit("issue", "lint", "write", "run the linter"), moveEdit("issue", "write", "plan"))
	if err != nil || !got.Changed || got.Structure == nil {
		t.Fatalf("%+v %v", got, err)
	}
	if w.remote.count("POST") != 0 || w.remote.count("GET /v1/repos") != 0 || w.remote.count("PUT") != 0 || w.remote.count("PATCH") != 0 || w.remote.count("DELETE") != 0 {
		t.Fatalf("a preview touched the factory: %v", w.remote.log)
	}
	s := got.Structure
	if !reflect.DeepEqual(s.Order, []appwire.FactoryPipelineOrderChange{{Pipeline: "issue",
		Before: []string{"plan", "approve", "write", "proof"}, After: []string{"plan", "write", "approve", "lint", "proof"}}}) {
		t.Fatalf("order %+v", s.Order)
	}
	if !reflect.DeepEqual(s.Added, []string{"lint"}) || len(s.Counts) != 1 || s.Counts[0] != (appwire.FactoryPipelineCount{Pipeline: "issue", Steps: 5}) {
		t.Fatalf("added %v counts %+v", s.Added, s.Counts)
	}
	// write now runs before the person answers, and the preview says so.
	if !reflect.DeepEqual(s.Gates, []appwire.FactoryPipelineGateChange{{Pipeline: "issue", Gate: "approve", Before: []string{"plan"}, After: []string{"plan", "write"}}}) {
		t.Fatalf("gates %+v", s.Gates)
	}
	if !got.CommentsDropped || !got.FormatRewritten || len(got.Steps) != 0 {
		t.Fatalf("%+v", got)
	}
}

// A structural edit that spells when, effort or fanout is a runtime edit: with
// no attestation it is refused at its own position before anything is read.
func TestAddStepWithARuntimeWordNeedsTheAttestation(t *testing.T) {
	one := 1
	for name, field := range map[string]func(*appwire.FactoryPipelineEdit){
		"effort": func(e *appwire.FactoryPipelineEdit) { e.Effort = str("cheap") },
		"when":   func(e *appwire.FactoryPipelineEdit) { e.When = str("thin") },
		"fanout": func(e *appwire.FactoryPipelineEdit) { e.Fanout = str("") },
	} {
		t.Run(name, func(t *testing.T) {
			w := newGuardWire(t)
			e := addEdit("issue", "lint", "write", "run the linter")
			field(&e)
			_, err := w.previewAt(prBaseSHA, editTestFile, moveEdit("issue", "write", "plan"), e)
			wantRefusal(t, err, rereadWords)
			wantRefusalDetail(t, err, "bad_op", &one)
			if w.remote.count("POST") != 0 || w.remote.count("GET /v1/repos") != 0 {
				t.Fatal(w.remote.log)
			}
		})
	}
	// With the factory's say-so for these bytes at this commit it previews.
	w := newGuardWire(t)
	w.attest(t, editTestFile)
	e := addEdit("issue", "lint", "write", "run the linter")
	e.Effort = str("cheap")
	got, err := w.previewAt(prBaseSHA, editTestFile, e)
	if err != nil || !strings.Contains(got.Text, "effort: cheap") {
		t.Fatalf("%+v %v", got, err)
	}
	// The same say-so does not reach another commit: the base is stale.
	_, err = w.previewAt(strings.Repeat("b", 40), editTestFile, e)
	wantRefusal(t, err, rereadWords)
}

// A base that already spells a runtime key is not reordered without the
// factory's say-so either, even by an edit that names no word itself.
func TestStructuralEditOfAnUnattestedRuntimeBaseIsRefused(t *testing.T) {
	w := newGuardWire(t)
	_, err := w.previewAt(prBaseSHA, runtimeSourceFile, moveEdit("issue", "write", "plan"))
	wantRefusal(t, err, rereadWords)
	wantRefusalDetail(t, err, "bad_op", nil)
}

// The text of a structural preview with no runtime key takes the pull request
// path it always took: one POST, no re-read.
func TestStructuralPreviewTextOpensOnThePathItAlwaysTook(t *testing.T) {
	w := newGuardWire(t)
	pv, err := w.previewAt(prBaseSHA, editTestFile, appwire.FactoryPipelineEdit{Op: "remove_step", Pipeline: "issue", Name: "plan"})
	if err != nil || !pv.Changed {
		t.Fatalf("%+v %v", pv, err)
	}
	w.remote.reset()
	if _, err := w.open(pv.Text, prBaseSHA); err != nil {
		t.Fatal(err)
	}
	if w.remote.count("POST") != 1 || w.remote.count("GET /v1/repos") != 0 {
		t.Fatalf("%v", w.remote.log)
	}
	// The POST carries the previewed bytes exactly.
	quoted, _ := json.Marshal(pv.Text)
	if !w.remote.postContains(strings.Trim(string(quoted), `"`)) {
		t.Fatalf("the POST did not carry the preview: %v", w.remote.log)
	}
}

func TestStructuralRefusalsAreTypedAndPositioned(t *testing.T) {
	w := newGuardWire(t)
	one, zero := 1, 0
	_, err := w.previewAt(prBaseSHA, editTestFile, addEdit("issue", "lint", "write", "x"), appwire.FactoryPipelineEdit{Op: "remove_step", Pipeline: "issue", Name: "proof"})
	wantRefusalDetail(t, err, "locked", &one)
	_, err = w.previewAt(prBaseSHA, editTestFile, moveEdit("issue", "write", "gone"))
	wantRefusalDetail(t, err, "unknown_step", &zero)
	_, err = w.previewAt(prBaseSHA, editTestFile, addEdit("issue", "proof", "write", "prove it"))
	wantRefusalDetail(t, err, "locked", &zero)
}

// The door a desktop uses: an old answer shape for field edits, the structure
// object only for a structural one.
func TestStructureThroughTheJSONRPCDoor(t *testing.T) {
	w := newGuardWire(t)
	raw := func(edit string) map[string]any {
		return map[string]any{"factory": w.id, "repo": prRepo, "base_sha": prBaseSHA, "base_text": editTestFile, "edits": []json.RawMessage{json.RawMessage(edit)}}
	}
	var legacy json.RawMessage
	w.must("factory.pipeline_preview", raw(`{"op":"set_step","name":"plan","prompt":"plan better"}`), &legacy)
	if strings.Contains(string(legacy), "structure") {
		t.Fatalf("%s", legacy)
	}
	var res map[string]any
	frame := rawFrame(w, "factory.pipeline_preview", raw(`{"op":"move_step","pipeline":"issue","name":"proof","after":""}`))
	if !strings.Contains(frame, `"code":"locked"`) {
		t.Fatalf("%s", frame)
	}
	w.must("factory.pipeline_preview", raw(`{"op":"move_step","pipeline":"issue","name":"write","after":"approve"}`), &res)
	s, _ := res["structure"].(map[string]any)
	for _, k := range []string{"order", "added", "counts", "gates"} {
		if _, ok := s[k].([]any); !ok {
			t.Fatalf("structure.%s is not an array: %v", k, res)
		}
	}
	if res["changed"] != false || len(s["order"].([]any)) != 0 {
		t.Fatalf("a move to where it is is no change: %v", res)
	}
}
