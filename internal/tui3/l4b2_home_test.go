package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// L4-M4a: A link entered on Home replaces the add-device steps and returns there after either decision.
func TestL4B2HomeApprovalStaysHome(t *testing.T) {
	for _, answer := range []string{"a", "d", "esc"} {
		t.Run(answer, func(t *testing.T) {
			door := &fakeApprovals{pending: tower(time.Now())}
			a, r := approveApp(t, door)
			a.openHome()
			a.addMachine = addMachine{size: 1, known: true, open: true, focused: true}
			a.addMachine.box.setText(testLink)
			cmd, took := a.addMachineInputKey(key("enter"))
			if !took {
				t.Fatal("link field refused Enter")
			}
			r.run(cmd)
			r.until("approval", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
			if !a.at(pageHome) {
				t.Fatal("approval left Home")
			}
			text := homeText(a)
			if !strings.Contains(text, "Check number 4821") || strings.Contains(text, addMachineStepPut) {
				t.Fatalf("approval not in place: %s", text)
			}
			r.press(answer)
			r.until("decision", func() bool { return !a.pair.open })
			if !a.at(pageHome) {
				t.Fatal("decision left Home")
			}
			if answer != "a" && !strings.Contains(homeText(a), addDeviceWord) {
				t.Fatal("normal card did not return")
			}
		})
	}
}

// L4-M5a: The offer shows both directions, preserves --via and approves a pasted link through the usual door.
func TestL4B2PairOfferAcceptsLink(t *testing.T) {
	a, r := approveApp(t, &fakeApprovals{pending: tower(time.Now())})
	code, err := pair.NewCode(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a.pair = pairPanel{open: true, offering: true, code: code, lines: pair.ChatCardLines(code, "http://127.0.0.1:8901")}
	text := plain(strings.Join(a.pair.rows(220, time.Now(), a.pal), "\n"))
	if !strings.Contains(text, "paste the link the new device shows, or on it run codeaf pair "+code.Shown()+" --via http://127.0.0.1:8901") || !strings.Contains(text, "Paste pairing link") {
		t.Fatal(text)
	}
	r.run(a.paste(testLink))
	r.press("enter")
	r.until("approval", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	if a.input.String() != "" {
		t.Fatal("link reached chat draft")
	}
}

// L4-M6a, L4-M6b, L4-M6c: Removal restores the Home card and gets its own line before pairing instructions.
func TestL4B2RemovedHomeAndPair(t *testing.T) {
	a, _ := approveApp(t, &fakeApprovals{})
	a.openHome()
	a.addMachine = addMachine{size: 3, known: true}
	a.removedComputer()
	text := homeText(a)
	want := "removed by another of your devices — pair again to sync"
	if !a.addMachineWanted() || !strings.Contains(text, want) || !strings.Contains(text, addDeviceWord) {
		t.Fatal(text)
	}
	if line, ok := a.placeMsgLine(200); ok && strings.Contains(line, "removed") {
		t.Fatalf("status on hint row: %s", line)
	}
	a.pairing = &fakePairing{hasChats: true}
	a.runPair("")
	text = plain(strings.Join(a.pair.rows(180, a.now(), a.pal), "\n"))
	if !a.pair.rejoining || strings.Index(text, want) < 0 || strings.Index(text, want) > strings.Index(text, "on a device still") || !strings.Contains(text, "Code from another device") {
		t.Fatal(text)
	}

}

// L4-M13a: Account cards opened from Home never call the conversation-opening door.
func TestL4B2HomeCommandsDoNotStartChats(t *testing.T) {
	for _, command := range []string{"/devices", "/pair", "/settings"} {
		t.Run(command, func(t *testing.T) {
			a, r := approveApp(t, &fakeApprovals{devices: []DeviceRow{{ID: "self", Self: true, Name: "here"}}})
			opened := 0
			a.fresh = func() (Agent, string, error) {
				opened++
				return &fakeAgent{model: "m"}, "/private/transcript.jsonl", nil
			}
			a.pairing = &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) { <-ctx.Done(); return "", ctx.Err() }}
			a.openHome()
			a.home.box.setText(command)
			r.run(a.homeSlash(command))
			if opened != 0 {
				t.Fatal("account command started a conversation")
			}
			if command != "/settings" && !a.at(pageHome) {
				t.Fatal("card left Home")
			}
		})
	}
}

// L4-M13b: A fresh conversation note names its project without leaking a transcript path.
func TestL4B2NewConversationNote(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.workspace = "/private/proj-a"
	a.finishRenew(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/private/hidden/transcript.jsonl", Workspace: a.workspace}, true, true)
	text := updateNotes(a)
	if !strings.Contains(text, "new conversation · proj-a") || strings.Contains(text, "transcript.jsonl") || strings.Contains(text, "new session") {
		t.Fatal(text)
	}
}

// L4-M6c: A removed computer can join through a code from a current device and still asks before replacement.
func TestL4B2RemovedPairUsesCurrentDeviceCode(t *testing.T) {
	a, r, joins := replaceApp(t, true)
	a.removedComputer()
	r.run(a.runPair(""))
	r.run(a.paste("71-869-571"))
	r.press("enter")
	r.until("confirmation", func() bool { return a.pair.ask != nil })
	if *joins != 0 {
		t.Fatal("rejoined before confirmation")
	}
	r.press("y")
	r.until("paired again", func() bool { return a.pair.done })
	if *joins != 1 {
		t.Fatal("did not use the replacement join")
	}
}

// L4-M17c: Home omits the strip whenever the feed cannot vouch for fresh presence.
func TestL4B2UnknownPresenceHidesStrip(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.devRow.devices = []chatlist.Device{{ID: "self", Self: true, Name: "This computer"}, {ID: "peer", Name: "laptop-a"}}
	feed := &fakeFeed{state: dirwatch.State{Up: true, Online: []string{"peer"}}}
	a.dirFeed = feed
	ctx := bandContext{width: 160, pal: a.pal}
	if len(drawDevicesBand(a, ctx)) == 0 {
		t.Fatal("fresh presence missing")
	}
	feed.state.Up = false
	if len(drawDevicesBand(a, ctx)) != 0 {
		t.Fatal("unknown presence drawn")
	}
	feed.state.Up = true
	if len(drawDevicesBand(a, ctx)) == 0 {
		t.Fatal("new answer did not restore strip")
	}
}

// L4-M6b: Status news on Home occupies its own dim row and never replaces or joins the key hints.
func TestL4B2HomeNewsHasOwnLine(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 36
	a.openHome()
	a.home.say("desk-b was declined.", "")
	text := homeText(a)
	found := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "desk-b was declined.") {
			found = true
			if strings.Contains(line, "enter ") || strings.Contains(line, "tab ") || strings.Contains(line, "→ options") {
				t.Fatalf("news joined key hints: %s", line)
			}
		}
	}
	if !found {
		t.Fatal("news disappeared")
	}
}
