package viewcache

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// These tests are written from what a person is promised, not from the file
// layout: a chat they were watching can still be read when its computer is gone
// (V27), the copy lives on their own disk and stays small, and nothing here can
// leave it or grow without end.

type fakeClock struct{ at time.Time }

func (f *fakeClock) Now() time.Time          { return f.at }
func (f *fakeClock) advance(d time.Duration) { f.at = f.at.Add(d) }

func newCache(t *testing.T) (*Cache, string, *fakeClock) {
	t.Helper()
	home := t.TempDir()
	clk := &fakeClock{at: time.Date(2026, 11, 23, 14, 2, 0, 0, time.UTC)}
	return OpenWith(home, clk), home, clk
}

func entries(n int, text string) []session.DisplayEntry {
	out := make([]session.DisplayEntry, n)
	for i := range out {
		out[i] = session.DisplayEntry{Role: "assistant", Text: fmt.Sprintf("%s %d", text, i)}
	}
	return out
}

// V27: a chat that was being watched can be read after its computer is gone.
func TestAWatchedChatComesBackAsItWasLastSeen(t *testing.T) {
	c, _, clk := newCache(t)
	seen := clk.Now()
	in := Snapshot{
		HostName: "Desktop", Title: "port the parser", Model: "m", Workspace: "parser",
		SeenAt: seen, Base: 0, Total: 3, Entries: entries(3, "line"),
	}
	if err := c.SaveNow("dev_desk", "cell1", in); err != nil {
		t.Fatal(err)
	}
	got, ok := c.Load("dev_desk", "cell1")
	if !ok {
		t.Fatal("a chat that was kept cannot be read back")
	}
	if got.Title != "port the parser" || got.HostName != "Desktop" || got.Host != "dev_desk" || got.Chat != "cell1" {
		t.Fatalf("what the person saw changed: %+v", got)
	}
	if !got.SeenAt.Equal(seen) {
		t.Fatalf("last seen %v, want %v: the banner would say the wrong time", got.SeenAt, seen)
	}
	if len(got.Entries) != 3 || got.Entries[2].Text != "line 2" {
		t.Fatalf("entries came back as %+v", got.Entries)
	}
	if _, ok := c.Load("dev_desk", "another-chat"); ok {
		t.Fatal("a chat never watched has a copy")
	}
	if _, ok := c.Load("dev_other", "cell1"); ok {
		t.Fatal("another computer's chat answered for this one")
	}
}

// V27: bounded. The copy holds the newest stretch, and scrolling up still counts
// from the start of the transcript.
func TestASnapshotIsBoundedAndPlainTextOnlyOnTheViewersDisk(t *testing.T) {
	c, home, _ := newCache(t)
	if err := c.SaveNow("dev_desk", "long", Snapshot{Total: 1000, Base: 700, Entries: entries(300, "e")}); err != nil {
		t.Fatal(err)
	}
	got, _ := c.Load("dev_desk", "long")
	if len(got.Entries) != MaxEntries {
		t.Fatalf("kept %d entries, want the newest %d", len(got.Entries), MaxEntries)
	}
	if got.Entries[len(got.Entries)-1].Text != "e 299" {
		t.Fatalf("kept the wrong end: last is %q", got.Entries[len(got.Entries)-1].Text)
	}
	if got.Base != 800 || got.Total != 1000 {
		t.Fatalf("base %d total %d: the copy no longer says where in the chat it is", got.Base, got.Total)
	}

	// A chat of very large entries is held to its byte bound, oldest out.
	big := strings.Repeat("x", 100<<10)
	huge := make([]session.DisplayEntry, 40)
	for i := range huge {
		huge[i] = session.DisplayEntry{Role: "tool", Text: fmt.Sprintf("%02d%s", i, big)}
	}
	if err := c.SaveNow("dev_desk", "huge", Snapshot{Total: 40, Entries: huge}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, "v3", "viewed", "dev_desk", "huge.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > MaxBytes {
		t.Fatalf("one chat weighs %d bytes, over the %d bound", info.Size(), MaxBytes)
	}
	h, _ := c.Load("dev_desk", "huge")
	if len(h.Entries) == 0 || !strings.HasPrefix(h.Entries[len(h.Entries)-1].Text, "39") {
		t.Fatalf("the newest entry was not the one kept")
	}
	if h.Base+len(h.Entries) != 40 {
		t.Fatalf("base %d plus %d entries is not the transcript's end", h.Base, len(h.Entries))
	}

	// One entry larger than the whole bound is dropped, and the chat is still
	// remembered with its title and the time.
	one := []session.DisplayEntry{{Role: "tool", Text: strings.Repeat("y", 2<<20)}}
	if err := c.SaveNow("dev_desk", "one", Snapshot{Title: "kept", Total: 1, Entries: one}); err != nil {
		t.Fatal(err)
	}
	o, ok := c.Load("dev_desk", "one")
	if !ok || o.Title != "kept" || len(o.Entries) != 0 {
		t.Fatalf("an oversize entry broke the copy: %+v ok=%v", o, ok)
	}

	// Plain text, in the viewer's own folder, and nothing else written.
	raw, err := os.ReadFile(filepath.Join(home, "v3", "viewed", "dev_desk", "long.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), `{"V":1,`) || !strings.Contains(string(raw), "e 299") {
		t.Fatalf("the file is not plain versioned text: %.80s", raw)
	}
	var stray []string
	_ = filepath.Walk(home, func(p string, i os.FileInfo, err error) error {
		if err == nil && !i.IsDir() && !strings.Contains(p, filepath.Join("v3", "viewed")) {
			stray = append(stray, p)
		}
		return nil
	})
	if len(stray) > 0 {
		t.Fatalf("wrote outside the viewed folder: %v", stray)
	}
}

// V27: the copy is for this person alone.
func TestTheCopyIsReadableByThisPersonOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not a thing on this platform")
	}
	c, home, _ := newCache(t)
	if err := c.SaveNow("dev_desk", "cell1", Snapshot{Entries: entries(1, "a")}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		filepath.Join(home, "v3", "viewed"),
		filepath.Join(home, "v3", "viewed", "dev_desk"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s is %o, want 700", dir, info.Mode().Perm())
		}
	}
	info, err := os.Stat(filepath.Join(home, "v3", "viewed", "dev_desk", "cell1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the copy is %o, want 600", info.Mode().Perm())
	}
}

