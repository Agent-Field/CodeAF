package tui3

// A RUN ON THE WORKER HARNESS IN ANOTHER WINDOW WEARS HOME'S WORKING MARK.
//
// Home draws a conversation it does not hold as working from its saved rollup
// alone (homebullets.go, #1426: session.Tasks.Running). A run on the worker
// harness used to leave that count at zero for as long as it ran — its window
// named no work in its presence file and the project's record had no row until
// the end — so a chat with a run in flight read as idle on Home. This drives a
// real session through the real task door with the run engine seated, lets it
// write its presence file, reads the machine the way Home does, and asks Home's
// own bullet what it draws.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// heldRunEngine is the run engine as this test drives it: the run is live from
// Start until the conversation closes, and its landing lands nothing.
type heldRunEngine struct{ started chan struct{} }

func (e *heldRunEngine) Start(ctx context.Context, _ session.RunSpec) session.RunSummary {
	close(e.started)
	<-ctx.Done()
	return session.RunSummary{Outcome: "ran and did not finish"}
}

func (e *heldRunEngine) Land(context.Context, *plandb.Store, string, string) (session.RunLanding, error) {
	return session.RunLanding{Refused: "nothing to land"}, nil
}

func beltRunRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"checkout", "-q", "-b", "work"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "seed.txt"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "seed"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

func TestHomeShowsAnotherWindowsLiveBeltRunAsWorking(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := beltRunRepo(t)
	root := t.TempDir()
	place := session.Place{Dir: filepath.Join(root, "-work-repo", "0123456789abcdef"), Workspace: repo}
	if err := os.MkdirAll(place.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	engine := &heldRunEngine{started: make(chan struct{})}
	session.RegisterRunEngine(engine)
	t.Cleanup(func() { session.RegisterRunEngine(nil) })

	agent, err := session.New(session.Config{
		Workspace:   repo,
		Model:       "vendor/m",
		APIKey:      "test",
		BaseURL:     "http://127.0.0.1:1/never-dialled",
		System:      "SYSTEM",
		Place:       place,
		SessionFile: place.Transcript(),
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	if err := session.SaveMeta(place.Dir, session.Meta{ID: place.ID(), LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(place.Transcript()); err != nil {
		if err := os.WriteFile(place.Transcript(), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := agent.StartTask(context.Background(), "make the change", false); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-engine.started

	// Home reads the machine the way it always does; the window's presence file
	// is written by its own heartbeat, so the reading is taken until it says so.
	var row session.SessionRow
	deadline := time.Now().Add(15 * time.Second)
	for {
		for _, project := range session.ReadWorld(root).Projects {
			for _, candidate := range project.Sessions {
				if candidate.ID == place.ID() {
					row = candidate
				}
			}
		}
		if row.Tasks.Running > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if row.Tasks.Running != 1 {
		t.Fatalf("the window with a live run counts %d running on Home, want 1 (rollup %+v)", row.Tasks.Running, row.Tasks)
	}

	a, _ := homeTabsFixture(t)
	a.linear = true
	cell := &homeCell{row: &switcherRow{session: row}}
	if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
		t.Fatalf("another window's live run is not drawn as working on Home: %q", got)
	}
}
