package tui3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// namingAgent is an agent that can suggest a team's name: the answer it is
// loaded with, and the titles it was asked about.
type namingAgent struct {
	Agent
	mu     sync.Mutex
	name   string
	err    error
	calls  int
	titles []string
}

func (n *namingAgent) NameTeam(_ context.Context, titles []string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	n.titles = append([]string(nil), titles...)
	return n.name, n.err
}

// namerDoors runs what cmd asks for and folds every door's answer back in,
// leaving the five-second wait unrun: it would only end a wait these tests
// end themselves.
func namerDoors(t *testing.T, a *app, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msgs := make(chan tea.Msg, 8)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		go func() {
			defer func() { _ = recover() }()
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, inner := range batch {
					if inner != nil {
						run(inner)
					}
				}
				return
			}
			msgs <- msg
		}()
	}
	run(cmd)
	deadline := time.After(time.Second)
	for {
		select {
		case msg := <-msgs:
			if door, ok := msg.(doorMsg); ok {
				_ = a.doorSaid(door)
				return
			}
		case <-deadline:
			t.Fatal("the name was never asked for")
		}
	}
}

// namingApp is the strip's three conversations on the wall with an agent that
// names, and the two picked that sit in different folders.
func namingApp(t *testing.T) (*app, *namingAgent, []wallTile) {
	t.Helper()
	a, _, _ := tabApp(t)
	namer := &namingAgent{Agent: a.agent}
	a.agent = namer
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	var apart []int
	folders := map[string]bool{}
	for i, tile := range tiles {
		if !folders[tile.tab.where] {
			folders[tile.tab.where] = true
			apart = append(apart, i)
		}
	}
	if len(apart) < 2 {
		t.Fatalf("the fixture's conversations share one folder: %+v", tiles)
	}
	for _, i := range apart[:2] {
		a.wallToggle(tiles, i)
	}
	return a, namer, tiles
}

// A TEAM IS NAMED ONCE, FROM WHAT ITS CONVERSATIONS ARE. The pleasant word is
// there at once, `naming…` beside it; the model's name replaces it when it
// comes, and nothing asks again.
func TestTeamNameSuggestionReplacesTheWord(t *testing.T) {
	a, namer, tiles := namingApp(t)
	namer.name = "Parser Port"
	cmd := a.wallStartNaming(tiles)
	curated := a.wall.name
	if curated == "" || !a.wall.nameAsking || cmd == nil {
		t.Fatalf("the card opened with %q, asking=%v", curated, a.wall.nameAsking)
	}
	if frame := wallPlainFrame(a.wallFrame(a.width, a.height)); !strings.Contains(frame, curated+"▌  naming…") {
		t.Fatalf("the card does not say it is naming:\n%s", frame)
	}
	namerDoors(t, a, cmd)
	if a.wall.name != "Parser Port" || a.wall.nameAsking || !a.wall.nameFresh {
		t.Fatalf("after the answer: name %q asking %v", a.wall.name, a.wall.nameAsking)
	}
	if namer.calls != 1 || len(namer.titles) != 2 {
		t.Fatalf("asked %d times about %q", namer.calls, namer.titles)
	}
	if frame := wallPlainFrame(a.wallFrame(a.width, a.height)); strings.Contains(frame, "naming…") {
		t.Fatal("the card still says naming")
	}
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if names := a.teamNames(); len(names) != 1 || names[0] != "Parser Port" {
		t.Fatalf("made %v", names)
	}
	if namer.calls != 1 {
		t.Fatalf("the name was asked for again: %d calls", namer.calls)
	}
}

// TYPING WINS. A name the person started is theirs, and the suggestion that
// arrives after it is dropped.
func TestTeamNameTypingWinsOverTheSuggestion(t *testing.T) {
	a, namer, tiles := namingApp(t)
	namer.name = "parser port"
	cmd := a.wallStartNaming(tiles)
	a.wallKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	namerDoors(t, a, cmd)
	if a.wall.name != "q" {
		t.Fatalf("the suggestion overwrote what was typed: %q", a.wall.name)
	}
	if frame := wallPlainFrame(a.wallFrame(a.width, a.height)); strings.Contains(frame, "naming…") {
		t.Fatal("the card says naming after the person typed")
	}
}

// A FAILURE OR A WAIT PAST FIVE SECONDS KEEPS THE WORD, and an answer that
// comes after the wait is not used.
func TestTeamNameFailureOrTimeoutKeepsTheWord(t *testing.T) {
	a, namer, tiles := namingApp(t)
	namer.err = errors.New("no provider answered")
	cmd := a.wallStartNaming(tiles)
	curated := a.wall.name
	namerDoors(t, a, cmd)
	if a.wall.name != curated || a.wall.nameAsking {
		t.Fatalf("a failure changed the word to %q (asking %v)", a.wall.name, a.wall.nameAsking)
	}

	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	namer.err, namer.name = nil, "late name"
	cmd = a.wallStartNaming(tiles)
	curated = a.wall.name
	_, _ = a.Update(wallNameTimeMsg{gen: a.wall.nameGen})
	if a.wall.nameAsking {
		t.Fatal("the wait did not end")
	}
	namerDoors(t, a, cmd)
	if a.wall.name != curated {
		t.Fatalf("an answer after the wait replaced the word: %q", a.wall.name)
	}
}

// A SHARED FOLDER IS THE NAME, AND NOTHING IS ASKED.
func TestTeamNameSharedFolderAsksNothing(t *testing.T) {
	a, _, _ := tabApp(t)
	namer := &namingAgent{Agent: a.agent, name: "unused"}
	a.agent = namer
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	var where string
	picked := 0
	for i, tile := range tiles {
		if tile.tab.where == "/tmp/lab" {
			a.wallToggle(tiles, i)
			where = tile.tab.where
			picked++
		}
	}
	if picked < 2 {
		t.Fatalf("the fixture has %d conversations in one folder", picked)
	}
	cmd := a.wallStartNaming(tiles)
	if cmd != nil || a.wall.nameAsking || namer.calls != 0 {
		t.Fatalf("a shared folder asked for a name: asking %v, %d calls", a.wall.nameAsking, namer.calls)
	}
	if a.wall.name != "lab" {
		t.Fatalf("the card offers %q for %s", a.wall.name, where)
	}
}
