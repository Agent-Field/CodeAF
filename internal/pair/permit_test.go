package pair

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
)

func boolPtr(v bool) *bool { return &v }

// V11: a watch-only device has drive turned off, and partial updates change
// only the non-nil field while keeping the other field from the effective permit.
func TestAWatchOnlyDeviceHasDriveOff(t *testing.T) {
	book := PermitsAt(filepath.Join(t.TempDir(), "permits.json"))
	const id = "id_alpha"
	const dev = "dev_laptop"

	if err := book.Set(id, dev, boolPtr(true), boolPtr(false), "person"); err != nil {
		t.Fatal(err)
	}
	got, class := book.For(id, dev)
	if !got.Watch || got.Drive || class != ClassExplicit || got.By != "person" || got.At.IsZero() {
		t.Fatalf("after watch=true drive=false: got (%+v, %v), want Watch=true Drive=false ClassExplicit", got, class)
	}

	if err := book.Set(id, dev, nil, boolPtr(true), "person"); err != nil {
		t.Fatal(err)
	}
	got, class = book.For(id, dev)
	if !got.Watch || !got.Drive || class != ClassExplicit {
		t.Fatalf("after drive=true: got (%+v, %v), want Watch=true Drive=true ClassExplicit", got, class)
	}

	if err := book.Set(id, dev, boolPtr(false), nil, "person"); err != nil {
		t.Fatal(err)
	}
	got, class = book.For(id, dev)
	if got.Watch || !got.Drive || class != ClassExplicit {
		t.Fatalf("after watch=false: got (%+v, %v), want Watch=false Drive=true ClassExplicit", got, class)
	}
}

// V25: a pairing recorded in the one-time legacy snapshot stays drive-off while
// a pairing made afterwards defaults to drive-on, and the snapshot never grows.
func TestAPairingMadeAfterThisShipsHasDriveOnAndOneBeforeStaysOff(t *testing.T) {
	book := PermitsAt(filepath.Join(t.TempDir(), "permits.json"))
	const id = "id_alpha"

	if err := book.SnapshotLegacy(id, []string{"dev_a", "dev_b"}); err != nil {
		t.Fatal(err)
	}
	for _, legacyDev := range []string{"dev_a", "dev_b"} {
		got, class := book.For(id, legacyDev)
		if !got.Watch || got.Drive || class != ClassLegacy {
			t.Fatalf("For(%q) = (%+v, %v), want Watch=true Drive=false ClassLegacy", legacyDev, got, class)
		}
	}
	if got, class := book.For(id, "dev_c"); !got.Watch || !got.Drive || class != ClassNew {
		t.Fatalf("For(\"dev_c\") = (%+v, %v), want Watch=true Drive=true ClassNew", got, class)
	}

	if err := book.SnapshotLegacy(id, []string{"dev_a", "dev_b", "dev_c"}); err != nil {
		t.Fatal(err)
	}
	if got, class := book.For(id, "dev_c"); !got.Watch || !got.Drive || class != ClassNew {
		t.Fatalf("after second SnapshotLegacy, For(\"dev_c\") = (%+v, %v), want Watch=true Drive=true ClassNew", got, class)
	}

	fresh := PermitsAt(filepath.Join(t.TempDir(), "fresh-permits.json"))
	if err := fresh.SnapshotLegacy("id_beta", nil); err != nil {
		t.Fatal(err)
	}
	for _, dev := range []string{"dev_a", "dev_b", "dev_c"} {
		if got, class := fresh.For("id_beta", dev); !got.Watch || !got.Drive || class != ClassNew {
			t.Fatalf("fresh For(%q) = (%+v, %v), want Watch=true Drive=true ClassNew", dev, got, class)
		}
	}
}

