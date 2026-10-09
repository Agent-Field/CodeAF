package tui3

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// shapeLab is the fake floor with a Shape door that counts its asks by item
// and answers line and err, and a Load that counts its reads.
type shapeLab struct {
	mu    sync.Mutex
	asked map[int]int
	reads int
	line  string
	err   error
}

func (s *shapeLab) hang(f *factoryFake, a *app) {
	seam := f.seam()
	load := seam.Load
	seam.Load = func() (factory.Snapshot, error) {
		s.mu.Lock()
		s.reads++
		s.mu.Unlock()
		return load()
	}
	seam.Shape = func(_ context.Context, id int) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.asked == nil {
			s.asked = map[int]int{}
		}
		s.asked[id]++
		return s.line, s.err
	}
	a.factory = seam
}

func (s *shapeLab) count(id int) (asked, reads int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[id], s.reads
}

// THE MANAGER READS THE ISSUE WHEN ITS PAGE OPENS: the first open of an item
// the manager never shaped asks the Shape door once, the manager is thinking
// (and the frame clock runs) while the door is out, and on its answer the
// thinking ends and the floor is read so the stages redraw. A second open in
// the same window asks nothing.
func TestFactoryOpenItemAsksTheShapeDoorOnce(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	lab := &shapeLab{line: "manager set review: read it for security"}
	lab.hang(f, a)
	factoryOn(t, a, 8)
	cmd, ok := a.factoryOpenItem()
	if !ok || cmd == nil {
		t.Fatalf("opening item 8 asked no shaping (opened %v)", ok)
	}
	if !a.fp.shaping[8] || !a.factorySpinning() {
		t.Fatalf("the manager is not thinking while the door is out: shaping %v, spinning %v", a.fp.shaping, a.factorySpinning())
	}
	_, before := lab.count(8)
	drive(t, a, runCmd(cmd)...)
	asked, reads := lab.count(8)
	if asked != 1 || reads <= before {
		t.Fatalf("the door was asked %d times and the floor read %d times after it (before %d)", asked, reads, before)
	}
	if a.fp.shaping[8] || a.factorySpinning() {
		t.Fatalf("the manager is still thinking after the door answered: %v", a.fp.shaping)
	}
	if a.pageMsg != "" {
		t.Fatalf("a shaping that worked put %q on the note line", a.pageMsg)
	}
	drive(t, a, key("esc"))
	drive(t, a, key("enter"))
	if asked, _ := lab.count(8); asked != 1 || !a.fp.open {
		t.Fatalf("a second open asked the door again (%d) or did not open (%v)", asked, a.fp.open)
	}
}

// WITHOUT THE DOOR NOTHING HAPPENS: the page opens, nothing is thinking, and
// no command is handed back for a shaping.
func TestFactoryOpenItemWithoutAShapeDoorShapesNothing(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)
	cmd, ok := a.factoryOpenItem()
	if !ok || cmd != nil || len(a.fp.shaping) != 0 {
		t.Fatalf("a seam with no Shape door shaped: opened %v, cmd %v, shaping %v", ok, cmd != nil, a.fp.shaping)
	}
}

// AN ITEM THE FLOOR SHOWS AS RUN IS NEVER ASKED FOR: item 1 has a stream.
func TestFactoryOpenItemThatRanAsksNothing(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	lab := &shapeLab{}
	lab.hang(f, a)
	factoryOn(t, a, 1)
	if cmd, _ := a.factoryOpenItem(); cmd != nil {
		t.Fatal("an item with a run behind it was given a shaping turn")
	}
}

// THE NOTE LINE SAYS THE DOOR'S LINE ONLY ON FAILURE, and `already shaped` is
// no failure and says nothing.
func TestFactoryOpenItemSaysTheLineOnlyOnFailure(t *testing.T) {
	for _, c := range []struct {
		line string
		err  error
		want string
	}{
		{line: "the manager did not answer · the recipe stands", err: errors.New("the manager did not answer · the recipe stands"), want: "the manager did not answer · the recipe stands"},
		{line: factory.ShapeAlready, want: ""},
		{line: "the recipe stands", want: ""},
	} {
		f := &factoryFake{}
		a := factoryVerbLab(t, f)
		lab := &shapeLab{line: c.line, err: c.err}
		lab.hang(f, a)
		factoryOn(t, a, 8)
		drive(t, a, key("enter"))
		if a.pageMsg != c.want {
			t.Errorf("door answered %q, %v: note %q, want %q", c.line, c.err, a.pageMsg, c.want)
		}
	}
}
