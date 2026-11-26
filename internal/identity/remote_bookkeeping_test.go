package identity

import (
	"os"
	"path/filepath"
	"testing"
)

// The validation contract for remote control's bookkeeping in a home.
//
// C2: whatever remote control writes into a home (the permit book, the day's
// byte count, the live door's sockets and pid files, the copy of another
// computer's chat) never makes the home count as having local work, so it
// never blocks a join and never makes a fresh home look used. The writers
// themselves are held to this in their own packages (internal/pair for the
// permit book, internal/livedoor for the byte count); this table holds the
// judge to the names, including the temporary files each writer replaces.

// remoteHome is a home that was only started, with one change applied, and
// without the memory graph: Joinable reads that graph as a real start makes it,
// and the fixture's hand-made one is a different shape, so a test of what else
// the home holds judges the home without it.
func remoteHome(t *testing.T, change func(home string)) string {
	t.Helper()
	return startedHome(t, func(home string) {
		for _, graph := range []string{"graph.db", "graph.db-wal", "graph.db-shm"} {
			if err := os.Remove(filepath.Join(home, graph)); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		if change != nil {
			change(home)
		}
	})
}

func TestRemoteControlBookkeepingNeverCountsAsLocalWork(t *testing.T) {
	// The control: with nothing added, the home can join, so a refusal below is
	// the added file's doing and not the fixture's.
	if ok, err := Joinable(remoteHome(t, nil)); err != nil || !ok {
		t.Fatalf("the started home cannot join before anything is added: joinable=%v err=%v", ok, err)
	}
	for _, name := range []string{
		"v3/remote/permits.json",
		"v3/remote/permits.json.new",
		"v3/remote/budget.json",
		"v3/remote/.budget-123456.tmp",
		"v3/live/4242.json",
		"v3/live/4242.sock",
		"v3/live/.live-123456.tmp",
		"v3/viewed/work-laptop/01CHAT.json",
	} {
		t.Run(name, func(t *testing.T) {
			home := remoteHome(t, func(home string) {
				put(t, filepath.Join(home, name), `{"v":1}`)
			})
			if ok, err := Joinable(home); err != nil || !ok {
				t.Fatalf("%s blocked a join: joinable=%v err=%v", name, ok, err)
			}
			// Pristine keeps the graph, which a real start leaves and the fixture
			// models, so a fresh home is judged as it really is.
			fresh := startedHome(t, func(home string) {
				put(t, filepath.Join(home, name), `{"v":1}`)
			})
			if ok, err := Pristine(fresh); err != nil || !ok {
				t.Fatalf("%s made a home that was only started look used: pristine=%v err=%v", name, ok, err)
			}
		})
	}
}

// Only the bookkeeping is let through. The remote folder also holds a
// computer's device key and whatever a later build puts there, and an entry
// this judge does not name is use, as everywhere else in a home.
func TestTheRemoteFolderStillProtectsWhatIsNotBookkeeping(t *testing.T) {
	for _, name := range []string{
		"v3/remote/device.key",
		"v3/remote/notes.txt",
		"v3/remote/permits.json.bak",
	} {
		t.Run(name, func(t *testing.T) {
			home := remoteHome(t, func(home string) {
				put(t, filepath.Join(home, name), "the person's")
			})
			if ok, err := Joinable(home); err != nil || ok {
				t.Fatalf("%s was let through as bookkeeping: joinable=%v err=%v", name, ok, err)
			}
		})
	}
}
