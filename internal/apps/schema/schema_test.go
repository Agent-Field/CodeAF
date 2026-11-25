package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The kit v0 part list is the contract in SPEC §5; the desk renders exactly these
// names, so the catalog may neither drop nor invent one without both sides moving.
var specKitV0 = []string{"Stack", "Row", "Panel", "SectionTabs", "Heading", "Text", "Badge", "Button", "Divider", "Empty", "RecordList", "RecordPage", "RecordForm", "BulkBar", "QueryField", "StateMeter", "StatTile", "Chart", "Sheet", "Toast"}

func TestKitCatalogIsTheSpecPartList(t *testing.T) {
	kit, err := KitV0()
	if err != nil {
		t.Fatal(err)
	}
	got := kit.Names()
	want := append([]string{}, specKitV0...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kit-v0.json parts\n got %v\nwant %v", got, want)
	}
	for _, p := range kit.Parts {
		if p.Children != "none" && p.Children != "nodes" && p.Children != "slots" {
			t.Errorf("%s: children is %q", p.Name, p.Children)
		}
		if p.Bindings == nil || p.Events == nil {
			t.Errorf("%s: bindings and events must be lists, even empty ones", p.Name)
		}
		if _, ok := schemas().props[p.Name]; !ok {
			t.Errorf("%s: props schema did not compile", p.Name)
		}
	}
	if !json.Valid(KitJSON()) {
		t.Fatal("KitJSON is not JSON")
	}
}

func load(t *testing.T, dir string) Definition {
	t.Helper()
	def, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func TestFixturesValidateClean(t *testing.T) {
	crm := load(t, "testdata/crm")
	if r := Validate(crm, nil); !r.OK() {
		t.Fatalf("crm fixture:\n%s", r)
	}
	if n := len(crm.ScreenFiles()); n != 3 {
		t.Fatalf("crm has %d screens, want 3", n)
	}
	if n := len(crm.RunFiles()); n != 2 {
		t.Fatalf("crm has %d runs, want 2", n)
	}
	var sample Sample
	if err := json.Unmarshal(crm.Files[SampleFile], &sample); err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, r := range sample.Records {
		rows += len(r)
	}
	if rows < 25 || rows > 40 {
		t.Fatalf("crm sample has %d records, want about 30", rows)
	}

	// Validating the CRM against a set that already holds its own types is what
	// the store does on every save after the first; owning them is not a clash.
	if r := Validate(crm, TypesOf(crm)); !r.OK() {
		t.Fatalf("crm against its own types:\n%s", r)
	}

	hiring := load(t, "testdata/hiring")
	if r := Validate(hiring, TypesOf(crm)); !r.OK() {
		t.Fatalf("hiring fixture beside the crm:\n%s", r)
	}
	if _, ok := TypesOf(hiring)["contact"]; ok {
		t.Fatal("TypesOf counted a used type as the hiring app's own")
	}
}

// edit returns a copy of the CRM fixture with one file changed. JSON files are
// edited as decoded values so a case says what it breaks, not where the bytes sit.
type edit func(t *testing.T, files map[string][]byte)

func jsonEdit(file string, change func(v map[string]any)) edit {
	return func(t *testing.T, files map[string][]byte) {
		t.Helper()
		var v map[string]any
		if err := json.Unmarshal(files[file], &v); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		change(v)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		files[file] = raw
	}
}

func textEdit(file, old, new string) edit {
	return func(t *testing.T, files map[string][]byte) {
		t.Helper()
		s := string(files[file])
		if !strings.Contains(s, old) {
			t.Fatalf("%s does not contain %q", file, old)
		}
		files[file] = []byte(strings.Replace(s, old, new, 1))
	}
}

// at walks a decoded JSON value by keys and indexes.
func at(v any, path ...any) map[string]any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			v = v.(map[string]any)[k]
		case int:
			v = v.([]any)[k]
		}
	}
	return v.(map[string]any)
}

