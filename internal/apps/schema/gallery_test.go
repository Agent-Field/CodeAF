package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestGalleryKitCatalog(t *testing.T) {
	var kit Kit
	if err := json.Unmarshal(KitJSON(), &kit); err != nil {
		t.Fatal(err)
	}
	if kit.Kit != 2 || len(kit.Parts) != 63 {
		t.Fatalf("catalog has kit %d and %d parts, want kit 2 and 63 parts", kit.Kit, len(kit.Parts))
	}
}

func TestGalleryDefinitionsValidate(t *testing.T) {
	fixtures := []string{
		"kyc", "ncr", "line-handover", "dispatch", "contract-review", "inventory-po", "slides",
		"ad-studio", "investor-update", "recruiting", "support-inbox", "course-builder",
		"incident-room", "month-end-close", "own-factory",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			def, err := Load("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if report := Validate(def, nil); !report.OK() {
				t.Fatal(report.String())
			}
		})
	}
}

func TestBrokenGalleryDefinitionsReportCodes(t *testing.T) {
	cases := []struct {
		app    string
		change edit
		code   string
	}{
		{"kyc", textEdit("screens/queue.json", `"$input.reason"`, `" "`), CodeActionVerb},
		{"kyc", textEdit("sample.json", `"w": 0.4`, `"w": 0.99`), CodeSampleBadValue},
		{"kyc", textEdit(AppFile, `"person": "$record.reviewer"`, `"person": "$record.maker"`), CodePolicy},
		{"ncr", textEdit("graph/affected.json", `"to": "drawing"`, `"to": "missing"`), CodeContent},
		{"ncr", textEdit("screens/trace.json", `"depth": 2`, `"depth": 0`), CodeSchema},
		{"line-handover", textEdit("docs/handover.md", "part:intro", "part:other"), CodeContent},
		{"dispatch", textEdit("places/routes.json", "-79.38", "-181.0"), CodeSchema},
		{"contract-review", textEdit("screens/contracts.json", "docs/agreement.md", "docs/missing.md"), CodeContentBinding},
		{"inventory-po", textEdit("sample.json", `"0040000000001"`, "40000000001"), CodeSampleBadValue},
		{"slides", textEdit("slides/1.json", `"x": 120`, `"x": 1900`), CodeContent},
		{"ad-studio", textEdit("screens/campaigns.json", "slides/left.json", "slides/missing.json"), CodeContentBinding},
		{"investor-update", textEdit(AppFile, `"when": "published"`, `"when": "bad_field"`), CodeExpression},
		{"recruiting", textEdit(AppFile, `"per": "stage"`, `"per": "unknown"`), CodeFieldUnknown},
		{"support-inbox", textEdit(AppFile, "timestamp(received_at)", "timestamp(missing)"), CodeExpression},
		{"support-inbox", textEdit(AppFile, `"event": "thread.received"`, `"event": "send"`), CodeToolNotAllowed},
		{"course-builder", textEdit("screens/quiz.json", `"format": "json"`, `"format": "word"`), CodeSampleBadValue},
		{"incident-room", textEdit("screens/room.json", `"kind": "live"`, `"kind": "pie"`), CodePartProps},
		{"incident-room", textEdit("screens/room.json", `"run": "monitor"`, `"run": "missing"`), CodeRunBinding},
		{"month-end-close", textEdit("sheets/formulas.json", `"ledger":`, `"unknown":`), CodeContentBinding},
		{"month-end-close", textEdit(AppFile, `"evidence": "file"`, `"evidence": "url"`), CodePolicy},
		{"own-factory", textEdit("runs/fix.yaml", "workspace: repo", "workspace: directory"), CodeWorkspaceShape},
		{"own-factory", textEdit("runs/fix.yaml", "      - id: test", "  - id: test"), CodeParse},
		{"own-factory", textEdit("runs/fix.yaml", "    gate: {approval: proof, who: me}\n", ""), CodeSendGateMissing},
	}
	for _, tc := range cases {
		t.Run(tc.app+"/"+tc.code, func(t *testing.T) {
			def := load(t, "testdata/"+tc.app)
			tc.change(t, def.Files)
			if r := Validate(def, nil); !hasCode(r, tc.code) {
				t.Fatalf("want %s, got\n%s", tc.code, r)
			}
		})
	}
}

