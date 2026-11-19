package approval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A RECURSIVE DELETE OUTSIDE THE AGENT'S FOLDER ASKS, HOWEVER IT IS SPELLED.
// The first rows are the ones canvas agents really ran on 2026-11-18, under an
// allow-all policy, with no question: the table matched `rm -rf /*` and the
// models had left the `-f` off.
func TestARecursiveDeleteOutsideTheFolderAsks(t *testing.T) {
	cases := []struct {
		name, command, names string
	}{
		{"no force flag", "rm -r /tmp/site_check", "rm -r /tmp/site_check"},
		{"after a cleanup inside", "rm -r _brand && rm -r /tmp/site_check", "rm -r /tmp/site_check"},
		{"capital R", "rm -R /tmp/site_check", "rm -R /tmp/site_check"},
		{"long flag", "rm --recursive /tmp/site_check", "rm --recursive /tmp/site_check"},
		{"split flags", "rm -r -f /tmp/site_check", "rm -r -f /tmp/site_check"},
		{"combined other order", "rm -Rfv /tmp/site_check", "rm -Rfv /tmp/site_check"},
		{"verbose first", "rm -vr /tmp/site_check", "rm -vr /tmp/site_check"},
		{"flag after the path", "rm /tmp/site_check -r", "rm /tmp/site_check -r"},
		{"path after double dash", "rm -r -- /tmp/site_check", "rm -r -- /tmp/site_check"},
		{"quoted path", `rm -r "/tmp/site check"`, `rm -r "/tmp/site check"`},
		{"full program path", "/bin/rm -r /tmp/site_check", "/bin/rm -r /tmp/site_check"},
		{"escaped alias", `\rm -r /tmp/site_check`, `\rm -r /tmp/site_check`},
		{"command wrapper", "command rm -r /tmp/site_check", "command rm -r /tmp/site_check"},
		{"sudo with a user", "sudo -u root rm -r /srv/site", "sudo -u root rm -r /srv/site"},
		{"env assignment", "LC_ALL=C rm -r /tmp/site_check", "LC_ALL=C rm -r /tmp/site_check"},
		{"home path", "rm -r ~/Downloads/old", "rm -r ~/Downloads/old"},
		{"a variable", `rm -r "$TMPDIR/site_check"`, `rm -r "$TMPDIR/site_check"`},
		{"a substitution", "rm -r $(cat dirs.txt)", "rm -r"},
		{"climbs out", "rm -r ../sibling", "rm -r ../sibling"},
		{"climbs out through a child", "rm -r build/../../sibling", "rm -r build/../../sibling"},
		{"one target outside of two", "rm -r build /tmp/site_check", "rm -r build /tmp/site_check"},
		{"after cd away", "cd /tmp && rm -r site_check", "rm -r site_check"},
		{"after cd away with semicolon", "cd /tmp; rm -r site_check", "rm -r site_check"},
		{"after cd up", "cd .. && rm -r other", "rm -r other"},
		{"after a bare cd", "cd && rm -r junk", "rm -r junk"},
		{"in a subshell after cd", "(cd /tmp && rm -r site_check)", "rm -r site_check"},
		{"find delete", "find /tmp/site_check -delete", "find /tmp/site_check -delete"},
		{"find delete with a filter", "find /tmp -name '*.png' -delete", "find /tmp -name '*.png' -delete"},
		{"find exec rm", `find /tmp -name build -exec rm -rf {} \;`, `find /tmp -name build -exec rm -rf {} \;`},
		{"find exec plain rm", "find /var/tmp -type f -exec rm {} +", "find /var/tmp -type f -exec rm {} +"},
		{"find after cd away", "cd /tmp && find . -delete", "find . -delete"},
		{"xargs from a find outside", "find /tmp -name build | xargs rm -r", "find /tmp -name build | xargs rm -r"},
		{"xargs with options", "find /tmp -print0 | xargs -0 -n 1 rm -rf", "find /tmp -print0 | xargs -0 -n 1 rm -rf"},
		{"xargs from a list", "cat dirs.txt | xargs rm -r", "cat dirs.txt | xargs rm -r"},
		{"xargs from a file", "xargs rm -r < dirs.txt", "xargs rm -r < dirs.txt"},
		{"through sh -c", `sh -c "rm -r /tmp/site_check"`, "rm -r /tmp/site_check"},
		{"through bash -lc after cd", `cd /tmp && bash -lc 'rm -r site_check'`, "rm -r site_check"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			segments, _ := splitSegments(test.command)
			hit, ok := criticalHit(test.command, segments, "")
			if !ok {
				t.Fatalf("criticalHit(%q) found nothing, want a question", test.command)
			}
			if hit != test.names {
				t.Fatalf("criticalHit(%q) named %q, want %q", test.command, hit, test.names)
			}
			if !AlwaysAsks(ToolBash, bashArgs(test.command)) {
				t.Fatalf("AlwaysAsks(%q) = false, want the floor", test.command)
			}
			decision := Policy{Default: ActionAllow}.CheckBash(test.command)
			if decision.Action != ActionPrompt || !strings.HasPrefix(decision.Rule, criticalCommandRulePrefix) {
				t.Fatalf("CheckBash(%q) under allow-all = %+v, want a critical-command prompt", test.command, decision)
			}
		})
	}
}

