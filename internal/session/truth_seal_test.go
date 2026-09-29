package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// In the cell layout every truth file sits under .cell/, and meta.json keeps
// the summary alone: no field is in both files.
func TestCellLayoutKeepsTruthUnderTheCellDir(t *testing.T) {
	dir := sessionInCell(t)
	place := Place{Dir: dir}
	for _, path := range []string{place.State(), place.Tasks(), place.truth(placeTeamCursors), layoutOf(dir).metaTruth(dir)} {
		if filepath.Dir(path) != filepath.Join(dir, cellStateDir) {
			t.Errorf("%s is outside .cell/", path)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, placeMeta))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"effort", "approval", "archived", "places", "launchDir"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("meta.json still holds %s:\n%s", field, raw)
		}
	}
	for _, name := range []string{placeState, placeTasks, placeTeamCursors} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s left at the folder's top: %v", name, err)
		}
	}
}

// Seal a session, materialize ONLY the sealed tree (workspace plus .cell/) into
// a fresh folder, and open it: the dials and cursors are the same.
func TestSealedTreeCarriesTheSessionTruth(t *testing.T) {
	t.Setenv(furrow.BinaryEnvVar, "") // the suite pins the engine away; this test seals for real
	bin, err := furrow.ResolveBinary()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	dir := sessionInCell(t)
	c, err := cell.OpenAt(dir, filepath.Base(dir))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	putFile(t, filepath.Join(workspace, "main.go"), "package main\n")
	e := cellstore.Engine{Binary: bin, DataRoot: t.TempDir(), Workspace: workspace}
	sealed, err := e.Seal(context.Background(), c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}

	// A fresh machine: an empty workspace and an empty cell, attached to the
	// same store, then restored to the sealed turn.
	freshWorkspace := t.TempDir()
	freshDir := filepath.Join(t.TempDir(), c.ID)
	if err := os.MkdirAll(filepath.Join(freshDir, cellStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	// Only to attach the store: the restore overwrites .cell/ whole.
	putFile(t, filepath.Join(freshDir, cell.MetaPath), slurp(t, filepath.Join(dir, cell.MetaPath)))
	target, err := cell.OpenAt(freshDir, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	there := cellstore.Engine{Binary: bin, DataRoot: e.DataRoot, Workspace: freshWorkspace}
	if _, err := there.Seal(context.Background(), target, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := there.Restore(context.Background(), target, sealed.Turn.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := slurp(t, filepath.Join(freshWorkspace, "main.go")); got != "package main\n" {
		t.Fatalf("workspace not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(freshDir, placeMeta)); !os.IsNotExist(err) {
		t.Fatalf("the derived meta.json travelled: %v", err)
	}

	for _, name := range []string{placeState, placeTasks, placeTeamCursors} {
		want, got := slurp(t, truthPath(dir, name)), slurp(t, truthPath(freshDir, name))
		if want != got {
			t.Errorf("%s differs after the round trip:\nwant %s\ngot  %s", name, want, got)
		}
	}
	meta, err := LoadMeta(freshDir)
	if err != nil || meta.Effort != "high" || meta.Approval != "allow" || !meta.Archived || len(meta.ArchivedTasks) != 1 {
		t.Errorf("truth meta = %+v, %v", meta, err)
	}
	place := Place{Dir: freshDir}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.Place = Place{Dir: freshDir, Workspace: freshWorkspace}
		cfg.SessionFile = cfg.Place.Transcript()
		cfg.Workspace = freshWorkspace
	})
	a.SettleWrites()
	if refused, _ := filepath.Glob(place.Tasks() + ".refused-*"); len(refused) != 0 {
		t.Errorf("the carried checkpoint was refused: %v", refused)
	}

}

// sessionInCell runs a real session inside a fresh cell, then sets the truth
// fields and files a person would have accrued.
func sessionInCell(t *testing.T) string {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	RunScriptedSession(t, c.Root, "map the parser migration failures")
	meta, err := LoadMeta(c.Root)
	if err != nil || meta.ID == "" {
		t.Fatalf("no meta after a real session: %+v %v", meta, err)
	}
	meta.Effort, meta.Approval, meta.Archived = "high", "allow", true
	meta.ArchivedTasks = map[string]bool{"t1": true}
	meta.LaunchDir = "/launch/here"
	if err := SaveMeta(c.Root, meta); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: c.Root}
	putFile(t, place.Tasks(), `{"type":"tasks","version":1}`+"\n")
	putFile(t, place.truth(placeTeamCursors), `{"team":3}`+"\n")
	if _, err := os.Stat(place.State()); err != nil {
		putFile(t, place.State(), `{"version":1}`+"\n")
	}
	return c.Root
}

func putFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func slurp(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
