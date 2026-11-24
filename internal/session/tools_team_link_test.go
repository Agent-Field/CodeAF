package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// team_link IS THE MANAGER'S, and only where the process can record a link:
// with no door it is off the belt; with one, a file the crew wrote is handed
// to the door by its full path, a missing file is refused, and a member never
// holds the verb.
func TestTeamLinkIsTheManagersWhereTheProcessCanRecordIt(t *testing.T) {
	fixture := newTeamFixture(t, true)
	SetTeamLinker(nil)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	if holds(manager, teamLinkToolName) {
		t.Fatal("team_link is on the belt in a process that cannot record a link")
	}

	var mu sync.Mutex
	var got []TeamLink
	SetTeamLinker(func(link TeamLink) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, link)
		if strings.Contains(link.Title, "refused") {
			return "", errors.New("the crews file is read-only")
		}
		return "i7", nil
	})
	t.Cleanup(func() { SetTeamLinker(nil) })
	manager.teamBoundary()
	if !holds(manager, teamLinkToolName) {
		t.Fatal("the manager was not offered team_link")
	}
	member := teamAgent(t, fixture, fixture.web, nil, nil)
	member.teamBoundary()
	if holds(member, teamLinkToolName) {
		t.Fatal("a member was offered team_link")
	}

	workspace := manager.config.Workspace
	if err := os.WriteFile(filepath.Join(workspace, "launch-post-final.md"), []byte("# Launch"), 0o600); err != nil {
		t.Fatal(err)
	}
	said, failed, err := manager.teamLinkTool(context.Background(), json.RawMessage(`{"ref":"launch-post-final.md"}`))
	if err != nil || failed || !strings.Contains(said, "i7") {
		t.Fatalf("linking a file the crew wrote: %q failed=%v err=%v", said, failed, err)
	}
	mu.Lock()
	link := got[0]
	mu.Unlock()
	if link.Team != fixture.teamID || link.Kind != "file" || link.Ref != filepath.Join(workspace, "launch-post-final.md") || link.Title != "launch-post-final.md" {
		t.Fatalf("the door was handed %+v", link)
	}
	if said, failed, _ := manager.teamLinkTool(context.Background(), json.RawMessage(`{"ref":"nowhere.md"}`)); !failed || !strings.Contains(said, "There is no file") {
		t.Fatalf("a missing file was linked: %q", said)
	}
	if said, failed, _ := manager.teamLinkTool(context.Background(), json.RawMessage(`{"ref":"#2731","kind":"pr","title":"refused"}`)); !failed || !strings.Contains(said, "read-only") {
		t.Fatalf("a door's refusal was not said: %q", said)
	}
}
