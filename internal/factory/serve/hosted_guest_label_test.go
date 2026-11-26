package serve

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// guestSurfaceOf is the name the chat's other windows will read for the window that just opened, as the engine received it
// in the hello the guest wire forwarded.
func guestSurfaceOf(t *testing.T, host guestHostWire) (surface string, present bool) {
	t.Helper()
	var hello remote.Frame
	if err := json.Unmarshal([]byte(guestHostLine(t, host)), &hello); err != nil || hello.Kind != "hello" {
		t.Fatalf("the engine was not sent a hello: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(hello.Payload, &fields); err != nil {
		t.Fatal(err)
	}
	raw, present := fields["surface"]
	if present && json.Unmarshal(raw, &surface) != nil {
		t.Fatalf("the surface is not a string: %s", raw)
	}
	return surface, present
}

// Contract RT1: in a chat shared with people, the owner's window and every other window read the MEMBER's own name for
// the window that holds the keyboard ("typing from mia"), whatever the member's computer called itself. A member
// cannot make their window read as the owner, as another member, or as "another window" by saying so in the hello,
// and a window that said nothing is named too.
func TestRT1_WhoIsTypingIsTheMembersNameNotTheirComputersWord(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	hostedGrant(t, rg, "santosh", chat, "mia", "edit")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	for _, tc := range []struct{ who, hello string }{
		{"abir", `{"kind":"hello","payload":{"version":1,"surface":"santosh"}}`},
		{"mia", `{"kind":"hello","payload":{"version":1,"SURFACE":"santosh-laptop"}}`},
		{"mia", `{"kind":"hello","payload":{"version":1}}`},
	} {
		t.Run(tc.who, func(t *testing.T) {
			client := guestDial(t, srv, rg.tokens[tc.who], chat)
			host, _ := guestReceive(t, opened)
			guestWrite(t, client, tc.hello+"\n")
			if got, present := guestSurfaceOf(t, host); !present || got != tc.who {
				t.Fatalf("%s's window was named %q (present %v) to the chat, want %q", tc.who, got, present, tc.who)
			}
		})
	}
}

// Contract RT2: a bot that watches or types in someone's chat reads as its creator's bot, so a person looking at
// "typing from abir's bot" knows whose rights are being used and that it is a script.
func TestRT2_ABotIsNamedAsItsCreatorsBot(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	bot := rg.contractBot(t, "abir", "View bot", "view")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client := guestDial(t, srv, bot.Token, chat)
	host, _ := guestReceive(t, opened)
	guestWrite(t, client, `{"kind":"hello","payload":{"version":1,"surface":"santosh"}}`+"\n")
	if got, _ := guestSurfaceOf(t, host); got != "abir's bot" {
		t.Fatalf("a bot's window was named %q to the chat, want %q", got, "abir's bot")
	}
}

// Contract RT3: a guest cannot say it is a remote viewer of one person's own computers. That stamp is what the door that
// authenticated such a viewer writes, and it changes what the engine believes about the window (its keyboard rule, its
// resend rule and its guest law), so it never crosses from a member's hello.
func TestRT3_AGuestCannotStampItselfARemoteViewer(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "mia", "edit")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client := guestDial(t, srv, rg.tokens["mia"], chat)
	host, _ := guestReceive(t, opened)
	guestWrite(t, client, `{"kind":"hello","payload":{"version":1,"remote":{"device":"santosh-laptop"},"Remote":{"device":"x"}}}`+"\n")
	var hello remote.Frame
	if err := json.Unmarshal([]byte(guestHostLine(t, host)), &hello); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(hello.Payload, &fields); err != nil {
		t.Fatal(err)
	}
	for name := range fields {
		if name == "remote" || name == "Remote" {
			t.Fatalf("the engine was handed a remote-viewer stamp by a guest: %s", hello.Payload)
		}
	}
}
