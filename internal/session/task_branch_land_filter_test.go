package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// L1: a global filter program unused by the land's paths permits both kinds of land.
func TestAnUnusedFilterDoesNotRefuseALand(t *testing.T) {
	t.Run("disabled global attributes", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo, place := landRepo(t)
		writeFile(t, filepath.Join(os.Getenv("HOME"), ".gitconfig"), "[filter \"lfs\"]\n clean = false\n smudge = false\n")
		gitT(t, repo, "config", "core.attributesFile", "")
		plan := planLand(t, place, repo)
		if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
			t.Fatalf("land with global attributes disabled: %v", err)
		}
	})
	t.Run("attribute without a matching program", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo, place := landRepo(t)
		writeFile(t, filepath.Join(os.Getenv("HOME"), ".gitconfig"), "[filter \"inactive\"]\n clean =\n smudge =\n process =\n")
		writeFile(t, filepath.Join(repo, ".git", "info", "attributes"), "b.txt filter=inactive\na.txt filter=\nc.txt -filter\n")
		plan := planLand(t, place, repo)
		if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
			t.Fatalf("land without a filter program: %v", err)
		}
	})
	t.Run("driver names are case sensitive", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo, place := landRepo(t)
		writeFile(t, filepath.Join(os.Getenv("HOME"), ".gitconfig"), "[filter \"Case.Sensitive\"]\n clean = false\n smudge = false\n")
		writeFile(t, filepath.Join(repo, ".git", "info", "attributes"), "b.txt filter=case.sensitive\n")
		plan := planLand(t, place, repo)
		if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
			t.Fatalf("land with another driver: %v", err)
		}
	})
	for _, ff := range []bool{false, true} {
		name := "merge"
		if ff {
			name = "fast forward"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			repo, place := landRepo(t)
			if ff {
				gitT(t, repo, "reset", "-q", "--hard", "HEAD~1")
			}
			writeFile(t, filepath.Join(repo, ".git", "info", "attributes"), "unrelated.bin filter=lfs\n")
			writeFile(t, filepath.Join(home, ".gitconfig"), "[filter \"lfs\"]\n clean = false\n smudge = false\n process = false\n required = true\n")
			plan := planLand(t, place, repo)
			landed, err := LandTaskBranch(landCtx(t), place, repo, plan)
			if err != nil || landed.HeadBefore != plan.Head || landed.FastForward != ff {
				t.Fatalf("land: %+v %v", landed, err)
			}
		})
	}
}

