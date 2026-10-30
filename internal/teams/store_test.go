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
	// The store is the database and its own WAL files; the lock is the only
	// other thing there.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		switch e.Name() {
		case FileName, FileName + "-wal", FileName + "-shm", FileName + ".lock":
		default:
			t.Fatalf("profile holds %s", e.Name())
		}
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
	bad := []byte("{not a database")
	if err := os.WriteFile(Path(dir), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("corrupt store loaded with %v", err)
	}
	called := false
	if err := Update(dir, func(*File) error { called = true; return nil }); err == nil || called {
		t.Fatalf("update over a corrupt store: err %v, fn called %v", err, called)
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
	// Starting again is an empty store that takes writes.
	must(t, Update(dir, func(f *File) error { f.Teams = append(f.Teams, Team{ID: "aaaaaaaaaaaa", Name: "a"}); return nil }))
	if f := mustLoad(t, dir); len(f.Teams) != 1 {
		t.Fatalf("after setting aside: %+v", f.Teams)
	}
}

// A STORE THAT READS IS NEVER SET ASIDE, whatever error sent the caller there.
func TestAReadableStoreIsNotSetAside(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "a"}}))
	if aside, err := SetAside(dir); err == nil || aside != "" {
		t.Fatalf("a good store was set aside: %q %v", aside, err)
	}
	if f := mustLoad(t, dir); len(f.Teams) != 1 {
		t.Fatalf("the store lost its team: %+v", f.Teams)
	}
}

// A STORE ANOTHER PROGRAM OR A NEWER BUILD MADE IS REFUSED, NOT READ AS EMPTY.
func TestAForeignOrNewerStoreIsUnreadable(t *testing.T) {
	for name, pragma := range map[string]string{
		"foreign": "PRAGMA application_id = 7",
		"newer":   fmt.Sprintf("PRAGMA user_version = %d", schemaVersion+1),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "a"}}))
			h, err := acquire(Path(dir), false)
			must(t, err)
			_, err = h.db.Exec(pragma)
			h.release()
			must(t, err)
			forget(Path(dir))
			if _, err := Load(dir); !errors.Is(err, ErrUnreadable) {
				t.Fatalf("loaded with %v", err)
			}
			if err := Update(dir, func(*File) error { return nil }); !errors.Is(err, ErrUnreadable) {
				t.Fatalf("updated with %v", err)
			}
		})
	}
}

