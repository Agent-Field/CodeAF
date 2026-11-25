package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// Written from the contract for the person turning remote control off on the
// computer being watched: the view that was open on it says that, in the one
// sentence the host's reason has, in the place a view's state is said. It never
// says the computer went offline, and it never carries the words of a link that
// is being repaired, because the computer is up and said no.
func TestAViewTheHostSwitchedOffSaysSoAndNeverSaysWentOffline(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.dismissWelcome()
	a.takeUp(Conversation{
		Agent: &fakeAgent{model: "m"},
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead {
				return LiveRead{Kind: LiveRevoked, Reason: "remote-control-off"}
			},
		},
	}, true)

	bar, ok := a.liveBar()
	if !ok {
		t.Fatal("a view the host switched off drew no sentence in the box's place")
	}
	const want = "Desktop turned remote control off."
	if bar != want {
		t.Fatalf("the bar reads %q, want exactly %q", bar, want)
	}
	if strings.Contains(bar, chatlist.LiveWentOffline("Desktop")) {
		t.Fatalf("the bar says the computer went offline: %q", bar)
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, want) {
		t.Fatalf("the frame does not say %q:\n%s", want, screen)
	}
	for _, never := range []string{"went offline", "reconnecting"} {
		if strings.Contains(screen, never) {
			t.Fatalf("the frame says %q although the computer is up and said no:\n%s", never, screen)
		}
	}
}
