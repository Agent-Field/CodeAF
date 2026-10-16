package session

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoopbackOriginAcceptsOnlyRealLoopbackHTTP(t *testing.T) {
	good := map[string]string{
		"http://localhost:3000":       "http://localhost:3000",
		"localhost:5173":              "http://localhost:5173",
		"http://127.0.0.1:8080/a?b=c": "http://127.0.0.1:8080",
		"  127.0.0.1:65535 ":          "http://127.0.0.1:65535",
	}
	for in, want := range good {
		if got, ok := LoopbackOrigin(in); !ok || got != want {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
	for _, in := range []string{
		"", "http://localhost", "http://localhost:0", "http://localhost:65536", "http://localhost:99999",
		"http://localhost:03000", "https://localhost:3000", "http://evil.com:3000", "http://localhost.evil.com:3000",
		"http://user@localhost:3000", "http://0.0.0.0:3000", "http://[::1]:3000", "ftp://localhost:3000",
		"http://127.0.0.2:3000", "http://localhost:3000@evil.com",
	} {
		if got, ok := LoopbackOrigin(in); ok {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestProcessURLIsTheFirstGenuineLoopbackAddress(t *testing.T) {
	cases := []struct{ out, want string }{
		{"ready on http://localhost:3000/\n", "http://localhost:3000"},
		{"xlocalhost:3000 then see https://evil.com/localhost:3000 then user@localhost:9 and localhost:99999 then 127.0.0.1:4000\n", "http://127.0.0.1:4000"},
		{"listening localhost:4321\nother localhost:5000\n", "http://localhost:4321"},
		{"no address here\n", ""},
		{"localhost:3000x\n", ""},
		{"https://localhost:3000\n", ""},
	}
	for _, c := range cases {
		sink := newJobSink(nil, "")
		_, _ = sink.Write([]byte(c.out))
		if got := sink.processURL(); got != c.want {
			t.Errorf("%q -> %q want %q", c.out, got, c.want)
		}
	}
	// An address split across two writes is still found, once, and later ones are ignored.
	sink := newJobSink(nil, "")
	_, _ = sink.Write([]byte("up at http://local"))
	_, _ = sink.Write([]byte("host:3001/\nand localhost:4000\n"))
	if got := sink.processURL(); got != "http://localhost:3001" {
		t.Errorf("split: %q", got)
	}
}

func pngFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pngOfSize(t, 4, 4), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSnapshotRegistersARealSavedPNGAsSavedByYou(t *testing.T) {
	root := t.TempDir()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	path := pngFile(t, root, "snap.png")
	row, created, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", path, "http://localhost:3000/dash?x=1#frag", []string{root})
	if err != nil || !created {
		t.Fatalf("%v %v", err, created)
	}
	if row.Fact.Origin != DeliverOriginSavedByYou || len(row.Fact.From) != 1 || row.Fact.From[0] != "http://localhost:3000/dash?x=1" || row.Kind != "image" || row.Fact.Turn != "" {
		t.Fatalf("fact %+v", row.Fact)
	}
	got := listDeliverables(t, index, DeliverableQuery{}, 10)
	if len(got) != 1 || got[0].ID != row.ID {
		t.Fatalf("listing %+v", got)
	}
	if src, ok := mediaRowOf(got[0].Artifact); !ok || src.Fact.Origin != MediaGenerated {
		t.Fatal("the snapshot is not also a media source")
	}
	// refusals
	if _, _, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", path, "https://example.com", []string{root}); err == nil {
		t.Error("off-loopback accepted")
	}
	outside := pngFile(t, t.TempDir(), "x.png")
	if _, _, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", outside, "http://localhost:3000", []string{root}); err == nil {
		t.Error("outside file accepted")
	}
	notPNG := filepath.Join(root, "n.png")
	os.WriteFile(notPNG, []byte("not a png at all, long enough"), 0o600)
	if _, _, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", notPNG, "http://localhost:3000", []string{root}); err == nil {
		t.Error("non-PNG accepted")
	}
	link := filepath.Join(root, "link.png")
	if os.Symlink(outside, link) == nil {
		if _, _, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", link, "http://localhost:3000", []string{root}); err == nil {
			t.Error("link out of the folder accepted")
		}
	}
}

// A match at the edge of the overlap window has no left neighbour in it: the
// byte before must come from the ring, or `xlocalhost:3000` split across two
// writes becomes an address.
func TestProcessURLBoundaryHoldsAcrossTheOverlapWindow(t *testing.T) {
	sink := newJobSink(nil, "")
	first := "xlocalhost:3000" + string(make([]byte, 0)) + strings.Repeat(".", 114)
	_, _ = sink.Write([]byte(first))
	_, _ = sink.Write([]byte("\n"))
	if got := sink.processURL(); got != "" {
		t.Fatalf("captured %q from text that fails the boundary rule", got)
	}
	// and the genuine one just past the same window is still found
	_, _ = sink.Write([]byte(" now listening on localhost:3001\n"))
	if got := sink.processURL(); got != "http://localhost:3001" {
		t.Fatalf("lost the genuine address: %q", got)
	}
}

// One file registered by many callers at once is one deliverable, and an
// absurd header is refused before a pixel is read.
func TestSnapshotRegistrationIsSerializedAndBounded(t *testing.T) {
	root := t.TempDir()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	path := pngFile(t, root, "snap.png")
	var wg sync.WaitGroup
	created := make(chan bool, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, made, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", path, "http://localhost:3000/", []string{root})
			if err != nil {
				t.Error(err)
			}
			created <- made
		}()
	}
	wg.Wait()
	close(created)
	made := 0
	for c := range created {
		if c {
			made++
		}
	}
	if rows := listDeliverables(t, index, DeliverableQuery{}, 20); made != 1 || len(rows) != 1 {
		t.Fatalf("created %d, listed %d", made, len(rows))
	}
	// a header that claims a sky-sized picture
	huge := pngOfSize(t, 4, 4)
	binary.BigEndian.PutUint32(huge[16:], 20000)
	binary.BigEndian.PutUint32(huge[20:], 20000)
	bigPath := filepath.Join(root, "huge.png")
	os.WriteFile(bigPath, huge, 0o600)
	if _, _, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", bigPath, "http://localhost:3000/", []string{root}); err == nil {
		t.Fatal("an absurd header was registered")
	}
}