// WHAT A LATER BUILD WROTE SURVIVES THIS ONE, through Load and Save and
// through Update.
func TestRoundTripKeepsUnknownFieldsAndTheManager(t *testing.T) {
	dir := t.TempDir()
	in := `{"version":2,"teams":[{"id":"abcdefabcdef","name":"harbor","parent":"","members":[{"key":"k1","file":"","where":"","word":""}],` +
		`"manager":"k1","hue":120,"tier":1,"made":"2026-09-20T10:00:00Z","pinned":true,"rules":{"quiet":["k2"]}}]}`
	if err := storeRaw(dir, []byte(in)); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir)
	if err != nil || len(f.Teams) != 1 || f.Teams[0].Manager != "k1" {
		t.Fatalf("loaded %+v %v", f, err)
	}
	check := func(when string) {
		t.Helper()
		raw, _ := storedRaw(dir)
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
	if err := storeRaw(dir, []byte(`{"version":2,"teams":[{"id":"aaaaaaaaaaaa","name":"a","members":[]}]}`)); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(f *File) error { f.Teams[0].Name = "b"; return nil }); err != nil {
		t.Fatal(err)
	}
	if raw, _ := storedRaw(dir); strings.Contains(string(raw), `"hue"`) {
		t.Fatalf("a writer without a palette invented a colour:\n%s", raw)
	}
	f, _ := LoadHued(dir, reservedForTest)
	if !f.Teams[0].Hued() {
		t.Fatal("the palette's load left the team uncoloured")
	}
	if raw, _ := storedRaw(dir); !strings.Contains(string(raw), `"hue"`) {
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
	if err := storeRaw(dir, []byte(in)); err != nil {
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

// REPAIRS ON LOAD: a manager who is not a member is cleared, members with
// titles get handles, and all of it is written back. A repeated id is the
// store's primary key, so no store holds one; a list handed to Save with one
// gets a new id for the second.
func TestLoadRepairsIdsManagersAndHandles(t *testing.T) {
	dir := t.TempDir()
	in := `{"version":2,"teams":[` +
		`{"id":"aaaaaaaaaaaa","name":"a","manager":"gone","hue":1,"members":[{"key":"k1","word":"Fix the login bug"},{"key":"k2","word":"Fix login page"},{"key":"k3","word":""}]},` +
		`{"id":"bbbbbbbbbbbb","name":"b","hue":2}]}`
	if err := storeRaw(dir, []byte(in)); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	twice := t.TempDir()
	must(t, Save(twice, []Team{{ID: "aaaaaaaaaaaa", Name: "a"}, {ID: "aaaaaaaaaaaa", Name: "b"}}))
	if g := mustLoad(t, twice); len(g.Teams) != 2 || g.Teams[0].ID == g.Teams[1].ID {
		t.Fatalf("a repeated id was kept: %+v", g.Teams)
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

// A FILE FROM BEFORE THE WRAP-UP CLOCK HAS NO wrap KEY, and loading it leaves
// the team with none. Writing it back does not invent one.
func TestAnOldTeamsFileLoadsWithNoWrapUp(t *testing.T) {
	dir := t.TempDir()
	in := `{"version":2,"teams":[{"id":"abcdefabcdef","name":"harbor","parent":"","members":[{"key":"k1","file":"","where":"","word":"one"}],` +
		`"manager":"k1","hue":120,"tier":1,"made":"2026-09-20T10:00:00Z"}]}`
	if err := storeRaw(dir, []byte(in)); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir)
	if err != nil || len(f.Teams) != 1 || f.Teams[0].Wrap != nil || f.Teams[0].Name != "harbor" {
		t.Fatalf("old file: %+v %v", f, err)
	}
	if err := Save(dir, f.Teams); err != nil {
		t.Fatal(err)
	}
	raw, err := storedRaw(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"wrap"`) {
		t.Fatalf("an old team grew a wrap key:\n%s", raw)
	}
}

// THE CLOCK SURVIVES THE FILE. Change writes the start and the bound, the
// stamp moves, a reload reads them back, and a second SetWrap keeps the first.
func TestWrapUpRoundTripThroughChange(t *testing.T) {
	dir := t.TempDir()
	started := time.Date(2026, 9, 24, 15, 4, 0, 0, time.UTC)
	bound := 15 * time.Minute
	if err := Save(dir, []Team{{ID: "abcdefabcdef", Name: "harbor", hued: true, Hue: 1}}); err != nil {
		t.Fatal(err)
	}
	before := Stamp(dir)
	wrote, stamp, err := Change(dir, func(f *File) error {
		return f.SetWrap("abcdefabcdef", started, bound)
	})
	if err != nil {
		t.Fatal(err)
	}
	if stamp == before || stamp == MissingStamp || stamp != Stamp(dir) {
		t.Fatalf("stamp %q, was %q, file %q", stamp, before, Stamp(dir))
	}
	if wrote == nil || wrote.Teams[0].Wrap == nil || !wrote.Teams[0].Wrap.Started.Equal(started) || wrote.Teams[0].Wrap.Bound != bound {
		t.Fatalf("wrote %+v", wrote)
	}
	got, err := Load(dir)
	if err != nil || got.Teams[0].Wrap == nil || !got.Teams[0].Wrap.Started.Equal(started) || got.Teams[0].Wrap.Bound != bound {
		t.Fatalf("reloaded %+v %v", got, err)
	}
	later := started.Add(time.Hour)
	if _, _, err := Change(dir, func(f *File) error {
		return f.SetWrap("abcdefabcdef", later, time.Minute)
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = Load(dir)
	if !got.Teams[0].Wrap.Started.Equal(started) || got.Teams[0].Wrap.Bound != bound {
		t.Fatalf("a second wrap-up reset the clock: %+v", got.Teams[0].Wrap)
	}
	if _, _, err := ChangeIf(dir, "not-the-stamp", func(f *File) error {
		return f.ClearWrap("abcdefabcdef")
	}); !errors.Is(err, ErrStale) {
		t.Fatalf("a stale clear: %v", err)
	}
	if _, _, err := Change(dir, func(f *File) error { return f.ClearWrap("abcdefabcdef") }); err != nil {
		t.Fatal(err)
	}
	got, _ = Load(dir)
	if got.Teams[0].Wrap != nil {
		t.Fatalf("cleared wrap still there: %+v", got.Teams[0].Wrap)
	}
	raw, _ := storedRaw(dir)
	if strings.Contains(string(raw), `"wrap"`) {
		t.Fatalf("a cleared wrap was still written:\n%s", raw)
	}
}
