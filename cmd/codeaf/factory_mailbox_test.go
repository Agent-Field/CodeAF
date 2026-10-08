package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// doorRunner is a fake runner that writes down every door it was called
// through, in order, and refuses each in a sentence naming the verb.
type doorRunner struct {
	mu    sync.Mutex
	calls []string
	due   bool
}

func (f *doorRunner) note(verb string, id int, rest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("%s %d%s", verb, id, rest))
	return fmt.Errorf("#%d refused %s", id, verb)
}

func (f *doorRunner) Launch(id int) error { return f.note("launch", id, "") }
func (f *doorRunner) Stop(id int) error   { return f.note("stop", id, "") }
func (f *doorRunner) Pause(id int) error  { return f.note("pause", id, "") }
func (f *doorRunner) Answer(id int, yes bool, words string) error {
	return f.note("answer", id, fmt.Sprintf(" %v %q", yes, words))
}
func (f *doorRunner) Steer(id int, words string) error {
	return f.note("steer", id, fmt.Sprintf(" %q", words))
}
func (f *doorRunner) SignOff(id int, edited bool) (bool, error) {
	_ = f.note("signoff", id, fmt.Sprintf(" %v", edited))
	f.mu.Lock()
	defer f.mu.Unlock()
	// A SIGN-OFF THAT WENT THROUGH says whether a habit is due, and no error.
	return f.due, nil
}
func (f *doorRunner) SendBack(id int, words string) error {
	return f.note("sendback", id, fmt.Sprintf(" %q", words))
}
func (f *doorRunner) Reverify(id int) error { return f.note("reverify", id, "") }

func (f *doorRunner) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func mailboxStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// TestEveryDoorFromAWindowReachesTheRunner: each of the eight verbs pressed in
// a window that does not run the floor reaches the owner's runner with its
// own arguments, and the runner's refusal is the sentence the window says.
func TestEveryDoorFromAWindowReachesTheRunner(t *testing.T) {
	st := mailboxStore(t)
	r := &doorRunner{due: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go drainFactoryMailbox(ctx, st.Mailbox(), r, 5*time.Millisecond)
	seam := factory.LocalSeam(st, time.Now(), factory.WithMailbox(st.Mailbox()))

	cases := []struct {
		verb, call string
		door       func() error
	}{
		{"launch", "launch 7", func() error { return seam.Launch(7) }},
		{"stop", "stop 7", func() error { return seam.Stop(7) }},
		{"pause", "pause 7", func() error { return seam.Pause(7) }},
		{"answer", `answer 7 true "go ahead"`, func() error { return seam.Answer(7, true, "go ahead") }},
		{"steer", `steer 7 "smaller diff"`, func() error { return seam.Steer(7, "smaller diff") }},
		{"sendback", `sendback 7 "prove the race"`, func() error { return seam.SendBack(7, "prove the race") }},
		{"reverify", "reverify 7", func() error { return seam.Reverify(7) }},
	}
	for _, c := range cases {
		if !seam.Has(c.verb) {
			t.Errorf("the window draws no %s", c.verb)
		}
		err := c.door()
		if err == nil || err.Error() != "#7 refused "+c.verb {
			t.Errorf("%s from the window = %v, not the runner's sentence", c.verb, err)
		}
		if seen := r.seen(); len(seen) == 0 || seen[len(seen)-1] != c.call {
			t.Errorf("%s reached the runner as %v, want %q last", c.verb, seen, c.call)
		}
	}
	if !seam.Has("signoff") {
		t.Error("the window draws no signoff")
	}
	due, err := seam.SignOff(7, true)
	if err != nil || !due {
		t.Fatalf("sign-off from the window = due %v, %v; want the habit due to cross", due, err)
	}
	if seen := r.seen(); seen[len(seen)-1] != "signoff 7 true" {
		t.Fatalf("sign-off reached the runner as %q", seen[len(seen)-1])
	}
}

// TestAWindowWithNobodyDrainingSaysTheRunnerDidNotAnswer: no owner, so the
// wait runs out and the window says the one sentence for that.
func TestAWindowWithNobodyDrainingSaysTheRunnerDidNotAnswer(t *testing.T) {
	st := mailboxStore(t)
	held := factory.MailboxWait
	factory.MailboxWait = 60 * time.Millisecond
	t.Cleanup(func() { factory.MailboxWait = held })
	seam := factory.LocalSeam(st, time.Now(), factory.WithMailbox(st.Mailbox()))
	err := seam.Stop(3)
	if !errors.Is(err, factory.ErrRunnerSilent) || err.Error() != "the floor's runner did not answer · is codeaf running?" {
		t.Fatalf("stop with nobody draining = %v", err)
	}
}

// TestTheOwnerDrainsInOrderAndDropsWhatNobodyWaitsFor: one pass carries the
// asks oldest first, and an ask older than a window waits is not carried out.
func TestTheOwnerDrainsInOrderAndDropsWhatNobodyWaitsFor(t *testing.T) {
	st := mailboxStore(t)
	mb := st.Mailbox()
	now := time.Now()
	stale, err := mb.Post(factory.Ask{ID: 1, Verb: factory.VerbLaunch, At: now.Add(-factory.MailboxWait - time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for _, a := range []factory.Ask{{ID: 2, Verb: factory.VerbLaunch}, {ID: 2, Verb: factory.VerbPause}, {ID: 3, Verb: factory.VerbStop}} {
		a.At = now
		seq, err := mb.Post(a)
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	r := &doorRunner{}
	answerFactoryAsks(mb, r, now)
	want := []string{"launch 2", "pause 2", "stop 3"}
	if got := r.seen(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the owner carried %v, want %v", got, want)
	}
	for i, seq := range seqs {
		reply, err := mb.Wait(seq, 0)
		if err != nil || reply.Seq != seq || reply.Err == "" {
			t.Errorf("ask %d's reply = %+v, %v", i, reply, err)
		}
	}
	if _, err := mb.Wait(stale, 0); !errors.Is(err, factory.ErrRunnerSilent) {
		t.Errorf("a stale ask was answered: %v", err)
	}
	if left, _ := mb.Take(); len(left) != 0 {
		t.Errorf("the drain left %d asks behind", len(left))
	}
}