// newDealButton is the deals screen's "New deal" button.
func newDealButton(v map[string]any) map[string]any {
	return at(v, "body", "children", 1, "children", 0, "children", 0, "children", 1)
}

func TestBrokenDefinitionsSayWhatIsWrong(t *testing.T) {
	crm := load(t, "testdata/crm")
	cases := []struct {
		name string
		edit edit
		code string
		file string
	}{
		{"not json", func(t *testing.T, f map[string][]byte) { f[AppFile] = []byte("{") }, CodeParse, AppFile},
		{"wrong kit version", jsonEdit(AppFile, func(v map[string]any) { v["kit"] = 2 }), CodeSchema, AppFile},
		{"enum without values", jsonEdit(AppFile, func(v map[string]any) {
			delete(at(v, "records", "deal", "fields", "stage"), "values")
		}), CodeSchema, AppFile},
		{"missing plan", func(t *testing.T, f map[string][]byte) { delete(f, PlanFile) }, CodeMissingFile, PlanFile},
		{"missing agent file", func(t *testing.T, f map[string][]byte) { delete(f, "agent.md") }, CodeMissingFile, "agent.md"},
		{"ref to a type nobody has", jsonEdit(AppFile, func(v map[string]any) {
			at(v, "records", "deal", "fields", "company")["to"] = "firm"
		}), CodeTypeUnknown, AppFile},
		{"title names no field", jsonEdit(AppFile, func(v map[string]any) { at(v, "records", "deal")["title"] = "label" }), CodeFieldUnknown, AppFile},
		{"enum default not a value", jsonEdit(AppFile, func(v map[string]any) {
			at(v, "records", "deal", "fields", "stage")["default"] = "New"
		}), CodeDefaultNotValue, AppFile},
		{"capability for a type the app lacks", jsonEdit(AppFile, func(v map[string]any) {
			at(v, "capabilities", "records")["ticket"] = "r"
		}), CodeCapabilityUnknown, AppFile},
		{"connector may send without approval", jsonEdit(AppFile, func(v map[string]any) {
			at(v, "capabilities", "connectors", "gmail")["approve"] = []any{}
		}), CodeSendGateMissing, AppFile},
		{"trigger starts a run with no file", jsonEdit(AppFile, func(v map[string]any) { at(v, "triggers", 0)["run"] = "nudge" }), CodeRunUnknown, AppFile},
		{"command starts a run with no file", jsonEdit(AppFile, func(v map[string]any) { at(v, "commands", 0)["run"] = "nudge" }), CodeRunUnknown, AppFile},
		{"trigger kind v0 does not run", jsonEdit(AppFile, func(v map[string]any) {
			tr := at(v, "triggers", 0)
			tr["on"] = "connector"
			tr["connector"] = "gmail"
		}), CodeTriggerV0, AppFile},
		{"schedule without a real cron", jsonEdit(AppFile, func(v map[string]any) { at(v, "triggers", 0)["cron"] = "weekdays" }), CodeTriggerShape, AppFile},
		{"duplicate trigger id", jsonEdit(AppFile, func(v map[string]any) { at(v, "triggers", 1)["id"] = "stale" }), CodeDuplicateID, AppFile},
		{"command that does nothing", jsonEdit(AppFile, func(v map[string]any) { delete(at(v, "commands", 0), "run") }), CodeCommandEmpty, AppFile},
		{"read-only type written by a screen", jsonEdit(AppFile, func(v map[string]any) {
			at(v, "capabilities", "records")["deal"] = "r"
		}), CodeNotAllowed, "screens/deals.json"},
		{"ai sort without the capability", jsonEdit(AppFile, func(v map[string]any) {
			at(v, "capabilities")["ai"] = []any{"summarise"}
		}), CodeNotAllowed, "screens/deals.json"},
		{"part the kit does not have", textEdit("screens/companies.json", `"part": "Divider"`, `"part": "DataGrid"`), CodePartUnknown, "screens/companies.json"},
		{"raw style on a part", jsonEdit("screens/deals.json", func(v map[string]any) {
			at(newDealButton(v), "props")["style"] = map[string]any{"color": "#e5484d"}
		}), CodePartProps, "screens/deals.json"},
		{"children on a part that takes none", jsonEdit("screens/deals.json", func(v map[string]any) {
			newDealButton(v)["children"] = []any{map[string]any{"part": "Text", "props": map[string]any{"text": "x"}}}
		}), CodePartChildren, "screens/deals.json"},
		{"interactive node without a label", jsonEdit("screens/deals.json", func(v map[string]any) {
			delete(newDealButton(v), "label")
		}), CodeLabelMissing, "screens/deals.json"},
		{"event the part never fires", jsonEdit("screens/deals.json", func(v map[string]any) {
			b := newDealButton(v)
			b["on"] = map[string]any{"hover": b["on"].(map[string]any)["click"]}
		}), CodeEventUnknown, "screens/deals.json"},
		{"two navigation rows", jsonEdit("screens/contacts.json", func(v map[string]any) {
			body := at(v, "body")
			body["children"] = append(body["children"].([]any), v["nav"])
		}), CodeNavTwice, "screens/contacts.json"},
		{"binding the part cannot take", jsonEdit("screens/contacts.json", func(v map[string]any) {
			at(v, "body", "children", 1)["bind"] = map[string]any{"records": "contact"}
		}), CodeBindNotAccepted, "screens/contacts.json"},
		{"selection of a list that is not there", textEdit("screens/deals.json", `"$selection.deals.count > 0"`, `"$selection.dealz.count > 0"`), CodeSelectionUnknown, "screens/deals.json"},
		{"bulk bar for a list that is not there", textEdit("screens/deals.json", `"for": "deals"`, `"for": "pipeline"`), CodeListUnknown, "screens/deals.json"},
		{"param the screen never declared", textEdit("screens/contacts.json", `"query": "sort:name"`, `"query": "company:$params.company"`), CodeParamUndeclared, "screens/contacts.json"},
		{"column names no field", textEdit("screens/contacts.json", `"columns": ["name", "title", "company", "email"]`, `"columns": ["name", "mobile"]`), CodeFieldUnknown, "screens/contacts.json"},
		{"board by a field that is not an enum", textEdit("screens/deals.json", `"board_by": "stage"`, `"board_by": "amount"`), CodeFieldNotEnum, "screens/deals.json"},
		{"board without board_by", textEdit("screens/deals.json", `"board_by": "stage",`, ``), CodeBoardByMissing, "screens/deals.json"},
		{"action with two verbs", jsonEdit("screens/deals.json", func(v map[string]any) {
			at(newDealButton(v), "on", "click", 1)["open"] = "deals"
		}), CodeActionVerb, "screens/deals.json"},
		{"open a screen that is not there", textEdit("screens/deals.json", `{ "open": "deal", "id": "$row.id" }`, `{ "open": "pipeline", "id": "$row.id" }`), CodeOpenUnknown, "screens/deals.json"},
		{"nav tab to a screen that is not there", textEdit("screens/contacts.json", `"screen": "companies"`, `"screen": "accounts"`), CodeOpenUnknown, "screens/contacts.json"},
		{"record page lists a command that is not there", textEdit("screens/deals.json", `"actions": ["follow-up", "mark-won"]`, `"actions": ["follow-up", "archive"]`), CodeCommandUnknown, "screens/deals.json"},
		{"ai sort of a field that is not an AI field", textEdit("screens/deals.json", `"field": "likely"`, `"field": "stage"`), CodeFieldNotAI, "screens/deals.json"},
		{"screen id differs from its file", textEdit("screens/contacts.json", `"screen": "contacts"`, `"screen": "people"`), CodeScreenIDMismatch, "screens/contacts.json"},
		{"loop without max", textEdit("runs/follow-up.yaml", `, max: 25 }`, ` }`), CodeLoopMax, "runs/follow-up.yaml"},
		{"outbound tool without a send gate", textEdit("runs/follow-up.yaml", "        gate: { approval: send, who: $me }\n", ""), CodeSendGateMissing, "runs/follow-up.yaml"},
		{"shell step", textEdit("runs/won-handoff.yaml", "  - id: settle\n    wait: { for: 1d }\n", "  - id: settle\n    shell: go test ./...\n"), CodeShellRefused, "runs/won-handoff.yaml"},
		{"workspace repo step", textEdit("runs/won-handoff.yaml", "  - id: settle\n    wait: { for: 1d }\n", "  - id: settle\n    workspace: repo\n    agent: CRM agent\n"), CodeWorkspaceRefused, "runs/won-handoff.yaml"},
		{"wait on an outside event", textEdit("runs/won-handoff.yaml", "wait: { for: 1d }", "wait: { for: gmail.reply, thread: $steps.post.thread }"), CodeWaitV0, "runs/won-handoff.yaml"},
		{"agent that is not the app's", textEdit("runs/won-handoff.yaml", "agent: CRM agent", "agent: Sales agent"), CodeAgentUnknown, "runs/won-handoff.yaml"},
		{"tool verb the app may not use", textEdit("runs/follow-up.yaml", "tool: gmail.search", "tool: gmail.archive"), CodeToolNotAllowed, "runs/follow-up.yaml"},
		{"step reads a step that is not there", textEdit("runs/follow-up.yaml", "$steps.draft.output", "$steps.drafted.output"), CodeStepRefUnknown, "runs/follow-up.yaml"},
		{"step with two kinds", textEdit("runs/won-handoff.yaml", "wait: { for: 1d }", "wait: { for: 1d }\n    agent: CRM agent"), CodeStepKind, "runs/won-handoff.yaml"},
		{"records step whose type cannot be told", textEdit("runs/won-handoff.yaml", "id: $record.id", "id: $steps.summary.id"), CodeStepTypeUnknown, "runs/won-handoff.yaml"},
		{"records step on a read-only type", jsonEdit(AppFile, func(v map[string]any) {
			at(v, "capabilities", "records")["deal"] = "r"
		}), CodeNotAllowed, "runs/won-handoff.yaml"},
		{"run spends more than the app allows", textEdit("runs/won-handoff.yaml", "per_run: 0.20", "per_run: 2.00"), CodeRunBudgetOver, "runs/won-handoff.yaml"},
		{"run id differs from its file", textEdit("runs/won-handoff.yaml", "run: won-handoff", "run: handoff"), CodeRunIDMismatch, "runs/won-handoff.yaml"},
		{"plan line for a type the app lacks", textEdit(PlanFile, `{ "id": "company"`, `{ "id": "account"`), CodePlanUnknownID, PlanFile},
		{"plan never mentions a screen", textEdit(PlanFile, `{ "id": "contacts", "line": "Contacts · list, contact in the panel" }`, `{ "id": "deals", "line": "again" }`), CodePlanMissingLine, PlanFile},
		{"sample enum value that is not a value", jsonEdit(SampleFile, func(v map[string]any) {
			at(v, "records", "deal", 0)["stage"] = "Closed"
		}), CodeSampleBadValue, SampleFile},
		{"sample ref to a missing row", jsonEdit(SampleFile, func(v map[string]any) {
			at(v, "records", "deal", 0)["company"] = "01KAT0000A0000000000000000"
		}), CodeSampleRefMissing, SampleFile},
		{"sample row without an id", jsonEdit(SampleFile, func(v map[string]any) {
			delete(at(v, "records", "company", 0), "id")
		}), CodeSampleIDMissing, SampleFile},
		{"sample of a type the app lacks", jsonEdit(SampleFile, func(v map[string]any) {
			at(v, "records")["ticket"] = []any{map[string]any{"id": "x"}}
		}), CodeSampleUnknownType, SampleFile},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{}
			for k, v := range crm.Files {
				files[k] = append([]byte(nil), v...)
			}
			tc.edit(t, files)
			r := Validate(Definition{Files: files}, nil)
			for _, p := range r.Problems {
				if p.Message == "" || p.Code == "" || p.File == "" && p.Code != CodeSchema {
					t.Errorf("problem without words: %+v", p)
				}
				if p.Code == tc.code && p.File == tc.file {
					seen[tc.code] = true
					return
				}
			}
			t.Fatalf("want %s in %s, got:\n%s", tc.code, tc.file, r)
		})
	}
	if t.Failed() {
		return
	}
	// Every problem code the validator can report has a case above that produces
	// it, so a new check cannot land without a broken definition that proves it.
	for _, code := range allCodes() {
		if !seen[code] && !untabled[code] {
			t.Errorf("no broken definition produces %s", code)
		}
	}
}

