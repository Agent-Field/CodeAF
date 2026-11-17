package syncsetup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// Two computers of one identity on a real directory handler: the desk writes
// to a chat the tower holds, the tower takes it, and the desk's outbox empties.
func TestAMessageReachesTheComputerHoldingTheChatAndLeavesTheOutboxOnceTaken(t *testing.T) {
	url := relay(t).URL
	desk, ok, err := Open(machine(t, url))
	if err != nil || !ok {
		t.Fatalf("desk = %v, %v", ok, err)
	}
	towerHome := t.TempDir()
	if _, err := identity.Adopt(towerHome, desk.Identity, false); err != nil {
		t.Fatal(err)
	}
	tower, ok, err := Open(towerHome)
	if err != nil || !ok {
		t.Fatalf("tower = %v, %v", ok, err)
	}
	ctx := context.Background()
	for _, s := range []*Sync{desk, tower} {
		if err := PutOwnDevice(ctx, s.Dir, s.Identity, s.Device, s.Device.ID()); err != nil {
			t.Fatal(err)
		}
	}
	const chat = "01J0000000000000000000000A"
	if _, err := tower.Dir.Create(ctx, chat, directory.CellInit{Head: head}); err != nil {
		t.Fatal(err)
	}

	id, err := desk.SendNote(ctx, chat, "  add a LICENSE too  ")
	if err != nil {
		t.Fatal(err)
	}
	sent, err := desk.MailAt(ctx)
	if err != nil || len(sent.Sent[chat]) != 1 || sent.Sent[chat][0].Text != "add a LICENSE too" || len(sent.In) != 0 {
		t.Fatalf("desk mail = %+v, %v; want its own message waiting, trimmed, and nothing for it to take", sent, err)
	}
	in, err := tower.MailAt(ctx)
	if err != nil || len(in.In) != 1 || in.In[0].ID != id || in.In[0].Text != "add a LICENSE too" || in.In[0].From != desk.Device.ID() {
		t.Fatalf("tower mail = %+v, %v; want the desk's message for the chat it holds", in, err)
	}

	// A name change rewrites the record whole; the message it carries stays.
	if err := PutOwnDevice(ctx, desk.Dir, desk.Identity, desk.Device, "renamed desk"); err != nil {
		t.Fatal(err)
	}
	if err := tower.TookNotes(ctx, []string{id}); err != nil {
		t.Fatal(err)
	}
	if again, _ := tower.MailAt(ctx); len(again.In) != 0 {
		t.Fatalf("tower mail after taking = %+v; a taken message is not taken twice", again.In)
	}
	taken, _ := desk.MailAt(ctx)
	if len(taken.Taken) != 1 || taken.Taken[0].ID != id || len(taken.Sent[chat]) != 0 {
		t.Fatalf("desk mail after the take = %+v; want it taken and no longer waiting", taken)
	}
	if err := desk.DropNotes(ctx, []string{id}); err != nil {
		t.Fatal(err)
	}
	// The tower forgets an id no sender lists any more.
	if err := tower.TookNotes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	l, err := tower.Dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out, took := l.Devices[desk.Device.ID()].Outbox, l.Devices[tower.Device.ID()].Took; len(out) != 0 || len(took) != 0 {
		t.Fatalf("outbox %v, took %v; want both empty once the message landed", out, took)
	}
	if strings.Contains(l.Devices[desk.Device.ID()].Name, "renamed") {
		t.Fatal("the name travelled in the clear")
	}
}

// A message is refused before it is written when it is empty, too long, or
// one too many; a chat no one here holds is not this computer's to take.
func TestAMessageIsRefusedWhenItCannotTravel(t *testing.T) {
	url := relay(t).URL
	desk, ok, err := Open(machine(t, url))
	if err != nil || !ok {
		t.Fatalf("desk = %v, %v", ok, err)
	}
	ctx := context.Background()
	if err := PutOwnDevice(ctx, desk.Dir, desk.Identity, desk.Device, "desk"); err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string]error{" ": ErrNoteEmpty, strings.Repeat("x", directory.MaxNoteBytes+1): ErrNoteLong} {
		if _, err := desk.SendNote(ctx, "c", text); !errors.Is(err, want) {
			t.Fatalf("SendNote(%d bytes) = %v, want %v", len(text), err, want)
		}
	}
	for range directory.MaxNotes {
		if _, err := desk.SendNote(ctx, "c", "hi"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := desk.SendNote(ctx, "c", "one more"); !errors.Is(err, ErrOutboxFull) {
		t.Fatalf("a ninth message = %v, want ErrOutboxFull", err)
	}
	if mail, _ := desk.MailAt(ctx); len(mail.In) != 0 {
		t.Fatalf("mail.In = %v; nobody here holds chat c", mail.In)
	}
}
