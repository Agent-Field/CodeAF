package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

func ambientBandContext(a *app, row session.SessionRow, now time.Time, width int) bandContext {
	return bandContext{subject: bandSubject{kind: bandKindSession, row: row}, width: width, now: now, pal: a.pal}
}

func TestNewsBandReadsWithoutDrainingFoldsAndFits(t *testing.T) {
	dir := t.TempDir()
	row := session.SessionRow{ID: "0123456789abcdef", Dir: dir, Transcript: filepath.Join(dir, session.TranscriptName)}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	for i, text := range []string{"report landed", "tests passed", "deploy needs a look", "invoice filed"} {
		if err := standing.Deliver(dir, standing.Note{At: now.Add(-time.Duration(i+1) * time.Minute), Words: "keep main green", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	a := newTestApp(nil)
	rows := drawNewsBand(a, ambientBandContext(a, row, now, 34))
	got := plain(strings.Join(rows, "\n"))
	if !strings.Contains(got, "◆ 4 things since you left") || !strings.Contains(got, "1m · keep main green") || !strings.Contains(got, "report landed") || !strings.Contains(got, "▸ …1 more things") {
		t.Fatalf("news band:\n%s", got)
	}
	if _, err := os.Stat(standing.InboxPath(dir)); err != nil {
		t.Fatalf("drawing drained the inbox: %v", err)
	}
	for _, line := range rows {
		if ansi.StringWidth(line) > 34 {
			t.Fatalf("news row is %d cells: %q", ansi.StringWidth(line), plain(line))
		}
	}
	assertNarrowRows(t, "news", drawNewsBand(a, ambientBandContext(a, row, now, 30)), 30, "report landed")
	empty := session.SessionRow{Transcript: filepath.Join(t.TempDir(), session.TranscriptName)}
	if got := drawNewsBand(newTestApp(nil), ambientBandContext(newTestApp(nil), empty, now, 34)); len(got) != 0 {
		t.Fatalf("empty news drew %q", got)
	}
}

func TestDeliverablesBandFiltersSessionFoldsAndFits(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, session.ArtifactsIndexName)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"one-long-report.md", "two.png", "three.csv", "four.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		session.RecordArtifact(index, session.Artifact{Path: path, Session: "mine", Title: name, Created: now.Add(-time.Duration(i+1) * time.Hour)})
	}
	session.RecordArtifact(index, session.Artifact{Path: filepath.Join(dir, "other.txt"), Session: "other", Title: "other", Created: now})
	a := newTestApp(nil)
	a.artifacts = index
	row := session.SessionRow{ID: "mine", Transcript: filepath.Join(dir, "mine", session.TranscriptName)}
	rows := drawDeliverablesBand(a, ambientBandContext(a, row, now, 24))
	got := plain(strings.Join(rows, "\n"))
	if !strings.Contains(got, "four.txt") || !strings.Contains(got, "· 4h") || !strings.Contains(got, "▸ …1 more files") || strings.Contains(got, "other.txt") {
		t.Fatalf("deliverables band:\n%s", got)
	}
	for _, line := range rows {
		if ansi.StringWidth(line) > 24 {
			t.Fatalf("deliverable row is %d cells: %q", ansi.StringWidth(line), plain(line))
		}
	}
	assertNarrowRows(t, "deliverables", drawDeliverablesBand(a, ambientBandContext(a, row, now, 30)), 30, "4h")
	empty := session.SessionRow{ID: "none", Transcript: filepath.Join(dir, "none", session.TranscriptName)}
	if got := drawDeliverablesBand(a, ambientBandContext(a, empty, now, 24)); len(got) != 0 {
		t.Fatalf("empty deliverables drew %q", got)
	}
}

func TestHostedDeliverablesBandReadsTheFarWorldNotTheLocalIndex(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, session.ArtifactsIndexName)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	session.RecordArtifact(index, session.Artifact{
		Path: filepath.Join(dir, "laptop.txt"), Session: "mine", Title: "from this machine", Created: now,
	})
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.artifacts = index
	a.home.world = session.World{Artifacts: []session.Artifact{{
		Path: "/srv/app/chart.png", Session: "mine", Title: "from the other machine", Kind: "image", Created: now,
	}}}
	row := session.SessionRow{ID: "mine", Transcript: "/srv/.aforge/v3/projects/app/mine/transcript.jsonl"}

	got := plain(strings.Join(drawDeliverablesBand(a, ambientBandContext(a, row, now, 50)), "\n"))
	if !strings.Contains(got, "chart.png") || strings.Contains(got, "laptop.txt") {
		t.Fatalf("hosted deliverables read the wrong machine:\n%s", got)
	}
}

func TestLeftOffBandDrawsThePairAndFits(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, session.TranscriptName)
	doc := strings.Join([]string{
		`{"type":"message","role":"user","content":"Please explain the unusually long migration plan"}`,
		`{"type":"message","role":"assistant","content":"First sentence. The final reply is deliberately long enough to wrap across several narrow rows without taking over the card."}`,
	}, "\n") + "\n"
	if err := os.WriteFile(transcript, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(nil)
	row := session.SessionRow{Transcript: transcript}
	rows := drawLeftOffBand(a, ambientBandContext(a, row, time.Now(), 26))
	got := plain(strings.Join(rows, "\n"))
	if !strings.HasPrefix(got, "› Please explain") || strings.Contains(got, "First sentence") || len(rows) != 3 {
		t.Fatalf("left-off band:\n%s", got)
	}
	for _, line := range rows {
		if ansi.StringWidth(line) > 26 {
			t.Fatalf("left-off row is %d cells: %q", ansi.StringWidth(line), plain(line))
		}
	}
	empty := session.SessionRow{Transcript: filepath.Join(t.TempDir(), session.TranscriptName)}
	if got := drawLeftOffBand(newTestApp(nil), ambientBandContext(newTestApp(nil), empty, time.Now(), 26)); len(got) != 0 {
		t.Fatalf("empty left-off drew %q", got)
	}
}
