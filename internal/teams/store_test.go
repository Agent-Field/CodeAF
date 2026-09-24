package teams

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// reservedForTest stands in for a palette's reserved hues.
var reservedForTest = []float64{70, 145, 25, 260}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	made := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	want := []Team{
		{ID: "0a0a0a0a0a0a", Name: "port", Made: made, Hue: 40, hued: true, Members: []Member{{Key: "k1", File: "f1", Where: "/w/a", Word: "one", Handle: "one"}}},
		{ID: "0b0b0b0b0b0b", Name: "docs", Parent: "0a0a0a0a0a0a", Made: made, Hue: 0, Tier: 1, hued: true, Members: []Member{{Key: "k2", File: "f2", Where: "/w/b", Word: "two", Handle: "two"}}},
	}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Teams, want) {
		t.Fatalf("round trip:\n got %#v\nwant %#v", got.Teams, want)
	}
	// The write is a rename, so nothing temporary is left beside the file;
	// the lock is the only other thing there.
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != FileName+","+FileName+".lock" {
		t.Fatalf("profile holds %v", names)
	}
}

func TestMissingFileIsNoTeamsAndNoError(t *testing.T) {
	dir := t.TempDir()
	f, err := Load(dir)
	if err != nil || f == nil || f.Teams != nil {
		t.Fatalf("missing file: %+v, %v", f, err)
	}
	// Reading made nothing.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a load of nothing left %v", entries)
	}
	// An Update that adds nothing does not create the file either.
	if err := Update(dir, func(*File) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("an empty update created the file: %v", err)
	}
}

// AN EMPTY PROFILE DIRECTORY IS THE ORDINARY LAUNCH: the file and the Traffic
// log go to this process's own profile, never nowhere.
func TestTheOrdinaryLaunchIsTheProfilesOwn(t *testing.T) {
	if got, want := Path(""), config.ProfilePath("", FileName); got != want || !filepath.IsAbs(got) {
		t.Fatalf("teams at %q, the profile keeps its files at %q", got, want)
	}
	if got, want := TrafficPath("", "abc"), config.ProfilePath("", filepath.Join("teams", "abc", "traffic.jsonl")); got != want || !filepath.IsAbs(got) {
		t.Fatalf("traffic at %q, want %q", got, want)
	}
}

func TestCorruptFileErrorsIsNotClobberedAndCanBeSetAside(t *testing.T) {
	dir := t.TempDir()
	bad := []byte("{not json")
	if err := os.WriteFile(Path(dir), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("corrupt file loaded without error")
	}
	called := false
	if err := Update(dir, func(*File) error { called = true; return nil }); err == nil || called {
		t.Fatalf("update over a corrupt file: err %v, fn called %v", err, called)
	}
	if raw, _ := os.ReadFile(Path(dir)); string(raw) != string(bad) {
		t.Fatalf("the file became %q", raw)
	}
	aside, err := SetAside(dir)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(aside); string(raw) != string(bad) || !strings.HasPrefix(aside, Path(dir)+".unreadable-") {
		t.Fatalf("set aside to %s holding %q", aside, raw)
	}
}

// THE FIRST BUILD'S FILE BECOMES TEAMS, ONCE, AND NOTHING IS LOST.
func TestMigratesTheFirstBuildsFile(t *testing.T) {
	dir := t.TempDir()
	v1 := `{"spaces":[` +
		`{"name":"harbor","members":[{"key":"k1","file":"f1","where":"/w/a","word":"one"}],"made":"2026-09-20T10:00:00Z"},` +
		`{"name":"orbit","members":[{"key":"k2"}],"made":"2026-09-21T10:00:00Z","hue":200,"tier":0},` +
		`{"name":"lumen","members":[]}]}`
	legacy := filepath.Join(dir, LegacyFileName)
	if err := os.WriteFile(legacy, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	// The colours the first build gave this file, worked out the way it did.
	first := &File{}
	if err := json.Unmarshal([]byte(`[{"name":"harbor"},{"name":"orbit","hue":200},{"name":"lumen"}]`), &first.Teams); err != nil {
		t.Fatal(err)
	}
	first.Colour(reservedForTest)

	f, err := LoadHued(dir, reservedForTest)
	if err != nil {
		t.Fatal(err)
	}
	got := f.Teams
	if len(got) != 3 || got[0].Name != "harbor" || got[1].Name != "orbit" || got[2].Name != "lumen" {
		t.Fatalf("migrated %+v", got)
	}
	if !reflect.DeepEqual(got[0].Members, []Member{{Key: "k1", File: "f1", Where: "/w/a", Word: "one", Handle: "one", HandleBy: HandleByWords}}) {
		t.Fatalf("members lost: %+v", got[0].Members)
	}
	seen := map[string]bool{}
	for i, tm := range got {
		if len(tm.ID) != 12 || strings.Trim(tm.ID, "0123456789abcdef") != "" || seen[tm.ID] {
			t.Fatalf("team %d has id %q", i, tm.ID)
		}
		seen[tm.ID] = true
		if tm.Parent != "" || tm.Manager != "" {
			t.Fatalf("team %d came up with parent %q manager %q", i, tm.Parent, tm.Manager)
		}
		if tm.HueSpec() != first.Teams[i].HueSpec() {
			t.Fatalf("team %d drawn %v before and %v after", i, first.Teams[i].HueSpec(), tm.HueSpec())
		}
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("spaces.json is still there: %v", err)
	}
	if raw, err := os.ReadFile(legacy + ".migrated"); err != nil || string(raw) != v1 {
		t.Fatalf("the old file was not kept as it was: %q %v", raw, err)
	}
	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Version int               `json:"version"`
		Teams   []json.RawMessage `json:"teams"`
	}
	if err := json.Unmarshal(raw, &d); err != nil || d.Version != 2 || len(d.Teams) != 3 {
		t.Fatalf("teams.json is %s", raw)
	}
	again, err := LoadHued(dir, reservedForTest)
	if err != nil || !reflect.DeepEqual(again.Teams, got) {
		t.Fatalf("reloaded %+v, %v", again, err)
	}
}