// The search for an earlier registration is one bounded question: a lane held
// past the caller's deadline, or a caller that has gone, answers busy and
// records nothing; the next registration records the picture once, and the
// one after it finds it.
func TestASnapshotSearchThatCannotFinishRecordsNothing(t *testing.T) {
	root := t.TempDir()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	path := pngFile(t, root, "snap.png")
	lane := snapshotLane(index, canonicalFile(path))
	lane <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, _, err := RegisterSnapshot(ctx, index, "0123456789abcdef", path, "http://localhost:3000/", []string{root})
	cancel()
	<-lane
	if !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("a held lane past the deadline: %v", err)
	}
	gone, stop := context.WithCancel(context.Background())
	stop()
	if _, _, err := RegisterSnapshot(gone, index, "0123456789abcdef", path, "http://localhost:3000/", []string{root}); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("a caller that has gone: %v", err)
	}
	if _, err := os.Stat(index); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an unknown answer recorded a row: %v", err)
	}
	first, made, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", path, "http://localhost:3000/", []string{root})
	if err != nil || !made {
		t.Fatalf("first registration: %v %v", err, made)
	}
	again, made, err := RegisterSnapshot(context.Background(), index, "0123456789abcdef", path, "http://localhost:3000/", []string{root})
	if err != nil || made || again.ID != first.ID {
		t.Fatalf("the same picture again: %v made=%v %s vs %s", err, made, again.ID, first.ID)
	}
}