// The cache keeps the newest forty chats across every computer and lets the
// oldest go.
func TestOnlyTheNewestFortyChatsAreKept(t *testing.T) {
	c, _, clk := newCache(t)
	for i := 0; i < MaxChats+5; i++ {
		clk.advance(time.Minute)
		host := fmt.Sprintf("dev_%d", i%3)
		if err := c.SaveNow(host, fmt.Sprintf("chat%02d", i), Snapshot{Entries: entries(1, "a")}); err != nil {
			t.Fatal(err)
		}
		// Files are aged by their own time, which a fake clock does not move.
		old := time.Now().Add(time.Duration(i-MaxChats-10) * time.Hour)
		_ = os.Chtimes(c.path(host, fmt.Sprintf("chat%02d", i)), old, old)
	}
	c.Prune()
	kept := 0
	for i := 0; i < MaxChats+5; i++ {
		_, ok := c.Load(fmt.Sprintf("dev_%d", i%3), fmt.Sprintf("chat%02d", i))
		if ok {
			kept++
			if i < 5 {
				t.Errorf("chat%02d, one of the oldest five, is still kept", i)
			}
		} else if i >= 5 {
			t.Errorf("chat%02d is newer than the oldest five and was removed", i)
		}
	}
	if kept != MaxChats {
		t.Fatalf("kept %d chats, want %d", kept, MaxChats)
	}
}

