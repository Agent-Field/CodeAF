package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

type workstreamWireContract struct {
	Methods []string `json:"methods"`
	Events  []struct {
		Event    string `json:"event"`
		DataType string `json:"data_type"`
	} `json:"events"`
	Errors []workstreamWireErrorType `json:"errors"`
	Enums  map[string][]string       `json:"enums"`
	Types  map[string]map[string]struct {
		Type     string `json:"type"`
		Required bool   `json:"required"`
	} `json:"types"`
}

type workstreamWireErrorType struct {
	Code       string  `json:"code"`
	DeskLocal  bool    `json:"desk_local"`
	DetailType *string `json:"detail_type"`
}

type workstreamWireExamples struct {
	Method     string   `json:"method"`
	Event      string   `json:"event"`
	ParamsType string   `json:"params_type"`
	ResultType string   `json:"result_type"`
	DataType   string   `json:"data_type"`
	Errors     []string `json:"errors"`
	Examples   struct {
		Params []json.RawMessage `json:"params"`
		Result []json.RawMessage `json:"result"`
		Error  []json.RawMessage `json:"error"`
		Data   []json.RawMessage `json:"data"`
	} `json:"examples"`
}

type workstreamWireDocQuestion struct {
	Path     string
	Question string
}

// Each entry is a difference between the engine and the wire contract that the
// contract must settle (a doc question), tolerated only at its path. Each
// alternate element must still match its own explicit example and pass
// conforms. An entry is stale once its path no longer differs.
var workstreamWireDocQuestions = []workstreamWireDocQuestion{}

type workstreamWireComparison struct {
	Questions map[string]string
	Differed  map[string]bool
}

func workstreamWireDecode(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("want one JSON value, got trailing data: %v", err)
	}
	return value
}

func workstreamWireEncode(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func workstreamWireKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func workstreamWireKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func workstreamWireNoNulls(path string, value any) []string {
	var failures []string
	switch value := value.(type) {
	case nil:
		failures = append(failures, path+": null is never allowed; optional fields must be absent")
	case map[string]any:
		for _, key := range workstreamWireKeys(value) {
			failures = append(failures, workstreamWireNoNulls(path+"."+key, value[key])...)
		}
	case []any:
		for i, item := range value {
			failures = append(failures, workstreamWireNoNulls(fmt.Sprintf("%s[%d]", path, i), item)...)
		}
	}
	return failures
}

// conforms is shared by actual wire replies and every declared example type.
// It deliberately uses the independent contract rather than Go wire structs.
func (contract *workstreamWireContract) conforms(path string, value any, typeString string) []string {
	if value == nil {
		return workstreamWireNoNulls(path, value)
	}
	want := func() []string {
		return []string{fmt.Sprintf("%s: want %s, got %s", path, typeString, workstreamWireKind(value))}
	}
	if element, array := strings.CutSuffix(typeString, "[]"); array {
		items, ok := value.([]any)
		if !ok {
			return want()
		}
		var failures []string
		for i, item := range items {
			failures = append(failures, contract.conforms(fmt.Sprintf("%s[%d]", path, i), item, element)...)
		}
		return failures
	}
	if words, enum := contract.Enums[typeString]; enum {
		word, ok := value.(string)
		if !ok {
			return want()
		}
		if !slices.Contains(words, word) {
			return []string{fmt.Sprintf("%s: %q is not in %s %v", path, word, typeString, words)}
		}
		return nil
	}
	if fields, object := contract.Types[typeString]; object {
		values, ok := value.(map[string]any)
		if !ok {
			return want()
		}
		var failures []string
		for _, key := range workstreamWireKeys(values) {
			field, known := fields[key]
			if !known {
				failures = append(failures, path+"."+key+": unknown field of "+typeString)
				continue
			}
			failures = append(failures, contract.conforms(path+"."+key, values[key], field.Type)...)
		}
		var required []string
		for key, field := range fields {
			if _, present := values[key]; field.Required && !present {
				required = append(required, key)
			}
		}
		slices.Sort(required)
		for _, key := range required {
			failures = append(failures, path+"."+key+": missing required field of "+typeString)
		}
		return failures
	}
	switch typeString {
	case "string":
		if _, ok := value.(string); !ok {
			return want()
		}
	case "string (RFC 3339)":
		text, ok := value.(string)
		if !ok {
			return want()
		}
		if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
			return []string{fmt.Sprintf("%s: invalid RFC 3339 time %q: %v", path, text, err)}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return want()
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return want()
		}
		// Exact arithmetic avoids accepting a rounded fractional number as an integer.
		rational, valid := new(big.Rat).SetString(number.String())
		if !valid || !rational.IsInt() || rational.Sign() < 0 {
			return []string{fmt.Sprintf("%s: want a non-negative integral JSON number, got %s", path, number)}
		}
	case "number (USD)":
		if _, ok := value.(json.Number); !ok {
			return want()
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return want()
		}
		return workstreamWireNoNulls(path, value)
	default:
		return []string{path + ": unknown contract type " + typeString}
	}
	return nil
}

