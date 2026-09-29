package tui3

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

// liveDirectory is a Source over a real directory client, the way the sync
// setup makes one: every ask lists the directory and opens the sealed names.
// It can be cut off, as a relay that stops answering is.
type liveDirectory struct {
	dir  directory.Client
	self string
	key  []byte
	down bool
}

func (l *liveDirectory) Rows(ctx context.Context) ([]chatlist.Row, error) {
	if l.down {
		return nil, directory.ErrUnreachable
	}
	listing, err := l.dir.List(ctx)
	if err != nil {
		return nil, err
	}
	open := func(sealed string) (string, error) { return directory.OpenName(l.key, sealed) }
	return chatlist.Rows(listing, l.self, open), nil
}

// TestHomeLiveSource is home reading a source that is alive: what the
// directory says changes between two asks, and the sessions panel follows it
// (a chat that runs elsewhere goes quiet, a branch appears, this machine takes
// the chat) until the directory cannot be reached, when the last listing stays
// on screen under the sentence that says so.
func TestHomeLiveSource(t *testing.T) {
	ctx := context.Background()
	clock := directorytest.NewFakeClock()
	world := directory.NewMemory(clock.Now)
	studio, here := world.For("dev_studio"), world.For("dev_here")
	key := directory.MetadataKey(bytes.Repeat([]byte{7}, 16))

	name, _ := directory.SealName(key, "studio")
	title, _ := directory.SealTitle(key, "Port the picker")
	if err := studio.PutDevice(ctx, "dev_studio", directory.Device{V: 1, Name: name}); err != nil {
		t.Fatal(err)
	}
	init := directory.CellInit{Head: "h1", Class: "chat", Title: title, Keys: map[string]map[string]string{}}
	if _, err := studio.Create(ctx, "c-live", init); err != nil {
		t.Fatal(err)
	}
	live := &liveDirectory{dir: here, self: "dev_here", key: key}

	a := machinesHome(t, live, 200)
	if text := homeText(a); !strings.Contains(text, "Port the picker") || !strings.Contains(text, "running on studio") {
		t.Fatalf("a chat running elsewhere is not listed as such:\n%s", text)
	}

	clock.Advance(directory.LeaseTTL + 1)
	drain(t, a, a.askMachines())
	if text := homeText(a); !strings.Contains(text, "studio off") || strings.Contains(text, "running on studio") {
		t.Fatalf("the chat went quiet and home did not follow:\n%s", text)
	}

	branch := directory.CellInit{Head: "h2", Class: "chat", ParentCell: "c-live", OrphanTurns: 2, Title: title, Keys: map[string]map[string]string{}}
	if _, err := studio.Create(ctx, "c-branch", branch); err != nil {
		t.Fatal(err)
	}
	drain(t, a, a.askMachines())
	if text := homeText(a); !strings.Contains(text, "2 turns from studio") {
		t.Fatalf("a branch that appeared is not listed:\n%s", text)
	}

	live.down = true
	drain(t, a, a.askMachines())
	text := homeText(a)
	if !strings.Contains(text, chatlist.Unreachable) || !strings.Contains(text, "studio off") {
		t.Fatalf("with the directory out of reach the last listing must stay, under the sentence:\n%s", text)
	}

	live.down = false
	if _, err := here.Acquire(ctx, "c-live"); err != nil {
		t.Fatal(err)
	}
	drain(t, a, a.askMachines())
	text = homeText(a)
	if strings.Contains(text, chatlist.Unreachable) || strings.Contains(text, "studio off") {
		t.Fatalf("the chat is held here now and the sentences must go:\n%s", text)
	}
}

// TestReleasedChatOnAnotherMachineOffersContinue: a chat the other machine let
// go of is not open on this one, so enter on it raises the takeover screen and
// the yes is `continue here`.
func TestReleasedChatOnAnotherMachineOffersContinue(t *testing.T) {
	taker := &fakeTaker{}
	a := continueHome(t, taker, chatlist.Static{
		{Cell: "c-idle", Title: "Notes on the migration", Device: "studio", Status: chatlist.Idle, DurableAgo: 26 * 3600e9},
	}, 200)
	standOn(t, a, "c-idle")
	if text := homeText(a); !strings.Contains(text, chatlist.OfferContinue) {
		t.Fatalf("a released chat does not offer to continue:\n%s", text)
	}
	confirm(t, a)
	if len(taker.cells) != 1 || taker.cells[0] != "c-idle" {
		t.Fatalf("Take was asked for %v", taker.cells)
	}
}

// TestNoticeSeamSaysASentenceAtOnce: a sentence put on the desk from another
// goroutine is a note in the conversation as soon as the loop reads it, whether
// the desk was joined before or after the sentence was said.
func TestNoticeSeamSaysASentenceAtOnce(t *testing.T) {
	a, _ := homeTabsFixture(t)
	desk := NewNotices()
	desk.Say("studio continued this chat; this window now only shows it")
	a.useNotices(desk) // a sentence said before the surface listened waits for it
	ringOf := func() tea.Msg { return a.noticeBell.waitRing()() }
	if _, ok := ringOf().(noticesMsg); !ok {
		t.Fatal("joining a desk that holds a sentence did not ring the door")
	}
	a.route(noticesMsg{})
	if !saidNote(a, "studio continued this chat") {
		t.Fatal("the sentence said before the surface joined was lost")
	}

	go desk.Say("this computer's clock is off by more than 5 minutes")
	msg := ringOf() // the door the loop keeps parked on
	if _, ok := msg.(noticesMsg); !ok {
		t.Fatalf("a sentence from another goroutine rang %T", msg)
	}
	a.route(msg)
	if !saidNote(a, "clock is off") {
		t.Fatal("a sentence said from another goroutine never reached the loop")
	}
}

func saidNote(a *app, want string) bool {
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, want) {
			return true
		}
	}
	return false
}