func TestGalleryRepoWorkspaceDoesNotLeakToSibling(t *testing.T) {
	def := load(t, "testdata/own-factory")
	def.Files["runs/fix.yaml"] = append(def.Files["runs/fix.yaml"], []byte("  - id: outside\n    shell: go test ./...\n")...)
	if r := Validate(def, nil); !hasCode(r, CodeShellRefused) {
		t.Fatalf("shell outside the repo scope should be refused:\n%s", r)
	}
}

func TestGalleryCustomManifestUsesTheSamePropsBindingsAndEvents(t *testing.T) {
	def := load(t, "testdata/inventory-po")
	def.Files["custom/summary.tsx"] = []byte("<part><title text=\"Low stock\"/><button text=\"Review stock\" event=\"pick\"/></part>")
	def.Files["custom/summary.json"] = []byte(`{
		"name":"custom/summary","version":1,"summary":"A labelled stock summary.",
		"inputs":[],"binding_required":true,"capabilities":"r","reason":"The catalog has no tactile stock proof.","sizes":[800,1120],"themes":["dark","black","light"],
		"source":"custom/summary.tsx","example":{"tone":"neutral"},
		"children":"none","interactive":true,"bindings":["records"],"events":["pick"],
		"props":{"type":"object","required":["tone"],"additionalProperties":false,"properties":{"tone":{"enum":["neutral","critical"]}}}
	}`)
	def.Files["screens/stock.json"] = []byte(`{
		"screen":"stock","title":"Stock","views":{"stock":{"type":"sku"}},
		"body":{"part":"custom/summary","id":"summary","label":"Low stock","props":{"tone":"critical"},
			"bind":{"records":"sku","view":"stock"},"on":{"pick":[{"open":"orders"}]}}
	}`)
	if r := Validate(def, nil); !r.OK() {
		t.Fatal(r.String())
	}

	source := def.Files["custom/summary.tsx"]
	for _, bad := range []string{
		`<part><img src="https://example.invalid"/></part>`,
		`<part><text text="Proof" onclick="localStorage.clear()"/></part>`,
		`<part><script>parent.document.title = "changed"</script></part>`,
		`<part><button text="Send" event="send"/></part>`,
	} {
		def.Files["custom/summary.tsx"] = []byte(bad)
		if r := Validate(def, nil); !hasCode(r, CodeManifest) {
			t.Fatalf("custom source %q passed Check: %s", bad, r)
		}
	}
	def.Files["custom/summary.tsx"] = source

	textEdit("screens/stock.json", `"critical"`, `"blue"`)(t, def.Files)
	textEdit("screens/stock.json", `"pick"`, `"hover"`)(t, def.Files)
	textEdit("screens/stock.json", `"records":"sku","view":"stock"`, `"runs":"missing"`)(t, def.Files)
	r := Validate(def, nil)
	for _, code := range []string{CodePartProps, CodeEventUnknown, CodeBindNotAccepted} {
		if !hasCode(r, code) {
			t.Fatalf("want %s:\n%s", code, r)
		}
	}

	def.Files["custom/summary.json"] = []byte(`{
		"name":"custom/summary","version":1,"summary":"A labelled stock summary.",
		"inputs":[],"binding_required":true,"capabilities":"r","reason":"The catalog has no tactile stock proof.","sizes":[800,1120],"themes":["dark","black","light"],
		"source":"custom/summary.tsx","example":{},
		"children":"none","interactive":true,"bindings":["records"],"events":["pick"],
		"props":{"$ref":"https://example.org/remote.json"}
	}`)
	if r := Validate(def, nil); !hasCode(r, CodeManifest) {
		t.Fatalf("custom schemas must be self-contained:\n%s", r)
	}
}

