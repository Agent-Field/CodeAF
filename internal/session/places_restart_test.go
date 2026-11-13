package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestSessionRecordKeepsPlacesOutsideItsRoots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace, outside := t.TempDir(), t.TempDir()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	want := []PlaceRef{
		{Path: filepath.Join(workspace, "context"), Arrival: PlaceSaid},
		{Path: outside, Arrival: PlaceSaid},
	}
	wantTrees := []StandingTree{{Folder: workspace, Dir: outside, Root: workspace}}
	if err := SaveMeta(c.Root, Meta{ID: c.ID, Workspace: workspace, Places: want, Trees: wantTrees}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(c.Root, ".cell", "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record SessionTruth
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Places) != 2 || record.Places[0].Path != "workspace:context" || record.Places[1].Path != outside {
		t.Fatalf("saved places = %+v; want a relative workspace place and the absolute outside place", record.Places)
	}
	reloaded, err := LoadMeta(c.Root)
	if err != nil || !reflect.DeepEqual(reloaded.Places, want) {
		t.Fatalf("reloaded places = %+v, %v; want %+v", reloaded.Places, err, want)
	}
	if !reflect.DeepEqual(reloaded.Trees, wantTrees) {
		t.Fatalf("a tree mixing encodable and absolute fields was lost: %+v", reloaded.Trees)
	}
}

func TestAttachedOutsidePlaceSurvivesEngineReopen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace, outside := t.TempDir(), t.TempDir()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	open := func() *Agent {
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
			cfg.Workspace = workspace
			cfg.Place = Place{Dir: c.Root, Workspace: workspace}
			cfg.SessionFile = cfg.Place.Transcript()
		})
		return a
	}
	first := open()
	ref, err := first.ReferPlace(outside, PlaceSaid)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := open()
	if got := second.Places(); len(got) != 1 || got[0].Path != ref.Path {
		t.Fatalf("reopened places = %+v; want %s", got, ref.Path)
	}
	if !strings.Contains(modelSees(t, second), ref.Path) {
		t.Fatal("the reopened engine did not tell the model about the outside folder")
	}
}

func TestMovedSessionKeepsMissingAbsolutePlace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace, outside := t.TempDir(), t.TempDir()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(c.Root, Meta{ID: c.ID, Workspace: workspace, Places: []PlaceRef{
		{Path: workspace, Arrival: PlaceSaid}, {Path: outside, Arrival: PlaceSaid},
	}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(c.Root, ".cell", "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A second computer has different roots and no copy of the outside folder.
	t.Setenv("HOME", t.TempDir())
	otherWorkspace := t.TempDir()
	other, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	otherRecord := filepath.Join(other.Root, ".cell", "session.json")
	if err := os.WriteFile(otherRecord, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(outside); err != nil {
		t.Fatal(err)
	}
	moved, err := LoadMetaIn(other.Root, otherWorkspace)
	if err != nil || len(moved.Places) != 2 || moved.Places[0].Path != otherWorkspace || moved.Places[1].Path != outside {
		t.Fatalf("moved places = %+v, %v", moved.Places, err)
	}
	if seen := attachedBlock(moved.Places, otherWorkspace); !strings.Contains(seen, outside+" — NOT on this disk right now") {
		t.Fatalf("missing outside folder was not described honestly: %s", seen)
	}
	moved.ID = c.ID
	if err := SaveMeta(other.Root, moved); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(otherRecord)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("saving on the second computer changed the portable record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, ".cell", "session.json"), after, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := LoadMeta(c.Root)
	if err != nil || len(back.Places) != 2 || back.Places[0].Path != workspace || back.Places[1].Path != outside {
		t.Fatalf("places after the second computer saved = %+v, %v", back.Places, err)
	}
}
