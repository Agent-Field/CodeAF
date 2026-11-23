package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair"
)

type l4PairDoor struct{ Pairing }

func (l4PairDoor) PairCommand() string { return pair.Command("http://127.0.0.1:8901", "") }

// L4-H1a: Home instructions render the selected service through the shared command builder.
func TestL4HomePairCommand(t *testing.T) {
	a := &app{pairing: l4PairDoor{}}
	lines := a.addMachineHow(bandContext{width: 160})
	if !strings.Contains(strings.Join(lines, "\n"), "codeaf pair --via http://127.0.0.1:8901") {
		t.Fatal(lines)
	}
}

// L4-H3d: an outage already said by either producer suppresses the second sentence.
func TestL4OneSyncOutageNotice(t *testing.T) {
	a := &app{file: "chat"}
	line := "the sync service at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back"
	if !a.acceptSyncNotice(line) || a.acceptSyncNotice(line) {
		t.Fatal("outage repeated")
	}
	b := &app{file: "chat", syncOutages: map[string]bool{"chat": true}}
	if b.acceptSyncNotice(line) {
		t.Fatal("the renewal outage was said again")
	}
}

// L4-H3b: an old healthy renewal reading cannot recover a newer upload outage.
func TestL4SyncRecoveryRequiresNewSuccess(t *testing.T) {
	source := &renewalAgent{fakeAgent: &fakeAgent{model: "m"}, held: true}
	a := newTestApp(source)
	a.file = "chat"
	a.syncService = "http://127.0.0.1:8901"
	now := time.Now()
	a.clock = func() time.Time { return now }
	a.note("the sync service at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back")
	a.takeRenewalNotice()
	if !a.syncOutages[a.file] {
		t.Fatal("an unchanged healthy renewal reading recovered a newer outage")
	}
	source.since = now.Add(-11 * time.Second)
	a.takeRenewalNotice()
	source.since = time.Time{}
	a.takeRenewalNotice()
	if a.syncOutages[a.file] {
		t.Fatal("a completed successful renewal did not recover the outage")
	}
	a.note("the sync service at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back")
	a.takeRenewalNotice()
	if !a.syncOutages[a.file] {
		t.Fatal("evidence from the previous outage recovered the next one")
	}
	source.since = now
	a.takeRenewalNotice()
	source.since = time.Time{}
	a.takeRenewalNotice()
	if a.syncOutages[a.file] {
		t.Fatal("a brief renewal failure and success did not recover the engine outage")
	}
}