// V25, V14: missing, unreadable, corrupt, foreign-identity, or unsnapshotted
// permit files all fail safe to watch-only legacy behavior without panicking.
func TestPermitBookFailsSafeToLegacyOnMissingCorruptForeignOrUnsnapshottedFiles(t *testing.T) {
	dir := t.TempDir()

	// No file on disk.
	absent := PermitsAt(filepath.Join(dir, "absent.json"))
	if got, class := absent.For("id_alpha", "dev_a"); !got.Watch || got.Drive || class != ClassLegacy {
		t.Fatalf("absent file: got (%+v, %v), want Watch=true Drive=false ClassLegacy", got, class)
	}

	// Garbage JSON on disk.
	garbagePath := filepath.Join(dir, "garbage.json")
	if err := os.WriteFile(garbagePath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	garbage := PermitsAt(garbagePath)
	if got, class := garbage.For("id_alpha", "dev_a"); !got.Watch || got.Drive || class != ClassLegacy {
		t.Fatalf("garbage file: got (%+v, %v), want Watch=true Drive=false ClassLegacy", got, class)
	}

	// Valid file bound to another identity.
	foreign := PermitsAt(filepath.Join(dir, "foreign.json"))
	if err := foreign.SnapshotLegacy("id_other", nil); err != nil {
		t.Fatal(err)
	}
	if err := foreign.Set("id_other", "dev_a", boolPtr(true), boolPtr(true), "person"); err != nil {
		t.Fatal(err)
	}
	if got, class := foreign.For("id_alpha", "dev_a"); !got.Watch || got.Drive || class != ClassLegacy {
		t.Fatalf("foreign identity file: got (%+v, %v), want Watch=true Drive=false ClassLegacy", got, class)
	}

	// Valid file for the same identity with no snapshot yet.
	noSnapPath := filepath.Join(dir, "nosnap.json")
	if err := os.WriteFile(noSnapPath, []byte(`{"v":1,"identity":"id_alpha","legacy":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	noSnap := PermitsAt(noSnapPath)
	if got, class := noSnap.For("id_alpha", "dev_a"); !got.Watch || got.Drive || class != ClassLegacy {
		t.Fatalf("unsnapshotted file: got (%+v, %v), want Watch=true Drive=false ClassLegacy", got, class)
	}
}

// V11, V25: an explicit row overrides a device's legacy or new class, and
// Forget removes the explicit row so the device returns to its class.
func TestExplicitPermitBeatsClassAndForgetRestoresClass(t *testing.T) {
	book := PermitsAt(filepath.Join(t.TempDir(), "permits.json"))
	const id = "id_alpha"

	if err := book.SnapshotLegacy(id, []string{"dev_legacy"}); err != nil {
		t.Fatal(err)
	}
	if err := book.Set(id, "dev_legacy", nil, boolPtr(true), "person"); err != nil {
		t.Fatal(err)
	}
	if got, class := book.For(id, "dev_legacy"); !got.Watch || !got.Drive || class != ClassExplicit {
		t.Fatalf("explicit override on legacy device: got (%+v, %v), want Watch=true Drive=true ClassExplicit", got, class)
	}

	if err := book.Forget("dev_legacy"); err != nil {
		t.Fatal(err)
	}
	if got, class := book.For(id, "dev_legacy"); !got.Watch || got.Drive || class != ClassLegacy {
		t.Fatalf("after Forget on legacy device: got (%+v, %v), want Watch=true Drive=false ClassLegacy", got, class)
	}

	if err := book.Set(id, "dev_new", nil, boolPtr(false), "person"); err != nil {
		t.Fatal(err)
	}
	if got, class := book.For(id, "dev_new"); !got.Watch || got.Drive || class != ClassExplicit {
		t.Fatalf("explicit override on new device: got (%+v, %v), want Watch=true Drive=false ClassExplicit", got, class)
	}
	if err := book.Forget("dev_new"); err != nil {
		t.Fatal(err)
	}
	if got, class := book.For(id, "dev_new"); !got.Watch || !got.Drive || class != ClassNew {
		t.Fatalf("after Forget on new device: got (%+v, %v), want Watch=true Drive=true ClassNew", got, class)
	}
}

// V25: NeedsAsk is true only when legacy devices exist and asked is empty;
// Asked stamps asked forever and grants drive only to listed legacy devices when allow is true.
func TestHomeAsksOnceForLegacyDevicesAndNeverAgain(t *testing.T) {
	const id = "id_alpha"

	emptyBook := PermitsAt(filepath.Join(t.TempDir(), "empty.json"))
	if emptyBook.NeedsAsk(id) {
		t.Fatal("NeedsAsk was true before any snapshot")
	}
	if err := emptyBook.SnapshotLegacy(id, nil); err != nil {
		t.Fatal(err)
	}
	if emptyBook.NeedsAsk(id) {
		t.Fatal("NeedsAsk was true with no legacy devices")
	}

	allowBook := PermitsAt(filepath.Join(t.TempDir(), "allow.json"))
	if err := allowBook.SnapshotLegacy(id, []string{"dev_a", "dev_b"}); err != nil {
		t.Fatal(err)
	}
	if !allowBook.NeedsAsk(id) {
		t.Fatal("NeedsAsk was false with legacy devices and asked empty")
	}
	if err := allowBook.Asked(id, true, []string{"dev_a", "dev_not_legacy"}); err != nil {
		t.Fatal(err)
	}
	if allowBook.NeedsAsk(id) {
		t.Fatal("NeedsAsk remained true after Asked(allow=true)")
	}
	if got, class := allowBook.For(id, "dev_a"); !got.Watch || !got.Drive || class != ClassExplicit || got.By != "asked" {
		t.Fatalf("dev_a after Asked(allow=true): got (%+v, %v), want explicit drive-on by \"asked\"", got, class)
	}
	if got, class := allowBook.For(id, "dev_b"); !got.Watch || got.Drive || class != ClassLegacy {
		t.Fatalf("unlisted legacy dev_b after Asked(allow=true): got (%+v, %v), want ClassLegacy drive-off", got, class)
	}
	if got, class := allowBook.For(id, "dev_not_legacy"); !got.Watch || !got.Drive || class != ClassNew {
		t.Fatalf("non-legacy device listed in Asked: got (%+v, %v), want ClassNew (no explicit row)", got, class)
	}

	denyBook := PermitsAt(filepath.Join(t.TempDir(), "deny.json"))
	if err := denyBook.SnapshotLegacy(id, []string{"dev_a"}); err != nil {
		t.Fatal(err)
	}
	if !denyBook.NeedsAsk(id) {
		t.Fatal("NeedsAsk was false before Asked(allow=false)")
	}
	if err := denyBook.Asked(id, false, nil); err != nil {
		t.Fatal(err)
	}
	if denyBook.NeedsAsk(id) {
		t.Fatal("NeedsAsk remained true after Asked(allow=false)")
	}
	if got, class := denyBook.For(id, "dev_a"); !got.Watch || got.Drive || class != ClassLegacy {
		t.Fatalf("dev_a after Asked(allow=false): got (%+v, %v), want unchanged ClassLegacy", got, class)
	}
}

// V25: writing the permit book enforces 0600 file and 0700 directory modes,
// persists no path separators in any JSON string or key, and leaves no temp file behind.
func TestPermitBookDiskModesNoPathsAndNoTempFileLeftBehind(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "v3", "remote")
	path := filepath.Join(dir, "permits.json")
	book := PermitsAt(path)
	const id = "id_alpha"

	if err := book.SnapshotLegacy(id, []string{"dev_a"}); err != nil {
		t.Fatal(err)
	}
	if err := book.Set(id, "dev_b", boolPtr(true), boolPtr(false), "person"); err != nil {
		t.Fatal(err)
	}
	if err := book.Asked(id, true, []string{"dev_a"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".new"); !os.IsNotExist(err) {
		t.Fatalf("temporary file %s.new still exists (err=%v)", path, err)
	}
	if runtime.GOOS != "windows" {
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fileInfo.Mode().Perm(); got != 0o600 {
			t.Fatalf("file mode = %04o, want 0600", got)
		}
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := dirInfo.Mode().Perm(); got != 0o700 {
			t.Fatalf("dir mode = %04o, want 0700", got)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var checkNoSlash func(v any)
	checkNoSlash = func(v any) {
		switch val := v.(type) {
		case string:
			if strings.Contains(val, "/") || strings.Contains(val, `\`) {
				t.Fatalf("persisted JSON string %q contains a path separator", val)
			}
		case []any:
			for _, item := range val {
				checkNoSlash(item)
			}
		case map[string]any:
			for k, item := range val {
				if strings.Contains(k, "/") || strings.Contains(k, `\`) {
					t.Fatalf("persisted JSON key %q contains a path separator", k)
				}
				checkNoSlash(item)
			}
		}
	}
	checkNoSlash(doc)
}

// V11: concurrent Set calls across eight goroutines are serialized so every device row lands.
func TestPermitBookConcurrentSetsAcrossEightGoroutinesAllLand(t *testing.T) {
	book := PermitsAt(filepath.Join(t.TempDir(), "permits.json"))
	const id = "id_alpha"
	const workers = 8

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		dev := fmt.Sprintf("dev_%d", i)
		drive := i%2 == 0
		wg.Add(1)
		go func(dev string, drive bool) {
			defer wg.Done()
			if err := book.Set(id, dev, boolPtr(true), boolPtr(drive), "person"); err != nil {
				errs <- err
			}
		}(dev, drive)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	for i := range workers {
		dev := fmt.Sprintf("dev_%d", i)
		wantDrive := i%2 == 0
		got, class := book.For(id, dev)
		if class != ClassExplicit || !got.Watch || got.Drive != wantDrive {
			t.Fatalf("For(%q) = (%+v, %v), want Watch=true Drive=%v ClassExplicit", dev, got, class, wantDrive)
		}
	}
}

// V11: Watch fires when another PermitBook instance writes the file, stays
// quiet when nothing changes, and returns when its context is canceled.
func TestPermitBookWatchFiresOnExternalWriteAndStopsOnCancel(t *testing.T) {
	prev := permitPollInterval
	permitPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { permitPollInterval = prev })

	path := filepath.Join(t.TempDir(), "permits.json")
	writer := PermitsAt(path)
	if err := writer.SnapshotLegacy("id_alpha", []string{"dev_a"}); err != nil {
		t.Fatal(err)
	}

	watcher := PermitsAt(path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count atomic.Int32
	fired := make(chan struct{}, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		watcher.Watch(ctx, 0, func() {
			count.Add(1)
			select {
			case fired <- struct{}{}:
			default:
			}
		})
	}()

	// Wait across several poll intervals with no write: changed must not fire.
	select {
	case <-fired:
		t.Fatal("Watch fired when nothing changed")
	case <-time.After(25 * time.Millisecond):
	}

	// Write through another PermitBook instance: changed must fire once.
	if err := writer.Set("id_alpha", "dev_a", boolPtr(true), boolPtr(true), "person"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("Watch did not fire after another PermitBook wrote")
	}

	// Wait across several more poll intervals without writing: no second fire.
	select {
	case <-fired:
		t.Fatalf("Watch fired again without another write (count=%d)", count.Load())
	case <-time.After(25 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Watch did not return after context cancellation")
	}
}

// V25: a computer that finishes Join or JoinByLink stamps permits.json with an
// empty legacy list so every device it later meets defaults to ClassNew (drive on).
func TestJoiningStampsAnEmptyLegacySnapshotSoLaterDevicesAreNew(t *testing.T) {
	t.Run("code join", func(t *testing.T) {
		r := newChatRig(t)
		t.Setenv("CODEAF_HOME", r.homeB)
		ctx := context.Background()
		a, b := newScreen(), newScreen()
		done := r.offer(ctx, a)
		code := a.nextCode(t)

		if _, err := r.join(ctx, r.homeB, code.Shown(), b); err != nil {
			t.Fatal(err)
		}
		if end := within(t, done); end.err != nil {
			t.Fatal(end.err)
		}

		permitPath := filepath.Join(r.homeB, "v3", "remote", "permits.json")
		raw, err := os.ReadFile(permitPath)
		if err != nil {
			t.Fatalf("permits.json not written on code join: %v", err)
		}
		var f permitFile
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		if f.Identity != r.a.ID() || f.Since == "" || len(f.Legacy) != 0 {
			t.Fatalf("code join permit file = %+v, want identity=%s non-empty since and empty legacy", f, r.a.ID())
		}
		if Permits().NeedsAsk(r.a.ID()) {
			t.Fatal("NeedsAsk was true on a freshly joined computer")
		}
		if got, class := Permits().For(r.a.ID(), "dev_later"); !got.Watch || !got.Drive || class != ClassNew {
			t.Fatalf("For(\"dev_later\") after code join = (%+v, %v), want Watch=true Drive=true ClassNew", got, class)
		}
	})

	t.Run("link join", func(t *testing.T) {
		r := newLinkRig(t)
		t.Setenv("CODEAF_HOME", r.new)
		ui, done := r.start(t, context.Background())
		in := waitInvite(t, ui)
		ref, err := ReadLink(in.Ref.Token())
		if err != nil {
			t.Fatal(err)
		}
		asking, err := r.approver.Look(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.approver.Approve(context.Background(), asking); err != nil {
			t.Fatal(err)
		}
		if res := waitResult(t, done); res.err != nil {
			t.Fatal(res.err)
		}

		idID := r.approver.Identity.ID()
		permitPath := filepath.Join(r.new, "v3", "remote", "permits.json")
		raw, err := os.ReadFile(permitPath)
		if err != nil {
			t.Fatalf("permits.json not written on link join: %v", err)
		}
		var f permitFile
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		if f.Identity != idID || f.Since == "" || len(f.Legacy) != 0 {
			t.Fatalf("link join permit file = %+v, want identity=%s non-empty since and empty legacy", f, idID)
		}
		if Permits().NeedsAsk(idID) {
			t.Fatal("NeedsAsk was true on a freshly link-joined computer")
		}
		if got, class := Permits().For(idID, "dev_later"); !got.Watch || !got.Drive || class != ClassNew {
			t.Fatalf("For(\"dev_later\") after link join = (%+v, %v), want Watch=true Drive=true ClassNew", got, class)
		}
	})
}

// V25, the offering side: the computer that was alone and now shares its chats
// records that everyone it meets next is a new pairing, so the first computer a
// person ever pairs types out of the box. A computer that already had a fleet
// keeps its older pairings older.
func TestTheComputerThatOffersAPairingRecordsThatTheNextOnesAreNew(t *testing.T) {
	t.Run("a computer that was alone", func(t *testing.T) {
		dir := t.TempDir()
		id, err := identity.EnsureSolo(dir)
		if err != nil {
			t.Fatal(err)
		}
		book := PermitsAt(filepath.Join(dir, "permits.json"))
		if err := EndSoloAt(dir, book); err != nil {
			t.Fatal(err)
		}
		if identity.Solo(dir) {
			t.Fatal("the computer still says it is alone after it shared its chats")
		}
		// The first full read of a roster that now holds the other computer must
		// not take it for an older pairing.
		if err := book.SnapshotLegacy(id.ID(), []string{"dev_host", "dev_laptop"}); err != nil {
			t.Fatal(err)
		}
		if got, class := book.For(id.ID(), "dev_laptop"); !got.Watch || !got.Drive || class != ClassNew {
			t.Fatalf("the first computer paired is (%+v, %v), want Watch=true Drive=true ClassNew", got, class)
		}
		if book.NeedsAsk(id.ID()) {
			t.Fatal("the computer asks whether its first pairing may type, which it never was told to refuse")
		}
	})

	t.Run("a computer that already had other computers", func(t *testing.T) {
		dir := t.TempDir()
		id, err := identity.Ensure(dir)
		if err != nil {
			t.Fatal(err)
		}
		if identity.Solo(dir) {
			t.Fatal("the fixture's identity is solo, so it does not stand for a fleet")
		}
		book := PermitsAt(filepath.Join(dir, "permits.json"))
		if err := EndSoloAt(dir, book); err != nil {
			t.Fatal(err)
		}
		if err := book.SnapshotLegacy(id.ID(), []string{"dev_host", "dev_old"}); err != nil {
			t.Fatal(err)
		}
		if got, class := book.For(id.ID(), "dev_old"); got.Drive || class != ClassLegacy {
			t.Fatalf("an older pairing is (%+v, %v) after another computer was offered a pairing, want watch only and legacy", got, class)
		}
	})
}