// untabled are the codes the CRM-based table cannot reach, each proven by its own
// test below.
var untabled = map[string]bool{CodeFileNotAllowed: true, CodeTypeTaken: true, CodeUsesUnknown: true, CodeUsesFieldTaken: true, CodeBindTypeUnknown: true, CodeLoopShape: true}

func allCodes() []string {
	return []string{CodeParse, CodeSchema, CodeMissingFile, CodeTypeUnknown, CodeTypeTaken, CodeUsesUnknown, CodeUsesFieldTaken, CodeFieldUnknown, CodeFieldNotEnum, CodeFieldNotAI, CodeDefaultNotValue, CodeCapabilityUnknown, CodeNotAllowed, CodeRunUnknown, CodeCommandUnknown, CodeCommandEmpty, CodeDuplicateID, CodeTriggerV0, CodeTriggerShape, CodePartUnknown, CodePartProps, CodePartChildren, CodeLabelMissing, CodeNavTwice, CodeBindNotAccepted, CodeBindTypeUnknown, CodeEventUnknown, CodeActionVerb, CodeOpenUnknown, CodeListUnknown, CodeSelectionUnknown, CodeParamUndeclared, CodeBoardByMissing, CodeScreenIDMismatch, CodeRunIDMismatch, CodeStepKind, CodeShellRefused, CodeWorkspaceRefused, CodeLoopShape, CodeLoopMax, CodeAgentUnknown, CodeToolNotAllowed, CodeSendGateMissing, CodeStepTypeUnknown, CodeStepRefUnknown, CodeWaitV0, CodeRunBudgetOver, CodePlanUnknownID, CodePlanMissingLine, CodeSampleUnknownType, CodeSampleBadValue, CodeSampleRefMissing, CodeSampleIDMissing, CodeFileNotAllowed}
}

