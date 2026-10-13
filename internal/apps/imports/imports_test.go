package imports

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type testStore struct {
	svc.Store
	app *spec.App
}

func (s testStore) Definition(context.Context, string) (*spec.Bundle, error) {
	return &spec.Bundle{App: s.app}, nil
}

type testSpaces struct{ entries []ActivityEntry }

func (*testSpaces) Members(context.Context, svc.Scope) ([]Member, error) {
	return []Member{{"mona", "mona@acme.com"}, {"raj", "raj@acme.com"}, {"jm", "jm@acme.com"}, {"santosh", "santosh@acme.com"}}, nil
}
func (s *testSpaces) Append(_ context.Context, entry ActivityEntry) error {
	s.entries = append(s.entries, entry)
	return nil
}

type fixture struct {
	service *Service
	all     *svc.Services
	scope   svc.Scope
	cfg     svc.Config
	spaces  *testSpaces
	app     *spec.App
}

func setup(t *testing.T) fixture {
	t.Helper()
	app := &spec.App{ID: "crm", Name: "CRM", Space: "personal", Records: map[string]*spec.RecordType{
		"company": {Title: "name", Label: "Company", Plural: "Companies", Fields: map[string]*spec.Field{"name": {Type: spec.FieldText, Required: true, Unique: true}}},
		"deal": {Title: "name", Label: "Deal", Plural: "Deals", Fields: map[string]*spec.Field{
			"name": {Type: spec.FieldText, Required: true}, "company": {Type: spec.FieldRef, To: "company"},
			"value": {Type: spec.FieldMoney}, "stage": {Type: spec.FieldEnum, Values: []string{"Lead", "Talking", "Proposal", "Won", "Lost"}, Default: json.RawMessage(`"Lead"`)},
			"owner": {Type: spec.FieldPerson, Default: json.RawMessage(`"$me"`)}, "created": {Type: spec.FieldDate}, "notes": {Type: spec.FieldLongtext},
		}},
	}}
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{Store: testStore{app: app}}
	all.Records = records.New(cfg, all)
	s := New(cfg, all)
	spaces := &testSpaces{}
	s.UseSpaces(spaces)
	return fixture{s, all, svc.Scope{App: "crm", Data: svc.DataReal, Caller: svc.Caller{Member: "santosh", Role: svc.RoleUser}}, cfg, spaces, app}
}
func csvParams(text string, mappings ...bridge.Mapping) bridge.ImportPreviewParams {
	return bridge.ImportPreviewParams{Type: "deal", Source: bridge.ImportSourceSelection{Kind: "csv", File: base64.StdEncoding.EncodeToString([]byte(text))}, Mapping: mappings}
}
func preview(t *testing.T, f fixture, params bridge.ImportPreviewParams) bridge.ImportPreview {
	t.Helper()
	p, err := f.service.Preview(context.Background(), f.scope, params)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func create(t *testing.T, f fixture, typ string, fields map[string]any) bridge.Record {
	t.Helper()
	r, err := f.all.Records.Create(context.Background(), f.scope, bridge.CreateRecordParams{Type: typ, Set: fields})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func queryRows(t *testing.T, f fixture, typ string) []bridge.Record {
	t.Helper()
	page, err := f.all.Records.Query(context.Background(), f.scope, bridge.QueryParams{Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	return page.Rows
}
func requireCode(t *testing.T, err error, code bridge.ErrorCode, reason string) {
	t.Helper()
	var wire *bridge.Error
	if !errors.As(err, &wire) || wire.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
	if reason != "" && wire.Details["reason"] != reason {
		t.Fatalf("error details = %v, want reason %s", wire.Details, reason)
	}
}
func run(t *testing.T, f fixture, id string, mappings ...bridge.Mapping) bridge.ImportResult {
	t.Helper()
	result, err := f.service.Run(context.Background(), f.scope, bridge.ImportRunParams{ImportID: id, Mapping: mappings})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCSVColumnMapping(t *testing.T) {
	for _, test := range []struct {
		column, field, value, match string
		want                        any
		choice                      bool
	}{
		{"Deal Name", "name", "Annual renewal", "name", "Annual renewal", false},
		{"DEAL_NAME", "name", "Annual renewal", "name", "Annual renewal", false},
		{"Amount", "value", "48000", "name", float64(48000), false},
		{"Create Date", "created", "2026-03-02", "name", "2026-03-02", false},
		{"Company", "company", "Globex", "ref", "Globex", false},
		{"Deal Stage", "stage", "appointmentscheduled", "enum", nil, true},
		{"Deal Owner", "owner", "mona@acme.com", "people", "mona", false},
		{"Deal Owner", "owner", "outside@elsewhere.com", "people", nil, true},
		{"Close Date", "", "2026-11-30", "none", nil, true},
		{"Probability", "", "0.6", "none", nil, true},
	} {
		t.Run(test.column+"/"+test.value, func(t *testing.T) {
			f := setup(t)
			p := preview(t, f, csvParams(test.column+"\n"+test.value+"\n"))
			col := p.Columns[0]
			if col.Field != test.field || col.Match != test.match || col.NeedsChoice != test.choice {
				t.Fatalf("column = %+v", col)
			}
			if test.field != "" && !reflect.DeepEqual(p.RowsShown[0].Fields[test.field], test.want) {
				t.Fatalf("mapped = %+v, want %v", p.RowsShown[0].Fields, test.want)
			}
		})
	}
	t.Run("label", func(t *testing.T) {
		f := setup(t)
		f.app.Records["deal"].Fields["value"].Label = "Contract amount"
		p := preview(t, f, csvParams("Contract Amount\n48\n"))
		if p.Columns[0].Field != "value" {
			t.Fatal(p.Columns)
		}
	})
}

func TestBoardPreviewChoicesRunAndActivity(t *testing.T) {
	f := setup(t)
	company := create(t, f, "company", map[string]any{"name": "Globex"})
	create(t, f, "deal", map[string]any{"name": "Annual renewal", "company": company.ID})
	door := &fakeConnectors{reply: hubspotTable()}
	f.all.Connectors = door
	p := preview(t, f, bridge.ImportPreviewParams{Type: "deal", Source: bridge.ImportSourceSelection{Kind: "hubspot", Object: "deals"}})
	if p.NeedChoice != 4 || p.Total != 6 || len(p.RowsShown) != 5 || p.Duplicates != 1 || p.Count != 5 || p.CanImport || p.NewRefs["company"] != 4 {
		t.Fatalf("preview = %+v", p)
	}
	if len(door.calls) != 1 || door.calls[0].tool != "hubspot.deals.list" || door.calls[0].scope != f.scope || door.calls[0].args["object"] != "deals" {
		t.Fatalf("calls = %+v", door.calls)
	}
	_, err := f.service.Run(context.Background(), f.scope, bridge.ImportRunParams{ImportID: p.ImportID})
	requireCode(t, err, bridge.CodeInvalid, bridge.ReasonNeedsChoice)
	if len(queryRows(t, f, "deal")) != 1 || len(queryRows(t, f, "company")) != 1 {
		t.Fatal("open choices wrote records")
	}
	result := run(t, f, p.ImportID,
		bridge.Mapping{Column: "Deal Stage", Values: map[string]string{"appointmentscheduled": "Lead", "qualifiedtobuy": "Talking", "closedwon": "Won"}},
		bridge.Mapping{Column: "Deal Owner", People: map[string]string{"outside@elsewhere.com": ""}},
		bridge.Mapping{Column: "Close Date", As: "note"}, bridge.Mapping{Column: "Probability", As: "skip"},
	)
	want := "@santosh imported 5 deals into CRM from HubSpot · 1 skipped as duplicates · 4 new companies"
	if result.Created != 5 || result.Skipped != 1 || result.NewRefs["company"] != 4 || result.Activity != want {
		t.Fatalf("result = %+v", result)
	}
	if len(f.spaces.entries) != 1 || f.spaces.entries[0].Text != strings.Split(want, " · ")[0] || f.spaces.entries[0].UndoMethod != "imports.undo" || f.spaces.entries[0].Action != "import.run" {
		t.Fatalf("activity = %+v", f.spaces.entries)
	}
	if len(door.calls) != 1 {
		t.Fatal("Run fetched a changed source instead of importing the preview")
	}
	rows := queryRows(t, f, "deal")
	if len(rows) != 6 {
		t.Fatalf("deals = %+v", rows)
	}
	for _, row := range rows {
		if row.Fields["name"] == "Security add-on" && row.Fields["owner"] != nil {
			t.Fatalf("skipped owner got a default: %+v", row)
		}
		if row.Fields["name"] == "Team plan" && (row.Fields["company"] == "Northwind" || row.Fields["value"] != float64(14400) || row.Fields["notes"] != "Close Date: 2026-11-30") {
			t.Fatalf("mapped row = %+v", row)
		}
	}
	again := run(t, f, p.ImportID)
	if !reflect.DeepEqual(result, again) || len(queryRows(t, f, "deal")) != 6 || len(f.spaces.entries) != 1 {
		t.Fatal("Run was not idempotent")
	}
	data, err := os.ReadFile(filepath.Join(f.cfg.Root, "crm", "imports", p.ImportID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted receipt
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status != "done" || len(persisted.Created) != 9 {
		t.Fatalf("receipt = %+v", persisted)
	}
	for _, created := range persisted.Created {
		if created.ID == "" || created.Version < 1 {
			t.Fatal("missing version receipt")
		}
	}
	if _, err := os.Stat(filepath.Join(f.cfg.Root, "crm", "definition")); !os.IsNotExist(err) {
		t.Fatal("import wrote definition files")
	}
}

func TestEnumAndPeopleChoices(t *testing.T) {
	for _, test := range []struct {
		name         string
		mapping      []bridge.Mapping
		need         int
		stage, owner any
	}{
		{"open", nil, 2, nil, nil},
		{"invalid enum", []bridge.Mapping{{Column: "Deal Stage", Values: map[string]string{"a": "not-a-stage", "b": ""}}}, 2, nil, nil},
		{"partial enum", []bridge.Mapping{{Column: "Deal Stage", Values: map[string]string{"a": "Won"}}}, 2, "Won", nil},
		{"unknown member", []bridge.Mapping{{Column: "Deal Owner", People: map[string]string{"outsider@example.com": "stranger"}}}, 2, nil, nil},
		{"choices", []bridge.Mapping{{Column: "Deal Stage", Values: map[string]string{"a": "Won", "b": ""}}, {Column: "Deal Owner", People: map[string]string{"outsider@example.com": "raj"}}}, 0, "Won", "raj"},
		{"skip", []bridge.Mapping{{Column: "Deal Stage", Values: map[string]string{"a": "", "b": ""}}, {Column: "Deal Owner", People: map[string]string{"outsider@example.com": ""}}}, 0, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			p := preview(t, f, csvParams("Deal Name,Deal Stage,Deal Owner\nA,a,outsider@example.com\nB,b,mona@acme.com\n", test.mapping...))
			if p.NeedChoice != test.need || p.RowsShown[0].Fields["stage"] != test.stage || p.RowsShown[0].Fields["owner"] != test.owner || p.RowsShown[1].Fields["owner"] != "mona" {
				t.Fatalf("preview = %+v", p)
			}
			if test.need == 0 {
				run(t, f, p.ImportID)
				for _, row := range queryRows(t, f, "deal") {
					if row.Fields["name"] == "B" && row.Fields["stage"] != nil {
						t.Fatal("skipped enum acquired its default")
					}
				}
			}
		})
	}
}

func TestUndoKeepsChangedAndReferencedRecordsAfterRestart(t *testing.T) {
	f := setup(t)
	existing := create(t, f, "company", map[string]any{"name": "Existing"})
	p := preview(t, f, csvParams("Deal Name,Company\nA,New A\nB,New B\nC,Existing\n"))
	run(t, f, p.ImportID)
	var changed, untouched string
	for _, row := range queryRows(t, f, "deal") {
		if row.Fields["name"] == "A" {
			changed = row.ID
		} else {
			untouched = row.ID
		}
	}
	_, err := f.all.Records.Update(context.Background(), f.scope, bridge.UpdateRecordParams{Type: "deal", ID: changed, Set: map[string]any{"value": float64(99)}})
	if err != nil {
		t.Fatal(err)
	}
	f.service = New(f.cfg, f.all)
	f.service.UseSpaces(f.spaces)
	result, err := f.service.Undo(context.Background(), f.scope, bridge.ImportUndoParams{ImportID: p.ImportID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 3 || len(result.KeptChanged) != 2 || !slicesContain(result.KeptChanged, changed) {
		t.Fatalf("undo = %+v", result)
	}
	if len(queryRows(t, f, "deal")) != 1 || len(queryRows(t, f, "company")) != 2 {
		t.Fatal("undo removed changed content or retained untouched records")
	}
	if _, err := f.all.Records.Get(context.Background(), f.scope, bridge.GetRecordParams{Type: "company", ID: existing.ID}); err != nil {
		t.Fatal("undo removed pre-existing reference")
	}
	if _, err := f.all.Records.Get(context.Background(), f.scope, bridge.GetRecordParams{Type: "deal", ID: untouched}); !hasCode(err, bridge.CodeNotFound) {
		t.Fatal("untouched imported record still exists")
	}
	second, err := f.service.Undo(context.Background(), f.scope, bridge.ImportUndoParams{ImportID: p.ImportID})
	if err != nil || second.Removed != 0 {
		t.Fatalf("second undo = %+v %v", second, err)
	}
}
func slicesContain(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func TestRealOnlyAndImportOwnership(t *testing.T) {
	f := setup(t)
	p := preview(t, f, csvParams("Deal Name\nA\n"))
	for _, mode := range []svc.DataMode{svc.DataSample, "draft", ""} {
		scope := f.scope
		scope.Data = mode
		_, err := f.service.Run(context.Background(), scope, bridge.ImportRunParams{ImportID: p.ImportID})
		requireCode(t, err, bridge.CodeConflict, "real_only")
		_, err = f.service.Undo(context.Background(), scope, bridge.ImportUndoParams{ImportID: p.ImportID})
		requireCode(t, err, bridge.CodeConflict, "real_only")
	}
	other := f.scope
	other.Caller.Member = "other"
	_, err := f.service.Run(context.Background(), other, bridge.ImportRunParams{ImportID: p.ImportID})
	requireCode(t, err, bridge.CodeDenied, "")
	run(t, f, p.ImportID)
	_, err = f.service.Undo(context.Background(), other, bridge.ImportUndoParams{ImportID: p.ImportID})
	requireCode(t, err, bridge.CodeDenied, "")
	other.Caller.Role = svc.RoleEditor
	result, err := f.service.Undo(context.Background(), other, bridge.ImportUndoParams{ImportID: p.ImportID})
	if err != nil || result.Removed != 1 {
		t.Fatalf("Editor undo = %+v %v", result, err)
	}
}

func TestCSVValidationAndNotes(t *testing.T) {
	for _, test := range []struct {
		name, text string
		mapping    []bridge.Mapping
		bad        bool
	}{
		{"quoted newline", "Deal Name,Extra\r\n\"A, Inc.\",\"hello\nworld\"\r\n", []bridge.Mapping{{Column: "Extra", As: "note"}}, false},
		{"header only", "Deal Name\n", nil, false},
		{"duplicate header", "Deal Name,Deal Name\nA,B\n", nil, true},
		{"missing header", "", nil, true},
		{"ragged", "Deal Name,Amount\nA\n", nil, true},
		{"invalid field", "Deal Name\nA\n", []bridge.Mapping{{Column: "Deal Name", Field: "missing"}}, true},
		{"invalid choice", "Deal Name\nA\n", []bridge.Mapping{{Column: "Deal Name", As: "other"}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			p, err := f.service.Preview(context.Background(), f.scope, csvParams(test.text, test.mapping...))
			if test.bad {
				requireCode(t, err, bridge.CodeInvalid, "")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "quoted newline" && p.RowsShown[0].Fields["notes"] != "Extra: hello\nworld" {
				t.Fatal(p.RowsShown)
			}
			if test.name == "header only" && p.CanImport {
				t.Fatal("empty import enabled")
			}
		})
	}
	t.Run("notes without a longtext field are skipped", func(t *testing.T) {
		f := setup(t)
		delete(f.app.Records["deal"].Fields, "notes")
		p := preview(t, f, csvParams("Deal Name,Extra\nA,hello\n", bridge.Mapping{Column: "Extra", As: "note"}))
		run(t, f, p.ImportID)
		if _, exists := queryRows(t, f, "deal")[0].Fields["notes"]; exists {
			t.Fatal("created a field")
		}
	})
}

func TestRunValidatesAllRowsBeforeWriting(t *testing.T) {
	f := setup(t)
	p := preview(t, f, csvParams("Deal Name,Company\nA,New A\n,New B\n"))
	_, err := f.service.Run(context.Background(), f.scope, bridge.ImportRunParams{ImportID: p.ImportID})
	requireCode(t, err, bridge.CodeInvalid, "")
	if len(queryRows(t, f, "deal")) != 0 || len(queryRows(t, f, "company")) != 0 {
		t.Fatal("validation left a partial import")
	}
}

func TestRunConcurrentAndDuplicateSourceTitles(t *testing.T) {
	f := setup(t)
	p := preview(t, f, csvParams("Deal Name,Company\nA,New\nA,Skipped company\n"))
	if p.Duplicates != 1 || p.NewRefs["company"] != 1 {
		t.Fatal(p)
	}
	var group sync.WaitGroup
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := f.service.Run(context.Background(), f.scope, bridge.ImportRunParams{ImportID: p.ImportID})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if len(queryRows(t, f, "deal")) != 1 || len(queryRows(t, f, "company")) != 1 || len(f.spaces.entries) != 1 {
		t.Fatal("concurrent Run imported twice")
	}
}

func TestReferencesUseTargetTitleAndQueryAllPages(t *testing.T) {
	f := setup(t)
	f.app.Records["company"].Title = "legal_name"
	f.app.Records["company"].Fields = map[string]*spec.Field{"legal_name": {Type: spec.FieldText, Required: true, Unique: true}}
	// Exceed records.query's page limit to prove reference matching does not
	// misclassify a company on a later page as a new reference.
	for i := range 101 {
		create(t, f, "company", map[string]any{"legal_name": fmt.Sprintf("Company %03d", i)})
	}
	p := preview(t, f, csvParams("Deal Name,Company\nA,Company 100\nB,Brand new\n"))
	if p.NewRefs["company"] != 1 {
		t.Fatal(p.NewRefs)
	}
	run(t, f, p.ImportID)
	page, err := f.all.Records.Query(context.Background(), f.scope, bridge.QueryParams{Type: "company"})
	if err != nil || page.Total != 102 {
		t.Fatalf("companies = %+v %v", page, err)
	}
}

func TestPreviewPersistsAndDoesNotExposeMutableMapping(t *testing.T) {
	f := setup(t)
	mapping := bridge.Mapping{Column: "Deal Stage", Values: map[string]string{"a": "Won"}}
	p := preview(t, f, csvParams("Deal Name,Deal Stage\nA,a\n", mapping))
	mapping.Values["a"] = "not-a-stage"
	f.service = New(f.cfg, f.all)
	result := run(t, f, p.ImportID)
	if result.Created != 1 || queryRows(t, f, "deal")[0].Fields["stage"] != "Won" {
		t.Fatal(result)
	}
}

func TestMultipleRefsAndPeopleAndEnums(t *testing.T) {
	f := setup(t)
	f.app.Records["deal"].Fields["companies"] = &spec.Field{Type: spec.FieldRefs, To: "company"}
	f.app.Records["deal"].Fields["owners"] = &spec.Field{Type: spec.FieldPeople}
	f.app.Records["deal"].Fields["stages"] = &spec.Field{Type: spec.FieldEnums, Values: []string{"Won", "Lost"}}
	p := preview(t, f, csvParams("Deal Name,Companies,Owners,Stages\nA,One;Two;One,mona@acme.com;raj@acme.com,a;b\n",
		bridge.Mapping{Column: "Stages", Values: map[string]string{"a": "Won", "b": ""}},
	))
	if p.NeedChoice != 0 || p.NewRefs["company"] != 2 {
		t.Fatal(p)
	}
	run(t, f, p.ImportID)
	row := queryRows(t, f, "deal")[0]
	if len(row.Fields["companies"].([]any)) != 2 || len(row.Fields["owners"].([]any)) != 2 || !reflect.DeepEqual(row.Fields["stages"], []any{"Won"}) {
		t.Fatal(row)
	}
}
