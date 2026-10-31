package pair

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/wireauth"
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

// L4-M1a, L4-FIX5-a: the first serving name is accepted by --at; the device name is a hint.
func TestL4ServeUsesDeviceName(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv(RelayEnv, "")
	const name = "seagull-lichen-63"
	for _, display := range []string{"desk-b", ""} {
		for _, codeErr := range []error{nil, ErrOffering} {
			h := Host{DisplayName: display}
			code := &Code{digits: "715302"}
			got := h.banner(name, code, codeErr)
			first, rest, _ := strings.Cut(got, "\n")
			want := "  this machine is reachable as  " + name
			if display != "" {
				want += " (desk-b)"
			}
			if first != want {
				t.Errorf("display=%q, code error=%v: first line %q; want %q", display, codeErr, first, want)
			}
			target := strings.Fields(first)[5]
			if !relay.ValidName(target) {
				t.Errorf("first name %q cannot be used with --at", target)
				continue
			}
			if codeErr == nil {
				_, plain, _ := strings.Cut(strings.TrimRight(Lines(name, code), "\n"), "\n")
				if rest != plain {
					t.Errorf("instructions changed with the hint: %q; want %q", rest, plain)
				}
			}
			reach := Reach{Name: target, Machines: BookAt(filepath.Join(t.TempDir(), "machines.json"))}
			if _, err := reach.Open(t.Context()); err == nil || err.Error() != NoRelay(name).Error() {
				t.Errorf("--at %s should pass the shape check and reach the no-service check: %v", target, err)
			}
		}
	}
	if got := BackLine(name); got != "back — reachable as seagull-lichen-63 again" {
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
