//go:build !windows

package processgroup

import (
	"syscall"
	"testing"
)

// signalRecorder swaps the identity reader and the signal sink so a test can
// present any identity it likes and count what, if anything, was sent.
func signalRecorder(t *testing.T, identity uint64, known bool) *[]syscall.Signal {
	t.Helper()
	sent := &[]syscall.Signal{}
	originalStart, originalSignal := processStart, groupSignalFn
	t.Cleanup(func() { processStart, groupSignalFn = originalStart, originalSignal })
	processStart = func(int) (uint64, bool) { return identity, known }
	groupSignalFn = func(pid int, signal syscall.Signal) error {
		*sent = append(*sent, signal)
		return nil
	}
	return sent
}

// TestAGroupSignalIsNeverSentToARecycledProcessGroup is the seam the whole
// teardown fix rests on. The recorded leader's pid now names an unrelated,
// live process — the number was handed out again — so kill(-pid) would reach
// whatever group holds that pgid now. Nothing may be sent.
func TestAGroupSignalIsNeverSentToARecycledProcessGroup(t *testing.T) {
	sent := signalRecorder(t, 200, true) // the identity the pid has NOW
	group := Group{pid: 4242, start: 100, known: true}

	if err := group.Terminate(); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if err := group.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if group.Alive() {
		t.Fatal("a recycled process group reported alive")
	}
	if len(*sent) != 0 {
		t.Fatalf("signalled a recycled process group: %v", *sent)
	}
}

// TestAGroupSignalIsWithheldWhenTheLeaderIsGone is the other refusal: a reaped
// child or a free pid. Identity cannot be verified, so nothing is sent.
func TestAGroupSignalIsWithheldWhenTheLeaderIsGone(t *testing.T) {
	sent := signalRecorder(t, 0, false)
	group := Group{pid: 4242, start: 100, known: true}

	if err := group.Terminate(); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if err := group.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if group.Alive() {
		t.Fatal("a group whose leader is gone reported alive")
	}
	if len(*sent) != 0 {
		t.Fatalf("signalled a group whose leader is gone: %v", *sent)
	}
}

// TestAGroupSignalIsSentToItsOwnProcessGroup is the other half of the contract:
// a matching identity is signalled exactly as it always was, so a live job is
// still torn down.
func TestAGroupSignalIsSentToItsOwnProcessGroup(t *testing.T) {
	sent := signalRecorder(t, 100, true) // the identity the pid had at launch
	group := Group{pid: 4242, start: 100, known: true}

	if err := group.Terminate(); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if err := group.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if len(*sent) != 2 || (*sent)[0] != syscall.SIGTERM || (*sent)[1] != syscall.SIGKILL {
		t.Fatalf("a matching identity was not signalled: %v", *sent)
	}
}

// TestARefusedSignalRecordsWhy covers the record half of the contract: a
// withheld signal says which pid and why, so a leak is visible rather than
// silent.
func TestARefusedSignalRecordsWhy(t *testing.T) {
	signalRecorder(t, 200, true)
	type note struct {
		pid    int
		reason string
	}
	var notes []note
	original := OnRefused
	t.Cleanup(func() { OnRefused = original })
	OnRefused = func(pid int, reason string) { notes = append(notes, note{pid, reason}) }

	group := Group{pid: 4242, start: 100, known: true}
	if err := group.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if len(notes) != 1 || notes[0].pid != 4242 || notes[0].reason == "" {
		t.Fatalf("a refused signal was not recorded: %+v", notes)
	}
}
