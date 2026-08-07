package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// guardedPackages are the trees this sweep holds to the law. Each one runs
// goroutines under a live terminal surface, so a fault in any of them is a
// fault the user would watch happen.
var guardedPackages = []string{
	"cmd/aforge",
	"internal/exec",
	"internal/provider",
	"internal/resident",
	"internal/store",
}

// spawnAllowlist names the spawn sites that carry their guard somewhere the
// sweep cannot see — inside the function being spawned — with the reason.
// Nothing else belongs here: "it cannot panic" is a claim the next edit breaks.
var spawnAllowlist = map[string]string{
	"internal/exec/jobs.go:go r.wait(job)":                                                       "wait recovers and settles the job's terminal state itself",
	"internal/exec/schedule.go:go s.work(leafCtx, id, task, retries[id], leafShape(node), done)": "work recovers and reports the fault as the leaf's completion",
}

// spawn matches a goroutine launch at the start of a statement.
var spawn = regexp.MustCompile(`^\s*go [a-zA-Z_(]`)

// guarded matches the idiom that makes a spawn safe: the guard's own spawner,
// its deferred recover, or a hand-written recover in the same few lines.
var guarded = regexp.MustCompile(`guard\.Recover\(|guard\.Go\(|recover\(\)`)

// guardWindow is how far below a `go` statement the sweep looks for the idiom.
// The guard is always the first thing in the goroutine's body, so this is
// generous rather than tuned.
const guardWindow = 10

// TestEveryGoroutineInTheGuardedTreeIsGuarded is the standing check behind the
// law that the terminal surface never dies from a panic. A new goroutine either
// starts with a guard or is named here with a reason.
func TestEveryGoroutineInTheGuardedTreeIsGuarded(t *testing.T) {
	root := repositoryRoot(t)
	var unguarded []string

	for _, pkg := range guardedPackages {
		err := filepath.WalkDir(filepath.Join(root, pkg), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			lines := strings.Split(string(raw), "\n")
			for index, line := range lines {
				if !spawn.MatchString(line) {
					continue
				}
				key := filepath.ToSlash(relative) + ":" + strings.TrimSpace(line)
				if _, allowed := spawnAllowlist[key]; allowed {
					continue
				}
				window := strings.Join(lines[index:min(index+guardWindow, len(lines))], "\n")
				if guarded.MatchString(window) {
					continue
				}
				unguarded = append(unguarded, fmt.Sprintf("%s:%d: %s",
					filepath.ToSlash(relative), index+1, strings.TrimSpace(line)))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", pkg, err)
		}
	}

	sort.Strings(unguarded)
	if len(unguarded) > 0 {
		t.Fatalf("these goroutines can take the terminal surface down with them.\n"+
			"Wrap each in guard.Go, or open it with `defer guard.Recover(\"scope\")`:\n  %s",
			strings.Join(unguarded, "\n  "))
	}
}

// TestSpawnAllowlistIsStillReal keeps the escape hatch honest: an allowlisted
// site that no longer exists is a stale exemption the next spawn could inherit.
func TestSpawnAllowlistIsStillReal(t *testing.T) {
	root := repositoryRoot(t)
	for key := range spawnAllowlist {
		file, statement, found := strings.Cut(key, ":")
		if !found {
			t.Fatalf("allowlist key %q is not file:statement", key)
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Fatalf("allowlisted %s: %v", file, err)
		}
		if !strings.Contains(string(raw), statement) {
			t.Fatalf("allowlisted spawn is gone from %s: %q", file, statement)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the guard package")
		}
		dir = parent
	}
}
