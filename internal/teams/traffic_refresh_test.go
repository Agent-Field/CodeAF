package teams

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// Rotation must not withdraw recent local context while it still fits the window.
func TestTrafficExportKeepsNewestWindowAcrossRotation(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 31; i++ {
		if err := AppendTraffic(dir, "alpha", Entry{From: FromManager, To: ToRoom, Kind: KindNote, Text: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(TrafficPath(dir, "alpha"), trafficRotated(TrafficPath(dir, "alpha"))); err != nil {
		t.Fatal(err)
	}
	if err := AppendTraffic(dir, "alpha", Entry{From: FromManager, To: ToRoom, Kind: KindNote, Text: "after rotation"}); err != nil {
		t.Fatal(err)
	}
	lines, err := TrafficForSync(dir, "alpha", "here")
	if err != nil || len(lines) != TrafficSyncLines || lines[0].Text != "0" || lines[len(lines)-1].Text != "after rotation" {
		t.Fatalf("window collapsed: %d %+v %v", len(lines), lines, err)
	}
}

// F1 preserves the existing sharing fixture and history contract.
func TestMirrorRefreshCannotAdvancePastUnprocessedLocalNews(t *testing.T) {
	dir := t.TempDir()
	if err := AppendTraffic(dir, "alpha", Entry{From: FromManager, To: ToRoom, Kind: KindNote}); err != nil {
		t.Fatal(err)
	}
	var w Watch
	_, cursor, version, _, err := w.History(dir, "alpha", "", "", 200, true)
	if err != nil {
		t.Fatal(err)
	}
	for range 201 {
		if err := AppendTraffic(dir, "alpha", Entry{From: FromManager, To: ToRoom, Kind: KindStart}); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteTrafficMirror(dir, "alpha", []Entry{{ID: "elsewhere/1", At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	_, next, _, _, err := w.History(dir, "alpha", cursor, version, 200, true)
	if err != nil || next != cursor {
		t.Fatalf("mirror acknowledged unread local lines: %q -> %q: %v", cursor, next, err)
	}
}

// F1 preserves the existing sharing fixture and history contract.
func TestEachHistoryConsumerSeesMirrorVersionIndependently(t *testing.T) {
	dir := t.TempDir()
	if err := AppendTraffic(dir, "alpha", Entry{From: FromManager, To: ToRoom, Kind: KindNote}); err != nil {
		t.Fatal(err)
	}
	var w Watch
	_, cursor, v1, _, err := w.History(dir, "alpha", "", "", 200, true)
	if err != nil {
		t.Fatal(err)
	}
	_, _, v2, _, err := w.History(dir, "alpha", "", "", 200, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteTrafficMirror(dir, "alpha", []Entry{{ID: "elsewhere/1", At: time.Now(), Text: "new"}}); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{v1, v2} {
		lines, next, current, reset, err := w.History(dir, "alpha", cursor, version, 200, true)
		if err != nil || !reset || next != cursor || current == version || len(lines) != 2 {
			t.Fatal("consumer missed mirror", lines, next, current, reset, err)
		}
	}
}