// A MIGRATION THROUGH Update is the same migration: the writer that gets there
// first carries the old list over.
func TestUpdateMigratesTheFirstBuildsFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, LegacyFileName)
	if err := os.WriteFile(legacy, []byte(`{"spaces":[{"name":"harbor","members":[{"key":"k1","word":"one"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(f *File) error {
		if len(f.Teams) != 1 || f.Teams[0].Name != "harbor" {
			return fmt.Errorf("update saw %+v", f.Teams)
		}
		return f.SetManager(f.Teams[0].ID, "k1")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Fatalf("spaces.json not moved aside: %v", err)
	}
	f, _ := Load(dir)
	if len(f.Teams) != 1 || f.Teams[0].Manager != "k1" {
		t.Fatalf("after update: %+v", f.Teams)
	}
}

// A TEAMS FILE ALREADY THERE WINS, and the old one is left alone.
func TestMigrationLeavesTheOldFileWhenTeamsExist(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "kept", hued: true}}); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, LegacyFileName)
	if err := os.WriteFile(legacy, []byte(`{"spaces":[{"name":"old"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := LoadHued(dir, nil)
	if err != nil || len(f.Teams) != 1 || f.Teams[0].Name != "kept" {
		t.Fatalf("loaded %+v %v", f, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("spaces.json was moved though teams.json was there: %v", err)
	}
}

// WHAT A LATER BUILD WROTE SURVIVES THIS ONE, through Load and Save and
// through Update.
func TestRoundTripKeepsUnknownFieldsAndTheManager(t *testing.T) {
	dir := t.TempDir()
	in := `{"version":2,"teams":[{"id":"abcdefabcdef","name":"harbor","parent":"","members":[{"key":"k1","file":"","where":"","word":""}],` +
		`"manager":"k1","hue":120,"tier":1,"made":"2026-09-20T10:00:00Z","pinned":true,"rules":{"quiet":["k2"]}}]}`
	if err := os.WriteFile(Path(dir), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir)
	if err != nil || len(f.Teams) != 1 || f.Teams[0].Manager != "k1" {
		t.Fatalf("loaded %+v %v", f, err)
	}
	check := func(when string) {
		t.Helper()
		raw, _ := os.ReadFile(Path(dir))
		var d struct {
			Teams []map[string]json.RawMessage `json:"teams"`
		}
		if err := json.Unmarshal(raw, &d); err != nil || len(d.Teams) != 1 {
			t.Fatalf("%s: saved %s", when, raw)
		}
		for key, want := range map[string]string{"pinned": `true`, "rules": `{"quiet":["k2"]}`, "manager": `"k1"`, "id": `"abcdefabcdef"`, "hue": `120`, "tier": `1`} {
			var flat bytes.Buffer
			if err := json.Compact(&flat, d.Teams[0][key]); err != nil || flat.String() != want {
				t.Fatalf("%s: %s saved as %s, want %s\n%s", when, key, d.Teams[0][key], want, raw)
			}
		}
	}
	if err := Save(dir, f.Teams); err != nil {
		t.Fatal(err)
	}
	check("save")
	if err := Update(dir, func(f *File) error { f.Teams[0].Name = "dock"; return nil }); err != nil {
		t.Fatal(err)
	}
	check("update")
}

// A TEAM WITH NO COLOUR IS WRITTEN WITH NONE by a writer that has no palette,
// so the interface still colours it on its next load rather than reading 0 as
// a choice.
func TestAnUncolouredTeamStaysUncolouredWithoutAPalette(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"version":2,"teams":[{"id":"aaaaaaaaaaaa","name":"a","members":[]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(f *File) error { f.Teams[0].Name = "b"; return nil }); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(Path(dir)); strings.Contains(string(raw), `"hue"`) {
		t.Fatalf("a writer without a palette invented a colour:\n%s", raw)
	}
	f, _ := LoadHued(dir, reservedForTest)
	if !f.Teams[0].Hued() {
		t.Fatal("the palette's load left the team uncoloured")
	}
	if raw, _ := os.ReadFile(Path(dir)); !strings.Contains(string(raw), `"hue"`) {
		t.Fatalf("the colour was not written back:\n%s", raw)
	}
}

// TWO WRITERS NEVER LOSE A WRITE. Each Update reads the file fresh under the
// lock, so every member either goroutine adds is there at the end.
func TestConcurrentUpdatesNeverLoseAWrite(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "busy"}}); err != nil {
		t.Fatal(err)
	}
	const each = 40
	var wg sync.WaitGroup
	errs := make(chan error, 2*each)
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				key := fmt.Sprintf("w%d-%d", w, i)
				errs <- Update(dir, func(f *File) error {
					return f.AddMember("aaaaaaaaaaaa", Member{Key: key, Word: "task " + key})
				})
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	f, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(f.Teams[0].Members); n != 2*each {
		t.Fatalf("%d members after %d adds", n, 2*each)
	}
	handles := map[string]bool{}
	for _, m := range f.Teams[0].Members {
		if m.Handle == "" || handles[m.Handle] {
			t.Fatalf("handle %q missing or repeated", m.Handle)
		}
		handles[m.Handle] = true
	}
}

// A WRITER THAT HOLDS THE LOCK TOO LONG IS A REFUSAL, NOT A WAIT FOREVER, and
// the refused write touched nothing.
func TestABusyLockIsRefusedAfterTheWait(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "a"}}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = withLock(dir, lockWait, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)
	err := lockedAt(lockPath(dir), 20*time.Millisecond, func() error {
		t.Error("ran under a lock somebody else holds")
		return nil
	})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("a held lock answered %v", err)
	}
	// A load does not wait on it.
	start := time.Now()
	if _, err := Load(dir); err != nil || time.Since(start) > time.Second {
		t.Fatalf("load under a held lock: %v after %v", err, time.Since(start))
	}
}

