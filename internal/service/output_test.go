package service

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestOutputIsReadInBoundedPages: pages, a tail, a line cut where it is too
// long, the older chunk before the live one, and a log that outgrew the
// window said to be truncated rather than read whole.
func TestOutputIsReadInBoundedPages(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString("old " + strconv.Itoa(i) + "\n")
	}
	if err := os.WriteFile(row.LogPath+".1", []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Reset()
	for i := 0; i < 10; i++ {
		b.WriteString("new " + strconv.Itoa(i) + "\n")
	}
	b.WriteString(strings.Repeat("x", 3*maxOutputLine) + "\n")
	b.WriteString("last")
	if err := os.WriteFile(row.LogPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := h.s.Output("agent-a", row.ID, 3, 4)
	if err != nil || page.TotalLines != 17 || page.FirstLine != 3 || page.NextLine != 7 ||
		strings.Join(page.Lines, "|") != "old 3|old 4|new 0|new 1" {
		t.Fatalf("a page: %+v %v", page, err)
	}
	tail, err := h.s.Tail("agent-a", row.ID, 2)
	if err != nil || len(tail.Lines) != 2 || tail.Lines[1] != "last" || tail.FirstLine != 15 ||
		!strings.HasSuffix(tail.Lines[0], cutLineMark) || len(tail.Lines[0]) > maxOutputLine+len(cutLineMark) {
		t.Fatalf("the tail: %+v %v", tail, err)
	}
	if huge, _ := h.s.Output("agent-a", row.ID, 0, 1_000_000); len(huge.Lines) > maxOutputLines {
		t.Fatal("a page was not bounded")
	}
	if _, err := h.s.Output("agent-a", row.ID, -1, 1); err == nil {
		t.Fatal("a negative line was taken")
	}
	if err := os.WriteFile(row.LogPath, []byte(strings.Repeat("y\n", outputWindow)), 0o600); err != nil {
		t.Fatal(err)
	}
	big, err := h.s.Tail("agent-a", row.ID, 1)
	if err != nil || !big.Truncated || big.TotalLines > outputWindow/2 {
		t.Fatalf("a log past the window: %+v %v", big, err)
	}
}

// TestARemovalTheArchiveCannotKeepRemovesNothing.
func TestARemovalTheArchiveCannotKeepRemovesNothing(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if err := os.WriteFile(h.s.archivePath("x")[:len(h.s.archivePath("x"))-len("x.json")-1], []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Remove("agent-a", row.ID); !errors.Is(err, ErrNotSaved) {
		t.Fatalf("remove reported %v", err)
	}
	if _, err := h.s.Get("agent-a", row.ID); err != nil {
		t.Fatal("a removal whose archive could not be written removed the record")
	}
}

// TestAnEpisodesResolutionIsWrittenOnce: later healthy checks never move it,
// and a new episode starts with none.
func TestAnEpisodesResolutionIsWrittenOnce(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	h.rt.exit(t, h.s, row.PID, 1)
	if got := h.get(row.ID); got.EpisodeResolvedAt != nil || !got.EpisodeOpen {
		t.Fatalf("an open episode has no resolution: %+v", got)
	}
	h.advance(FirstBackoff)
	h.advance(CheckEvery)
	resolved := h.get(row.ID).EpisodeResolvedAt
	if resolved == nil {
		t.Fatal("recovery did not resolve the episode")
	}
	h.advance(5 * CheckEvery)
	if again := h.get(row.ID).EpisodeResolvedAt; again == nil || !again.Equal(*resolved) {
		t.Fatalf("healthy checks moved the resolution: %v then %v", resolved, again)
	}
	h.rt.exit(t, h.s, h.get(row.ID).PID, 1)
	if got := h.get(row.ID); got.Episode == row.ID+".1" || got.EpisodeResolvedAt != nil {
		t.Fatalf("a new episode: %+v", got)
	}
}

// A large short-line log exercises the full-size tail, including wraparound.
func TestLargeShortLineTailKeepsChronologicalSuffix(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyNever)
	var b strings.Builder
	const count = 100_000
	for i := 0; i < count; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(row.LogPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := h.s.Tail("agent-a", row.ID, maxOutputLines)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalLines != count || page.FirstLine != count-maxOutputLines || len(page.Lines) != maxOutputLines {
		t.Fatalf("incorrect range: %+v", page)
	}
	for i, line := range page.Lines {
		if line != strconv.Itoa(count-maxOutputLines+i) {
			t.Fatalf("line %d = %q", i, line)
		}
	}
}

func TestRemovedOutputRetainsRotationDisclosureAfterReopen(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyNever)
	if err := os.WriteFile(row.LogPath, []byte(strings.Repeat("x\n", logChunk)), 0o600); err != nil {
		t.Fatal(err)
	}
	h.advance(CheckEvery)
	if !h.get(row.ID).OutputTruncated {
		t.Fatal("fixture did not rotate output")
	}
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if err := h.s.Remove("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	h.reopen()
	page, err := h.s.Tail("agent-a", row.ID, 1)
	if err != nil || !page.Truncated {
		t.Fatalf("removed output lost rotation disclosure: %+v %v", page, err)
	}
}

func BenchmarkServiceTail100kLines(b *testing.B) {
	s := &Supervisor{dir: b.TempDir(), entries: map[string]*entry{}}
	if err := os.MkdirAll(s.dir+"/logs", 0700); err != nil {
		b.Fatal(err)
	}
	if err := s.writeArchive(Archived{ID: "s1", Owner: "owner"}); err != nil {
		b.Fatal(err)
	}
	var text strings.Builder
	for i := 0; i < 100_000; i++ {
		text.WriteString(strconv.Itoa(i))
		text.WriteByte('\n')
	}
	if err := os.WriteFile(s.logPath("s1"), []byte(text.String()), 0600); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := s.Tail("owner", "s1", 1000)
		if err != nil || len(p.Lines) != 1000 {
			b.Fatalf("tail: %v", err)
		}
	}
}
