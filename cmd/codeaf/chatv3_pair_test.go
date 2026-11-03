package main

// The chat's pairing door (chatv3_pair.go). The plain refusal is said the
// chat's way: the terminal sentence names its --replace flag, and here the
// same fact points at the /pair keystroke the person is already sitting at.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/teams"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair"
)

func TestChatJoinRefusalNamesTheInChatReplace(t *testing.T) {
	err := chatJoinRefusal(pair.ErrDifferentChats)
	for _, want := range []string{"/pair <code> --replace", "already has chats of its own"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal lacks %q: %s", want, err)
		}
	}
	// Any other failure is carried as it is.
	other := errors.New("the relay is down")
	if chatJoinRefusal(other) != other {
		t.Fatal("an unrelated error was rewritten")
	}
	if chatJoinRefusal(nil) != nil {
		t.Fatal("a nil error was rewritten")
	}
}

// The replacing errand reaches pair.Join with replace set: the interface keeps
// the two doors from disagreeing about what a replace is.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestChatPairDoorImplementsBothJoins(t *testing.T) {
	var _ interface {
		Join(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error)
		JoinReplacing(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error)
		HasOwnChats() bool
	} = chatPairDoor{}
}

// F3 includes teams in terminal replacement confirmation even without an identity or chats.
func TestTerminalPairReplaceWithOnlyTeamsAsksBeforeRelay(t *testing.T) {
	for _, link := range []bool{false, true} {
		t.Run(fmt.Sprintf("link=%v", link), func(t *testing.T) {
			dir := t.TempDir()
			if err := teams.Update(dir, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "local", Name: "harbor"}}; return nil }); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(teams.Path(dir))
			out := &bytes.Buffer{}
			d := newPairDoor(strings.NewReader("n\n"), out)
			d.asksName = true
			d.joining = func(replace bool) pair.Joining { return pair.Joining{Home: dir, Replace: replace} }
			d.mailbox = func(string) (pair.Mailbox, error) {
				t.Fatal("declined replacement reached relay")
				return pair.Mailbox{}, nil
			}
			var err error
			if link {
				err = d.askToJoin(t.Context(), "", true)
			} else {
				err = d.join(t.Context(), "", "71-869-571", true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), pair.ReplaceAskLine) {
				t.Fatal("terminal omitted teams confirmation", out.String())
			}
			after, _ := os.ReadFile(teams.Path(dir))
			if !bytes.Equal(before, after) {
				t.Fatal("declining changed teams")
			}
		})
	}
}

// F1 uses the same quiet pairing gate for the production session and surface callbacks.
func TestTeamPairedFollowsPairingAndSyncOff(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	t.Setenv(syncsetup.URLVar, "")
	if teamPaired() {
		t.Fatal("missing identity was paired")
	}
	if _, err := identity.EnsureSolo(root); err != nil {
		t.Fatal(err)
	}
	if teamPaired() {
		t.Fatal("solo identity was paired")
	}
	if err := identity.EndSolo(root); err != nil {
		t.Fatal(err)
	}
	if !teamPaired() {
		t.Fatal("shared identity was unpaired")
	}
	t.Setenv(syncsetup.URLVar, "off")
	if teamPaired() {
		t.Fatal("sync off kept sharing effective")
	}
}

// R3/F3 preserves non-terminal --replace for existing chats and teams without reading a silent pipe.
func TestTerminalPairReplaceWithoutTerminalNeverWaitsForInput(t *testing.T) {
	for _, ownChats := range []bool{false, true} {
		for _, link := range []bool{false, true} {
			t.Run(fmt.Sprintf("ownChats=%v/link=%v", ownChats, link), func(t *testing.T) {
				dir := t.TempDir()
				if ownChats {
					if _, err := identity.Ensure(dir); err != nil {
						t.Fatal(err)
					}
				}
				if err := teams.Update(dir, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "local", Name: "harbor"}}; return nil }); err != nil {
					t.Fatal(err)
				}
				in, writer := io.Pipe()
				defer in.Close()
				defer writer.Close()
				out := &pairScreenText{}
				d := newPairDoor(in, out)
				d.joining = func(replace bool) pair.Joining { return pair.Joining{Home: dir, Replace: replace} }
				reached := errors.New("reached pairing without input")
				d.mailbox = func(string) (pair.Mailbox, error) { return pair.Mailbox{}, reached }
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					if link {
						done <- d.askToJoin(ctx, "", true)
					} else {
						done <- d.join(ctx, "", "71-869-571", true)
					}
				}()
				select {
				case err := <-done:
					if !errors.Is(err, reached) {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("non-terminal replacement waited for input")
				}
				if strings.Contains(out.String(), pair.ReplaceAskLine) {
					t.Fatal("non-terminal replacement asked a question")
				}
			})
		}
	}
}

// R3/F3 completes non-terminal teams-only replacement and prints the preserved team's actual path.
func TestTerminalTeamsOnlyReplacePrintsAsideReceiptWithoutInput(t *testing.T) {
	rig := newPairRig(t)
	if err := teams.Update(rig.homeB, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "local", Name: "harbor"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(teams.Path(rig.homeB))
	if err != nil {
		t.Fatal(err)
	}
	shown := rig.show(t)
	code := shown.code(t)
	in, writer := io.Pipe()
	defer in.Close()
	defer writer.Close()
	out := &pairScreenText{}
	d := rig.door(rig.homeB, in, out)
	joining := d.joining
	d.joining = func(replace bool) pair.Joining {
		j := joining(replace)
		j.TeamsPaired = func() bool { return true }
		return j
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.run(ctx, []string{code, "--replace"}) }()
	shown.out.waitFor(t, `y / n `)
	if _, err := io.WriteString(shown.typed, "yes\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("teams-only replacement waited for terminal input")
	}
	if err := shown.end(t); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(teams.Path(rig.homeB) + ".unreadable-*")
	if err != nil || len(paths) != 1 {
		t.Fatal("replacement did not set teams aside", paths, err, out.String())
	}
	if !strings.Contains(out.String(), pair.TeamsAsideLine(paths[0])) || strings.Contains(out.String(), pair.ReplaceAskLine) {
		t.Fatal("missing non-terminal receipt", out.String())
	}
	got, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(got, before) {
		t.Fatal("receipt's file did not preserve teams", string(got), err)
	}
}
