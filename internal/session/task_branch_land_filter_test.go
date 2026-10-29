package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
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
			resetLandAttributeCapabilities()
			t.Cleanup(resetLandAttributeCapabilities)
			real := landCommand
			t.Cleanup(func() { landCommand = real })
			landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
				// Modern Git's usage answer is not evidence of an unsupported query.
				if failure == "old attribute-file query" && len(args) > 0 && args[len(args)-1] == "version" {
					return real(ctx, dir, "-c", "codeaf.testversion=git version 2.50.1", "config", "--get", "codeaf.testversion")
				}
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
	resetLandAttributeCapabilities()
	t.Cleanup(resetLandAttributeCapabilities)
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
		if joined := strings.Join(args, " "); strings.Contains(joined, "check-attr") || strings.Contains(joined, "GIT_ATTR_") || strings.Contains(joined, "read-tree") || strings.Contains(joined, "core.attributesFile") || strings.Contains(joined, "codeaf.landattributes") || len(args) == 1 && args[0] == "version" {
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
	resetLandAttributeCapabilities()
	t.Cleanup(resetLandAttributeCapabilities)
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

// The identity command is only constructed, so leave its resolved executable
// intact. Executed version questions use the bounded runner's prefixed args.
func landAttributeGit(t *testing.T, version string, tree, files bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the unsupported-query fixture uses /bin/sh")
	}
	resetLandAttributeCapabilities()
	t.Cleanup(resetLandAttributeCapabilities)
	real := landCommand
	t.Cleanup(func() { landCommand = real })
	landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		command := real(ctx, dir, args...)
		if len(args) > 1 && args[len(args)-1] == "version" {
			command = real(ctx, dir, "-c", "codeaf.testversion="+version, "config", "--get", "codeaf.testversion")
		} else if tree && strings.Contains(joined, "--source=") {
			command = landAttributeReply(ctx, "", "error: unknown option `source=tree'\nusage: git check-attr\n", 129)
		} else if files && strings.Contains(joined, "var GIT_ATTR_") {
			command = landAttributeReply(ctx, "", "usage: git var (-l | <variable>)\n", 129)
		}
		command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1")
		return command
	}
}

func landAttributeReply(ctx context.Context, out, diagnostics string, code int) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", `printf '%s' "$1"; printf '%s' "$2" >&2; exit "$3"`, "land-attribute-fixture", out, diagnostics, fmt.Sprint(code))
}

func landAttributeTreeFixture(t *testing.T, repo, source string, nested bool) string {
	t.Helper()
	path := "b.txt"
	if source == "base" {
		gitT(t, repo, "checkout", "-q", "--detach", "main~1")
		path = "a.txt"
	} else if source == "task" {
		gitT(t, repo, "checkout", "-q", landBranch)
	}
	attrs := path + " filter=Case.Sensitive\n"
	if nested {
		path = "nested/a file\twith newline\n.bin"
		writeFile(t, filepath.Join(repo, path), "content\n")
		writeFile(t, filepath.Join(repo, "nested", "overridden.bin"), "content\n")
		attrs = "[attr]large filter=Case.Sensitive\n"
		writeFile(t, filepath.Join(repo, "nested", ".gitattributes"), "*.bin large\n")
	}
	writeFile(t, filepath.Join(repo, ".gitattributes"), attrs)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "attributes")
	if source == "task" {
		gitT(t, repo, "checkout", "-q", "main")
	}
	if source == "base" {
		base := headOf(t, repo)
		for _, branch := range []string{landBranch, "main"} {
			gitT(t, repo, "checkout", "-q", "-B", branch, base)
			writeFile(t, filepath.Join(repo, ".gitattributes"), "")
			if nested {
				writeFile(t, filepath.Join(repo, "nested", ".gitattributes"), "")
			}
			file := "b.txt"
			if branch == "main" {
				file = "c.txt"
			}
			writeFile(t, filepath.Join(repo, file), "tip\n")
			gitT(t, repo, "add", ".")
			gitT(t, repo, "commit", "-q", "-m", "tip")
		}
	}
	return path
}

