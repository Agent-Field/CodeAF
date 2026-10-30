package syncsetup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type countedTeamDirectory struct {
	directory.Client
	calls    int
	err      error
	deadline time.Duration
}

func (d *countedTeamDirectory) List(ctx context.Context) (directory.Listing, error) {
	d.calls++
	if at, ok := ctx.Deadline(); ok {
		d.deadline = time.Until(at)
	}
	return directory.Listing{}, d.err
}

// A failed lookup is cached too, so an outage cannot turn a multi-member send
// into a row of directory requests. Missing files are never remote evidence.
func TestTeamOwnershipUsesOneShortCachedReadingAndFailureAllowsLocalWork(t *testing.T) {
	root := t.TempDir()
	id, err := identity.Ensure(root)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.LoadDevice(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := &countedTeamDirectory{err: errors.New("offline")}
	s := &Sync{Home: root, Identity: id, Device: dev, Dir: dir}
	members := []teams.Member{{Key: "local-a", SyncID: "cell:a"}, {Key: "local-b", SyncID: "cell:b"}, {Key: teams.RemoteKey("c")}}
	if names, err := s.TeamComputers(t.Context(), members[:2]); err != nil || len(names) != 0 || dir.calls != 0 {
		t.Fatal("local team added a request", names, err, dir.calls)
	}
	for range 2 {
		names, err := s.TeamComputers(t.Context(), members)
		if err != nil || names["local-a"] != "" || names["local-b"] != "" || names[teams.RemoteKey("c")] != "the other computer" {
			t.Fatal(names, err)
		}
	}
	if dir.calls != 1 || dir.deadline <= 0 || dir.deadline > teamComputerTimeout {
		t.Fatal("unbounded or repeated lookup", dir.calls, dir.deadline)
	}
	for _, key := range []string{"claim", "local-missing"} {
		if name, err := s.TeamComputer(t.Context(), teams.Member{Key: key}); err != nil || name != "" {
			t.Fatal(name, err)
		}
	}
	if dir.calls != 1 {
		t.Fatal("pathless claim did directory work", dir.calls)
	}
}

func TestTrafficRosterFilterNeverRequestsTheDirectory(t *testing.T) {
	root := t.TempDir()
	id, err := identity.Ensure(root)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.LoadDevice(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := &countedTeamDirectory{err: errors.New("must not be called")}
	s := &Sync{Home: root, Identity: id, Device: dev, Dir: dir}
	peers, err := s.pairedTrafficDevices()
	if err != nil || peers != nil || dir.calls != 0 {
		t.Fatal("mirror added a request", peers, err, dir.calls)
	}
	p := &teamTraffic{teamKeys: teamKeys{root, root, dev.ID(), nil}, peers: s.pairedTrafficDevices}
	if err := teams.Update(root, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "alpha", Name: "alpha"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(map[string]string{"alpha/elsewhere": teamValue([]teams.Entry{{ID: "elsewhere/1", Text: "history"}})}); err != nil {
		t.Fatal(err)
	}
	lines, err := teams.ReadHistory(root, "alpha", 0)
	if err != nil || len(lines) != 1 || dir.calls != 0 {
		t.Fatal("fallback did not mirror other writers locally", lines, err, dir.calls)
	}
}