// L2: every attribute source git uses can select a configured filter, with exact paths.
func TestAUsedFilterRefusesALandWithItsPaths(t *testing.T) {
	for i, source := range []string{"target", "task", "base", "info", "global", "global default", "nested macro", "removed", "renamed", "compared", "task copy", "task copy configuration"} {
		programs := []string{[]string{"clean", "smudge", "process"}[i%3]}
		if source == "info" {
			programs = []string{"clean", "smudge", "process"}
		}
		for _, program := range programs {
			t.Run(source+"/"+program, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				repo, place := landRepo(t)
				path := "b.txt"
				driverRoot := repo
				switch source {
				case "target", "task", "nested macro":
					if source != "target" {
						gitT(t, repo, "checkout", "-q", landBranch)
					}
					if source == "nested macro" {
						path = "nested/a file\twith newline\n.bin"
						writeFile(t, filepath.Join(repo, path), "task\n")
						writeFile(t, filepath.Join(repo, ".gitattributes"), "[attr]large filter=Case.Sensitive\n")
						writeFile(t, filepath.Join(repo, "nested", ".gitattributes"), "*.bin large\n")
					} else {
						writeFile(t, filepath.Join(repo, ".gitattributes"), "b.txt filter=Case.Sensitive\n")
					}
					gitT(t, repo, "add", ".")
					gitT(t, repo, "commit", "-q", "-m", "attributes")
					if source != "target" {
						gitT(t, repo, "checkout", "-q", "main")
					}
				case "base", "removed", "renamed":
					gitT(t, repo, "checkout", "-q", "--detach", "main~1")
					writeFile(t, filepath.Join(repo, ".gitattributes"), "a.txt filter=Case.Sensitive\n")
					gitT(t, repo, "add", ".")
					gitT(t, repo, "commit", "-q", "-m", "base attributes")
					base := headOf(t, repo)
					gitT(t, repo, "checkout", "-q", "-B", landBranch, base)
					writeFile(t, filepath.Join(repo, "b.txt"), "task\n")
					path = "a.txt"
					if source == "renamed" {
						gitT(t, repo, "mv", "a.txt", "renamed.txt")
					} else if source == "removed" {
						gitT(t, repo, "rm", "-q", "a.txt")
					} else {
						writeFile(t, filepath.Join(repo, ".gitattributes"), "a.txt !filter\n")
					}
					gitT(t, repo, "add", ".")
					gitT(t, repo, "commit", "-q", "-m", "task")
					gitT(t, repo, "checkout", "-q", "-B", "main", base)
					writeFile(t, filepath.Join(repo, ".gitattributes"), "a.txt !filter\n")
					gitT(t, repo, "add", ".")
					gitT(t, repo, "commit", "-q", "-m", "target")
				case "info", "compared":
					if source == "compared" {
						path = "a.txt"
					}
					writeFile(t, filepath.Join(repo, ".git", "info", "attributes"), path+" filter=Case.Sensitive\n")
				case "global":
					attrs := filepath.Join(t.TempDir(), "attributes")
					writeFile(t, attrs, "b.txt filter=Case.Sensitive\n")
					gitT(t, repo, "config", "core.attributesFile", attrs)
				case "global default":
					xdg := filepath.Join(os.Getenv("HOME"), ".config")
					t.Setenv("XDG_CONFIG_HOME", xdg)
					writeFile(t, filepath.Join(xdg, "git", "attributes"), "b.txt filter=Case.Sensitive\n")
				case "task copy", "task copy configuration":
					if source == "task copy configuration" {
						gitT(t, repo, "config", "extensions.worktreeConfig", "true")
					}
					copy := filepath.Join(t.TempDir(), "copy")
					gitT(t, repo, "worktree", "add", "-q", copy, landBranch)
					writeFile(t, filepath.Join(copy, ".gitattributes"), "b.txt filter=Case.Sensitive\n")
					if source == "task copy configuration" {
						driverRoot = copy
					}
				}
				marker := filepath.Join(t.TempDir(), "filter-ran")
				args := []string{"config"}
				if source == "task copy configuration" {
					args = append(args, "--worktree")
				}
				args = append(args, "filter.Case.Sensitive."+program, "echo ran > '"+filepath.ToSlash(marker)+"'; cat")
				gitT(t, driverRoot, args...)
				head := headOf(t, repo)
				_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
				refusal := wantRefusal(t, err, LandFilter)
				if !reflect.DeepEqual(refusal.Paths, []string{path}) {
					t.Fatalf("paths: got %q, want %q", refusal.Paths, []string{path})
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("filter ran before refusal: %v", err)
				}
				untouched(t, repo, head)
			})
		}
	}
}

// L3: attributes and driver configuration are re-read under the plan's lock.
func TestFilterFactsChangingAfterAPlanRefuseTheLand(t *testing.T) {
	// L3 with L6: an attribute that appears after the plan matters only where a
	// filter program could then run, so those cases configure one up front.
	for _, change := range []string{"attribute", "global attribute", "driver", "tree attribute", "unused driver"} {
		t.Run(change, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo, place := landRepo(t)
			attrs := filepath.Join(repo, ".git", "info", "attributes")
			if change == "global attribute" {
				attrs = filepath.Join(t.TempDir(), "attributes")
				writeFile(t, attrs, "")
				gitT(t, repo, "config", "core.attributesFile", attrs)
			}
			if change == "driver" {
				writeFile(t, attrs, "b.txt filter=land-test-late\n")
			}
			if change == "attribute" || change == "global attribute" {
				gitT(t, repo, "config", "filter.land-test-late.clean", "cat")
			}
			plan := planLand(t, place, repo)
			switch change {
			case "driver", "unused driver":
				gitT(t, repo, "config", "filter.land-test-late.clean", "cat")
			case "tree attribute":
				gitT(t, repo, "checkout", "-q", landBranch)
				writeFile(t, filepath.Join(repo, ".gitattributes"), "b.txt filter=land-test-late\n")
				gitT(t, repo, "add", ".")
				gitT(t, repo, "commit", "-q", "-m", "late attributes")
				gitT(t, repo, "checkout", "-q", "main")
			default:
				writeFile(t, attrs, "b.txt filter=land-test-late\n")
			}
			head := headOf(t, repo)
			_, err := LandTaskBranch(landCtx(t), place, repo, plan)
			wantRefusal(t, err, LandChanged)
			untouched(t, repo, head)
		})
	}
}