// A DELETE INSIDE THE AGENT'S OWN FOLDER STAYS ASK-FREE. Cleaning up its own
// work is most of what an agent deletes, and a question on every `rm -r
// build` would teach a person to answer without reading.
func TestARecursiveDeleteInsideTheFolderDoesNotAsk(t *testing.T) {
	cases := []struct{ name, command string }{
		{"a folder", "rm -r build"},
		{"forced", "rm -rf build"},
		{"long flag", "rm --recursive dist"},
		{"dot slash", "rm -r ./tmp/x"},
		{"a glob", "rm -rf dist/*"},
		{"everything here", "rm -rf *"},
		{"climbs and comes back", "rm -r build/../dist"},
		{"several targets", "rm -r build dist .cache"},
		{"after cd into a child", "cd site && rm -r old"},
		{"after cd into a child and back up", "cd site && cd .. && rm -r old"},
		{"errors to null", "rm -r build 2>/dev/null"},
		{"errors joined", "rm -rf build > log.txt 2>&1"},
		{"a plain file delete outside", "rm /tmp/one-file.txt"},
		{"a forced file delete outside", "rm -f /tmp/one-file.txt"},
		{"find delete here", "find . -name '*.tmp' -delete"},
		{"find with no root", "find -name '*.tmp' -delete"},
		{"find delete in a child", "find build -type f -delete"},
		{"find outside without deleting", "find /tmp -name '*.png'"},
		{"find exec something else", `find /tmp -name x -exec ls {} \;`},
		{"xargs from a find here", "find . -name node_modules -prune | xargs rm -rf"},
		{"xargs without a recursive rm", "find /tmp -name x | xargs rm"},
		{"xargs of something else", "cat list | xargs grep -l TODO"},
		{"through sh -c inside", `sh -c "rm -r build"`},
		{"a word that is not rm", "rmdir /tmp/empty"},
		{"rm named in an argument", "echo rm -r /tmp/x"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			segments, _ := splitSegments(test.command)
			if hit, ok := criticalHit(test.command, segments, ""); ok {
				t.Fatalf("criticalHit(%q) = %q, want no question", test.command, hit)
			}
			if AlwaysAsks(ToolBash, bashArgs(test.command)) {
				t.Fatalf("AlwaysAsks(%q) = true, want ordinary work", test.command)
			}
			if decision := (Policy{Default: ActionAllow}).CheckBash(test.command); decision.Action != ActionAllow {
				t.Fatalf("CheckBash(%q) under allow-all = %+v, want it to run", test.command, decision)
			}
		})
	}
}

// The floor is under allow and nothing more: a person's own deny still refuses
// a recursive delete outside, rather than being softened into a question.
func TestARecursiveDeleteOutsideStillYieldsToADeny(t *testing.T) {
	policy := Policy{Default: ActionAllow, BashPatterns: []Rule{{Match: "rm -r /tmp/*", Action: ActionDeny}}}
	if decision := policy.CheckBash("rm -r /tmp/site_check"); decision.Action != ActionDeny {
		t.Fatalf("CheckBash = %+v, want the deny rule to stand", decision)
	}
}

