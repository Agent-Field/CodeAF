package tui3

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The worker is scripted, but both halves of its landing use the production
// Git road. The surface talks to the session through the ordinary local host.
type landBeltEngine struct{ release <-chan struct{} }

func (e landBeltEngine) Start(ctx context.Context, spec session.RunSpec) session.RunSummary {
	select {
	case <-e.release:
	case <-ctx.Done():
		return session.RunSummary{}
	}
	if err := os.WriteFile(filepath.Join(spec.Workspace, "README.md"), []byte("hello\n"), 0644); err != nil {
		return session.RunSummary{Result: err.Error()}
	}
	if err := spec.Store.CompleteRoot("added README.md"); err != nil {
		return session.RunSummary{Result: err.Error()}
	}
	return session.RunSummary{Outcome: "done", Result: "added README.md", Nodes: 1, Steps: 1}
}
func (e landBeltEngine) Land(_ context.Context, _ *plandb.Store, workspace, base, _ string) (session.RunLanding, error) {
	branch, changed, refused, err := session.LandRunTree(workspace, base, "add README.md", "")
	return session.RunLanding{Branch: branch, Changed: changed, Refused: refused}, err
}

func TestLocalHostLandFindsBeltTaskBeforeAndAfterRestart(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_FURROW", filepath.Join(t.TempDir(), "no-furrow"))
	t.Setenv("CODEAF_HOME", t.TempDir())
	// With no provider key, naming and summary requests refuse before touching
	// the network; the scripted worker still drives the real task and Git roads.
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%t", restart), func(t *testing.T) {
			repo, place := t.TempDir(), t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "-b", "main")
			git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
			before := git("rev-parse", "HEAD")
			release := make(chan struct{})
			session.RegisterRunEngine(landBeltEngine{release: release})
			t.Cleanup(func() { session.RegisterRunEngine(nil) })
			home := session.Place{Dir: place}
			config := session.Config{
				Workspace: repo, Place: home, SessionFile: home.Transcript(),
				Model: "test/model", BaseURL: "http://provider.invalid/v1", System: "Test only.",
			}
			open := func() (*session.Agent, *remote.Loop) {
				t.Helper()
				engine, err := session.New(config)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = engine.Close(); engine.SettleWrites() })
				loop, err := remote.Loopback(remote.Hello{Version: remote.Version, Workspace: repo}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
					return &remote.Engine{Agent: engine, Workspace: repo, SessionFile: config.SessionFile}, nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = loop.Close() })
				return engine, loop
			}
			engine, loop := open()
			updates, stop := loop.Client.Agent().WatchTaskUpdates()
			defer stop()
			if _, _, _, err := loop.Client.Agent().StartTask(context.Background(), "add a README.md with one line", false); err != nil {
				t.Fatal(err)
			}
			close(release)
			timeout := time.After(15 * time.Second)
			var notice *session.TaskNotice
			for notice == nil {
				select {
				case event, open := <-updates:
					if !open {
						t.Fatal("the task lane closed before the belt task finished")
					}
					if event.Task != nil && event.Task.State == session.TaskDone {
						notice = event.Task
					}
				case <-timeout:
					t.Fatal("no finished belt task reached the local host")
				}
			}
			if notice.Merge != "kept" || notice.Branch == "" {
				t.Fatalf("landing = %+v", notice)
			}
			if got := git("rev-parse", "HEAD"); got != before {
				t.Fatal("automatic landing changed main")
			}
			if got := git("show", notice.Branch+":README.md"); got != "hello" {
				t.Fatalf("kept README = %q", got)
			}
			if restart {
				stop()
				if err := loop.Close(); err != nil {
					t.Fatal(err)
				}
				if err := engine.Close(); err != nil {
					t.Fatal(err)
				}
				engine.SettleWrites()
				engine, loop = open()
			}
			a := newTestApp(loop.Client.Agent())
			a.width, a.height = 160, 40
			if cmd := a.slash("/land"); cmd != nil {
				drive(t, a, cmd())
			}
			said := plain(lastNote(t, a))
			for _, want := range []string{"changes for ", "1 file", "README.md", "/land now"} {
				if !strings.Contains(said, want) {
					t.Fatalf("/land = %q, missing %q", said, want)
				}
			}
			cmd := a.slash("/land now")
			if cmd == nil {
				t.Fatal("/land now did not start a landing")
			}
			drive(t, a, cmd())
			if said := plain(lastNote(t, a)); !strings.Contains(said, "now has the changes") {
				t.Fatalf("/land now = %q", said)
			}
			body, err := os.ReadFile(filepath.Join(repo, "README.md"))
			if err != nil || string(body) != "hello\n" {
				t.Fatalf("landed README = %q, %v", body, err)
			}
			if row := a.landRow(a.width); row != "" {
				t.Fatalf("landing left waiting row: %q", row)
			}
		})
	}
}