func hasCode(r Report, code string) bool {
	for _, c := range r.Codes() {
		if c == code {
			return true
		}
	}
	return false
}

func TestSharedTypes(t *testing.T) {
	crm := load(t, "testdata/crm")
	hiring := load(t, "testdata/hiring")
	crmTypes := TypesOf(crm)

	if r := Validate(hiring, nil); !hasCode(r, CodeUsesUnknown) {
		t.Fatalf("hiring alone should not find contact to use:\n%s", r)
	}

	// Making a second contact type when the person already has one is refused, so
	// data never forks into two copies of the same people.
	own := map[string][]byte{}
	for k, v := range hiring.Files {
		own[k] = v
	}
	own[AppFile] = []byte(strings.Replace(string(hiring.Files[AppFile]), `"uses": true, "fields": { "role_applied"`, `"fields": { "role_applied"`, 1))
	if r := Validate(Definition{Files: own}, crmTypes); !hasCode(r, CodeTypeTaken) {
		t.Fatalf("a second contact type should be refused:\n%s", r)
	}

	taken := map[string][]byte{}
	for k, v := range hiring.Files {
		taken[k] = v
	}
	taken[AppFile] = []byte(strings.Replace(string(hiring.Files[AppFile]), `"role_applied"`, `"email"`, 1))
	if r := Validate(Definition{Files: taken}, crmTypes); !hasCode(r, CodeUsesFieldTaken) {
		t.Fatalf("re-adding contact.email should be refused:\n%s", r)
	}

	// Hiring may only read contacts, so a screen that edits one is not allowed.
	writes := map[string][]byte{}
	for k, v := range hiring.Files {
		writes[k] = v
	}
	writes["screens/candidates.json"] = []byte(strings.Replace(string(hiring.Files["screens/candidates.json"]),
		`"props": { "fields": ["name", "role", "stage", "contact", "applied_on"] }`,
		`"props": { "fields": ["name"] } }, { "part": "RecordForm", "label": "New contact", "props": { "type": "contact" }`, 1))
	if r := Validate(Definition{Files: writes}, crmTypes); !hasCode(r, CodeNotAllowed) {
		t.Fatalf("hiring creating contacts should be refused:\n%s", r)
	}
}

