package pair

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// The whole introduction, end to end: A shows a code, B types it, both show the
// same three words, A says yes, and B holds A's identity under its own device.
func TestPairingSharesTheChatsAndTheWordsAgree(t *testing.T) {
	r := newChatRig(t)
	ctx := context.Background()
	a, b := newScreen(), newScreen()
	done := r.offer(ctx, a)
	code := a.nextCode(t)

	joined, err := r.join(ctx, r.homeB, code.Shown(), b)
	if err != nil {
		t.Fatal(err)
	}
	end := within(t, done)
	if end.err != nil || end.label != "laptop" {
		t.Fatalf("A ended with %q, %v", end.label, end.err)
	}
	if joined.Already {
		t.Fatal("a fresh computer was told it was already paired")
	}
	if got, want := <-a.asked, <-b.words; got != want {
		t.Fatalf("the two screens show different words: %q and %q", got, want)
	}
	held, err := identity.Load(r.homeB)
	if err != nil || held.ID() != r.a.ID() {
		t.Fatalf("B holds %v (%v), want A's identity", held.ID(), err)
	}
}
