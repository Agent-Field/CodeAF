package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func wordEdit(step string, when, effort, fanout *string) appwire.FactoryPipelineEdit {
	return appwire.FactoryPipelineEdit{Op: "set_step", Name: step, When: when, Effort: effort, Fanout: fanout}
}

func wantRefusalDetail(t *testing.T, err error, code string, op *int) {
	t.Helper()
	var we *appwire.Error
	if !errors.As(err, &we) {
		t.Fatalf("%v", err)
	}
	d, ok := we.Detail.(appwire.FactoryPipelineEditRefusal)
	if !ok || d.Code != code || (op == nil) != (d.Op == nil) || (op != nil && *op != *d.Op) {
		t.Fatalf("%+v", we.Detail)
	}
}

func TestFirstRuntimeEditCountsAnExplicitEmptyValue(t *testing.T) {
	if firstRuntimeEdit(nil) != -1 || firstRuntimeEdit([]appwire.FactoryPipelineEdit{promptEdit("plan", "x")}) != -1 {
		t.Fatal("a prompt edit is not a runtime edit")
	}
	for _, e := range []appwire.FactoryPipelineEdit{wordEdit("plan", str(""), nil, nil), wordEdit("plan", nil, str(""), nil), wordEdit("plan", nil, nil, str(""))} {
		if got := firstRuntimeEdit([]appwire.FactoryPipelineEdit{promptEdit("plan", "x"), e}); got != 1 {
			t.Fatalf("%+v: %d", e, got)
		}
	}
}

func TestUnattestedRuntimeEditsAreRefusedBeforeAnythingIsRead(t *testing.T) {
	zero := 0
	for name, e := range map[string]appwire.FactoryPipelineEdit{
		"set when":     wordEdit("plan", str("thin"), nil, nil),
		"clear fanout": wordEdit("plan", nil, nil, str("")),
		"clear effort": wordEdit("plan", nil, str(""), nil),
		"set default":  wordEdit("plan", nil, nil, str("one")),
	} {
		t.Run(name, func(t *testing.T) {
			w := newGuardWire(t)
			_, err := w.previewAt(prBaseSHA, editTestFile, e)
			wantRefusal(t, err, rereadWords)
			wantRefusalDetail(t, err, "bad_op", &zero)
			if w.remote.count("POST") != 0 || w.remote.count("GET /v1/repos") != 0 {
				t.Fatal(w.remote.log)
			}
		})
	}
}

func TestAttestedLegacyBaseGainsAWordAndShowsBeforeAndAfter(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, editTestFile)
	got, err := w.previewAt(prBaseSHA, editTestFile, wordEdit("plan", nil, nil, str("per-file")))
	if err != nil || !got.Changed || len(got.Steps) != 1 || strings.Join(got.Steps[0].Fields, ",") != "fanout" {
		t.Fatalf("%+v %v", got, err)
	}
	if b, a := got.Steps[0].Before, got.Steps[0].After; b.Fanout != "" || a.Fanout != "per-file" || b.When != "" || a.When != "" || a.Prompt != b.Prompt {
		t.Fatalf("%+v %+v", b, a)
	}
	if !strings.Contains(got.Text, "fanout: per-file") {
		t.Fatal(got.Text)
	}
	// The default word is written as a key, which is still a runtime edit.
	got, err = w.previewAt(prBaseSHA, editTestFile, wordEdit("plan", str("always"), nil, nil))
	if err != nil || !got.Changed || strings.Join(got.Steps[0].Fields, ",") != "when" || !strings.Contains(got.Text, "when: always") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAttestedRuntimeEditPreviewValuesAreExact(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	got, err := w.previewAt(prBaseSHA, runtimeSourceFile, wordEdit("plan", str("always"), str(""), nil))
	if err != nil || !got.Changed || len(got.Steps) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	c := got.Steps[0]
	if strings.Join(c.Fields, ",") != "when,effort" {
		t.Fatalf("%v", c.Fields)
	}
	if c.Before.When != "thin" || c.Before.Effort != "cheap" || c.Before.Fanout != "per-file" ||
		c.After.When != "always" || c.After.Effort != "" || c.After.Fanout != "per-file" {
		t.Fatalf("%+v", c)
	}
	// A prompt edit on the same file leaves the words unchanged and equal on both sides.
	got, err = w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("plan", "plan better"))
	if err != nil || got.Steps[0].Before.When != got.Steps[0].After.When || got.Steps[0].After.Fanout != "per-file" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAttestedRuntimeEditNoOpChangesNothing(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	got, err := w.previewAt(prBaseSHA, runtimeSourceFile, wordEdit("plan", str("thin"), nil, nil))
	if err != nil || got.Changed || got.Text != runtimeSourceFile || len(got.Steps) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAttestedRuntimeEditRefusesUnsupportedWordsAndLockedSteps(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	zero := 0
	_, err := w.previewAt(prBaseSHA, runtimeSourceFile, wordEdit("plan", str("sometimes"), nil, nil))
	wantRefusalDetail(t, err, "bad_value", &zero)
	// A check takes no model's words: no fanout, no effort.
	_, err = w.previewAt(prBaseSHA, runtimeSourceFile, wordEdit("test", nil, nil, str("per-file")))
	wantRefusalDetail(t, err, "bad_value", &zero)
	_, err = w.previewAt(prBaseSHA, runtimeSourceFile, wordEdit("test", nil, str("strong"), nil))
	wantRefusalDetail(t, err, "bad_value", &zero)
}

func TestRuntimeEditAttestationDoesNotSurviveACallerFlip(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, editTestFile)
	edit := wordEdit("plan", nil, nil, str("per-file"))
	if _, err := w.previewAt(prBaseSHA, editTestFile, edit); err != nil {
		t.Fatal(err)
	}
	w.hub.hold(w.id, "tok_somebody_else_0123456789abcdef", 0)
	_, err := w.previewAt(prBaseSHA, editTestFile, edit)
	wantRefusal(t, err, rereadWords)
}