func TestRecordBindingAndLoopShape(t *testing.T) {
	crm := load(t, "testdata/crm")
	files := map[string][]byte{}
	for k, v := range crm.Files {
		files[k] = v
	}
	files["screens/contacts.json"] = []byte(strings.Replace(string(crm.Files["screens/contacts.json"]), `"record": "$selection.contacts.id"`, `"record": "$me"`, 1))
	files["runs/follow-up.yaml"] = []byte(strings.Replace(string(crm.Files["runs/follow-up.yaml"]), "loop: { over: $params.deals, as: deal, max: 25 }", "loop: { as: deal, max: 25 }", 1))
	r := Validate(Definition{Files: files}, nil)
	if !hasCode(r, CodeBindTypeUnknown) || !hasCode(r, CodeLoopShape) {
		t.Fatalf("want bind_type_unknown and loop_shape:\n%s", r)
	}
}

func TestLoadReadsOnlyTheAppFolder(t *testing.T) {
	def := load(t, "testdata/crm")
	var names []string
	for k := range def.Files {
		names = append(names, k)
	}
	sort.Strings(names)
	want := []string{"agent.md", "app.json", "plan.json", "runs/follow-up.yaml", "runs/won-handoff.yaml", "sample.json", "screens/companies.json", "screens/contacts.json", "screens/deals.json"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Load read %v", names)
	}
}