// THE AGENT'S OWN FOLDER, NAMED BY ITS ABSOLUTE PATH, IS STILL ITS OWN FOLDER.
// A canvas agent ran the first row on 2026-11-19 with its bash tool standing in
// exactly that folder, and the floor asked as though it had left (critical,
// irreversible, the turn blocked and the person's next message never sent),
// because the reader treated every absolute `cd` as away. Told the folder, it
// places the path; everything #2825 made ask still asks.
func TestARecursiveDeleteInTheKnownFolderIsPlaced(t *testing.T) {
	const work = "/tmp/connected-e2e.Km0WC460/home/canvases/cv_bngq2bi4klrn/work"
	real := "cd " + work + " && H=f_be5dqzqvnebr\n" +
		"canvas site text cv_bngq2bi4klrn $H hero-cta --set \"See the menu\" 2>&1 | head -c 60; echo\n" +
		"rm -rf site-check/site-check\n" +
		"for f in f_be5dqzqvnebr f_k25qohw3gyqi; do for w in 1280 390; do canvas render cv_bngq2bi4klrn $f --width $w --out site-check/final-$f-$w.png >/dev/null 2>&1; done; done\n" +
		"ls site-check/final-*.png"
	cases := []struct {
		name, command string
		asks          bool
	}{
		{"the real command", real, false},
		{"cd to the folder", "cd " + work + " && rm -rf site-check", false},
		{"cd to the folder with a trailing slash", "cd " + work + "/ && rm -r site-check", false},
		{"cd to a subfolder", "cd " + work + "/site && rm -rf old", false},
		{"cd to a subfolder and up to the folder", "cd " + work + "/site && rm -rf ../old", false},
		{"cd away and back", "cd /tmp && cd " + work + " && rm -rf old", false},
		{"absolute rm inside", "rm -rf " + work + "/site-check", false},
		{"absolute rm of a glob inside", "rm -rf " + work + "/dist/*", false},
		{"absolute rm without force inside", "rm -r " + work + "/site-check", false},
		{"absolute find delete inside", "find " + work + "/site-check -name '*.png' -delete", false},
		{"absolute rm inside after cd away", "cd /tmp && rm -rf " + work + "/site-check", false},
		{"through sh -c after cd to the folder", "cd " + work + " && sh -c 'rm -rf old'", false},

		{"cd to a sibling", "cd " + work + "/../other && rm -rf site-check", true},
		{"cd to a sibling by name", "cd /tmp/connected-e2e.Km0WC460/home/canvases/cv_bngq2bi4klrn/other && rm -rf x", true},
		{"cd to the parent", "cd /tmp/connected-e2e.Km0WC460/home/canvases/cv_bngq2bi4klrn && rm -rf work", true},
		{"cd to the folder then up", "cd " + work + " && rm -rf ../x", true},
		{"a longer name that only shares the start", "cd " + work + "2 && rm -rf x", true},
		{"absolute rm with a longer name", "rm -rf " + work + "2/x", true},
		{"absolute rm that climbs out", "rm -rf " + work + "/../x", true},
		{"the root", "rm -rf /", true},
		{"the root glob", "rm -fr /*", true},
		{"elsewhere in tmp", "rm -r /tmp/site_check", true},
		{"home", "cd " + work + " && rm -rf ~/x", true},
		{"a variable", "cd " + work + " && rm -rf \"$OUT/x\"", true},
		{"cd to a variable", "cd \"$WORK\" && rm -rf x", true},
		{"cd to the folder then a bare cd", "cd " + work + " && cd && rm -rf x", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			segments, _ := splitSegments(test.command)
			hit, asks := criticalHit(test.command, segments, work)
			if asks != test.asks {
				t.Fatalf("criticalHit(%q) in %s = (%q, %v), want a question %v", test.command, work, hit, asks, test.asks)
			}
			if got := AlwaysAsksIn(ToolBash, bashArgs(test.command), work); got != test.asks {
				t.Fatalf("AlwaysAsksIn(%q) = %v, want %v", test.command, got, test.asks)
			}
			decision := Policy{Default: ActionAllow, Workspace: work}.CheckBash(test.command)
			if want := map[bool]Action{true: ActionPrompt, false: ActionAllow}[test.asks]; decision.Action != want {
				t.Fatalf("CheckBash(%q) under allow-all in %s = %+v, want %s", test.command, work, decision, want)
			}
		})
	}
}

// WITH NO FOLDER, THE LINE IS READ ALONE, as it was before the folder was
// known: an absolute `cd` and an absolute target are outside, so a caller that
// has only the command keeps the cautious answer.
func TestWithNoFolderAnAbsolutePathStillAsks(t *testing.T) {
	const work = "/tmp/a/work"
	for _, command := range []string{
		"cd " + work + " && rm -rf site-check",
		"rm -r " + work + "/site-check",
		"rm -rf " + work + "/site-check",
	} {
		if !AlwaysAsks(ToolBash, bashArgs(command)) {
			t.Fatalf("AlwaysAsks(%q) with no folder = false, want the floor", command)
		}
		for _, workspace := range []string{"", "relative/work", "/"} {
			if !AlwaysAsksIn(ToolBash, bashArgs(command), workspace) {
				t.Fatalf("AlwaysAsksIn(%q, %q) = false, want the floor: that is no folder", command, workspace)
			}
		}
	}
}

// A FOLDER GIVEN THROUGH A LINK IS THE FOLDER IT LINKS TO. An agent that spells
// the resolved path (macOS gives /private/tmp for /tmp) is still at home; a
// path that only reaches the folder through a link of its own is not followed,
// and asks, exactly as a relative target's links never were.
func TestTheFolderIsPlacedThroughItsOwnLink(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	real := filepath.Join(root, "real", "work")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	given := filepath.Join(link, "work")
	cases := []struct {
		name, workspace, command string
		asks                     bool
	}{
		{"given through the link, cd to the resolved path", given, "cd " + real + " && rm -rf old", false},
		{"given through the link, rm the resolved path", given, "rm -rf " + real + "/old", false},
		{"given through the link, cd to the link", given, "cd " + given + " && rm -rf old", false},
		{"given resolved, cd through a link is not followed", real, "cd " + given + " && rm -rf old", true},
		{"given through the link, its resolved sibling asks", given, "rm -rf " + filepath.Join(root, "real", "other"), true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := AlwaysAsksIn(ToolBash, bashArgs(test.command), test.workspace); got != test.asks {
				t.Fatalf("AlwaysAsksIn(%q, %q) = %v, want %v", test.command, test.workspace, got, test.asks)
			}
		})
	}
}