// L4: unsupported attribute queries, read warnings and incomplete answers never permit a land.
func TestUnknownFilterAttributesRefuseALand(t *testing.T) {
	for _, failure := range []string{"old git", "old attribute-file query", "read failure", "read failure with spaces", "incomplete answer", "warning"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo, place := landRepo(t)
			// L4 is the land that needs the attributes: a filter program is
			// configured, so whether a path uses it must be known (L6 has none).
			gitT(t, repo, "config", "filter.land-test-unused.clean", "cat")
			if failure == "read failure" {
				gitT(t, repo, "config", "core.attributesFile", filepath.Join(repo, ".git"))
			}
			if failure == "read failure with spaces" {
				attrs := filepath.Join(repo, ".git", "bad attributes ")
				if err := os.Mkdir(attrs, 0o755); err != nil {
					t.Fatal(err)
				}
				gitT(t, repo, "config", "core.attributesFile", attrs)
			}
			warning := filepath.Join(t.TempDir(), "attributes")
			if failure == "warning" {
				writeFile(t, warning, "* =invalid\n")
			}
			real := landCommand
			t.Cleanup(func() { landCommand = real })
			landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
				if failure == "old attribute-file query" && strings.Contains(strings.Join(args, " "), "GIT_ATTR_") {
					return real(ctx, dir, "var", "UNSUPPORTED_ATTRIBUTE_VARIABLE")
				}
				if strings.Contains(strings.Join(args, " "), "check-attr") {
					switch failure {
					case "old git":
						return real(ctx, dir, "check-attr", "--no-such-option")
					case "incomplete answer":
						return real(ctx, dir, "version")
					case "warning":
						return real(ctx, dir, append([]string{"-c", "core.attributesFile=" + warning}, args...)...)
					}
				}
				return real(ctx, dir, args...)
			}
			head := headOf(t, repo)
			_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
			wantRefusal(t, err, LandFilter)
			untouched(t, repo, head)
		})
	}
}

// L6: with no filter program configured, a land asks git nothing about
// attributes, so a git without those queries (before 2.42) still plans and lands.
func TestALandWithNoFilterProgramNeverAsksAboutAttributes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, place := landRepo(t)
	writeFile(t, filepath.Join(repo, ".git", "info", "attributes"), "b.txt filter=lfs\n")
	real := landCommand
	t.Cleanup(func() { landCommand = real })
	asked := false
	// No filter program anywhere: HOME is empty, and the system config is left
	// out too (git-lfs installs a system-wide driver on CI runners and with Git
	// for Windows), since the land strips any GIT_* this process inherits.
	hermetic := func(command *exec.Cmd) *exec.Cmd {
		command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1")
		return command
	}
	landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		if joined := strings.Join(args, " "); strings.Contains(joined, "check-attr") || strings.Contains(joined, "GIT_ATTR_") {
			asked = true
			return hermetic(real(ctx, dir, "check-attr", "--no-such-option"))
		}
		return hermetic(real(ctx, dir, args...))
	}
	plan := planLand(t, place, repo)
	if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
		t.Fatalf("a land with no filter program was refused: %v", err)
	}
	if asked {
		t.Fatal("a land with no filter program asked git about attributes")
	}
}

// L3/L6: with no filter program configured, an attribute naming a filter that
// appears after the plan is inert (git has nothing to run), so the land goes ahead.
func TestAnInertFilterAttributeAfterThePlanDoesNotRefuseALand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, place := landRepo(t)
	real := landCommand
	t.Cleanup(func() { landCommand = real })
	landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		command := real(ctx, dir, args...)
		command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1")
		return command
	}
	plan := planLand(t, place, repo)
	writeFile(t, filepath.Join(repo, ".git", "info", "attributes"), "b.txt filter=land-test-late\n")
	if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
		t.Fatalf("an inert attribute after the plan refused the land: %v", err)
	}
}