// A broken app.json is reported once, in app.json, rather than echoed by every
// screen and run that leans on its types.
func TestBrokenAppDoesNotCascade(t *testing.T) {
	crm := load(t, "testdata/crm")
	files := map[string][]byte{}
	for k, v := range crm.Files {
		files[k] = v
	}
	files[AppFile] = []byte(strings.Replace(string(crm.Files[AppFile]), `"kit": 0`, `"kit": 2`, 1))
	r := Validate(Definition{Files: files}, nil)
	for _, p := range r.Problems {
		if p.File != AppFile {
			t.Errorf("a broken app.json echoed into %s: %s", p.File, p.Message)
		}
	}
	if len(r.Problems) == 0 {
		t.Fatal("kit 2 should be refused")
	}
}

func crmFiles(t *testing.T) map[string][]byte {
	t.Helper()
	crm := load(t, "testdata/crm")
	files := map[string][]byte{}
	for k, v := range crm.Files {
		files[k] = append([]byte(nil), v...)
	}
	return files
}

// A send is judged by the tool's last segment and before an MCP tool listing can
// wave it through, so neither a whole-server listing nor a longer name hides it.
func TestSendGateCannotBeBypassed(t *testing.T) {
	files := crmFiles(t)
	jsonEdit(AppFile, func(v map[string]any) { at(v, "capabilities")["tools"] = []any{"mail"} })(t, files)
	jsonEdit("screens/deals.json", func(v map[string]any) {
		at(newDealButton(v), "on")["click"] = []any{map[string]any{"tools.call": []any{"mail", "send", map[string]any{}}}}
	})(t, files)
	if r := Validate(Definition{Files: files}, nil); !hasCode(r, CodeSendGateMissing) {
		t.Fatalf("mail.send from a screen through an MCP listing should need approval:\n%s", r)
	}

	files = crmFiles(t)
	jsonEdit(AppFile, func(v map[string]any) { at(v, "capabilities")["tools"] = []any{"slack.chat.post"} })(t, files)
	textEdit("runs/won-handoff.yaml", "    gate: { approval: send, who: $me }\n    tool: slack.post", "    tool: slack.chat.post")(t, files)
	r := Validate(Definition{Files: files}, nil)
	if !hasCode(r, CodeSendGateMissing) {
		t.Fatalf("slack.chat.post without a gate should be refused:\n%s", r)
	}
}

