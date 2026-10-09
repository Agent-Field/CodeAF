package placegraph

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The rule is date arithmetic on an injected clock, so every boundary below is
// pinned to the nanosecond and no test reads the real clock or sleeps.

var staleNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func staleSnap(places ...Place) *Snapshot {
	return newSnapshot(&State{Version: SchemaVersion, Places: places, Memberships: []Membership{}, Pinned: []string{}})
}

func idle(id string, opened time.Time) Place {
	return Place{ID: id, Name: id, Parents: []string{}, CreatedAt: staleNow.Add(-400 * day), LastOpenedAt: opened}
}

func staleIDs(list []StalePlace) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.ID)
	}
	return out
}

func TestSixtyDaysIsInclusiveToTheNanosecond(t *testing.T) {
	sixty := StaleAfterDays * day
	snap := staleSnap(
		idle("just_under", staleNow.Add(-sixty+time.Nanosecond)),
		idle("exactly", staleNow.Add(-sixty)),
		idle("just_over", staleNow.Add(-sixty-time.Nanosecond)),
	)
	got := StalePlaces(StaleInput{Snap: snap, Now: staleNow})
	if want := "just_over exactly"; strings.Join(staleIDs(got), " ") != want {
		t.Fatalf("got %v, want %s (longest untouched first, 60 days counted)", staleIDs(got), want)
	}
	if got[1].DaysUntouched != 60 || got[0].DaysUntouched != 60 {
		t.Fatalf("days: %+v", got)
	}
	earlier := StalePlaces(StaleInput{Snap: snap, Now: staleNow.Add(-time.Nanosecond)})
	if strings.Join(staleIDs(earlier), " ") != "just_over" {
		t.Fatalf("a nanosecond earlier only the place already past the line is offered: %v", staleIDs(earlier))
	}
}

func TestAPlaceNeverOpenedIsAsOldAsItsCreation(t *testing.T) {
	made := Place{ID: "made", Name: "made", Parents: []string{}, CreatedAt: staleNow.Add(-61 * day)}
	fresh := Place{ID: "fresh", Name: "fresh", Parents: []string{}, CreatedAt: staleNow.Add(-5 * day)}
	got := StalePlaces(StaleInput{Snap: staleSnap(made, fresh), Now: staleNow})
	if len(got) != 1 || got[0].ID != "made" || got[0].DaysUntouched != 61 {
		t.Fatalf("got %+v", got)
	}
}

func TestAnUnknownAgeIsNeverGuessed(t *testing.T) {
	undated := Place{ID: "undated", Name: "undated", Parents: []string{}}
	if got := StalePlaces(StaleInput{Snap: staleSnap(undated), Now: staleNow}); len(got) != 0 {
		t.Fatalf("a place with no dates was offered: %+v", got)
	}
	if got := StalePlaces(StaleInput{Snap: staleSnap(idle("a", staleNow.Add(-90*day))), Now: time.Time{}}); len(got) != 0 {
		t.Fatalf("no clock, no answer: %+v", got)
	}
}

func TestRecentChatActivityKeepsAPlaceFromBeingStale(t *testing.T) {
	snap := staleSnap(idle("talked", staleNow.Add(-90*day)), idle("quiet", staleNow.Add(-90*day)))
	got := StalePlaces(StaleInput{Snap: snap, Now: staleNow, Activity: map[string]time.Time{"talked": staleNow.Add(-3 * day)}})
	if strings.Join(staleIDs(got), " ") != "quiet" {
		t.Fatalf("got %v", staleIDs(got))
	}
}

func TestPinnedArchivedAndBusyPlacesAreLeftAlone(t *testing.T) {
	old := staleNow.Add(-200 * day)
	archived := idle("archived", old)
	archived.Archived = true
	snap := staleSnap(idle("pinned", old), archived, idle("busy", old), idle("plain", old))
	snap.Pinned = []string{"pinned"}
	got := StalePlaces(StaleInput{Snap: snap, Now: staleNow, Busy: func(id string) bool { return id == "busy" }})
	if strings.Join(staleIDs(got), " ") != "plain" {
		t.Fatalf("got %v, want only the plain place", staleIDs(got))
	}
}

func TestAQuietParentOfALivePlaceIsNotOffered(t *testing.T) {
	old := staleNow.Add(-200 * day)
	parent := idle("parent", old)
	child := idle("child", staleNow.Add(-2*day))
	child.Parents = []string{"parent"}
	archivedKid := idle("kid", staleNow)
	archivedKid.Parents = []string{"parent"}
	archivedKid.Archived = true
	got := StalePlaces(StaleInput{Snap: staleSnap(parent, child), Now: staleNow})
	if len(got) != 0 {
		t.Fatalf("a parent with a place touched two days ago is offered: %+v", got)
	}
	// An archived child is no longer part of the work, so it does not hold the parent up.
	got = StalePlaces(StaleInput{Snap: staleSnap(parent, archivedKid), Now: staleNow})
	if strings.Join(staleIDs(got), " ") != "parent" {
		t.Fatalf("got %v", staleIDs(got))
	}
}