// A FILE THAT LOOPS OR NAMES A MISSING PARENT IS PUT RIGHT ON LOAD.
func TestLoadCutsLoopsAndMissingParents(t *testing.T) {
	dir := t.TempDir()
	in := `{"version":2,"teams":[` +
		`{"id":"aaaaaaaaaaaa","name":"a","parent":"bbbbbbbbbbbb","hue":1},` +
		`{"id":"bbbbbbbbbbbb","name":"b","parent":"aaaaaaaaaaaa","hue":2},` +
		`{"id":"cccccccccccc","name":"c","parent":"gone00000000","hue":3}]}`
	if err := os.WriteFile(Path(dir), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := f.Teams
	if got[2].Parent != "" {
		t.Fatalf("a missing parent was kept: %q", got[2].Parent)
	}
	if ParentLoops(got, got[0].ID, got[0].Parent) || ParentLoops(got, got[1].ID, got[1].Parent) {
		t.Fatalf("the loop is still there: %+v", got)
	}
}

// REPAIRS ON LOAD: a repeated id gets a new one, a manager who is not a member
// is cleared, members with titles get handles, and all of it is written back.
func TestLoadRepairsIdsManagersAndHandles(t *testing.T) {
	dir := t.TempDir()
	in := `{"version":2,"teams":[` +
		`{"id":"aaaaaaaaaaaa","name":"a","manager":"gone","hue":1,"members":[{"key":"k1","word":"Fix the login bug"},{"key":"k2","word":"Fix login page"},{"key":"k3","word":""}]},` +
		`{"id":"aaaaaaaaaaaa","name":"b","hue":2}]}`
	if err := os.WriteFile(Path(dir), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Teams[0].ID == f.Teams[1].ID {
		t.Fatal("a repeated id was kept")
	}
	if f.Teams[0].Manager != "" {
		t.Fatalf("a manager who is not a member was kept: %q", f.Teams[0].Manager)
	}
	var handles []string
	for _, m := range f.Teams[0].Members {
		handles = append(handles, m.Handle)
	}
	if strings.Join(handles, ",") != "login,page," {
		t.Fatalf("handles %q", handles)
	}
	again, _ := Load(dir)
	if !reflect.DeepEqual(again.Teams, f.Teams) {
		t.Fatalf("the repair was not written:\n%+v\n%+v", again.Teams, f.Teams)
	}
}
