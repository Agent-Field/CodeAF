package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// copyKeeper is a factory store's copies, in memory.
type copyKeeper struct {
	copies  map[string]*factory.PipelineCopy
	version map[string]int
	broken  error
}

func newCopyKeeper() *copyKeeper {
	return &copyKeeper{copies: map[string]*factory.PipelineCopy{}, version: map[string]int{}}
}

func (k *copyKeeper) PipelineCopy(repo string) (*factory.PipelineCopy, int, error) {
	if k.broken != nil {
		return nil, 0, k.broken
	}
	key := factory.PipelineCopyKey(repo)
	return k.copies[key].Clone(), k.version[key] + 1, nil
}

func (k *copyKeeper) ChangePipelineCopy(repo string, change func(*factory.PipelineCopy, int) (*factory.PipelineCopy, bool, error)) (*factory.PipelineCopy, int, error) {
	key := factory.PipelineCopyKey(repo)
	next, write, err := change(k.copies[key].Clone(), k.version[key]+1)
	if err != nil || !write {
		return k.copies[key].Clone(), k.version[key] + 1, err
	}
	k.version[key]++
	k.copies[key] = next.Clone()
	return next.Clone(), k.version[key] + 1, nil
}

const copyText = "pipeline:\n  issue: [plan, write, proof]\nrules:\n  - keep the copy's rule\n"

func mainRead(t *testing.T, text string) File {
	t.Helper()
	if text == "" {
		f := Defaults()
		f.Source, f.From = SourceDefaults, factory.FromDefault
		return f
	}
	f, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	f.Source, f.From = SourceFile, factory.FromMainBranch
	return f
}

// PRECEDENCE: THE FACTORY'S COPY, ELSE MAIN'S FILE, ELSE THE BUILT-IN STEPS. A
// copy stands in for main whatever main says, and main's read error no longer
// matters; without a copy the read is main's, as it was.
func TestTheFactorysCopyStandsInForMainsFileAndTheDefaults(t *testing.T) {
	keep := newCopyKeeper()
	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	mainFile := "pipeline:\n  issue: [write, proof]\nrules:\n  - main's rule\n"

	// No copy: main's file, then the defaults, unchanged.
	for _, text := range []string{mainFile, ""} {
		got, err := WithFactoryCopy(mainRead(t, text), nil, keep, "acme/api", "")
		if err != nil || got.Copy != nil || got.From == factory.FromFactoryCopy {
			t.Fatalf("no copy read %+v (%v)", got.From, err)
		}
	}
	keep.copies["acme/api"] = &factory.PipelineCopy{Repo: "acme/api", Text: copyText, By: "test-owner", At: at}
	for name, c := range map[string]struct {
		main string
		err  error
	}{"over main's file": {main: mainFile}, "over the defaults": {}, "over an unreadable main": {err: errors.New("github is down")}} {
		t.Run(name, func(t *testing.T) {
			got, err := WithFactoryCopy(mainRead(t, c.main), c.err, keep, "ACME/api", "")
			if err != nil || got.From != factory.FromFactoryCopy || got.Source != SourceFile || got.Text != copyText || got.Copy == nil || got.Copy.By != "test-owner" {
				t.Fatalf("read from %q %+v (%v)", got.From, got.Copy, err)
			}
			if steps := got.Pipeline[factory.KindIssue]; strings.Join(steps, ",") != "plan,write,proof" {
				t.Fatalf("steps %v", steps)
			}
			if strings.Join(got.Rules, "|") != "keep the copy's rule" {
				t.Fatalf("rules %v", got.Rules)
			}
		})
	}
	// THE FACTORY'S OWN RULES STILL GO ON TOP of the copy.
	got, _ := WithFactoryCopy(mainRead(t, mainFile), nil, keep, "acme/api", "")
	got = WithHouseRules(got, factory.HouseRules{Rules: []string{"house rule"}, AskFirst: []string{"touch auth/"}})
	if strings.Join(got.Rules, "|") != "keep the copy's rule|house rule" || strings.Join(got.Start.AskFirst, "|") != "touch auth/" || got.Copy == nil {
		t.Fatalf("house rules on the copy: %v %v", got.Rules, got.Start.AskFirst)
	}
}

// A PERSONAL FILE TIGHTENS THE COPY as it tightens main's file.
func TestThePersonalFileTightensTheCopy(t *testing.T) {
	keep := newCopyKeeper()
	keep.copies["acme/api"] = &factory.PipelineCopy{Repo: "acme/api", Text: copyText + "default_budget: 20\n", At: time.Now()}
	personal := filepath.Join(t.TempDir(), "factory.yaml")
	if err := os.WriteFile(personal, []byte("default_budget: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := WithFactoryCopy(mainRead(t, ""), nil, keep, "acme/api", personal)
	if err != nil || !got.Personal || got.DefaultBudget != 5 || got.Copy == nil || got.From != factory.FromFactoryCopy {
		t.Fatalf("budget %v personal %v from %q (%v)", got.DefaultBudget, got.Personal, got.From, err)
	}
}

// THE COPY CLEARS ITSELF once main's file, read cleanly from the main branch,
// is the same file (layout and comments aside); a different main, or one only
// the working tree holds, never clears it.
func TestTheCopyClearsItselfWhenMainSaysTheSame(t *testing.T) {
	keep := newCopyKeeper()
	keep.copies["acme/api"] = &factory.PipelineCopy{Repo: "acme/api", Text: copyText, At: time.Now()}
	other := mainRead(t, "pipeline:\n  issue: [write, proof]\n")
	if got, _ := WithFactoryCopy(other, nil, keep, "acme/api", ""); got.Copy == nil || keep.copies["acme/api"] == nil {
		t.Fatal("a different main cleared the copy")
	}
	tree := mainRead(t, copyText)
	tree.From = factory.FromWorkingTree
	if got, _ := WithFactoryCopy(tree, nil, keep, "acme/api", ""); got.Copy == nil || keep.copies["acme/api"] == nil {
		t.Fatal("a working tree cleared the copy")
	}
	merged := mainRead(t, "# merged by hand\npipeline:\n  issue:\n    - plan\n    - write\n    - proof\nrules: [\"keep the copy's rule\"]\n")
	got, err := WithFactoryCopy(merged, nil, keep, "acme/api", "")
	if err != nil || got.Copy != nil || got.From != factory.FromMainBranch || keep.copies["acme/api"] != nil || keep.version["acme/api"] != 1 {
		t.Fatalf("main says the same, and the copy stayed: %+v from %q (%v)", got.Copy, got.From, err)
	}
}

// A COPY THAT CANNOT BE READ REFUSES THE READ like the factory's rules do: a
// job must never quietly run main's steps when a person replaced them.
func TestAnUnreadableCopyRefusesLikeTheFactorysRules(t *testing.T) {
	keep := newCopyKeeper()
	keep.broken = errors.New("disk")
	_, err := WithFactoryCopy(mainRead(t, ""), nil, keep, "acme/api", "")
	if !errors.Is(err, ErrHouseRulesUnread) {
		t.Fatalf("err %v", err)
	}
}

func TestSameFileComparesWhatTheFileSays(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{copyText, copyText, true},
		{copyText, "# a comment\n" + copyText, true},
		{copyText, "pipeline:\n  issue: [plan, proof]\n", false},
		{"", "", false},
		{"pipeline: [\n", "pipeline: [\n  ", false},
	} {
		if SameFile(c.a, c.b) != c.same {
			t.Errorf("SameFile(%q, %q) is %v", c.a, c.b, !c.same)
		}
	}
}
