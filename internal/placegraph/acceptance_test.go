package placegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The acceptance names for the place graph store. Behaviour the store already
// had is pinned here under the names the lane is held to; the assertions are
// the laws, not a second implementation.

func TestCreateReadRenameRoundTripsManager(t *testing.T) {
	s, path := newStore(t)
	created, _, err := s.CreatePlace(NewPlace{
		Name:    "Inbox",
		Context: Context{Instructions: "keep the voice"},
		Policy:  Policy{Model: "deepseek/deepseek-v4.1-flash", Permissions: "ask"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := snap(t, s).Place(created.ID)
	if !ok || got.Name != "Inbox" || got.Context.Instructions != "keep the voice" || got.Policy.Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("read after create: %+v ok=%v", got, ok)
	}
	if _, err := s.Rename(created.ID, "Inbox work"); err != nil {
		t.Fatal(err)
	}
	got, _ = snap(t, s).Place(created.ID)
	if got.ID != created.ID || got.Name != "Inbox work" {
		t.Fatalf("rename changed identity: %+v", got)
	}

	// Manager is reserved. It arrives as opaque JSON and must survive a later
	// rename without the store interpreting a field of it.
	var st State
	if err := json.Unmarshal(readFile(t, path), &st); err != nil {
		t.Fatal(err)
	}
	const raw = `{"mode":"later","n":[1,2],"keep":true}`
	planted := false
	for i := range st.Places {
		if st.Places[i].ID == created.ID {
			st.Places[i].Manager = json.RawMessage(raw)
			planted = true
		}
	}
	if !planted {
		t.Fatal("created place missing from the file")
	}
	body, err := json.Marshal(&st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(created.ID, "Still inbox"); err != nil {
		t.Fatal(err)
	}
	again, _ := snap(t, s).Place(created.ID)
	if again.Name != "Still inbox" {
		t.Fatalf("name = %q", again.Name)
	}
	var decoded map[string]any
	if err := json.Unmarshal(again.Manager, &decoded); err != nil {
		t.Fatalf("manager was not JSON after rename: %s (%v)", again.Manager, err)
	}
	nums, _ := decoded["n"].([]any)
	if decoded["mode"] != "later" || decoded["keep"] != true || len(nums) != 2 {
		t.Fatalf("manager was interpreted or dropped: %s", again.Manager)
	}
}

func TestAParentEditThatWouldLoopIsRefusedNamingTheLoop(t *testing.T) {
	s, _ := newStore(t)
	alpha := mk(t, s, "Alpha")
	beta := mk(t, s, "Beta", alpha.ID)
	gamma := mk(t, s, "Gamma", beta.ID)
	before, _ := s.Generation()

	_, err := s.AddParent(alpha.ID, gamma.ID)
	if !errors.Is(err, ErrCycle) {
		t.Fatalf("err = %v, want ErrCycle", err)
	}
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("loop %q does not name %s", err.Error(), name)
		}
	}
	if !strings.Contains(err.Error(), "Alpha -> Beta -> Gamma -> Alpha") {
		t.Fatalf("loop = %q", err.Error())
	}
	if got, _ := snap(t, s).Place(alpha.ID); len(got.Parents) != 0 {
		t.Fatalf("a refused loop changed parents: %v", got.Parents)
	}
	if after, _ := s.Generation(); after != before {
		t.Fatalf("generation moved from %d to %d on a refusal", before, after)
	}

	if _, err := s.AddParent(alpha.ID, alpha.ID); !errors.Is(err, ErrCycle) || !strings.Contains(err.Error(), "Alpha -> Alpha") {
		t.Fatalf("self parent: %v", err)
	}
}

func TestChildInheritsFirstParentTintNeverBlends(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	ok(t)(s.SetTint(top.ID, TintTide))
	other := mk(t, s, "Other")
	ok(t)(s.SetTint(other.ID, TintRose))
	child := mk(t, s, "Child", top.ID, other.ID)

	tint := func(id string) Tint {
		t.Helper()
		v, ok := snap(t, s).EffectiveTint(id)
		if !ok {
			t.Fatalf("no tint for %s", id)
		}
		return v
	}
	if tint(child.ID) != TintTide {
		t.Fatalf("child = %s, want the first parent's tide", tint(child.ID))
	}
	ok(t)(s.Reparent(child.ID, []string{other.ID, top.ID}))
	if tint(child.ID) != TintRose {
		t.Fatalf("after the first parent changed = %s, want rose", tint(child.ID))
	}
	grand := mk(t, s, "Grand", child.ID)
	if tint(grand.ID) != TintRose {
		t.Fatalf("grandchild = %s, want the first parent's rose, not a blend", tint(grand.ID))
	}
	ok(t)(s.SetTint(child.ID, TintSage))
	if tint(child.ID) != TintSage || tint(grand.ID) != TintSage {
		t.Fatalf("own tint must win: child %s grand %s", tint(child.ID), tint(grand.ID))
	}
	ok(t)(s.SetTint(child.ID, ""))
	if tint(grand.ID) != TintRose {
		t.Fatalf("cleared override = %s, want rose again", tint(grand.ID))
	}
	ok(t)(s.SetTint(top.ID, ""))
	if tint(top.ID) != TintGraphite {
		t.Fatalf("a top-level place with no tint = %s, want graphite", tint(top.ID))
	}
}

func TestTopLevelTintIsTheLeastUsedHue(t *testing.T) {
	s, _ := newStore(t)
	if got := snap(t, s).NewTopLevelTint(); got != TintTide {
		t.Fatalf("empty graph suggests %s, want tide (palette order)", got)
	}
	var got []Tint
	for i := 0; i < 6; i++ {
		got = append(got, mk(t, s, fmt.Sprintf("P%d", i)).Tint)
	}
	want := []Tint{TintTide, TintIris, TintRose, TintSand, TintSage, TintTide}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("assigned %v, want %v", got, want)
		}
		if got[i] == TintGraphite {
			t.Fatal("graphite is not a top-level assignment")
		}
	}
	// Tide is now used twice and the other four once, so the least-used is iris.
	if next := snap(t, s).NewTopLevelTint(); next != TintIris {
		t.Fatalf("next = %s, want iris", next)
	}
	top := snap(t, s).Places[0]
	child := mk(t, s, "Kid", top.ID)
	if child.Tint != "" {
		t.Fatalf("a child was given %q; only a top-level place is assigned a hue", child.Tint)
	}
}