func TestALandFallsBackWhenAttributeQueriesAreUnavailable(t *testing.T) {
	for _, mode := range []string{"source", "files", "both"} {
		t.Run(mode, func(t *testing.T) {
			for _, source := range []string{"unused lfs", "task", "target", "base"} {
				programs := []string{"clean", "smudge", "process"}
				if source == "unused lfs" {
					programs = []string{"all"}
				}
				for _, program := range programs {
					t.Run(source+"/"+program, func(t *testing.T) {
						t.Setenv("HOME", t.TempDir())
						repo, place := landRepo(t)
						path := ""
						if source != "unused lfs" {
							path = landAttributeTreeFixture(t, repo, source, false)
						}
						marker := filepath.Join(t.TempDir(), "filter-ran")
						if source == "unused lfs" {
							writeFile(t, filepath.Join(os.Getenv("HOME"), ".gitconfig"), "[filter \"lfs\"]\n clean = git-lfs clean -- %f\n smudge = git-lfs smudge -- %f\n process = git-lfs filter-process\n required = true\n")
						} else {
							gitT(t, repo, "config", "filter.Case.Sensitive."+program, "echo ran > '"+filepath.ToSlash(marker)+"'; cat")
						}
						landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", mode != "files", mode != "source")
						head := headOf(t, repo)
						plan, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
						if path == "" {
							if err != nil {
								t.Fatal(err)
							}
							if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
								t.Fatal(err)
							}
						} else {
							refusal := wantRefusal(t, err, LandFilter)
							if !reflect.DeepEqual(refusal.Paths, []string{path}) {
								t.Fatalf("paths: %q, want %q", refusal.Paths, path)
							}
							untouched(t, repo, head)
						}
						if _, err := os.Stat(marker); !os.IsNotExist(err) {
							t.Fatalf("filter ran: %v", err)
						}
					})
				}
			}
		})
	}
	for _, tc := range []struct {
		version     string
		tree, files bool
	}{
		{"git version 2.50.1", false, true},
		{"git version 2.45.0", true, false},
		{"git version 2.39.5", false, true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo, place := landRepo(t)
			gitT(t, repo, "config", "filter.unused.clean", "cat")
			landAttributeGit(t, tc.version, tc.tree, tc.files)
			if tc.tree {
				original := landCommand
				// Supply clean native file-location answers even on this Mac, so this
				// case reaches the modern-version rejection of the tree query itself.
				landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
					if strings.Contains(strings.Join(args, " "), "var GIT_ATTR_") {
						return landAttributeReply(ctx, "\n", "", 0)
					}
					return original(ctx, dir, args...)
				}
			}
			_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
			wantRefusal(t, err, LandFilter)
		})
	}
}

func TestALandReadsEachTreesAttributesWithoutTheCheckout(t *testing.T) {
	for _, source := range []string{"target", "task", "base"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo, place := landRepo(t)
			path := landAttributeTreeFixture(t, repo, source, true)
			// The checkout contradicts committed nested attributes. The info rule
			// overrides one committed path in both native and indexed readings.
			writeFile(t, filepath.Join(repo, ".gitattributes"), "* !filter\n")
			writeFile(t, filepath.Join(repo, "nested", ".gitattributes"), "* !filter\n")
			writeFile(t, filepath.Join(repo, ".git", "info", "attributes"), "nested/overridden.bin !filter\n")
			gitT(t, repo, "config", "filter.Case.Sensitive.clean", "cat")
			landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", true, false)
			_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
			refusal := wantRefusal(t, err, LandFilter)
			if !reflect.DeepEqual(refusal.Paths, []string{path}) {
				t.Fatalf("paths: %q", refusal.Paths)
			}

			gitT(t, repo, "config", "filter.Case.Sensitive.clean", "")
			gitT(t, repo, "config", "filter.unused.clean", "cat")
			// Compare only the attribute binding: the checkout is intentionally dirty.
			config, err := landConfig(landCtx(t), repo)
			if err != nil {
				t.Fatal(err)
			}
			sources := []string{"", headOf(t, repo), strings.TrimSpace(gitOut(t, repo, "rev-parse", landBranch)), strings.TrimSpace(gitOut(t, repo, "merge-base", "HEAD", landBranch))}
			fallback, err := landFilterAttributes(landCtx(t), repo, config, []string{path, "nested/overridden.bin"}, sources...)
			if err != nil {
				t.Fatal(err)
			}
			probe := exec.Command("git", "check-attr", "--source="+sources[1], "filter", "--", path)
			probe.Dir = repo
			if out, err := probe.CombinedOutput(); err != nil {
				t.Logf("host lacks native tree query: %s", out)
				return
			}
			// Keep the same file-location resolver so this compares tree answers only.
			original := landCommand
			landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
				if strings.Contains(strings.Join(args, " "), "--source=") {
					return execGitForLand(ctx, dir, args...)
				}
				return original(ctx, dir, args...)
			}
			resetLandAttributeCapabilities()
			native, err := landFilterAttributes(landCtx(t), repo, config, []string{path, "nested/overridden.bin"}, sources...)
			if err != nil || native != fallback {
				t.Fatalf("native %q, fallback %q: %v", native, fallback, err)
			}
		})
	}
}