func TestGeneratedDeskRegistryMatchesEngine(t *testing.T) {
	desk, err := os.ReadFile("kit-v2.json")
	if err != nil {
		t.Fatalf("desk catalog missing: %v", err)
	}
	engine := KitJSON()
	if digest := fmt.Sprintf("%x", sha256.Sum256(engine)); digest != kitV2SourceDigest {
		t.Fatalf("Go catalog is stale: run go generate ./internal/apps/schema")
	}
	if !bytes.Equal(engine, desk) {
		t.Fatal("desk catalog is stale: run go generate ./internal/apps/schema")
	}
}

func TestGallerySourceWriteNeedsExactGrantAndApproval(t *testing.T) {
	def := load(t, "testdata/recruiting")
	doc := object(def.Files[AppFile])
	source := "connections.crm.update"
	capabilities := doc["capabilities"].(map[string]any)
	capabilities["tools"] = []string{source}
	plan := object(def.Files[PlanFile])
	moves, _ := plan["automation"].([]any)
	plan["automation"] = append(moves, map[string]any{"id": "update-crm", "line": "Update the deal after approval."})
	save := func(name string, obj map[string]any) {
		t.Helper()
		raw, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		def.Files[name] = raw
	}
	save(PlanFile, plan)
	run := func(gate string) {
		def.Files["runs/update-crm.yaml"] = []byte("run: update-crm\nsteps:\n  - id: update\n    tool: " + source + "\n    as: me\n    args: {id: abc, set: {stage: Qualified}}\n" + gate)
	}
	run("    gate: {approval: send}\n")
	save(AppFile, doc)
	if report := Validate(def, nil); !report.OK() {
		t.Fatal(report.String())
	}
	run("")
	if report := Validate(def, nil); !contains(report.Codes(), CodeSendGateMissing) {
		t.Fatalf("source write ran without approval: %s", report)
	}
	run("    gate: {approval: send}\n")
	capabilities["tools"] = []string{"connections.crm"}
	save(AppFile, doc)
	if report := Validate(def, nil); !contains(report.Codes(), CodeToolNotAllowed) {
		t.Fatalf("source write ran without its exact grant: %s", report)
	}
}

func TestGalleryBeforeRecordRequiresReadableDatetimeAndPositiveLead(t *testing.T) {
	def := load(t, "testdata/recruiting")
	doc := object(def.Files[AppFile])
	doc["triggers"] = []any{map[string]any{
		"id": "prep", "on": "record.before", "type": "meeting",
		"at": "starts_at", "lead": "30m", "run": "prep",
	}}
	plan := object(def.Files[PlanFile])
	for group, id := range map[string]string{"data": "meeting", "automation": "prep"} {
		entries, _ := plan[group].([]any)
		plan[group] = append(entries, map[string]any{"id": id, "line": "Prepare for meeting."})
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	def.Files[PlanFile] = encoded
	records := doc["records"].(map[string]any)
	records["meeting"] = map[string]any{"fields": map[string]any{
		"title":     map[string]any{"type": "text"},
		"starts_at": map[string]any{"type": "datetime"},
	}}
	doc["capabilities"].(map[string]any)["records"].(map[string]any)["meeting"] = "r"
	def.Files["runs/prep.yaml"] = []byte("run: prep\nsteps:\n  - id: review\n    gate: {approval: proof}\n")
	save := func() {
		t.Helper()
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		def.Files[AppFile] = raw
	}
	save()
	if result := Validate(def, nil); !result.OK() {
		t.Fatal(result.String())
	}
	trigger := doc["triggers"].([]any)[0].(map[string]any)
	trigger["at"], trigger["lead"] = "title", "0m"
	save()
	report := Validate(def, nil)
	for _, path := range []string{"/triggers/0/at", "/triggers/0/lead"} {
		found := false
		for _, problem := range report.Problems {
			if problem.Path == path && problem.Code == CodeTriggerShape {
				found = true
			}
		}
		if !found {
			t.Fatalf("Check did not flag %s: %s", path, report)
		}
	}
}
