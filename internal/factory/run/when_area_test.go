package run

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestRunnerDecidesFileConditionsAtTheStep(t *testing.T) {
	for _, tc := range []struct {
		name, when, path string
		before, want     bool
	}{
		{"auth after write", "touches auth", "internal/auth/x.go", false, true},
		{"docs after write", "touches auth", "docs/a.md", false, false},
		{"pages after write", "has ui", "src/pages/a.tsx", false, true},
		{"docs without ui", "has ui", "docs/a.md", false, false},
		{"auth before write", "touches auth", "docs/a.md", true, true},
		{"ui before write", "has ui", "docs/a.md", true, true},
		{"auth filename", "touches auth", "src/Auth.go", false, true},
		{"billing", "touches auth", "internal/BILLING/x.go", false, true},
		{"crypto", "touches auth", "crypto.go", false, true},
		{"authors is not auth", "touches auth", "authors.go", false, false},
		{"ui segment", "has ui", "src/UI/x.go", false, true},
		{"tui", "has ui", "internal/tui/x.go", false, true},
		{"render filename", "has ui", "render.go", false, true},
		{"uint is not ui", "has ui", "internal/uint/x.go", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := gitRig(t)
			var mu sync.Mutex
			var ran []string
			g := newRig(t, map[factory.StageKind]Executor{
				factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
					mu.Lock()
					ran = append(ran, job.Stage.Name)
					mu.Unlock()
					if job.Stage.Name == "write" {
						path := filepath.Join(job.Dir, tc.path)
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							return factory.StageResult{}, err
						}
						if err := os.WriteFile(path, []byte("change\n"), 0600); err != nil {
							return factory.StageResult{}, err
						}
					}
					return done("finished"), nil
				}),
			}, func(o *Options) { o.RepoDir = func(string) string { return dir } })
			conditional := chat("inspect")
			conditional.When = tc.when
			stages := []factory.Stage{chat("write"), conditional}
			if tc.before {
				stages = []factory.Stage{conditional, chat("write")}
			}
			id := g.add("conditional change", stages...)
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			it := g.waitState(id, factory.StateLanded)
			mu.Lock()
			got := slices.Contains(ran, "inspect")
			mu.Unlock()
			if got != tc.want {
				t.Fatalf("inspect ran = %v, want %v; phases=%+v", got, tc.want, it.Stream.Phases)
			}
			if !tc.want {
				reason := "skipping inspect: when " + tc.when + " does not match the change files"
				if !logHas(it, reason) {
					t.Fatalf("missing skip reason %q: %+v", reason, it.Stream.Log)
				}
    // The worker's real report must carry the reason to the live job.
    server := factory.Item{ID: id, State: factory.StateRunning, Stages: it.Stages, Stream: &factory.Stream{}}
    report, ok := Diff(server, it)
    if !ok { t.Fatal("skip produced no worker report") }
    if err := ApplyReport(&server, report, g.r.now()); err != nil { t.Fatal(err) }
    job := api.FromItem(server)
    if job.Desk == nil || job.Desk.Run == nil { t.Fatal("job has no run log") }
    found := false
    for _, line := range job.Desk.Run.Log { if line.Text == reason { found = true } }
    if !found { t.Fatalf("live job lost skip reason: %+v", job.Desk.Run.Log) }

			} else if !logHas(it, "inspect: finished") {
				t.Fatalf("missing run result: %+v", it.Stream.Log)
			}
			if it.Change == nil || len(it.Change.Files) == 0 || !strings.EqualFold(it.Change.Files[0].Path, tc.path) {
				t.Fatalf("change not recorded: %+v", it.Change)
			}
		})
	}
}