func TestRuntimeEditRoundTripsToExactlyOnePR(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, editTestFile)
	pv, err := w.previewAt(prBaseSHA, editTestFile, wordEdit("plan", str("thin"), nil, nil))
	if err != nil || !strings.Contains(pv.Text, "when: thin") {
		t.Fatalf("%+v %v", pv, err)
	}
	w.remote.reset()
	if _, err := w.open(pv.Text, prBaseSHA); err != nil {
		t.Fatal(err)
	}
	if w.remote.count("POST") != 1 || w.remote.count("GET /v1/repos") != 1 {
		t.Fatalf("%v", w.remote.log)
	}
	if !w.remote.postContains("when: thin") {
		t.Fatalf("the POST did not carry the edit: %v", w.remote.log)
	}
}

// rawFrame sends one request and returns the whole answer line, detail included.
func rawFrame(w *guardWire, method string, params any) string {
	w.t.Helper()
	w.next++
	b, _ := json.Marshal(params)
	fmt.Fprintf(w.in, `{"id":%d,"method":%q,"params":%s}`+"\n", w.next, method, b)
	for line := range w.answers {
		var f struct {
			ID *int `json:"id"`
		}
		if json.Unmarshal(line, &f) == nil && f.ID != nil && *f.ID == w.next {
			return string(line)
		}
	}
	w.t.Fatalf("the wire ended during %s", method)
	return ""
}

// The door a desktop uses: raw JSON through decode, the backend and the encoder.
func TestRuntimeEditThroughTheJSONRPCDoor(t *testing.T) {
	w := newGuardWire(t)
	raw := func(edit string) map[string]any {
		return map[string]any{"factory": w.id, "repo": prRepo, "base_sha": prBaseSHA, "base_text": editTestFile, "edits": []json.RawMessage{json.RawMessage(edit)}}
	}
	frame := rawFrame(w, "factory.pipeline_preview", raw(`{"op":"set_step","name":"plan","when":"thin"}`))
	if !strings.Contains(frame, `"bad_params"`) || !strings.Contains(frame, `"code":"bad_op"`) || !strings.Contains(frame, `"op":0`) {
		t.Fatalf("%s", frame)
	}
	w.attest(t, editTestFile)
	var res map[string]any
	w.must("factory.pipeline_preview", raw(`{"op":"set_step","name":"plan","when":"thin"}`), &res)
	steps, _ := res["steps"].([]any)
	after := steps[0].(map[string]any)["after"].(map[string]any)
	if after["when"] != "thin" {
		t.Fatalf("%v", res)
	}
	var legacy json.RawMessage
	w.must("factory.pipeline_preview", raw(`{"op":"set_step","name":"plan","prompt":"plan better"}`), &legacy)
	for _, k := range []string{`"when"`, `"effort"`, `"fanout"`} {
		if strings.Contains(string(legacy), k) {
			t.Fatalf("a legacy edit's result carries %s: %s", k, legacy)
		}
	}
}
