package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// These tests cover explicit navigation from the overview to a conversation.

// teamsOpenLab is the teams page lab with orbit given a manager this window is
// NOT holding: a transcript in a folder of its own, which is not the window's
// (`/tmp/lab`). made says whether the transcript is on the disk. The open door
// is a fake that records what it was asked and answers with err when set.
type teamsOpenLab struct {
	a             *app
	harbor, orbit string
	file, where   string
	key           string
	asked         []string
	err           error
}

func newTeamsOpenLab(t *testing.T, made bool) *teamsOpenLab {
	t.Helper()
	l := &teamsOpenLab{}
	l.a, l.harbor, l.orbit = teamsPlaceLabIDs(t)
	l.where = t.TempDir()
	l.file = filepath.Join(l.where, "manager.jsonl")
	if made {
		if err := os.WriteFile(l.file, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l.key = l.a.convKey(l.file)
	l.a.open = func(where, file string) (Conversation, error) {
		l.asked = append(l.asked, where+" "+file)
		if l.err != nil {
			return Conversation{}, l.err
		}
		return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: where, SessionFile: file}, nil
	}
	if err := l.a.teamEdit(func(f *teamstore.File) error {
		if err := f.AddMember(l.orbit, teamstore.Member{Key: l.key, File: l.file, Where: l.where, Word: "run orbit", Handle: "boss"}); err != nil {
			return err
		}
		return f.SetManager(l.orbit, l.key)
	}); err != nil {
		t.Fatal(err)
	}
	return l
}

// selectOrbit chooses orbit on the rail, as a press does, and runs what that
// asked for.
func (l *teamsOpenLab) selectOrbit(t *testing.T) {
	t.Helper()
	drive(t, l.a, runCmd(l.a.teamsSelect(l.orbit))...)
}

func TestTeamsOverviewSelectionDoesNotOpenOrFocusManager(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	front := l.a.frontTabKey()
	l.selectOrbit(t)
	if len(l.asked) != 0 || l.a.frontTabKey() != front || !l.a.at(pageTeams) {
		t.Fatalf("team selection changed chat focus: calls %v front %q", l.asked, l.a.frontTabKey())
	}
	if text := teamsFrameText(l.a); !strings.Contains(text, "@boss") || !strings.Contains(text, "Recent interactions") {
		t.Fatal(text)
	}
}

func TestTeamsOverviewManagerClickOpensChatsWithOriginatingTeam(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.selectOrbit(t)
	drive(t, l.a, runCmd(l.a.teamsManagerGo(l.orbit))...)
	if len(l.asked) != 1 || l.a.frontTabKey() != l.key || l.a.pageShowing() || l.a.wall.activeID != l.orbit {
		t.Fatalf("manager click did not navigate explicitly: calls %v front %q team %q", l.asked, l.a.frontTabKey(), l.a.wall.activeID)
	}
}

func TestTeamsOverviewLateManagerDoesNotStealFocus(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	front := l.a.frontTabKey()
	l.selectOrbit(t)
	drive(t, l.a, tea.WindowSizeMsg{Width: 140, Height: 40})
	if l.a.frontTabKey() != front || len(l.asked) != 0 {
		t.Fatal("background update opened a manager")
	}
}

func TestTeamsOverviewManagerRefusalIsSaidInChats(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.err = errors.New("the engine said no")
	l.selectOrbit(t)
	drive(t, l.a, runCmd(l.a.teamsManagerGo(l.orbit))...)
	if l.a.at(pageTeams) || len(l.asked) != 1 {
		t.Fatal("explicit navigation did not ask the ordinary chat door")
	}
	if !strings.Contains(teamsFrameText(l.a), "the engine said no") {
		t.Fatal(teamsFrameText(l.a))
	}
}
