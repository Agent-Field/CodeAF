package schema

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestCheckIdentifiesInvalidDraftReplyReference(t *testing.T) {
	// The first baseline build could not read replies from an untyped agent.
	const file = "runs/draft-thread.yaml"
	recorded, err := os.ReadFile("testdata/marketing-approval-reference/" + file)
	if err != nil {
		t.Fatal(err)
	}
	const app = `{
		"id":"marketing-desk", "name":"Marketing desk", "version":1, "kit":2,
		"agent":{"name":"Marketing agent","file":"agent.md"},
		"records":{
			"post":{"title":"text","fields":{"text":{"type":"text"}}},
			"draft":{"title":"text","fields":{"text":{"type":"text"},"post":{"type":"ref","to":"post"}}}
		},
		"capabilities":{"records":{"post":"rw","draft":"rw"}},
		"budget":{"per_run":0.10}
	}`
	def := Definition{Files: map[string][]byte{
		AppFile: []byte(app), "agent.md": []byte("Draft replies."), file: recorded,
		PlanFile: []byte(`{"ask":"Draft replies","data":[{"id":"post","line":"Posts"},{"id":"draft","line":"Drafts"}],"automation":[{"id":"draft-thread","line":"Draft thread"}],"screens":[{"id":"posts","line":"Posts"}]}`),
		"screens/posts.json": []byte(`{
			"screen":"posts", "title":"Posts", "views":{"posts":{"type":"post"}},
			"body":{"part":"RecordList","id":"posts","label":"Posts",
				"props":{"columns":["text"]},"bind":{"records":"post","view":"posts"}}
		}`),
	}}
	assertFinding := func(path, ref, hint string) {
		t.Helper()
		report := Validate(def, nil)
		for _, p := range report.Problems {
			if p.File == file && p.Path == path && strings.Contains(p.Message, ref) && strings.Contains(p.Message, hint) {
				return
			}
		}
		t.Fatalf("Check should locate %s at %s and suggest %s, got:\n%s", ref, path, hint, report)
	}
	assertFinding("/steps/1/loop/over", "$steps.draft.replies", "ai step")

	// Replay the same unresolved output in the approval payload itself.
	start := bytes.Index(recorded, []byte("  - id: save"))
	end := bytes.Index(recorded, []byte("  - id: approve"))
	if start == -1 || end <= start {
		t.Fatal("recorded run should save drafts before approval")
	}
	gateOnly := append(bytes.Clone(recorded[:start]), recorded[end:]...)
	def.Files[file] = bytes.Replace(gateOnly, []byte("text: $steps.draft.output"), []byte("text: '{{steps.draft.replies}}'"), 1)
	assertFinding("/steps/1/args/text", "steps.draft.replies", "$steps.draft.output")

	def.Files[file] = bytes.Replace(gateOnly, []byte("text: $steps.draft.output"), []byte("text: $steps.drafted.output"), 1)
	assertFinding("/steps/1/args/text", "$steps.drafted.output", "draft")

	def.Files[file] = gateOnly
	if report := Validate(def, nil); !report.OK() {
		t.Fatalf("Check should accept the plain agent's text in the approval:\n%s", report)
	}

	example, err := BuilderExampleFile("draft-review", file)
	if err != nil {
		t.Fatal(err)
	}
	def.Files[file] = example
	if report := Validate(def, nil); !report.OK() {
		t.Fatalf("Check should accept the typed example served to the builder:\n%s", report)
	}
}
