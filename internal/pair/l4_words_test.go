package pair

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"strings"
	"testing"
)

// L4-H1a: every instruction uses the selected sync service, even without a flag.
func TestL4PairCommand(t *testing.T) {
	if got := Command("http://127.0.0.1:8901", "04-063-213"); got != "codeaf pair 04-063-213 --via http://127.0.0.1:8901" {
		t.Fatal(got)
	}
	if got := Command("https://codeaf.agentfield.ai/fabric", ""); got != "codeaf pair" {
		t.Fatal(got)
	}
}

// L4-H1b, L4-H1c: a hosted link must not be mistaken for an expired local link.
func TestL4LinkServiceMismatch(t *testing.T) {
	for _, tc := range []struct{ link, own, want string }{
		{"https://codeaf.agentfield.ai/p/k7m2q9xd", "http://127.0.0.1:8901", "that link is waiting on codeaf.agentfield.ai, but this computer syncs through 127.0.0.1:8901 — on the new device run: codeaf pair --via http://127.0.0.1:8901"},
		{"http://127.0.0.1:8901/p/k7m2q9xd", "https://codeaf.agentfield.ai/fabric", "that link is waiting on 127.0.0.1:8901, but this computer syncs through codeaf.agentfield.ai — on the new device run: codeaf pair"},
	} {
		err := CheckLinkService(tc.link, tc.own)
		if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), "not waiting") {
			t.Fatalf("%v", err)
		}
	}
	if err := CheckLinkService("k7m2q9xd", "http://127.0.0.1:8901"); err != nil {
		t.Fatal(err)
	}
}

// L4-M12a through L4-M12c: both roads name the peer and end with the same next step.
func TestL4PairCompletionVoice(t *testing.T) {
	if got := (Joined{With: "laptop-a"}).Sentence(); got != "Paired with laptop-a. Run codeaf to open your chats." {
		t.Fatal(got)
	}
	if got := PairedChatsLine("desk-b"); got != "desk-b is now paired. Your chats can continue there." {
		t.Fatal(got)
	}
	if got := WaitingChatsLine("a b c"); got != "Waiting for approval on your other device; it should show: a b c" {
		t.Fatal(got)
	}
}

// L4-M1a: serving retains its rendezvous name beside the familiar device name.
func TestL4ServeUsesDeviceName(t *testing.T) {
	h := Host{DisplayName: "desk-b"}
	got := h.banner("seagull-lichen-63", nil, ErrOffering)
	if !strings.Contains(got, "this machine is reachable as  desk-b (seagull-lichen-63)") {
		t.Fatal(got)
	}
}

// L4-M16a: process failure yields one named connection sentence without raw detail.
func TestL4ConnectionEnded(t *testing.T) {
	if got := ConnectionEndedLine("laptop-a", errors.New("write /dev/stdout: splice: connection reset by peer; exit status 1")); got != "the connection to laptop-a ended" {
		t.Fatal(got)
	}
}

// L4-M7a: CLI connection words use the sync service vocabulary.
func TestL4SyncVocabulary(t *testing.T) {
	for _, err := range []error{NoRelay("desk-b"), NoDoor("https://example.com"), NotConnected("desk-b"), Busy(), ErrSyncOff, ErrNoSync} {
		if strings.Contains(err.Error(), "sync address") || strings.Contains(err.Error(), "the service") {
			t.Fatal(err)
		}
	}
}

// L4-H6e: the serving machine is never asked to reach itself through ssh.
func TestL4ServeRemedyIsLocal(t *testing.T) {
	if got := NoDoorForServe("http://127.0.0.1:8901").Error(); strings.Contains(got, "--host") || !strings.Contains(got, "codeaf relay") {
		t.Fatal(got)
	}
}

// L4-H6d: code-pairing server failures use the same sentence as devices and cluster.
func TestL4CodePairServiceFailure(t *testing.T) {
	got := phraseBox(wireauth.ErrUnavailable, "http://127.0.0.1:8901")
	if got.Error() != "the sync service at 127.0.0.1:8901 is up but cannot agree on changes right now; it usually recovers within a minute" {
		t.Fatal(got)
	}
}
