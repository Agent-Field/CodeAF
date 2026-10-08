package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// cloneFake is the verb fake with a floor that says where repositories are
// checked out, and, when cloner is true, a Clone door that records the folder
// so the next Load sees it.
type cloneFake struct {
	*factoryFake
	have   map[string]string
	cloner bool
}

func newCloneFake(cloner bool, have map[string]string) *cloneFake {
	c := &cloneFake{factoryFake: &factoryFake{}, have: have, cloner: cloner}
	c.shape = func(s *factory.Snapshot) {
		c.mu.Lock()
		defer c.mu.Unlock()
		s.Checkouts = map[string]string{}
		for k, v := range c.have {
			s.Checkouts[k] = v
		}
	}
	return c
}

func (c *cloneFake) seam() factory.Seam {
	s := c.factoryFake.seam()
	if c.cloner {
		s.Clone = func(ctx context.Context, repo string) (string, error) {
			err := c.rec("Clone", repo)
			if err == nil {
				c.mu.Lock()
				c.have[repo] = "/clones/" + repo
				c.mu.Unlock()
			}
			return "/clones/" + repo, err
		}
	}
	return s
}

func cloneLab(t *testing.T, c *cloneFake) *app {
	t.Helper()
	a := placeApp(t)
	a.factory = c.seam()
	a.width, a.height = 150, 44
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) || !a.fp.loaded {
		t.Fatal("the factory place did not open over the clone fake")
	}
	c.said()
	return a
}

// A RUN NEVER STARTS WITHOUT A CHECKOUT. `r` on an item whose repository has
// none asks to clone it and launches nothing; `y` clones, then launches.
func TestFactoryRunWithoutACheckoutAsksToCloneThenLaunches(t *testing.T) {
	c := newCloneFake(true, map[string]string{})
	a := cloneLab(t, c)
	factoryOn(t, a, 8)
	drive(t, a, key("r"))
	if got := c.said(); len(got) != 0 {
		t.Fatalf("r with no checkout asked %v before the question was answered", got)
	}
	if a.fp.act.clone == nil {
		t.Fatal("r with no checkout put no question up")
	}
	// Beside the peek the question breaks after the repository; on a wide
	// row it is one line.
	text := factoryFrameText(a)
	for _, want := range []string{"codeaf is not checked out on this machine", "clone it into ~/.codeaf/v3/factory/repos? [y] clone · [n] not now"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the clone question is missing %q:\n%s", want, text)
		}
	}
	if rows := a.factoryCloneRows(200); len(rows) != 1 || !strings.Contains(ansi.Strip(rows[0]), "codeaf is not checked out on this machine · clone it into ~/.codeaf/v3/factory/repos? [y] clone · [n] not now") {
		t.Fatalf("at 200 the question is %q", rows)
	}
	// Every other key is the question's.
	drive(t, a, key("d"))
	if got := c.said(); len(got) != 0 || a.fp.act.clone == nil {
		t.Fatalf("a stray key under the question asked %v", got)
	}
	drive(t, a, key("y"))
	if got := strings.Join(c.said(), " "); got != "Clone(agentfield/codeaf) Launch(8)" {
		t.Fatalf("y asked %q, want the clone and then the launch", got)
	}
	if a.fp.act.clone != nil || a.fp.act.doing != "" {
		t.Fatalf("after y the question or the spinner still stands: %+v %q", a.fp.act.clone, a.fp.act.doing)
	}
}

// `n` LAUNCHES NOTHING and says how to go on.
func TestFactoryRunWithoutACheckoutNoSaysNotRun(t *testing.T) {
	c := newCloneFake(true, map[string]string{})
	a := cloneLab(t, c)
	factoryOn(t, a, 8)
	drive(t, a, key("r"), key("n"))
	if got := c.said(); len(got) != 0 {
		t.Fatalf("n asked %v", got)
	}
	if want := "not run · clone codeaf first, or tell codeaf where it is in the repos list"; a.pageMsg != want {
		t.Fatalf("n said %q, want %q", a.pageMsg, want)
	}
}