func execGitForLand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir, command.Env = dir, landEnv()
	return command
}

func TestALandResolvesLegacyAttributeFiles(t *testing.T) {
	for _, kind := range []string{"absolute", "relative", "tilde", "whitespace", "disabled", "xdg", "empty xdg"} {
		for _, editBound := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/bound=%t", kind, editBound), func(t *testing.T) {
				home, xdg := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CONFIG_HOME", xdg)
				repo, place := landRepo(t)
				defaultFile := filepath.Join(home, ".config", "git", "attributes")
				xdgFile := filepath.Join(xdg, "git", "attributes")
				bound, ignored := filepath.Join(home, "attributes"), xdgFile
				configured := bound
				switch kind {
				case "relative":
					bound, configured = filepath.Join(repo, "attributes"), "attributes"
				case "tilde":
					configured = "~/attributes"
				case "whitespace":
					bound = filepath.Join(home, "attributes with space\tand newline\n")
					configured = bound
				case "disabled":
					configured, bound, ignored = "", "", defaultFile
				case "xdg":
					bound, ignored = xdgFile, defaultFile
				case "empty xdg":
					t.Setenv("XDG_CONFIG_HOME", "")
					bound, ignored = defaultFile, xdgFile
				}
				if kind != "xdg" && kind != "empty xdg" {
					gitT(t, repo, "config", "core.attributesFile", configured)
				}
				if bound != "" {
					writeFile(t, bound, "# initial\n")
				}
				writeFile(t, ignored, "# ignored\n")
				// The relative file lives in the checkout but is not a task change.
				if kind == "relative" {
					writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), "/attributes\n")
				}
				gitT(t, repo, "config", "filter.unused.clean", "cat")
				landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", false, true)
				plan := planLand(t, place, repo)
				edited := ignored
				if editBound && bound != "" {
					edited = bound
				}
				writeFile(t, edited, "# initial\n# changed without changing an answer\n")
				_, err := LandTaskBranch(landCtx(t), place, repo, plan)
				if editBound && bound != "" {
					wantRefusal(t, err, LandChanged)
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	t.Run("system files and effective environment", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo, _ := landRepo(t)
		landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", false, true)
		system, err := landLegacySystemAttributes(landCtx(t), repo, true, "")
		expected, pathErr := landAttributePath(landReadInput(landCtx(t), repo, 64<<10, nil, "-c", "codeaf.landattributes=%(prefix)/share/git-core/gitattributes", "config", "--null", "--path", "--get", "codeaf.landattributes"))
		if err != nil || pathErr != nil || !reflect.DeepEqual(system, []string{expected, "/etc/gitattributes"}) {
			t.Fatalf("system paths %q: %v %v", system, err, pathErr)
		}
		for _, value := range []string{"", "false", "NO", "off", "0", "true", "YES", "on", "1", "-2", "bogus"} {
			paths, err := landLegacySystemAttributes(landCtx(t), repo, true, value)
			if value == "bogus" {
				wantRefusal(t, err, LandFilter)
				continue
			}
			disabled := value == "true" || value == "YES" || value == "on" || value == "1" || value == "-2"
			if err != nil || (len(paths) == 0) != disabled {
				t.Fatalf("NOSYSTEM=%q: %q %v", value, paths, err)
			}
		}
		if _, err := landLegacySystemAttributes(landCtx(t), repo, false, ""); err == nil {
			t.Fatal("accepted unknown non-Apple layout")
		}
		original := landCommand
		for _, value := range []string{"1", "bogus"} {
			landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
				command := original(ctx, dir, args...)
				// The last occurrence wins, even when an earlier value disagrees.
				command.Env = append(command.Env, "GIT_ATTR_NOSYSTEM=bogus", "GIT_ATTR_NOSYSTEM="+value, "XDG_CONFIG_HOME=", "HOME="+os.Getenv("HOME"))
				return command
			}
			files, disabled, err := landLegacyAttributeLocations(landCtx(t), repo, &landAttributeCapabilities{apple: true})
			if value == "bogus" {
				wantRefusal(t, err, LandFilter)
				continue
			}
			if err != nil || !reflect.DeepEqual(disabled, []string{"GIT_ATTR_SYSTEM"}) || !reflect.DeepEqual(files, []string{os.Getenv("HOME") + "/.config/git/attributes"}) {
				t.Fatalf("effective env files %q disabled %q: %v", files, disabled, err)
			}
		}
	})
}

