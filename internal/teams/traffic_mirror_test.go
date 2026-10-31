package teams

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A mirror must remain invisible to every old and current local delivery reader.
func TestTrafficMirrorNeverChangesLocalLogAndKeepsPortableThreads(t *testing.T) {
	dir := t.TempDir()
	if err := AppendTraffic(dir, "alpha", Entry{Kind: KindNote, From: FromYou, To: ToManager, Text: "local", Origin: "forged"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(TrafficPath(dir, "alpha"))
	if bytes.Contains(before, []byte("origin")) {
		t.Fatal("local append retained origin")
	}
	lines := []Entry{{ID: "a/1", At: time.Unix(10, 0), Kind: KindQuestion, Text: "remote"}, {ID: "a/2", At: time.Unix(11, 0), Kind: KindAnswer, Reply: "a/1", Answers: "a/1"}}
	for range 3 {
		if err := WriteTrafficMirror(dir, "alpha", lines); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(TrafficPath(dir, "alpha"))
	if !bytes.Equal(before, after) {
		t.Fatal("mirror changed local log")
	}
	local, _ := ReadTraffic(dir, "alpha", "", 0)
	if len(local) != 1 {
		t.Fatal(local)
	}
	got, err := ReadHistory(dir, "alpha", 0)
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Origin != "a/1" || got[1].Reply != "a/1" || got[1].Answers != "a/1" {
		t.Fatal(got)
	}
	info, _ := os.Stat(TrafficMirrorPath(dir, "alpha"))
	if err := WriteTrafficMirror(dir, "alpha", lines); err != nil {
		t.Fatal(err)
	}
	again, _ := os.Stat(TrafficMirrorPath(dir, "alpha"))
	if !info.ModTime().Equal(again.ModTime()) {
		t.Fatal("unchanged mirror rewritten")
	}
}

func TestTrafficExportBoundsTextAtRuneBoundaryAndReadsBoundedTail(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 100; i++ {
		if err := AppendTraffic(dir, "alpha", Entry{Kind: KindNote, From: FromYou, To: ToManager, Text: strings.Repeat("界", 2000), Handles: []string{strings.Repeat("x", 20000)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(trafficRotated(TrafficPath(dir, "alpha")), []byte("not json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := TrafficForSync(dir, "alpha", "a")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	if len(raw) > TrafficSyncBytes || len(got) > TrafficSyncLines || len(got) == 0 {
		t.Fatalf("%d lines %d bytes", len(got), len(raw))
	}
	for _, e := range got {
		if !utf8.ValidString(e.Text) || len(e.Text) > TrafficSyncTextBytes+len(" …") || !strings.HasSuffix(e.Text, " …") {
			t.Fatal("text dropped or cut badly")
		}
	}
	// Encoded bytes, rather than string lengths, decide whether optional
	// fields fit. Escaped metadata must not evict a shortened line's text.
	escaped := t.TempDir()
	if err := AppendTraffic(escaped, "alpha", Entry{Kind: KindNote, From: strings.Repeat("\x01", TrafficSyncTextBytes), To: strings.Repeat("\x02", TrafficSyncTextBytes), Text: strings.Repeat("\x03", TrafficSyncTextBytes+1)}); err != nil {
		t.Fatal(err)
	}
	got, err = TrafficForSync(escaped, "alpha", "a")
	if err != nil || len(got) != 1 || got[0].Text != strings.Repeat("\x03", TrafficSyncTextBytes)+" …" {
		t.Fatalf("encoded metadata excluded the text: %+v %v", got, err)
	}
	raw, _ = json.Marshal(got)
	if len(raw) > TrafficSyncBytes {
		t.Fatal("encoded window exceeded its bound")
	}
}

func TestDeletingATeamRemovesItsDisplayMirrorAlongWithItsFolder(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, func(f *File) error { f.Teams = []Team{{ID: "alpha", Name: "alpha", State: TeamClosed}}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := WriteTrafficMirror(dir, "alpha", []Entry{{ID: "a/1", At: time.Now(), Kind: KindNote, Text: "remote history"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Delete(dir, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(TrafficMirrorPath(dir, "alpha")); !os.IsNotExist(err) {
		t.Fatal("delete left mirror behind")
	}
}

func TestOverviewTailCannotHideAMirrorChangeFromTheTrafficPane(t *testing.T) {
	dir := t.TempDir()
	if err := AppendTraffic(dir, "alpha", Entry{Kind: KindNote, From: FromManager, To: ToRoom, Text: "local"}); err != nil {
		t.Fatal(err)
	}
	var w Watch
	_, cursor, version, _, err := w.History(dir, "alpha", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteTrafficMirror(dir, "alpha", []Entry{{ID: "a/1", At: time.Now(), Text: "new mirror"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := w.History(dir, "alpha", "", "", 100); err != nil {
		t.Fatal(err)
	}
	lines, next, _, reset, err := w.History(dir, "alpha", cursor, version, 100)
	if err != nil || !reset || next != cursor || len(lines) != 2 {
		t.Fatal("overview consumed the pane's refresh", lines, next, reset, err)
	}
}

func TestTrafficExportTakesOnlyTheNewestLineCount(t *testing.T) {
	dir := t.TempDir()
	for range 100 {
		if err := AppendTraffic(dir, "alpha", Entry{Kind: KindNote, From: FromYou, To: ToRoom, Text: "short"}); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := TrafficForSync(dir, "alpha", "a")
	if err != nil || len(lines) != TrafficSyncLines || lines[len(lines)-1].ID != "a/000000000100" {
		t.Fatal(len(lines), err)
	}
}

func TestMirrorSurvivesLocalRotationWithoutDuplicatingRemoteLines(t *testing.T) {
	dir := t.TempDir()
	old := trafficRotateBytes
	trafficRotateBytes = 500
	t.Cleanup(func() { trafficRotateBytes = old })
	remote := []Entry{{ID: "a/1", At: time.Now(), Kind: KindDirective, From: FromManager, To: "worker", Text: "history only"}}
	if err := WriteTrafficMirror(dir, "alpha", remote); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if err := AppendTraffic(dir, "alpha", Entry{Kind: KindNote, From: FromYou, To: ToRoom, Text: strings.Repeat("x", 800)}); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if err := WriteTrafficMirror(dir, "alpha", remote); err != nil {
			t.Fatal(err)
		}
	}
	local, _ := ReadTraffic(dir, "alpha", "", 0)
	for _, e := range local {
		if e.Origin != "" || e.Text == "history only" {
			t.Fatal("rotation admitted mirror into local log")
		}
	}
	all, _ := ReadHistory(dir, "alpha", 0)
	count := 0
	for _, e := range all {
		if e.Origin == "a/1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d remote copies after rotation", count)
	}
}

func TestHistoryThreadsFollowTimeAcrossWritersAndKeepReferences(t *testing.T) {
	entries := []Entry{
		{ID: "z/1", Origin: "z/1", At: time.Unix(1, 0), Text: "question"},
		{ID: "z/2", Origin: "z/2", At: time.Unix(2, 0), Answers: "z/1", Text: "first reply"},
		{ID: "a/1", Origin: "a/1", At: time.Unix(3, 0), Answers: "z/1", Text: "later reply"},
		{ID: "000000000001", At: time.Unix(4, 0), Text: "newest local"},
	}
	threads := Threads(entries)
	if len(threads) != 2 || threads[0].Root.Text != "newest local" || threads[1].Root.ID != "z/1" || len(threads[1].Replies) != 2 || threads[1].Latest != "a/1" {
		t.Fatalf("portable ids decided thread order or broke references: %+v", threads)
	}
}

func TestHistoryWithoutMirrorKeepsLocalSequenceWhenClockMovesBack(t *testing.T) {
	profile := t.TempDir()
	for _, at := range []time.Time{time.Unix(200, 0), time.Unix(100, 0)} {
		if err := AppendTraffic(profile, "alpha", Entry{Kind: KindNote, From: FromYou, To: ToRoom, At: at, Text: "local"}); err != nil {
			t.Fatal(err)
		}
	}
	local, err := ReadTraffic(profile, "alpha", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	history, err := ReadHistory(profile, "alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != len(local) {
		t.Fatal("history changed local length")
	}
	for i := range local {
		if history[i].ID != local[i].ID {
			t.Fatal("history changed local sequence")
		}
	}
}