// WITH A CHECKOUT `r` LAUNCHES AT ONCE, and without a Clone door a missing
// checkout is refused in words, with nothing asked.
func TestFactoryRunWithACheckoutLaunchesAndNoDoorRefuses(t *testing.T) {
	c := newCloneFake(true, map[string]string{"agentfield/codeaf": "/work/codeaf"})
	a := cloneLab(t, c)
	factoryOn(t, a, 8)
	drive(t, a, key("r"))
	if got := strings.Join(c.said(), " "); got != "Launch(8)" || a.fp.act.clone != nil {
		t.Fatalf("r with a checkout asked %q (question %v)", got, a.fp.act.clone != nil)
	}

	d := newCloneFake(false, map[string]string{})
	b := cloneLab(t, d)
	factoryOn(t, b, 8)
	drive(t, b, key("r"))
	if got := d.said(); len(got) != 0 || b.fp.act.clone != nil {
		t.Fatalf("r with no door asked %v (question %v)", got, b.fp.act.clone != nil)
	}
	if want := "not run · codeaf does not know where codeaf is checked out · open it from that folder once"; b.pageMsg != want {
		t.Fatalf("no door said %q, want %q", b.pageMsg, want)
	}
}

// `L` OVER MARKS ON TWO REPOSITORIES WITHOUT A CHECKOUT ASKS ONCE PER
// REPOSITORY, IN TURN, and launches every marked item after the last clone.
func TestFactoryLaunchMarkedAsksPerMissingRepo(t *testing.T) {
	c := newCloneFake(true, map[string]string{})
	a := cloneLab(t, c)
	factoryOn(t, a, 7)
	drive(t, a, key(" "))
	factoryOn(t, a, 8)
	drive(t, a, key(" "))
	drive(t, a, key("L"), key("y"))
	first := a.fp.act.clone
	if first == nil || len(c.said()) != 0 {
		t.Fatal("L's yes over repositories with no checkout did not ask to clone first")
	}
	drive(t, a, key("y"))
	second := a.fp.act.clone
	if second == nil || second.repo == first.repo {
		t.Fatalf("the second repository was not asked about: %+v", second)
	}
	drive(t, a, key("y"))
	got := strings.Join(c.said(), " ")
	if !strings.HasPrefix(got, "Clone(") || strings.Count(got, "Clone(") != 2 || !strings.Contains(got, "Launch(7)") || !strings.Contains(got, "Launch(8)") {
		t.Fatalf("L asked %q, want two clones then both launches", got)
	}
	if a.pageMsg != "launched 2" {
		t.Fatalf("L said %q", a.pageMsg)
	}
}

// THE PICKER'S `here` COLUMN says `clone on first run` for a repository with
// no checkout when the seam can clone, and its last line says so; without the
// door the column is blank and the old sentence stands.
func TestFactoryPickerSaysCloneOnFirstRun(t *testing.T) {
	f := newPickerFake()
	a := factorySettingsLab(t, f, 150)
	clone := a.factory
	clone.Clone = func(ctx context.Context, repo string) (string, error) { return "", nil }
	a.factory = clone
	drive(t, a, key("R"))
	body := strings.Join(factoryExactBody(t, a, 150, 30), "\n")
	for _, want := range []string{"here", factoryPickCloneWords} {
		if !strings.Contains(body, want) {
			t.Fatalf("the picker is missing %q:\n%s", want, body)
		}
	}
	for _, r := range a.fp.pick.rows {
		if r.dir == "" {
			if foot := a.factoryPickFoot(r, 150); !strings.Contains(foot, "not checked out here · it is cloned on the first run") {
				t.Fatalf("the last line for %s is %q", r.full, foot)
			}
			break
		}
	}

	b := factorySettingsLab(t, newPickerFake(), 150)
	drive(t, b, key("R"))
	body = strings.Join(factoryExactBody(t, b, 150, 30), "\n")
	if strings.Contains(body, factoryPickCloneWords) {
		t.Fatalf("a seam with no Clone door offers a clone:\n%s", body)
	}
}