// Prepared params compare arrays by index and length because preparation may
// change only values. Replies compare every array element to the first example;
// an empty example array leaves element validation to the declared field type.
func (comparison *workstreamWireComparison) shape(path string, value, example any, prepared bool) []string {
	if value == nil || example == nil {
		return []string{path + ": null is never allowed"}
	}
	if workstreamWireKind(value) != workstreamWireKind(example) {
		return []string{fmt.Sprintf("%s: want example kind %s, got %s", path, workstreamWireKind(example), workstreamWireKind(value))}
	}
	var failures []string
	switch example := example.(type) {
	case map[string]any:
		actual := value.(map[string]any)
		for _, key := range workstreamWireKeys(example) {
			item, present := actual[key]
			if !present {
				failures = append(failures, path+"."+key+": example field is absent")
				continue
			}
			failures = append(failures, comparison.shape(path+"."+key, item, example[key], prepared)...)
		}
		for _, key := range workstreamWireKeys(actual) {
			if _, present := example[key]; !present {
				failures = append(failures, path+"."+key+": field is absent from example")
			}
		}
	case []any:
		actual := value.([]any)
		if prepared && len(actual) != len(example) {
			failures = append(failures, fmt.Sprintf("%s: preparation changed array length from %d to %d", path, len(example), len(actual)))
		}
		if len(example) == 0 {
			return failures
		}
		if len(actual) == 0 {
			return append(failures, path+": expected a populated example array")
		}
		for i, item := range actual {
			// An answer element is compared with the example element at the
			// same index (arrays may mix shapes, such as a job and a draft
			// plan item); past the example's end, with its first element.
			index := 0
			if i < len(example) {
				index = i
			}
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			differences := comparison.shape(itemPath, item, example[index], prepared)
			if _, question := comparison.Questions[itemPath]; !prepared && question && i < len(example) && len(differences) > 0 {
				comparison.Differed[itemPath] = true
				// The question permits only the shape explicitly illustrated for
				// this element, so it cannot hide new keys, nulls or wrong kinds.
				differences = comparison.shape(itemPath, item, example[i], true)
			}
			failures = append(failures, differences...)
		}
	}
	return failures
}

func workstreamWireCheck(t *testing.T, failures []string) {
	t.Helper()
	for _, failure := range failures {
		t.Error(failure)
	}
}