// A copy is written on a quiet beat, never per frame.
func TestACopyIsNotRewrittenMoreThanOnceInTwoSeconds(t *testing.T) {
	c, _, clk := newCache(t)
	if err := c.Save("dev_desk", "c", Snapshot{Title: "first", Entries: entries(1, "a")}); err != nil {
		t.Fatal(err)
	}
	clk.advance(WriteEvery - time.Millisecond)
	if err := c.Save("dev_desk", "c", Snapshot{Title: "second", Entries: entries(1, "b")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Load("dev_desk", "c"); got.Title != "first" {
		t.Fatalf("wrote again inside the window: %q", got.Title)
	}
	// The beats that end a view are not held back.
	if err := c.SaveNow("dev_desk", "c", Snapshot{Title: "closing", Entries: entries(1, "c")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Load("dev_desk", "c"); got.Title != "closing" {
		t.Fatalf("the view that was ending was not kept: %q", got.Title)
	}
	clk.advance(WriteEvery)
	if err := c.Save("dev_desk", "c", Snapshot{Title: "later", Entries: entries(1, "d")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Load("dev_desk", "c"); got.Title != "later" {
		t.Fatalf("never wrote again after the window: %q", got.Title)
	}
}

// Removing a computer, or moving a chat here, takes its copy with it.
func TestForgettingAComputerOrAChatRemovesOnlyThat(t *testing.T) {
	c, home, _ := newCache(t)
	for _, k := range [][2]string{{"dev_a", "1"}, {"dev_a", "2"}, {"dev_b", "1"}} {
		if err := c.SaveNow(k[0], k[1], Snapshot{Entries: entries(1, "a")}); err != nil {
			t.Fatal(err)
		}
	}
	c.ForgetChat("dev_a", "1")
	if _, ok := c.Load("dev_a", "1"); ok {
		t.Fatal("the chat that was forgotten is still kept")
	}
	if _, ok := c.Load("dev_a", "2"); !ok {
		t.Fatal("forgetting one chat removed its neighbour")
	}
	c.Forget("dev_a")
	if _, ok := c.Load("dev_a", "2"); ok {
		t.Fatal("a removed computer's chat is still kept")
	}
	if _, ok := c.Load("dev_b", "1"); !ok {
		t.Fatal("removing one computer removed another's copy")
	}
	if _, err := os.Stat(filepath.Join(home, "v3", "viewed", "dev_a")); err == nil {
		t.Fatal("a removed computer's folder is still there")
	}
}

// Moving a chat here does not know which computer its copy was last seen on,
// so it takes that chat's copy from every computer and leaves the others alone.
func TestForgettingAChatEverywhereKeepsEveryOtherChat(t *testing.T) {
	c, _, _ := newCache(t)
	for _, k := range [][2]string{{"dev_a", "1"}, {"dev_b", "1"}, {"dev_b", "2"}} {
		if err := c.SaveNow(k[0], k[1], Snapshot{Entries: entries(1, "a")}); err != nil {
			t.Fatal(err)
		}
	}
	c.ForgetChatEverywhere("1")
	for _, host := range []string{"dev_a", "dev_b"} {
		if _, ok := c.Load(host, "1"); ok {
			t.Fatalf("the copy of chat 1 kept for %s survived", host)
		}
	}
	if _, ok := c.Load("dev_b", "2"); !ok {
		t.Fatal("forgetting chat 1 everywhere removed chat 2")
	}
	// A cache with nothing in it forgets nothing and says nothing.
	empty, _, _ := newCache(t)
	empty.ForgetChatEverywhere("1")
}

// A name a computer chose can never name a place outside the cache.
func TestNamesThatLookLikePathsStayInsideTheCache(t *testing.T) {
	c, home, _ := newCache(t)
	if err := c.SaveNow("../../escape", "../../../etc/passwd", Snapshot{Entries: entries(1, "a")}); err != nil {
		t.Fatal(err)
	}
	var outside []string
	_ = filepath.Walk(home, func(p string, i os.FileInfo, err error) error {
		if err == nil && !i.IsDir() && !strings.HasPrefix(p, filepath.Join(home, "v3", "viewed")+string(filepath.Separator)) {
			outside = append(outside, p)
		}
		return nil
	})
	if len(outside) > 0 {
		t.Fatalf("left the cache: %v", outside)
	}
	if _, ok := c.Load("../../escape", "../../../etc/passwd"); !ok {
		t.Fatal("a chat saved under an odd name cannot be read back under it")
	}
}

// A copy this build cannot vouch for is not shown.
func TestACopyThatIsDamagedOrAboutAnotherChatIsNotShown(t *testing.T) {
	c, _, _ := newCache(t)
	if err := c.SaveNow("dev_a", "one", Snapshot{Entries: entries(1, "a")}); err != nil {
		t.Fatal(err)
	}
	// Somebody copied one chat's file over another's name.
	raw, _ := os.ReadFile(c.path("dev_a", "one"))
	if err := os.WriteFile(c.path("dev_a", "two"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("dev_a", "two"); ok {
		t.Fatal("a file that says it is another chat answered for this one")
	}
	if err := os.WriteFile(c.path("dev_a", "one"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("dev_a", "one"); ok {
		t.Fatal("a damaged file was shown")
	}
	if err := os.WriteFile(c.path("dev_a", "one"), []byte(`{"V":99,"Host":"dev_a","Chat":"one"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("dev_a", "one"); ok {
		t.Fatal("a format from the future was shown")
	}
}