func TestALandsTemporaryIndexLeavesTheRepositoryUntouched(t *testing.T) {
	for _, outcome := range []string{"plan and land", "refusal", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			repo, place := landRepo(t)
			if outcome == "refusal" {
				landAttributeTreeFixture(t, repo, "task", false)
			}
			gitT(t, repo, "config", "core.splitIndex", "true")
			marker := filepath.Join(t.TempDir(), "hook-ran")
			hook := filepath.Join(repo, ".git", "hooks", "post-index-change")
			writeFile(t, hook, "#!/bin/sh\necho ran > '"+filepath.ToSlash(marker)+"'\n")
			if err := os.Chmod(hook, 0o755); err != nil {
				t.Fatal(err)
			}
			driver := "unused"
			if outcome == "refusal" {
				driver = "Case.Sensitive"
			}
			gitT(t, repo, "config", "filter."+driver+".clean", "cat")
			before, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", true, true)
			ctx := landCtx(t)
			if outcome == "cancel" {
				original := landCommand
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
					if strings.Contains(strings.Join(args, " "), "read-tree") {
						time.AfterFunc(20*time.Millisecond, cancel)
						return exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30")
					}
					return original(ctx, dir, args...)
				}
			}
			plan, err := PlanTaskBranchLand(ctx, place, repo, landBranch)
			if outcome == "refusal" {
				wantRefusal(t, err, LandFilter)
			} else if outcome == "cancel" {
				var refusal *LandRefusal
				if !errors.Is(err, ErrLandBusy) && !errors.As(err, &refusal) {
					t.Fatalf("cancelled plan: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("plan changed real index: %v", err)
			}
			assertClean := func() {
				t.Helper()
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("index hook ran: %v", err)
				}
				for _, pattern := range []string{filepath.Join(repo, ".git", "sharedindex.*"), filepath.Join(repo, ".git", "index.lock"), filepath.Join(scratch, "codeaf-land-attributes-*")} {
					found, err := filepath.Glob(pattern)
					if err != nil || len(found) != 0 {
						t.Fatalf("scratch artifact %q: %q %v", pattern, found, err)
					}
				}
			}
			assertClean()
			if outcome == "plan and land" {
				// The actual merge is allowed to run this hook and use split indexes.
				// Remove those settings and replan before testing successful acceptance.
				if err := os.Remove(hook); err != nil {
					t.Fatal(err)
				}
				gitT(t, repo, "config", "core.splitIndex", "false")
				plan = planLand(t, place, repo)
				if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
					t.Fatal(err)
				}
				assertClean()
			}
		})
	}
}