func TestGenerationMovesOnEveryWriteAndOnlyThen(t *testing.T) {
	s, path := newStore(t)
	if g, err := s.Generation(); err != nil || g != 0 {
		t.Fatalf("fresh generation = %d, %v", g, err)
	}
	if g, err := ReadGeneration(path); err != nil || g != 0 {
		t.Fatalf("fresh ReadGeneration = %d, %v", g, err)
	}
	p := mk(t, s, "A")
	assertGen := func(want uint64) {
		t.Helper()
		g, err := s.Generation()
		if err != nil || g != want {
			t.Fatalf("generation = %d, %v; want %d", g, err, want)
		}
		disk, err := ReadGeneration(path)
		if err != nil || disk != want {
			t.Fatalf("on disk = %d, %v; want %d", disk, err, want)
		}
		rev, err := s.Revision()
		if err != nil || rev != want {
			t.Fatalf("revision = %d, %v; generation and revision diverged", rev, err)
		}
	}
	assertGen(1)
	if rc, err := s.Rename(p.ID, "A"); err != nil || !rc.Noop() {
		t.Fatalf("same-name rename: %v %+v", err, rc)
	}
	if err := s.TouchOpened(p.ID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	assertGen(1)
	if _, err := s.Rename(p.ID, "B"); err != nil {
		t.Fatal(err)
	}
	assertGen(2)
	if _, err := s.AddParent(p.ID, p.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("self parent: %v", err)
	}
	assertGen(2)
}

func TestTwoWritersNeverLoseAnEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places.json")
	open := func(prefix string) *Store {
		t.Helper()
		n := 0
		var mu sync.Mutex
		s, err := Open(Options{Path: path, NewID: func(p string) string {
			mu.Lock()
			defer mu.Unlock()
			n++
			return fmt.Sprintf("%s%s%d", p, prefix, n)
		}})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := open("a"), open("b")
	const n = 200
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := a
			if i%2 == 1 {
				s = b
			}
			if _, _, err := s.CreatePlace(NewPlace{Name: fmt.Sprintf("P%03d", i)}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	sn := snap(t, a)
	if len(sn.Places) != n {
		t.Fatalf("places = %d, want %d (an edit was lost)", len(sn.Places), n)
	}
	seen := map[string]bool{}
	for _, p := range sn.Places {
		seen[p.Name] = true
	}
	for i := 0; i < n; i++ {
		if !seen[fmt.Sprintf("P%03d", i)] {
			t.Fatalf("missing P%03d", i)
		}
	}
	if sn.Revision != n {
		t.Fatalf("revision = %d, want %d", sn.Revision, n)
	}
	if g, err := b.Generation(); err != nil || g != n {
		t.Fatalf("second store generation = %d, %v", g, err)
	}
}

func TestAnUnreadableFileIsNeverOverwritten(t *testing.T) {
	s, path := newStore(t)
	mk(t, s, "Keep")
	const junk = "this is not json {"
	if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
		t.Fatal(err)
	}
	if g, err := ReadGeneration(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("ReadGeneration = %d, %v; want an error naming %s", g, err, path)
	}
	if g, err := s.Generation(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("Generation = %d, %v", g, err)
	}
	if got := string(readFile(t, path)); got != junk {
		t.Fatalf("a generation read overwrote the file with %q", got)
	}
	if _, err := ReadSnapshot(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("ReadSnapshot = %v, want an error naming the file", err)
	}
	if got := string(readFile(t, path)); got != junk {
		t.Fatalf("a snapshot read overwrote the file with %q", got)
	}
	// The locked store sets the unreadable bytes aside and continues. It does
	// not truncate them: the sidecar is the file, byte for byte.
	if _, _, err := s.CreatePlace(NewPlace{Name: "After"}); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "places.json.corrupt-*"))
	if len(matches) != 1 || string(readFile(t, matches[0])) != junk {
		t.Fatalf("unreadable bytes were not kept: %v", matches)
	}
}

func TestTwoHundredPlacesLoadAndListUnderTenMillis(t *testing.T) {
	if testing.Short() {
		t.Skip("bench-style bound")
	}
	s, path := newStore(t)
	for i := 0; i < 200; i++ {
		mk(t, s, fmt.Sprintf("Place %03d", i))
	}
	start := time.Now()
	sn, err := ReadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range sn.Places {
		if p.Name != "" {
			n++
		}
	}
	kids := sn.Children(RootID, false)
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Fatalf("load and list took %s", elapsed)
	}
	if n != 200 || len(kids) != 200 {
		t.Fatalf("listed %d places, %d top-level", n, len(kids))
	}
}
