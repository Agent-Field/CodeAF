package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