func TestALandCachesOnlyAttributeCapabilities(t *testing.T) {
	t.Run("repeated plans and executable identity", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo, place := landRepo(t)
		gitT(t, repo, "config", "filter.unused.clean", "cat")
		realGit, err := exec.LookPath("git")
		if err != nil {
			t.Fatal(err)
		}
		landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", true, true)
		original := landCommand
		trees, files := 0, 0
		landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "--source=") {
				trees++
			}
			if strings.Contains(joined, "var GIT_ATTR_") {
				files++
			}
			return original(ctx, dir, args...)
		}
		planLand(t, place, repo)
		planLand(t, place, repo)
		if trees != 1 || files != 1 {
			t.Fatalf("probes repeated: trees=%d files=%d", trees, files)
		}
		bin := t.TempDir()
		if err := os.Symlink(realGit, filepath.Join(bin, "git")); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		planLand(t, place, repo)
		if trees != 2 || files != 2 {
			t.Fatalf("new executable path not probed: trees=%d files=%d", trees, files)
		}
	})
	for _, probe := range []string{"version", "files", "tree"} {
		t.Run("transient "+probe, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo, place := landRepo(t)
			gitT(t, repo, "config", "filter.unused.clean", "cat")
			landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", true, true)
			original := landCommand
			attempts := 0
			landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
				joined := strings.Join(args, " ")
				match := probe == "files" && strings.Contains(joined, "var GIT_ATTR_") || probe == "tree" && strings.Contains(joined, "--source=") || probe == "version" && len(args) > 1 && args[len(args)-1] == "version"
				if match {
					attempts++
					// The first executed version is the existing minimum-version check.
					failAt := 1
					if probe == "version" {
						failAt = 2
					}
					if attempts == failAt {
						return landAttributeReply(ctx, "", "temporary failure\n", 128)
					}
				}
				return original(ctx, dir, args...)
			}
			_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
			wantRefusal(t, err, LandFilter)
			planLand(t, place, repo)
			if attempts < 2 {
				t.Fatalf("transient failure cached: %d attempts", attempts)
			}
		})
	}
}

func TestALandRefusesWhenFallbackAttributesAreUnknown(t *testing.T) {
	for _, failure := range []string{"read-tree exit", "read-tree warning", "read-tree output", "cached exit", "cached warning", "cached incomplete", "cached malformed", "cached oversized"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			repo, place := landRepo(t)
			gitT(t, repo, "config", "filter.unused.clean", "cat")
			landAttributeGit(t, "git version 2.39.5 (Apple Git-154)", true, true)
			original := landCommand
			landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
				joined := strings.Join(args, " ")
				tree := strings.Contains(joined, "read-tree")
				cached := strings.Contains(joined, "check-attr --cached")
				if tree && strings.HasPrefix(failure, "read-tree") || cached && strings.HasPrefix(failure, "cached") {
					switch failure {
					case "read-tree exit", "cached exit":
						return landAttributeReply(ctx, "", "read failed\n", 128)
					case "read-tree warning", "cached warning":
						return landAttributeReply(ctx, "", "warning\n", 0)
					case "read-tree output":
						return landAttributeReply(ctx, "unexpected\n", "", 0)
					case "cached incomplete":
						return landAttributeReply(ctx, "", "", 0)
					case "cached malformed":
						return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'wrong\000filter\000unspecified\000wrong\000filter\000unspecified\000wrong\000filter\000unspecified\000'`)
					case "cached oversized":
						return exec.CommandContext(ctx, "/bin/sh", "-c", "head -c 8388609 /dev/zero")
					}
				}
				return original(ctx, dir, args...)
			}
			head := headOf(t, repo)
			_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
			reason := LandFilter
			if failure == "cached oversized" {
				reason = LandTooLarge
			}
			wantRefusal(t, err, reason)
			untouched(t, repo, head)
			found, err := filepath.Glob(filepath.Join(scratch, "codeaf-land-attributes-*"))
			if err != nil || len(found) != 0 {
				t.Fatalf("scratch artifacts: %q %v", found, err)
			}
		})
	}
}