func workstreamWireLoad(t *testing.T) (workstreamWireContract, map[string]workstreamWireExamples) {
	t.Helper()
	root := filepath.Join("testdata", "workstreams-wire")
	raw, err := os.ReadFile(filepath.Join(root, "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract workstreamWireContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]workstreamWireExamples)
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		if name == "contract" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var examples workstreamWireExamples
		if err := json.Unmarshal(raw, &examples); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		files[name] = examples
	}
	return contract, files
}

// Warm would open real provider/process owners. The existing harness already
// supplies their fakes; method dispatch and Close still belong to appBackend.
type workstreamWireBackend struct{ *appBackend }

func (*workstreamWireBackend) Warm() {}

type workstreamWireUnavailable struct{ *appBackend }

func (*workstreamWireUnavailable) Warm()                   {}
func (*workstreamWireUnavailable) ServesWorkstreams() bool { return false }

type workstreamWireConnection struct {
	In    *io.PipeWriter
	Lines <-chan []byte
	Err   <-chan error
	Next  int
}

func workstreamWireConnect(t *testing.T, backend appwire.Engine) *workstreamWireConnection {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	server := appwire.NewServer(outW)
	go func() {
		done <- server.Serve(ctx, inR, appwire.Hello{Version: "test"}, backend)
		_ = outW.Close()
	}()
	lines := make(chan []byte, 32)
	readErr := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(lines)
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		for scanner.Scan() {
			select {
			case lines <- bytes.Clone(scanner.Bytes()):
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("wire Serve: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("wire Serve did not close the backend")
		}
		cancel()
		_ = inR.Close()
		_ = outR.Close()
		select {
		case <-readerDone:
		case <-time.After(5 * time.Second):
			t.Error("wire reader did not stop")
		}
	})
	connection := &workstreamWireConnection{In: inW, Lines: lines, Err: readErr}
	hello := connection.read(t)
	if hello["event"] != appwire.EventHello {
		t.Fatalf("want hello before replies, got %v", hello)
	}
	return connection
}

func (connection *workstreamWireConnection) read(t *testing.T) map[string]any {
	t.Helper()
	select {
	case raw, open := <-connection.Lines:
		if !open {
			t.Fatalf("wire ended before reply: %v", <-connection.Err)
		}
		frame, ok := workstreamWireDecode(t, raw).(map[string]any)
		if !ok {
			t.Fatalf("wire frame is not an object: %s", raw)
		}
		return frame
	case <-time.After(30 * time.Second):
		t.Fatal("wire reply timed out")
		return nil
	}
}

func (connection *workstreamWireConnection) call(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	connection.Next++
	raw := workstreamWireEncode(t, map[string]any{"id": connection.Next, "method": method, "params": params})
	if _, err := connection.In.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	for {
		frame := connection.read(t)
		id, reply := frame["id"].(json.Number)
		if !reply {
			if _, event := frame["event"].(string); !event {
				t.Fatalf("frame has neither a numeric id nor an event: %v", frame)
			}
			continue
		}
		if id.String() != strconv.Itoa(connection.Next) {
			t.Fatalf("reply id %s, want %d", id, connection.Next)
		}
		if len(frame) != 2 {
			t.Fatalf("reply must have id and one result or error, got keys %v", workstreamWireKeys(frame))
		}
		return frame
	}
}

func workstreamWireResult(t *testing.T, frame map[string]any) any {
	t.Helper()
	result, present := frame["result"]
	if !present || result == nil {
		t.Fatalf("want result without error, got %v", frame)
	}
	return result
}

type workstreamWireFixture struct {
	Backend  *appBackend
	Recorder *appRecorder
	IDs      map[string]string
	Now      time.Time
	Stream   appwire.Workstream
}

func workstreamWireSeed(t *testing.T, name string, situation int) *workstreamWireFixture {
	t.Helper()
	backend, recorder, _, origin := workstreamBackend(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	backend.now = func() time.Time { return now }
	backend.workstreams.archiveChats = func([]string) error { return nil }
	fixture := &workstreamWireFixture{
		Backend: backend, Recorder: recorder, Now: now,
		IDs: map[string]string{"conv_5b9e": origin.ID, "ms_usage": "conformance-milestone", "draft_418c": "conformance-draft"},
	}
	if name == "workstream.create" {
		// An unobserved member job keeps all optional numbers unknown on create.
		return fixture
	}
	zero := name == "workstream.get" && situation == 1
	unknown := name == "workstream.list" && situation == 1
	params := appwire.WorkstreamCreateParams{Name: "Usage-based billing", Why: workstreamPtr("Charge for recorded usage.")}
	if !zero {
		params.OriginChatID = &origin.ID
		if name != "collections.add" {
			ref := workstreamJobRef("1107")
			ref.Factory = workstreamPtr("f_local")
			params.Refs = []appwire.WorkstreamRef{ref}
		}
	}
	if zero {
		params.Why = nil
	}
	made, err := backend.WorkstreamCreate(params)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Stream = made.Workstream
	fixture.IDs["ws_8f2c1a"] = made.Workstream.ID
	fixture.IDs["conv_8d22"] = made.Workstream.AgentID
	if unknown {
		backend.workstreams.spend = func(string) *float64 { return nil }
	} else {
		backend.workstreams.queue = func(context.Context, string) (api.Queue, error) {
			return api.Queue{Jobs: []api.Job{{ID: 1107, Num: 2107, State: "needs_you", Cost: 2.5}}}, nil
		}
	}
	if !zero && !unknown && strings.HasPrefix(name, "workstream.") && name != "workstream.changed" {
		if _, err := backend.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: made.Workstream.ID, Patch: appwire.WorkstreamPatch{BudgetUSD: workstreamPtr(100.0)}}); err != nil {
			t.Fatal(err)
		}
	}
	pages, err := backend.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: made.Workstream.ID})
	if err != nil || len(pages.Pages) != 1 {
		t.Fatalf("seed Overview: %+v, %v", pages, err)
	}
	fixture.IDs["pg_31d0"] = pages.Pages[0].ID
	switch name {
	case "workstream.reopen":
		if _, err := backend.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: made.Workstream.ID}); err != nil {
			t.Fatal(err)
		}
	case "page.update", "page.delete", "page.move", "workstream.changed":
		parent := fixture.IDs["pg_31d0"]
		if name == "page.move" {
			parent = ""
		}
		fixture.page(t, "pg_32e1", parent)
		if name == "page.move" && situation == 1 {
			fixture.page(t, "pg_548c", fixture.IDs["pg_31d0"])
		}
		if name == "page.delete" {
			fixture.page(t, "conformance-descendant", fixture.IDs["pg_32e1"])
		}
	case "page.history", "page.restore":
		fixture.saveByAgent(t)
	}
	if name == "record.list" || name == "record.keep" || name == "record.drop" || name == "record.supersede" || name == "workstream.changed" {
		fixture.record(t)
		if name == "record.supersede" {
			if _, err := backend.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{ID: fixture.IDs["rec_7a1e"]}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if strings.HasPrefix(name, "goahead.") || name == "workstream.changed" {
		fixture.plan(t)
		if name != "goahead.grant" {
			fixture.grant(t)
		}
	}
	return fixture
}

func (fixture *workstreamWireFixture) page(t *testing.T, alias, parent string) {
	t.Helper()
	params := appwire.WorkstreamPageCreateParams{WorkstreamID: fixture.Stream.ID, Title: "Metering decisions"}
	if parent != "" {
		params.ParentID = &parent
	}
	page, err := fixture.Backend.WorkstreamPageCreate(params)
	if err != nil {
		t.Fatal(err)
	}
	fixture.IDs[alias] = page.Page.ID
}

func (fixture *workstreamWireFixture) source() *appwire.WorkstreamSource {
	return &appwire.WorkstreamSource{Kind: "message", ID: "msg_19a2", ChatID: workstreamPtr(fixture.IDs["conv_5b9e"]), Label: workstreamPtr("Billing scope")}
}

func (fixture *workstreamWireFixture) saveByAgent(t *testing.T) {
	t.Helper()
	store, err := fixture.Backend.workstreamStore(false)
	if err != nil || store == nil {
		t.Fatalf("seed page store: %v", err)
	}
	_, _, err = store.SavePage(context.Background(), fixture.IDs["pg_31d0"], 1,
		json.RawMessage(`{"type":"doc","content":[]}`), workstream.Attribution{By: "agent", From: workstreamSource(fixture.source())})
	if err != nil {
		t.Fatal(err)
	}
}

func (fixture *workstreamWireFixture) record(t *testing.T) {
	t.Helper()
	record, err := fixture.Backend.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{
		WorkstreamID: fixture.Stream.ID, Kind: "question", Text: "Should retries count toward usage?", Source: fixture.source(),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.IDs["rec_7a1e"] = record.Record.ID
}

func (fixture *workstreamWireFixture) plan(t *testing.T) {
	t.Helper()
	_, err := fixture.Backend.WorkstreamPlanSet(appwire.WorkstreamPlanSetParams{
		ID:         fixture.Stream.ID,
		Milestones: []appwire.WorkstreamMilestone{{ID: fixture.IDs["ms_usage"], Title: "Recorded usage"}},
		Waves: []appwire.WorkstreamWave{{N: 1, Title: workstreamPtr("Metering"), MilestoneID: workstreamPtr(fixture.IDs["ms_usage"]), Items: []appwire.WorkstreamPlanItem{
			{Kind: "job", Factory: workstreamPtr("f_local"), Job: workstreamPtr(1107)},
			{Kind: "draft", DraftID: workstreamPtr(fixture.IDs["draft_418c"]), Title: workstreamPtr("Expose usage totals"), Repo: workstreamPtr("acme/billing"), Text: workstreamPtr("Show the recorded usage.")},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (fixture *workstreamWireFixture) grant(t *testing.T) {
	t.Helper()
	grant, err := fixture.Backend.WorkstreamGoAheadGrant(appwire.WorkstreamGoAheadGrantParams{
		WorkstreamID: fixture.Stream.ID, Scope: "job", Factory: workstreamPtr("f_local"), Job: workstreamPtr(1107), CapUSD: 40, EndsAt: fixture.Now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.IDs["ga_0c44"] = grant.GoAhead.ID
}

// The decoded example is a fresh tree on every call. Only existing values are
// replaced; shape(..., true) independently proves that keys and kinds survive.
func (fixture *workstreamWireFixture) prepare(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var replace func(any) any
	replace = func(value any) any {
		switch value := value.(type) {
		case map[string]any:
			for key, item := range value {
				if key == "ends_at" {
					value[key] = fixture.Now.Add(time.Hour).Format(time.RFC3339Nano)
				} else {
					value[key] = replace(item)
				}
			}
		case []any:
			for i, item := range value {
				value[i] = replace(item)
			}
		case string:
			if seeded, known := fixture.IDs[value]; known {
				return seeded
			}
		}
		return value
	}
	return replace(workstreamWireDecode(t, raw)).(map[string]any)
}

func (contract *workstreamWireContract) errorType(code string) (workstreamWireErrorType, bool) {
	for _, definition := range contract.Errors {
		if definition.Code == code {
			return definition, true
		}
	}
	return workstreamWireErrorType{}, false
}

func (contract *workstreamWireContract) checkError(t *testing.T, path string, value any, code string, example any, exactMessage bool) {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s: want error object, got %s", path, workstreamWireKind(value))
	}
	workstreamWireCheck(t, workstreamWireNoNulls(path, object))
	if object["code"] != code {
		t.Errorf("%s.code: got %v, want %s", path, object["code"], code)
	}
	message, ok := object["message"].(string)
	if !ok || strings.TrimSpace(message) == "" {
		t.Errorf("%s.message: want non-empty string, got %v", path, object["message"])
	}
	definition, known := contract.errorType(code)
	if !known || definition.DeskLocal {
		t.Fatalf("%s: %s is not an engine-sent contract error", path, code)
	}
	for key := range object {
		if key != "code" && key != "message" && key != "detail" {
			t.Errorf("%s.%s: unexpected error field", path, key)
		}
	}
	detail, present := object["detail"]
	if definition.DetailType == nil {
		if present {
			t.Errorf("%s.detail: must be absent for %s", path, code)
		}
	} else if !present {
		t.Errorf("%s.detail: missing %s", path, *definition.DetailType)
	} else {
		workstreamWireCheck(t, contract.conforms(path+".detail", detail, *definition.DetailType))
	}
	if example != nil {
		comparison := &workstreamWireComparison{}
		workstreamWireCheck(t, comparison.shape(path, object, example, false))
		if exactMessage && object["message"] != example.(map[string]any)["message"] {
			t.Errorf("%s.message: got %q, want %q", path, message, example.(map[string]any)["message"])
		}
	}
}

type workstreamWireErrorCase struct {
	Name    string
	Method  string
	Code    string
	Example string
}

// TestAppWorkstreamWireConformance keeps the table independent of the fixtures:
// adding a contract method, event, file or engine-sent error must require a case.
func TestAppWorkstreamWireConformance(t *testing.T) {
	contract, files := workstreamWireLoad(t)
	cases := []string{
		"workstream.list", "workstream.get", "workstream.create", "workstream.update",
		"workstream.mark_done", "workstream.reopen", "workstream.plan_get", "workstream.plan_set",
		"page.list", "page.get", "page.save", "page.create", "page.update", "page.move",
		"page.delete", "page.history", "page.restore", "record.list", "record.propose",
		"record.keep", "record.drop", "record.supersede", "goahead.list", "goahead.grant",
		"goahead.revoke", "collections.add", "workstream.changed",
	}
	errorCases := []workstreamWireErrorCase{
		{Name: "bad_params", Method: "workstream.get", Code: "bad_params"},
		{Name: "not_found", Method: "workstream.get", Code: "not_found", Example: "workstream.get"},
		{Name: "conflict", Method: "workstream.create", Code: "conflict"},
		{Name: "collection_owner", Method: "collections.add", Code: "in_other_workstream", Example: "collections.add"},
		{Name: "create_owner", Method: "workstream.create", Code: "in_other_workstream", Example: "collections.add"},
		{Name: "page_version", Method: "page.save", Code: "version_conflict", Example: "page.save"},
		{Name: "plan_version", Method: "workstream.plan_set", Code: "version_conflict", Example: "page.save"},
		{Name: "cycle", Method: "page.move", Code: "cycle"},
		{Name: "too_large", Method: "page.save", Code: "too_large"},
		{Name: "unknown_method", Method: "workstream.list", Code: "unknown_method", Example: "workstream.list"},
		{Name: "busy", Method: "workstream.update", Code: "busy"},
	}
	comparison := &workstreamWireComparison{Questions: make(map[string]string), Differed: make(map[string]bool)}
	for _, question := range workstreamWireDocQuestions {
		if question.Path == "" || strings.TrimSpace(question.Question) == "" || comparison.Questions[question.Path] != "" {
			t.Fatalf("invalid or duplicate doc question: %+v", question)
		}
		comparison.Questions[question.Path] = question.Question
	}

	// (a), (c): coverage is checked in both directions, including auxiliary files
	// and all engine-sent errors. No busy exemption is needed: it has a real case.
	if !t.Run("coverage", func(t *testing.T) {
		workstreamWireCoverage(t, &contract, files, cases, errorCases)
	}) {
		return
	}

	// (b), (d): the same strict type validator checks all documentation examples.
	t.Run("shape_rules", func(t *testing.T) {
		for _, rule := range []struct {
			Name  string
			Type  string
			JSON  string
			Valid bool
		}{
			{"string", "string", `"text"`, true},
			{"string_number", "string", `1`, false},
			{"nano_time", "string (RFC 3339)", `"2026-11-02T14:05:00.123456789Z"`, true},
			{"bad_time", "string (RFC 3339)", `"yesterday"`, false},
			{"boolean", "boolean", `false`, true},
			{"boolean_string", "boolean", `"false"`, false},
			{"zero", "integer", `0`, true},
			{"integral_decimal", "integer", `1.0`, true},
			{"fraction", "integer", `1.5`, false},
			{"precise_fraction", "integer", `9007199254740992.1`, false},
			{"negative", "integer", `-1`, false},
			{"numeric_string", "integer", `"1"`, false},
			{"usd", "number (USD)", `2.5`, true},
			{"usd_string", "number (USD)", `"2.5"`, false},
			{"enum", "WorkstreamStage", `"building"`, true},
			{"unknown_enum", "WorkstreamStage", `"working"`, false},
			{"empty_array", "WorkstreamChangedPart[]", `[]`, true},
			{"unknown_array_enum", "WorkstreamChangedPart[]", `["jobs","other"]`, false},
			{"null_array", "WorkstreamChangedPart[]", `null`, false},
			{"optional_absent", "WorkstreamChanged", `{"id":"seeded"}`, true},
			{"optional_null", "WorkstreamChanged", `{"id":"seeded","parts":null}`, false},
			{"missing_required", "WorkstreamChanged", `{"parts":[]}`, false},
			{"unknown_field", "WorkstreamChanged", `{"id":"seeded","extra":true}`, false},
			{"opaque_object", "object", `{"arbitrary":[{"value":0}]}`, true},
			{"opaque_nested_null", "object", `{"arbitrary":[null]}`, false},
		} {
			t.Run(rule.Name, func(t *testing.T) {
				path := "shape rule " + rule.Name + " "
				failures := contract.conforms(path, workstreamWireDecode(t, []byte(rule.JSON)), rule.Type)
				if (len(failures) == 0) != rule.Valid {
					t.Errorf("valid = %v, want %v: %v", len(failures) == 0, rule.Valid, failures)
				}
				for _, failure := range failures {
					if !strings.HasPrefix(failure, path) {
						t.Errorf("failure lost its JSON path: %s", failure)
					}
				}
			})
		}
	})

	// (b), (d): malformed examples are contract failures, before any engine call.
	t.Run("doc_self_check", func(t *testing.T) {
		for _, name := range cases {
			t.Run(name, func(t *testing.T) {
				examples := files[name]
				for _, part := range []struct {
					Name string
					Type string
					Rows []json.RawMessage
				}{
					{"params", examples.ParamsType, examples.Examples.Params},
					{"result", examples.ResultType, examples.Examples.Result},
					{"data", examples.DataType, examples.Examples.Data},
				} {
					for i, raw := range part.Rows {
						typeString := part.Type
						if name == "collections.add" {
							typeString = "object"
						}
						path := fmt.Sprintf("%s %s[%d] ", name, part.Name, i)
						workstreamWireCheck(t, contract.conforms(path, workstreamWireDecode(t, raw), typeString))
					}
				}
				for i, raw := range examples.Examples.Error {
					value := workstreamWireDecode(t, raw)
					object, ok := value.(map[string]any)
					if !ok {
						t.Fatalf("%s error[%d] is not an object", name, i)
					}
					code, ok := object["code"].(string)
					if !ok || (name != "collections.add" && !slices.Contains(examples.Errors, code)) {
						t.Fatalf("%s error[%d] has unlisted code %v", name, i, object["code"])
					}
					contract.checkError(t, fmt.Sprintf("%s error[%d]", name, i), value, code, nil, false)
				}
			})
		}
	})

	// (b), (d): each params/result pairing gets an isolated seeded situation.
	// A single params example with multiple results exercises every situation.
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			examples := files[name]
			if examples.Event != "" {
				workstreamWireEvent(t, &contract, examples, comparison)
				return
			}
			count := max(len(examples.Examples.Params), len(examples.Examples.Result))
			for i := 0; i < count; i++ {
				t.Run(fmt.Sprintf("example_%d", i), func(t *testing.T) {
					paramsIndex, resultIndex := i, i
					if len(examples.Examples.Params) == 1 {
						paramsIndex = 0
					}
					if len(examples.Examples.Result) == 1 {
						resultIndex = 0
					}
					fixture := workstreamWireSeed(t, name, i)
					params := fixture.prepare(t, examples.Examples.Params[paramsIndex])
					paramsPath := fmt.Sprintf("%s params[%d] ", name, paramsIndex)
					workstreamWireCheck(t, comparison.shape(paramsPath, params, workstreamWireDecode(t, examples.Examples.Params[paramsIndex]), true))
					if examples.ParamsType != "" {
						workstreamWireCheck(t, contract.conforms(paramsPath, params, examples.ParamsType))
					}
					connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: fixture.Backend})
					result := workstreamWireResult(t, connection.call(t, name, params))
					resultPath := fmt.Sprintf("%s result[%d] ", name, resultIndex)
					if examples.ResultType != "" {
						workstreamWireCheck(t, contract.conforms(resultPath, result, examples.ResultType))
					}
					workstreamWireCheck(t, workstreamWireNoNulls(resultPath, result))
					workstreamWireCheck(t, comparison.shape(resultPath, result, workstreamWireDecode(t, examples.Examples.Result[resultIndex]), false))
					if name == "collections.add" {
						workstreamWireCollectionResult(t, result)
					}
				})
			}
		})
	}

	// (c): every engine-sent code is produced by a real wire call, including
	// both ownership doors and both optimistic-version doors.
	t.Run("errors", func(t *testing.T) {
		for _, errorCase := range errorCases {
			t.Run(errorCase.Name, func(t *testing.T) {
				workstreamWireError(t, &contract, files, errorCase)
			})
		}
	})

	// (d): fixing an example or changing the observed discrepancy requires
	// removing its exception; doc questions cannot silently become permanent.
	t.Run("doc_questions_are_current", func(t *testing.T) {
		for _, question := range workstreamWireDocQuestions {
			if !comparison.Differed[question.Path] {
				t.Errorf("stale doc question at %s: %s", question.Path, question.Question)
			}
		}
	})
}

func workstreamWireCoverage(t *testing.T, contract *workstreamWireContract, files map[string]workstreamWireExamples, cases []string, errorCases []workstreamWireErrorCase) {
	t.Helper()
	if !reflect.DeepEqual(contract.Methods, appwire.WorkstreamMethods) {
		t.Errorf("contract methods/order %v differ from appwire.WorkstreamMethods %v", contract.Methods, appwire.WorkstreamMethods)
	}
	counts := make(map[string]int)
	for _, name := range cases {
		counts[name]++
		if _, exists := files[name]; !exists {
			t.Errorf("stale case %s has no example file", name)
		}
	}
	events := make(map[string]string)
	for _, event := range contract.Events {
		if _, duplicate := events[event.Event]; duplicate {
			t.Errorf("duplicate contract event %s", event.Event)
		}
		events[event.Event] = event.DataType
		if _, exists := files[event.Event]; !exists || counts[event.Event] != 1 {
			t.Errorf("event %s needs one file and one case", event.Event)
		}
	}
	methods := make(map[string]bool)
	for _, name := range contract.Methods {
		if methods[name] {
			t.Errorf("duplicate contract method %s", name)
		}
		methods[name] = true
		if _, exists := files[name]; !exists || counts[name] != 1 {
			t.Errorf("method %s needs one file and one case, got %d cases", name, counts[name])
		}
	}
	for name, examples := range files {
		if counts[name] != 1 {
			t.Errorf("file %s.json needs exactly one case, got %d", name, counts[name])
		}
		if dataType, event := events[name]; event {
			if examples.Event != name || examples.Method != "" || examples.DataType != dataType || len(examples.Examples.Data) == 0 {
				t.Errorf("event file %s disagrees with contract or lacks examples", name)
			}
			continue
		}
		if !methods[name] && name != "collections.add" {
			t.Errorf("file/case %s names no contract method or event", name)
		}
		if examples.Method != name || examples.Event != "" || len(examples.Examples.Params) == 0 || len(examples.Examples.Result) == 0 {
			t.Errorf("method file %s disagrees with its name or lacks examples", name)
		}
		params, results := len(examples.Examples.Params), len(examples.Examples.Result)
		if params != results && params != 1 && results != 1 {
			t.Errorf("%s: unpaired example counts %d/%d need explicit situations", name, params, results)
		}
		if name != "collections.add" {
			for _, typeString := range []string{examples.ParamsType, examples.ResultType} {
				if _, known := contract.Types[typeString]; !known {
					t.Errorf("%s: missing declared type %q", name, typeString)
				}
			}
		}
		for _, code := range examples.Errors {
			if _, known := contract.errorType(code); !known {
				t.Errorf("%s: unlisted error code %s", name, code)
			}
		}
	}
	if !slices.Contains(appwire.Methods(), "collections.add") {
		t.Error("collections.add is not a registered wire method")
	}
	coveredErrors := make(map[string]bool)
	caseNames := make(map[string]bool)
	for _, errorCase := range errorCases {
		if caseNames[errorCase.Name] {
			t.Errorf("duplicate error case %s", errorCase.Name)
		}
		caseNames[errorCase.Name] = true
		definition, known := contract.errorType(errorCase.Code)
		if !known || definition.DeskLocal {
			t.Errorf("stale error case %s: code %s is not engine-sent", errorCase.Name, errorCase.Code)
		}
		if _, exists := files[errorCase.Method]; !exists {
			t.Errorf("stale error case %s: missing method file %s", errorCase.Name, errorCase.Method)
		}
		if errorCase.Method != "collections.add" && !slices.Contains(files[errorCase.Method].Errors, errorCase.Code) {
			t.Errorf("%s does not list %s", errorCase.Method, errorCase.Code)
		}
		if errorCase.Example != "" {
			found := false
			for _, raw := range files[errorCase.Example].Examples.Error {
				if workstreamWireDecode(t, raw).(map[string]any)["code"] == errorCase.Code {
					found = true
				}
			}
			if !found {
				t.Errorf("stale error example %s/%s", errorCase.Example, errorCase.Code)
			}
		}
		coveredErrors[errorCase.Code] = true
	}
	listedErrors := make(map[string]bool)
	for _, definition := range contract.Errors {
		if listedErrors[definition.Code] {
			t.Errorf("duplicate contract error %s", definition.Code)
		}
		listedErrors[definition.Code] = true
		if !definition.DeskLocal && !coveredErrors[definition.Code] {
			t.Errorf("engine-sent contract error %s has no wire case", definition.Code)
		}
	}
}

func workstreamWireCollectionResult(t *testing.T, result any) {
	t.Helper()
	object := result.(map[string]any)
	added, addedArray := object["added"].([]any)
	already, alreadyArray := object["already"].([]any)
	if !addedArray || len(added) != 1 || !alreadyArray || len(already) != 0 {
		t.Fatalf("collections.add result: want one added job and empty already array, got %v", result)
	}
	for i, value := range added {
		member := value.(map[string]any)
		if member["kind"] != "factory_job" || member["id"] != "1107" || member["factory"] != "f_local" {
			t.Errorf("collections.add .added[%d]: wrong factory job ref %v", i, member)
		}
		at, ok := member["added_at"].(string)
		if _, err := time.Parse(time.RFC3339Nano, at); !ok || err != nil {
			t.Errorf("collections.add .added[%d].added_at: want RFC 3339 string, got %v", i, member["added_at"])
		}
	}
}

func workstreamWireEvent(t *testing.T, contract *workstreamWireContract, examples workstreamWireExamples, comparison *workstreamWireComparison) {
	t.Helper()
	fixture := workstreamWireSeed(t, "workstream.changed", 0)
	before := len(workstreamEvents(fixture.Recorder))
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: fixture.Backend})
	workstreamWireResult(t, connection.call(t, "workstream.update", map[string]any{
		"id": fixture.Stream.ID, "patch": map[string]any{"why": "Updated through the wire"},
	}))
	events := workstreamEvents(fixture.Recorder)
	if len(events) <= before {
		t.Fatal("wire mutation did not emit workstream.changed")
	}
	for i, event := range events {
		value := workstreamWireDecode(t, workstreamWireEncode(t, event))
		path := fmt.Sprintf("workstream.changed data[%d] ", i)
		workstreamWireCheck(t, contract.conforms(path, value, examples.DataType))
		exampleIndex := 0
		if _, parts := value.(map[string]any)["parts"]; !parts {
			exampleIndex = 1
		}
		if exampleIndex >= len(examples.Examples.Data) {
			t.Fatalf("%s: no example for parts presence", path)
		}
		workstreamWireCheck(t, comparison.shape(path, value, workstreamWireDecode(t, examples.Examples.Data[exampleIndex]), false))
		if event.ID != fixture.Stream.ID {
			t.Errorf("%s.id: got %s, want seeded workstream %s", path, event.ID, fixture.Stream.ID)
		}
	}
}

func workstreamWireError(t *testing.T, contract *workstreamWireContract, files map[string]workstreamWireExamples, errorCase workstreamWireErrorCase) {
	t.Helper()
	fixture := workstreamWireSeed(t, "workstream.get", 0)
	backend := fixture.Backend
	examples := files[errorCase.Method]
	params := fixture.prepare(t, examples.Examples.Params[0])
	var engine appwire.Engine = &workstreamWireBackend{appBackend: backend}
	release := func() {}
	switch errorCase.Name {
	case "bad_params":
		delete(params, "id")
	case "not_found":
		params["id"] = "missing-workstream"
	case "conflict":
		params["name"] = fixture.Stream.Name
	case "collection_owner":
		other := workstreamCreateForTest(t, backend, "Account migration")
		params["id"] = other.ID
	case "create_owner":
		params["name"] = "Another workstream"
	case "page_version":
		fixture.saveByAgent(t)
	case "plan_version":
		fixture.plan(t)
	case "cycle":
		fixture.page(t, "pg_32e1", fixture.IDs["pg_31d0"])
		fixture.page(t, "conformance-descendant", fixture.IDs["pg_32e1"])
		params["id"] = fixture.IDs["pg_32e1"]
		params["parent_id"] = fixture.IDs["conformance-descendant"]
	case "too_large":
		params["doc"] = map[string]any{"type": "doc", "content": []any{
			map[string]any{"type": "paragraph", "content": []any{
				map[string]any{"type": "text", "text": strings.Repeat("x", (1<<20)+1)},
			}},
		}}
	case "unknown_method":
		engine = &workstreamWireUnavailable{appBackend: backend}
	case "busy":
		release = workstreamWireHoldWriter(t, backend)
	default:
		t.Fatalf("unimplemented error case %s", errorCase.Name)
	}
	if errorCase.Name != "bad_params" && errorCase.Name != "too_large" {
		comparison := &workstreamWireComparison{}
		workstreamWireCheck(t, comparison.shape(errorCase.Method+" error params ", params, workstreamWireDecode(t, examples.Examples.Params[0]), true))
	}
	connection := workstreamWireConnect(t, engine)
	frame := connection.call(t, errorCase.Method, params)
	release()
	value, present := frame["error"]
	if !present {
		t.Fatalf("%s: want %s error, got %v", errorCase.Method, errorCase.Code, frame)
	}
	var example any
	if errorCase.Example != "" {
		for _, raw := range files[errorCase.Example].Examples.Error {
			candidate := workstreamWireDecode(t, raw).(map[string]any)
			if candidate["code"] == errorCase.Code {
				example = candidate
				break
			}
		}
	}
	contract.checkError(t, errorCase.Method+" error", value, errorCase.Code, example, errorCase.Code == "unknown_method")
	object := value.(map[string]any)
	if errorCase.Code == "in_other_workstream" {
		detail, ok := object["detail"].(map[string]any)
		if !ok || detail["workstream_id"] != fixture.Stream.ID || detail["workstream_name"] != fixture.Stream.Name || detail["factory"] != "f_local" || detail["job"] != json.Number("1107") {
			t.Errorf("ownership detail does not identify the seeded owner/job: %v", object["detail"])
		}
	}
	if errorCase.Code == "version_conflict" {
		detail, ok := object["detail"].(map[string]any)
		id, version, by := fixture.IDs["pg_31d0"], "2", "agent"
		if errorCase.Name == "plan_version" {
			id, version, by = fixture.Stream.ID, "1", "you"
		}
		if !ok || detail["id"] != id || detail["version"] != json.Number(version) || detail["updated_by"] != by {
			t.Errorf("stale detail does not describe the current seeded version: %v", object["detail"])
		}
	}
}

// Hold a writer on a separate handle to the same real store. The backend must
// exhaust the normal busy timeout and translate that refusal on the wire.
func workstreamWireHoldWriter(t *testing.T, backend *appBackend) func() {
	t.Helper()
	other, err := workspace.Open(backend.collectionsPath)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	unlock := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- other.Write(context.Background(), func(*sql.Tx) error {
			close(entered)
			<-unlock
			return nil
		})
	}()
	var once sync.Once
	release := func() {
		once.Do(func() {
			close(unlock)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("held writer: %v", err)
				}
			case <-time.After(30 * time.Second):
				t.Error("held writer did not release")
			}
			if err := other.Close(); err != nil {
				t.Errorf("held writer close: %v", err)
			}
		})
	}
	t.Cleanup(release)
	select {
	case <-entered:
	case err := <-done:
		// Put the result back so the shared cleanup can still consume it.
		done <- err
		t.Fatalf("writer did not take the lock: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("writer did not take the lock")
	}
	return release
}
