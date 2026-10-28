package cellstore

import (
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// A fresh checkout whose tests carry fake keys used to say one sentence per
// fixture on its first seal. One screen is one notice now, its lead counts the
// files, and every file is named with its rule — in the words and as facts.
func TestOneScreenIsOneNoticeNamingEveryFileItKeptOut(t *testing.T) {
	r := newRig(t)
	for _, rel := range []string{"a_test.go", "b_test.go", "c/d_test.go"} {
		r.write(rel, "const key = \""+fakeKey+"\"\n")
	}
	r.screen()

	if got := r.policy().withheld; len(got) != 3 {
		t.Fatalf("withheld %v, want all three", got)
	}
	if len(r.told) != 1 {
		t.Fatalf("notices %q: want one for the screen", r.notice)
	}
	n := r.told[0]
	if want := "3 files look like they hold secrets, so they are kept out of the saved history; they travel through the key vault only to paired computers"; n.Lead != want {
		t.Fatalf("lead %q, want %q", n.Lead, want)
	}
	var paths []string
	for _, f := range n.Files {
		paths = append(paths, f.Path)
		if f.Rule == "" || f.Rule == keys.RuleDotenv {
			t.Fatalf("file %s carries rule %q", f.Path, f.Rule)
		}
		if !strings.Contains(n.Text, "\n· "+f.Path+" ("+f.Rule+")") {
			t.Fatalf("the words do not name %s with its rule: %q", f.Path, n.Text)
		}
	}
	if !reflect.DeepEqual(sortedCopy(paths), []string{"a_test.go", "b_test.go", "c/d_test.go"}) {
		t.Fatalf("files %v", paths)
	}
	if !strings.HasPrefix(n.Text, n.Lead+"\n") || strings.Contains(n.Text, fakeKey) {
		t.Fatalf("text %q: the lead, then the files, never the value", n.Text)
	}
	r.screen()
	if len(r.told) != 1 {
		t.Fatalf("a file already kept out was announced again: %q", r.notice)
	}
}

// The user's rule: a file git tracks, exactly as HEAD has it, is no news. It is
// still kept out of the saved history; only its sentence goes. An edited
// tracked file, one staged but never committed, and an untracked one are news.
func TestAFileGitHoldsUnchangedIsKeptOutButNotAnnounced(t *testing.T) {
	r := newRig(t)
	secret := "const key = \"" + fakeKey + "\"\n"
	r.write("fixtures/carried_test.go", secret)
	r.write("fixtures/edited_test.go", secret)
	r.write("README.md", "hello\n")
	commitAll(t, r.tree)
	r.write("fixtures/edited_test.go", secret+"// edited\n")
	r.write("staged.go", secret)
	if out, err := gitIn(r.tree, "add", "staged.go"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	r.write("loose.go", secret)
	r.screen()

	if got := sortedCopy(r.policy().withheld); !reflect.DeepEqual(got, []string{"fixtures/carried_test.go", "fixtures/edited_test.go", "loose.go", "staged.go"}) {
		t.Fatalf("withheld %v: the tracked fixture must still be kept out", got)
	}
	if len(r.told) != 1 {
		t.Fatalf("notices %q", r.notice)
	}
	var named []string
	for _, f := range r.told[0].Files {
		named = append(named, f.Path)
	}
	if !reflect.DeepEqual(sortedCopy(named), []string{"fixtures/edited_test.go", "loose.go", "staged.go"}) || strings.Contains(r.told[0].Text, "carried_test.go") {
		t.Fatalf("named %v in %q: want every file but the one git holds unchanged", named, r.told[0].Text)
	}
}

// Only files git holds unchanged: the screen says nothing at all, and the
// files are kept out all the same.
func TestAScreenOfOnlyHistorySaysNothing(t *testing.T) {
	r := newRig(t)
	r.write("one_test.go", "k := \""+fakeKey+"\"\n")
	r.write("two_test.go", "k := \""+fakeKey+"\"\n")
	commitAll(t, r.tree)
	r.screen()
	if got := r.policy().withheld; len(got) != 2 {
		t.Fatalf("withheld %v", got)
	}
	if len(r.notice) != 0 {
		t.Fatalf("history was announced: %q", r.notice)
	}
}

// A workspace below the repository's root reads git's answer relative to
// itself, and a name with a glob's characters is a name like any other.
func TestHistoryIsReadFromAWorkspaceBelowTheRoot(t *testing.T) {
	r := newRig(t)
	root := r.tree
	r.tree = filepath.Join(root, "svc")
	r.write("held[1].go", "k := \""+fakeKey+"\"\n")
	r.write("held1.go", "package svc\n")
	commitAll(t, root)
	r.write("held1.go", "k := \""+fakeKey+"\"\n")
	r.write("new.go", "k := \""+fakeKey+"\"\n")
	r.screen()

	if len(r.told) != 1 {
		t.Fatalf("notices %q", r.notice)
	}
	var named []string
	for _, f := range r.told[0].Files {
		named = append(named, f.Path)
	}
	if !reflect.DeepEqual(sortedCopy(named), []string{"held1.go", "new.go"}) {
		t.Fatalf("named %v: held[1].go is history, held1.go was edited", named)
	}
}

// A tree that is not a git checkout, or a repository with no commit, has no
// history to consult: every file is announced, as before the rule.
func TestWithoutHistoryEveryFileIsAnnounced(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, dir string){
		"no checkout": func(*testing.T, string) {},
		"no commit": func(t *testing.T, dir string) {
			if out, err := gitIn(dir, "init", "-q"); err != nil {
				t.Fatalf("git init: %v\n%s", err, out)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			prepare(t, r.tree)
			r.write("a.go", "k := \""+fakeKey+"\"\n")
			r.write("b.go", "k := \""+fakeKey+"\"\n")
			r.screen()
			if len(r.told) != 1 || len(r.told[0].Files) != 2 {
				t.Fatalf("notices %+v", r.told)
			}
		})
	}
}

// Paths this machine cannot read are grouped the same way, and never dropped
// as history: their permissions are the person's to fix.
func TestUnreadablePathsAreOneNotice(t *testing.T) {
	skipIfRoot(t)
	r := newRig(t)
	r.write("odd.bin", "x")
	r.write("odder.bin", "y")
	commitAll(t, r.tree)
	r.chmod("odd.bin", 0)
	r.chmod("odder.bin", 0)
	r.lookAndNote()

	if len(r.told) != 1 {
		t.Fatalf("notices %q", r.notice)
	}
	n := r.told[0]
	if want := "2 files cannot be read here, so they are left out of the saved history until their permissions allow it"; n.Lead != want {
		t.Fatalf("lead %q, want %q", n.Lead, want)
	}
	if len(n.Files) != 2 || n.Files[0].Rule != keys.RuleUnreadable || n.Files[1].Rule != keys.RuleUnreadable {
		t.Fatalf("files %+v", n.Files)
	}
}

// The words of every shape a screen can say. One file keeps the sentence it
// always had; several are counted, with the same meanings.
func TestSealNoticeWords(t *testing.T) {
	secret := keys.Finding{Path: "a.go", Rule: "api-key-sk"}
	env := keys.Finding{Path: ".env", Rule: keys.RuleDotenv}
	odd := keys.Finding{Path: "odd.bin", Rule: keys.RuleUnreadable}
	for _, c := range []struct {
		name                 string
		withheld, unreadable []keys.Finding
		text, lead           string
	}{
		{name: "nothing"},
		{name: "one secret", withheld: []keys.Finding{secret},
			text: "a.go looks like it holds a secret (api-key-sk), so it is kept out of the saved history; the file travels through the key vault only to paired computers"},
		{name: "one environment file", withheld: []keys.Finding{env},
			text: ".env is kept out of the saved history; its variables are kept locally for your tools, and the file travels through the key vault only to paired computers"},
		{name: "one unreadable", unreadable: []keys.Finding{odd},
			text: "odd.bin cannot be read here, so it is left out of the saved history until its permissions allow it"},
		{name: "environment files", withheld: []keys.Finding{env, {Path: "web/.env", Rule: keys.RuleDotenv}},
			lead: "2 environment files are kept out of the saved history; their variables are kept locally for your tools, and the files travel through the key vault only to paired computers"},
		{name: "secrets and environment files", withheld: []keys.Finding{secret, env, {Path: "web/.env", Rule: keys.RuleDotenv}},
			lead: "3 files look like they hold secrets, so they are kept out of the saved history; they travel through the key vault only to paired computers, and the variables of the 2 environment files among them are kept locally for your tools"},
		{name: "one of each", withheld: []keys.Finding{secret}, unreadable: []keys.Finding{odd},
			lead: "1 file looks like it holds a secret, so it is kept out of the saved history; it travels through the key vault only to paired computers. 1 file cannot be read here, so it is left out of the saved history until its permissions allow it"},
		{name: "an environment file and an unreadable one", withheld: []keys.Finding{env}, unreadable: []keys.Finding{odd},
			lead: "1 environment file is kept out of the saved history; its variables are kept locally for your tools, and the file travels through the key vault only to paired computers. 1 file cannot be read here, so it is left out of the saved history until its permissions allow it"},
	} {
		t.Run(c.name, func(t *testing.T) {
			n := sealNotice(c.withheld, c.unreadable)
			if c.lead == "" {
				if n.Text != c.text || n.Lead != "" || n.Files != nil {
					t.Fatalf("notice %+v, want the one sentence %q", n, c.text)
				}
				return
			}
			if n.Lead != c.lead {
				t.Fatalf("lead %q\nwant %q", n.Lead, c.lead)
			}
			if len(n.Files) != len(c.withheld)+len(c.unreadable) || strings.Count(n.Text, "\n· ") != len(n.Files) {
				t.Fatalf("notice %+v names the wrong files", n)
			}
		})
	}
}

// A prose reader cannot fold a list: past the bound the words count the rest,
// and the facts still carry every file.
func TestALongListIsCountedInWordsAndWholeInFacts(t *testing.T) {
	var found []keys.Finding
	for i := range noticeLinesSaid + 5 {
		found = append(found, keys.Finding{Path: "f" + strconv.Itoa(i) + ".go", Rule: "api-key-sk"})
	}
	n := sealNotice(found, nil)
	if len(n.Files) != noticeLinesSaid+5 || strings.Count(n.Text, "\n· f") != noticeLinesSaid || !strings.HasSuffix(n.Text, "\n· and 5 more") {
		t.Fatalf("notice %q with %d files", n.Text, len(n.Files))
	}
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
