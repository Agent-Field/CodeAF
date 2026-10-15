package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/competence"
	"github.com/Agent-Field/codeaf/internal/home"
)

// competenceHome is a state root with a ledger of the given results, and a
// project bucket for the one place still worked in.
func competenceHome(t *testing.T) (worked, gone string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	worked, gone = filepath.Join(root, "api"), filepath.Join(root, "ffi")
	conversation := filepath.Join(home.Join("v3", "projects"), encodeWorkspace(worked), "0123456789abcdef")
	if err := os.MkdirAll(conversation, 0o700); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	for index, place := range []string{worked, gone} {
		err := competence.Append(competence.LedgerPath(), competence.Record{
			Kind: competence.KindResult, ID: competence.TaskID("0123456789abcdef", string(rune('1'+index))),
			At: at, Model: "m/flash", Place: place, Work: "parser changes", Title: "Parser changes",
			Source: competence.Source{Kind: competence.SourceTask, Agent: "0123456789abcdef", Task: string(rune('1' + index)),
				Transcript: filepath.Join(conversation, "transcript.jsonl")},
			Checks: []competence.CheckRun{{Command: "go test ./...", Outcome: competence.Passed}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return worked, gone
}

// `codeaf competence --json` IS THE APP'S competence.map, byte for byte the
// same shape, and it never needs the resident's store.
func TestCompetenceJSONIsTheAppsMap(t *testing.T) {
	worked, gone := competenceHome(t)
	var out bytes.Buffer
	if err := runCompetenceTo([]string{"--json", "--db", filepath.Join(t.TempDir(), "absent.db")}, &out, time.Now()); err != nil {
		t.Fatal(err)
	}
	var answer appwire.CompetenceMap
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if answer.Model != "m/flash" || len(answer.Groups) != 4 || answer.Totals.Scopes != 2 || answer.Totals.Results != 2 {
		t.Fatalf("answer %+v", answer)
	}
	removed := map[string]bool{}
	for _, scope := range answer.Groups[1].Scopes {
		removed[scope.Place.Path] = scope.Place.Removed
		if scope.Since == nil || scope.Class != appwire.ClassFrontier {
			t.Fatalf("scope %+v", scope)
		}
	}
	if removed[worked] || !removed[gone] {
		t.Fatalf("removed %v", removed)
	}
	if _, err := os.Stat(filepath.Join(home.Join("v3", "projects"), encodeWorkspace(gone))); !os.IsNotExist(err) {
		t.Fatal("asking whether a place is worked in made its bucket")
	}
	if err := runCompetenceTo([]string{"--scope", "place:x"}, &out, time.Now()); err == nil {
		t.Fatal("--scope without --json was accepted")
	}
}

// AN EMPTY MACHINE IS AN EMPTY MAP, said honestly.
func TestCompetenceJSONOnAnEmptyMachine(t *testing.T) {
	t.Setenv(home.EnvVar, filepath.Join(t.TempDir(), "state"))
	var out bytes.Buffer
	if err := runCompetenceTo([]string{"--json"}, &out, time.Now()); err != nil {
		t.Fatal(err)
	}
	var answer appwire.CompetenceMap
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Model != "" || len(answer.Models) != 0 || len(answer.Groups) != 4 || answer.Groups[0].Scopes == nil {
		t.Fatalf("empty answer %s", out.String())
	}
}

// THE APP'S TWO METHODS: a page of evidence with the task's wire id, and a
// bad scope or cursor answered as bad_params.
func TestTheAppReadsCompetenceEvidence(t *testing.T) {
	competenceHome(t)
	backend := &appBackend{}
	answer, err := backend.CompetenceMap(appwire.CompetenceMapParams{})
	if err != nil {
		t.Fatal(err)
	}
	scope := answer.Groups[1].Scopes[0].ID
	page, err := backend.CompetenceEvidence(appwire.CompetenceEvidenceParams{Model: "m/flash", Scope: scope, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Counts.All != 1 || page.NextCursor != "" {
		t.Fatalf("page %+v", page)
	}
	item := page.Items[0]
	if item.Source.Kind != "task" || item.Source.Agent != "0123456789abcdef" || item.Source.ID != appTaskID("0123456789abcdef", uint64(item.ID[len(item.ID)-1]-'0')) ||
		item.SourceMissing != "" || item.Check == nil || item.Outcome != "passed" {
		t.Fatalf("item %+v", item)
	}
	for _, bad := range []appwire.CompetenceEvidenceParams{
		{Model: "m/flash", Scope: "scope:nope"},
		{Model: "m/flash", Scope: scope, Cursor: "not-a-cursor"},
	} {
		_, err := backend.CompetenceEvidence(bad)
		var wire *appwire.Error
		if !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
			t.Fatalf("%+v answered %v", bad, err)
		}
	}
	if _, err := backend.CompetenceMap(appwire.CompetenceMapParams{Scope: "work:nope"}); err == nil {
		t.Fatal("an unknown filter was accepted")
	}
}

// keysOf is an object's keys, sorted.
func keysOf(t *testing.T, value any) string {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%v is not an object", value)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// THE WIRE'S EXACT SHAPE, from the real adapter over a fixed ledger and clock.
// It pins the public spelling an app decodes — source.id/agent/title, never
// the ledger's task or source_title — and, with COMPETENCE_WIRE_FIXTURE_DIR
// set, writes the two answers there for an app's own regression.
func TestTheCompetenceWireShapeIsExact(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	const agent = "0123456789abcdef"
	const api, ffi = "/work/acme/api", "/work/acme/ffi"
	conversation := filepath.Join(home.Join("v3", "projects"), encodeWorkspace(api), agent)
	if err := os.MkdirAll(conversation, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conversation, "meta.json"), []byte(`{"id":"`+agent+`","title":"Release","workspace":"`+api+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(conversation, "transcript.jsonl")
	gone := filepath.Join(root, "deleted", "transcript.jsonl")
	now := time.Date(2026, 10, 15, 14, 2, 0, 0, time.UTC)
	for _, row := range []struct {
		task, place, title, transcript string
		ago                            time.Duration
		checks                         []competence.CheckRun
	}{
		{"3", api, "Fix the parser", transcript, 24 * time.Hour, []competence.CheckRun{
			{Command: "go vet ./internal/parse/...", Outcome: competence.Passed},
			{Command: "go test ./internal/parse/...", Outcome: competence.Failed}}},
		{"2", api, "Fix the parser", transcript, 48 * time.Hour, []competence.CheckRun{
			{Command: "go test ./internal/parse/...", Outcome: competence.Passed}}},
		{"1", api, "Fix the parser", gone, 72 * time.Hour, []competence.CheckRun{
			{Command: "go test ./internal/parse/...", Outcome: competence.Passed}}},
		{"9", ffi, "Rust bindings", gone, 40 * 24 * time.Hour, []competence.CheckRun{
			{Command: "cargo test", Outcome: competence.Passed}}},
	} {
		err := competence.Append(competence.LedgerPath(), competence.Record{
			Kind: competence.KindResult, ID: competence.TaskID(agent, row.task), At: now.Add(-row.ago),
			Model: "deepseek/deepseek-v4-flash", Place: row.place, PlaceKind: competence.PlaceRepo,
			Work: competence.WorkOf(row.title), Title: row.title,
			Source: competence.Source{Kind: competence.SourceTask, Agent: agent, Task: row.task, Transcript: row.transcript},
			Checks: row.checks,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	reader := competenceReader()
	reader.Now = func() time.Time { return now }
	answer, err := readCompetenceMap(reader, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Groups[1].Scopes) != 1 || len(answer.Groups[3].Scopes) != 1 {
		t.Fatalf("groups %+v", answer.Groups)
	}
	parser := answer.Groups[1].Scopes[0]
	page, err := readCompetenceEvidence(reader, appwire.CompetenceEvidenceParams{Model: answer.Model, Scope: parser.ID, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	mapJSON, _ := json.MarshalIndent(answer, "", "  ")
	pageJSON, _ := json.MarshalIndent(page, "", "  ")

	var wireMap, wirePage map[string]any
	if err := json.Unmarshal(mapJSON, &wireMap); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(pageJSON, &wirePage); err != nil {
		t.Fatal(err)
	}
	groups := wireMap["groups"].([]any)
	wireScope := groups[1].(map[string]any)["scopes"].([]any)[0]
	staleScope := groups[3].(map[string]any)["scopes"].([]any)[0]
	items := wirePage["items"].([]any)
	for name, test := range map[string]struct {
		value any
		want  string
	}{
		"map":         {wireMap, "filters,groups,model,models,omitted,policy,read_at,scope,totals,truncated"},
		"scope":       {wireScope, "class,failed,id,last_at,passed,place,results,since,undone,work"},
		"stale scope": {staleScope, "class,failed,id,last_at,passed,place,results,undone,work"},
		"place":       {wireScope.(map[string]any)["place"], "id,path,place_kind,removed"},
		"page":        {wirePage, "counts,items,model,next_cursor,scope"},
		"item":        {items[0], "at,check,checks,id,outcome,source,source_missing,title"},
		"source":      {items[0].(map[string]any)["source"], "agent,id,kind,title"},
		"check":       {items[0].(map[string]any)["check"], "command,outcome"},
		"omitted":     {wireMap["omitted"], "filters,models,scopes"},
	} {
		if got := keysOf(t, test.value); got != test.want {
			t.Errorf("%s keys %s, want %s", name, got, test.want)
		}
	}
	item := page.Items[0]
	if item.Source.ID != agent+":3" || item.Source.Agent != agent || item.Source.Title != "Release" ||
		item.Outcome != "failed" || item.Check.Command != "go test ./internal/parse/..." || item.SourceMissing != "" {
		t.Fatalf("item %+v", item)
	}
	if answer.Groups[3].Scopes[0].Place.Removed != true || parser.Place.Removed {
		t.Fatalf("removed: parser %v, bindings %v", parser.Place.Removed, answer.Groups[3].Scopes[0].Place.Removed)
	}
	next, err := readCompetenceEvidence(reader, appwire.CompetenceEvidenceParams{Model: answer.Model, Scope: parser.ID, Cursor: page.NextCursor, Limit: 2})
	if err != nil || len(next.Items) != 1 || next.Items[0].SourceMissing != "conversation" || next.Items[0].Source.Title != "" || next.NextCursor != "" {
		t.Fatalf("second page %+v %v", next, err)
	}
	if dir := os.Getenv("COMPETENCE_WIRE_FIXTURE_DIR"); dir != "" {
		nextJSON, _ := json.MarshalIndent(next, "", "  ")
		for name, data := range map[string][]byte{
			"competence-map.json": mapJSON, "competence-evidence-page1.json": pageJSON, "competence-evidence-page2.json": nextJSON,
		} {
			if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// A LEDGER THE READER CANNOT ANSWER FOR IS UNAVAILABLE, in the engine's words,
// from both doors: never a map of part of it.
func TestCompetenceUnavailableHistoryIsAnHonestEngineError(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{competence.ErrHistoryLimit, "checked-result history exceeds this bounded read; competence unavailable"},
		{competence.ErrUnreadableHistory, "checked-result history has an unreadable row; competence unavailable"},
	} {
		var wire *appwire.Error
		got := competenceError(test.err)
		if !errors.As(got, &wire) || wire.Code != appwire.CodeEngine || wire.Message != test.want {
			t.Fatalf("%v became %#v", test.err, got)
		}
	}
}
