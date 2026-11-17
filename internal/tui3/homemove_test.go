package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// The continue's line says what it is doing and how much has come through.
func TestMoveSaid(t *testing.T) {
	for _, c := range []struct {
		p    MoveProgress
		want string
	}{
		{MoveProgress{Step: "preparing"}, continuing},
		{MoveProgress{Step: "copying", Done: 412 << 20, Total: 1433 << 20}, "copying to this computer: 412 of 1,433 MB…"},
		{MoveProgress{Step: "copying", Done: 3 << 20}, "copying to this computer: 3 MB so far…"},
		{MoveProgress{Step: "copying", Done: 900 << 10, Total: 1000 << 10}, "copying to this computer: 900 of 1,000 KB…"},
		{MoveProgress{Step: "opening", Done: 5}, "opening it here…"},
	} {
		if got := moveSaid(c.p); got != c.want {
			t.Errorf("moveSaid(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
}

// movingTaker is a fakeTaker that says how far its continue has got.
type movingTaker struct {
	fakeTaker
	at      MoveProgress
	active  bool
	retried string
}

func (m *movingTaker) MoveProgress(string) (MoveProgress, bool) {
	if !m.active && m.at == (MoveProgress{}) {
		return MoveProgress{}, false
	}
	return m.at, m.active || m.at != (MoveProgress{})
}

func (m *movingTaker) RetryMove(cell string) bool {
	m.retried = cell
	m.at = MoveProgress{Step: "arriving", Done: 40 << 20, Total: 112 << 20}
	m.active = true
	return true
}

// Home's line follows a running continue, and only while home still shows it.
func TestHomeFollowsAMove(t *testing.T) {
	taker := &movingTaker{at: MoveProgress{Step: "copying", Done: 412 << 20, Total: 1433 << 20}}
	a := continueHome(t, taker, continueRows, 200)
	a.homeTaking, a.homeTakeSeq = "c-off", 7
	a.home.say(continuing, "")
	if next := a.followMove(homeMoveTickMsg{seq: 7}); next == nil {
		t.Fatal("a running continue stopped being followed")
	}
	if a.home.msg != "copying to this computer: 412 of 1,433 MB…" {
		t.Fatalf("home says %q", a.home.msg)
	}
	a.home.say("something else", "")
	_ = a.followMove(homeMoveTickMsg{seq: 7})
	if a.home.msg != "something else" {
		t.Fatalf("progress wrote over another line: %q", a.home.msg)
	}
	if next := a.followMove(homeMoveTickMsg{seq: 6}); next != nil {
		t.Fatal("an older continue's tick kept running")
	}
}

// A continue that stalled says so, and where the chat still is.
func TestAStalledContinueSaysWhy(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	got := a.continueFailure(homeTakenMsg{row: continueRows[0], err: fmt.Errorf("%w: handoff: fetch", ErrMoveStalled)})
	if !strings.Contains(got, "stopped: nothing came through for 2 minutes; the chat is still on host-a") {
		t.Fatalf("a stalled continue says %q", got)
	}
}

// Before a big folder the continue card says what moving it copies.
func TestContinueCardWarnsBeforeABigFolder(t *testing.T) {
	big := chatlist.Row{Cell: "c-big", Title: "Index", Device: "host-a", Status: chatlist.Off, DurableAgo: time.Hour, Folder: "CodeAF", Size: 1500 << 20}
	if got := continueReason(big); !strings.Contains(got, "this chat works in the CodeAF folder on host-a; moving it copies that folder too: 1.5 GB, which can take several minutes") {
		t.Fatalf("the card says %q", got)
	}
	small := big
	small.Size = 20 << 20
	if got := continueReason(small); strings.Contains(got, "copies that folder") {
		t.Fatalf("a small chat warned: %q", got)
	}
	unnamed := big
	unnamed.Folder = ""
	if got := continueReason(unnamed); !strings.Contains(got, "this chat works in a folder on host-a") {
		t.Fatalf("a chat with no folder name says %q", got)
	}
}

// Contract C2 and C4: the open chat's status line shows arriving workspace
// bytes (never 0 of 0), names a failed background fetch in plain words with a
// retry door, and clears when the files finish arriving.
func TestC2FilesArrivingStatusLineAppearsShowsBytesAndClears(t *testing.T) {
	for _, c := range []struct {
		p    MoveProgress
		want string
	}{
		{MoveProgress{Step: "arriving", Done: 0, Total: 0}, "files arriving"},
		{MoveProgress{Step: "arriving", Done: 0, Total: 112 << 20}, "files arriving"},
		{MoveProgress{Step: "arriving", Done: 100, Total: 200}, "files arriving"},
		{MoveProgress{Step: "arriving", Done: 40 << 20, Total: 112 << 20}, "files arriving · 40 of 112 MB"},
		{MoveProgress{Step: "arriving", Done: 40 << 20, Total: 0}, "files arriving · 40 MB"},
		{MoveProgress{Step: "failed", Err: "sync could not be reached"}, "files couldn't arrive: sync could not be reached · retry"},
		{MoveProgress{Step: "done", Done: 112 << 20, Total: 112 << 20}, ""},
	} {
		got := filesArrivingSaid(c.p)
		if got != c.want {
			t.Errorf("filesArrivingSaid(%+v) = %q, want %q", c.p, got, c.want)
		}
		if strings.Contains(got, "0 of 0") {
			t.Errorf("filesArrivingSaid(%+v) violated emptiness law: %q", c.p, got)
		}
	}

	taker := &movingTaker{active: true, at: MoveProgress{Step: "arriving", Done: 40 << 20, Total: 112 << 20}}
	a := continueHome(t, taker, continueRows, 200)
	a.closeHome()
	a.file = "/tmp/c-off/.cell/transcript.jsonl"
	a.homeTakeSeq = 9

	// Appears on the open chat's status line and quotes arriving bytes (C2).
	_, plain := a.seamTelemetryLabel(lineParts(a.seamParts(200)))
	if !strings.Contains(plain, "files arriving · 40 of 112 MB") {
		t.Fatalf("status line while files arrive = %q, want files arriving · 40 of 112 MB", plain)
	}
	if cmd := a.followMove(homeMoveTickMsg{seq: 9}); cmd == nil {
		t.Fatal("followMove stopped ticking while files are still arriving")
	}

	// Failure names the problem in plain words with a retry door (C4).
	taker.at = MoveProgress{Step: "failed", Done: 40 << 20, Total: 112 << 20, Err: "sync could not be reached"}
	ledger, alive := lineParts(a.seamParts(200))
	_, plain = a.seamTelemetryLabel(ledger, alive)
	if !strings.Contains(plain, "files couldn't arrive: sync could not be reached · retry") {
		t.Fatalf("status line on failed background fetch = %q", plain)
	}
	a.doors = nil
	a.markDoors(alive, 0, legendDoorRow)
	var retryDoor *statusDoor
	for i := range a.doors {
		if a.doors[i].kind == segFiles {
			retryDoor = &a.doors[i]
			break
		}
	}
	if retryDoor == nil {
		t.Fatal("failed files segment did not register a retry door")
	}
	cmd, handled := a.doorPress(*retryDoor)
	if !handled || cmd == nil || taker.retried != "c-off" {
		t.Fatalf("pressing retry door: handled=%v cmd=%v retried=%q", handled, cmd != nil, taker.retried)
	}

	// Clears when the files finish arriving (C2).
	taker.at = MoveProgress{Step: "done", Done: 112 << 20, Total: 112 << 20}
	taker.active = false
	_, plain = a.seamTelemetryLabel(lineParts(a.seamParts(200)))
	if strings.Contains(plain, "files arriving") || strings.Contains(plain, "files couldn't arrive") {
		t.Fatalf("status line did not clear after files arrived: %q", plain)
	}
	if cmd := a.followMove(homeMoveTickMsg{seq: 9}); cmd != nil {
		t.Fatal("followMove kept ticking after files finished arriving")
	}
}