func TestAppIDIsASlugOrAULID(t *testing.T) {
	for id, ok := range map[string]bool{"crm": true, "01KAT0000A0000000000000001": true, "CRM!": false, "01KAT0000A00000000000000IL": false} {
		files := crmFiles(t)
		jsonEdit(AppFile, func(v map[string]any) { v["id"] = id })(t, files)
		r := Validate(Definition{Files: files}, nil)
		if r.OK() != ok {
			t.Errorf("id %q: ok=%v, want %v:\n%s", id, r.OK(), ok, r)
		}
	}
}

func TestOnlyAppFilesInsideTheFolder(t *testing.T) {
	for _, name := range []string{"../escape.json", "/etc/passwd", "screens/../app2.json", "notes.txt", "screens/sub/x.json", "runs/x.sh", "./plan.json"} {
		files := crmFiles(t)
		files[name] = []byte("{}")
		r := Validate(Definition{Files: files}, nil)
		found := false
		for _, p := range r.Problems {
			if p.Code == CodeFileNotAllowed && p.File == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%q should be refused:\n%s", name, r)
		}
	}

	files := crmFiles(t)
	jsonEdit(AppFile, func(v map[string]any) { at(v, "agent")["file"] = "../secret.md" })(t, files)
	if r := Validate(Definition{Files: files}, nil); !hasCode(r, CodeFileNotAllowed) {
		t.Fatalf("an agent file outside the folder should be refused:\n%s", r)
	}
	if _, ok := AgentPath("./prompts/../agent.md"); !ok {
		t.Fatal("AgentPath should clean a path that stays inside the folder")
	}
}

func TestLoadRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	src := load(t, "testdata/crm")
	for name, raw := range src.Files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte(`{"screen":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "screens", "x.json")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	def := load(t, dir)
	if _, read := def.Files["screens/x.json"]; read {
		t.Fatal("Load followed a link")
	}
	if r := Validate(def, nil); !hasCode(r, CodeFileNotAllowed) {
		t.Fatalf("a linked screen should be refused:\n%s", r)
	}
}

func TestGotoAndDuplicateRuns(t *testing.T) {
	files := crmFiles(t)
	textEdit("runs/won-handoff.yaml", "  - id: settle\n", "  - id: settle\n    on_error: goto nowhere\n")(t, files)
	if r := Validate(Definition{Files: files}, nil); !hasCode(r, CodeStepRefUnknown) {
		t.Fatalf("goto a missing step should be refused:\n%s", r)
	}
	files = crmFiles(t)
	files["runs/won-handoff.yml"] = files["runs/won-handoff.yaml"]
	if r := Validate(Definition{Files: files}, nil); !hasCode(r, CodeDuplicateID) {
		t.Fatalf("two files for one run should be refused:\n%s", r)
	}
}

// The A2 panel shape: a Panel bound to a list's selection, its RecordPage reading
// $record, and a params binding on the root that declares $params.
func TestPanelSelectionAndParamsBindings(t *testing.T) {
	files := crmFiles(t)
	jsonEdit("screens/contacts.json", func(v map[string]any) {
		body := at(v, "body")
		body["bind"] = map[string]any{"params": []any{"company"}}
		at(body, "children", 0, "bind")["query"] = "company:$params.company"
		panel := at(body, "children", 1)
		panel["bind"] = map[string]any{"selection": "contacts"}
		at(panel, "children", 0)["bind"] = map[string]any{"record": "$record.id"}
	})(t, files)
	if r := Validate(Definition{Files: files}, nil); !r.OK() {
		t.Fatalf("the A2 panel shape should validate:\n%s", r)
	}
}
