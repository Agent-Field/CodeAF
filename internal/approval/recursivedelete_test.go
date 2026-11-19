package approval

import (
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
			hit, ok := criticalHit(test.command, segments)
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
			if hit, ok := criticalHit(test.command, segments); ok {
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