func TestSnoozeEndsAtItsBoundary(t *testing.T) {
	snap := staleSnap(idle("old", staleNow.Add(-90*day)))
	until := staleNow.Add(10 * day)
	in := StaleInput{Snap: snap, Now: until.Add(-time.Nanosecond), Snoozed: map[string]time.Time{"old": until}}
	if got := StalePlaces(in); len(got) != 0 {
		t.Fatalf("still snoozed one nanosecond before the end: %+v", got)
	}
	in.Now = until
	if got := StalePlaces(in); len(got) != 1 {
		t.Fatalf("offered again exactly when the snooze ends: %+v", got)
	}
}

func TestOldestFirstThenNameAndTheListIsBounded(t *testing.T) {
	var places []Place
	for i := 0; i < MaxStaleListed+5; i++ {
		places = append(places, idle(string(rune('a'+i%26))+string(rune('a'+i/26)), staleNow.Add(-time.Duration(70+i)*day)))
	}
	got := StalePlaces(StaleInput{Snap: staleSnap(places...), Now: staleNow})
	if len(got) != MaxStaleListed {
		t.Fatalf("got %d, want the %d cap", len(got), MaxStaleListed)
	}
	for i := 1; i < len(got); i++ {
		if got[i].TouchedAt.Before(got[i-1].TouchedAt) {
			t.Fatalf("not oldest first at %d: %+v", i, got)
		}
	}
	same := staleSnap(idle("b", staleNow.Add(-70*day)), idle("a", staleNow.Add(-70*day)))
	if g := StalePlaces(StaleInput{Snap: same, Now: staleNow}); strings.Join(staleIDs(g), " ") != "a b" {
		t.Fatalf("equal ages should sort by name: %v", staleIDs(g))
	}
}

// ---- the snooze file -------------------------------------------------------

func TestSnoozeSurvivesReopeningAndEndsOnTheThirtiethDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "places-stale.json")
	book, err := OpenStale(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := book.Snooze("pl_a", staleNow)
	if err != nil {
		t.Fatal(err)
	}
	if want := staleNow.Add(StaleSnoozeDays * day); !got.Until.Equal(want) {
		t.Fatalf("until %v, want %v", got.Until, want)
	}
	// A second window opens the same file.
	other, err := OpenStale(path)
	if err != nil {
		t.Fatal(err)
	}
	until := staleNow.Add(StaleSnoozeDays * day)
	if _, ok := other.Active(until.Add(-time.Nanosecond))["pl_a"]; !ok {
		t.Fatal("another window did not see the snooze")
	}
	if _, ok := other.Active(until)["pl_a"]; ok {
		t.Fatal("the snooze must be over exactly thirty days later")
	}
}

func TestSnoozeAgainReplacesAndDropsEndedOnes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places-stale.json")
	book, _ := OpenStale(path)
	if _, err := book.Snooze("pl_old", staleNow); err != nil {
		t.Fatal(err)
	}
	later := staleNow.Add(40 * day)
	if _, err := book.Snooze("pl_new", later); err != nil {
		t.Fatal(err)
	}
	doc, err := readStaleDoc(path)
	if err != nil || len(doc.Snoozes) != 1 || doc.Snoozes[0].PlaceID != "pl_new" {
		t.Fatalf("the ended snooze should be gone: %+v %v", doc, err)
	}
	again := later.Add(5 * day)
	if _, err := book.Snooze("pl_new", again); err != nil {
		t.Fatal(err)
	}
	doc, _ = readStaleDoc(path)
	if len(doc.Snoozes) != 1 || !doc.Snoozes[0].Until.Equal(again.Add(StaleSnoozeDays*day)) {
		t.Fatalf("a second Not now should replace the first: %+v", doc)
	}
}

func TestADamagedSnoozeFileCostsOnlyARepeatedSuggestion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places-stale.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	book, _ := OpenStale(path)
	if len(book.Active(staleNow)) != 0 {
		t.Fatal("a damaged file must read as no snoozes")
	}
	if _, err := book.Snooze("pl_a", staleNow); err != nil {
		t.Fatal(err)
	}
	kept, _ := filepath.Glob(path + ".damaged-*")
	if len(kept) != 1 {
		t.Fatalf("the damaged bytes were not kept aside: %v", kept)
	}
	if _, ok := book.Active(staleNow)["pl_a"]; !ok {
		t.Fatal("the fresh file did not take the snooze")
	}
}

func TestANewerSnoozeFileIsNeverRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places-stale.json")
	if err := os.WriteFile(path, []byte(`{"version":9,"snoozes":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	book, _ := OpenStale(path)
	if _, err := book.Snooze("pl_a", staleNow); err == nil {
		t.Fatal("an older build overwrote a newer build's file")
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), `"version":9`) {
		t.Fatalf("file changed: %s", raw)
	}
}

func TestSnoozeRefusesAnEmptyOrOversizedID(t *testing.T) {
	book, _ := OpenStale(filepath.Join(t.TempDir(), "places-stale.json"))
	for _, id := range []string{"", strings.Repeat("x", MaxIDBytes+1)} {
		if _, err := book.Snooze(id, staleNow); err == nil {
			t.Fatalf("snoozed %q", id)
		}
	}
}

// A failed write, short write, sync or rename must reach the caller, leave the
// previous file byte-for-byte, and leave no temp file behind.
func TestAFailedSnoozeWriteKeepsTheOldFileAndSaysSo(t *testing.T) {
	boom := errors.New("injected")
	cases := map[string]func(){
		"write": func() {
			staleFileOps.write = func(f *os.File, b []byte) (int, error) { f.Write(b[:len(b)/2]); return len(b) / 2, boom }
		},
		"short":  func() { staleFileOps.write = func(f *os.File, b []byte) (int, error) { return f.Write(b[:len(b)/2]) } },
		"sync":   func() { staleFileOps.sync = func(*os.File) error { return boom } },
		"rename": func() { staleFileOps.rename = func(string, string) error { return boom } },
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "places-stale.json")
			book, _ := OpenStale(path)
			if _, err := book.Snooze("pl_a", staleNow); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			saved := staleFileOps
			t.Cleanup(func() { staleFileOps = saved })
			inject()
			if _, err := book.Snooze("pl_b", staleNow); err == nil {
				t.Fatal("the failure was swallowed and reported as a saved snooze")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatalf("the old file changed:\n%s", after)
			}
			left, _ := filepath.Glob(filepath.Join(dir, ".places-stale-*.tmp"))
			if len(left) != 0 {
				t.Fatalf("temp files left behind: %v", left)
			}
		})
	}
}

func TestACorruptSnoozeFileIsRefusedBeforeItCostsMemory(t *testing.T) {
	ok := `{"at":"2026-10-09T12:00:00Z","until":"2026-11-08T12:00:00Z"`
	rec := func(id string) string { return fmt.Sprintf(`{"placeId":%q,%s}`, id, ok[1:]) }
	many := make([]string, MaxStaleSnoozes+1)
	for i := range many {
		many[i] = rec(fmt.Sprintf("pl_%d", i))
	}
	bad := map[string]string{
		"no version":     `{"snoozes":[]}`,
		"future version": `{"version":2,"snoozes":[]}`,
		"empty id":       `{"version":1,"snoozes":[` + rec("") + `]}`,
		"reserved id":    `{"version":1,"snoozes":[` + rec(RootID) + `]}`,
		"zero time":      `{"version":1,"snoozes":[{"placeId":"pl_a"}]}`,
		"until<at":       `{"version":1,"snoozes":[{"placeId":"pl_a","at":"2026-10-09T12:00:00Z","until":"2026-10-01T00:00:00Z"}]}`,
		"duplicate":      `{"version":1,"snoozes":[` + rec("pl_a") + `,` + rec("pl_a") + `]}`,
		"one too many":   `{"version":1,"snoozes":[` + strings.Join(many, ",") + `]}`,
		"oversize":       `{"version":1,"pad":"` + strings.Repeat("x", maxStaleBytes) + `"}`,
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "places-stale.json")
			os.WriteFile(path, []byte(body), 0o600)
			if _, err := readStaleDoc(path); err == nil {
				t.Fatal("accepted")
			}
			if n := len((&StaleBook{path: path}).Active(staleNow)); n != 0 {
				t.Fatalf("a refused file still hid %d places", n)
			}
		})
	}
	// The cap is exact: a full, valid file reads.
	path := filepath.Join(t.TempDir(), "places-stale.json")
	os.WriteFile(path, []byte(`{"version":1,"snoozes":[`+strings.Join(many[:MaxStaleSnoozes], ",")+`]}`), 0o600)
	if doc, err := readStaleDoc(path); err != nil || len(doc.Snoozes) != MaxStaleSnoozes {
		t.Fatalf("a full valid file must read: %v", err)
	}
	if info, _ := os.Stat(path); info.Size() > maxStaleBytes {
		t.Fatalf("maxStaleBytes %d is smaller than a full realistic file (%d)", maxStaleBytes, info.Size())
	}
}
