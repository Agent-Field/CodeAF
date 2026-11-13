package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// The target is read the way the ssh door reads its own, because it is the same
// idea said about a different kind of address.
func TestTheAtTargetSplitsOnTheFirstColon(t *testing.T) {
	for _, one := range []struct {
		typed, name, workspace string
	}{
		{"otter-lamp-42", "otter-lamp-42", ""},
		{"otter-lamp-42:", "otter-lamp-42", ""},
		{"otter-lamp-42:code/app", "otter-lamp-42", "code/app"},
		{"otter-lamp-42:/srv/code/app", "otter-lamp-42", "/srv/code/app"},
		{"  otter-lamp-42:code  ", "otter-lamp-42", "code"},
	} {
		name, workspace, err := parseAtTarget(one.typed)
		if err != nil {
			t.Fatalf("%q: %v", one.typed, err)
		}
		if name != one.name || workspace != one.workspace {
			t.Fatalf("%q read as %q and %q", one.typed, name, workspace)
		}
	}
}

// A REFUSAL NAMES THE SHAPE IT WANTED. Somebody who typed the wrong thing needs
// an example, not a complaint.
func TestAnEmptyOrHeadlessAtTargetIsRefusedWithAnExample(t *testing.T) {
	_, _, err := parseAtTarget("   ")
	if err == nil || !strings.Contains(err.Error(), "otter-lamp-42") {
		t.Fatalf("an empty --at said %v", err)
	}
	if _, _, err := parseAtTarget(":code/app"); err == nil || !strings.Contains(err.Error(), "no machine in front of the colon") {
		t.Fatalf("a headless --at said %v", err)
	}
}

// The two flags that build a session cannot travel, and are refused rather than
// quietly ignored — the same law the ssh door states, for the same reason.
func TestTheFlagsThatBuildASessionCannotTravelOverAt(t *testing.T) {
	err := atLaunch{target: "otter-lamp-42", yolo: true, noCompact: true}.check()
	if err == nil {
		t.Fatal("--yolo and --no-compact travelled over --at")
	}
	said := err.Error()
	for _, want := range []string{"--no-compact", "--yolo", "otter-lamp-42", "settings panel"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the refusal does not mention %q: %q", want, said)
		}
	}
	if err := (atLaunch{target: "otter-lamp-42"}).check(); err != nil {
		t.Fatalf("a launch with neither flag was refused: %v", err)
	}
}

// WITH NO RELAY SET UP, BOTH DOORS SAY SO IN ONE SENTENCE AND NAME THE ROAD
// THAT DOES WORK. Sync falls back to the hosted relay, so the only machine
// with no relay is one whose person turned sync off, and they must not be
// left guessing whether their network is broken.
func TestWithNoRelayBothDoorsSayWhatIsWrongAndWhatToDoInstead(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_RELAY", "")
	t.Setenv(syncsetup.URLVar, "off")

	err := openChatV3At(atLaunch{target: "otter-lamp-42"})
	if err == nil {
		t.Fatal("--at opened a connection with no relay set up")
	}
	said := err.Error()
	for _, want := range []string{"no sync service is set up on this machine", "CODEAF_RELAY", "--host over ssh", "otter-lamp-42"} {
		if !strings.Contains(said, want) {
			t.Fatalf("--at with no relay said %q, which does not mention %q", said, want)
		}
	}
	if strings.Contains(said, "\n") {
		t.Fatalf("--at with no relay said more than one line: %q", said)
	}

	err = runServe(nil)
	if err == nil {
		t.Fatal("codeaf serve started with no relay set up")
	}
	said = err.Error()
	for _, want := range []string{"no sync service is set up on this machine", "CODEAF_RELAY", "set " + pair.RelayEnv + " to your sync service"} {
		if !strings.Contains(said, want) {
			t.Fatalf("codeaf serve with no relay said %q, which does not mention %q", said, want)
		}
	}
	if strings.Contains(said, "--host") {
		t.Fatalf("codeaf serve told this computer to reach itself: %q", said)
	}
}

// SERVE, --at, PAIRING AND SYNC AGREE ABOUT THE RELAY. A machine whose chats
// sync is reachable through that same relay with nothing else set up, whether
// sync's address came from the variable, a saved pairing or the hosted default.
func TestServeAtPairingAndSyncResolveOneRelay(t *testing.T) {
	cases := []struct{ name, variable, saved, want string }{
		{"the hosted default", "", "", syncsetup.HostedRelayURL},
		{"the relay a pairing saved", "", "https://saved.example/fabric", "https://saved.example/fabric"},
		{"the variable", "https://variable.example", "https://saved.example/fabric", "https://variable.example"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv(pair.RelayEnv, "")
			t.Setenv(syncsetup.URLVar, c.variable)
			if c.saved != "" {
				if err := syncsetup.SaveRelayURL(home.Dir(), c.saved); err != nil {
					t.Fatal(err)
				}
			}
			mailbox, err := pairMailbox("")
			if err != nil {
				t.Fatal(err)
			}
			sync := syncsetup.Resolve(home.Dir()).URL
			serve := pair.Relay(syncRelayURL())
			at := pair.RelayFor(pair.Known{}, false, syncRelayURL())
			if sync != c.want || mailbox.URL != c.want || serve != c.want || at != c.want {
				t.Fatalf("sync %q, pairing %q, serve %q, --at %q; want all %q", sync, mailbox.URL, serve, at, c.want)
			}
		})
	}
}

// `codeaf devices` on a machine nothing has paired with is one sentence and an
// invitation, not a heading over an empty table.
func TestDevicesOnAFreshMachineSaysNothingIsPaired(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	if err := runDevices(nil); err != nil {
		t.Fatal(err)
	}
	if err := runDevices([]string{"revoke"}); err == nil {
		t.Fatal("revoke with no name was accepted")
	}
	if err := runDevices([]string{"revoke", "laptop"}); err == nil {
		t.Fatal("revoking a device nothing has paired was accepted")
	}
	if err := runDevices([]string{"nonsense"}); err == nil {
		t.Fatal("an unknown word was accepted")
	}
}

// Contract item 7 (Finding 10): `codeaf serve --help` names the full relay
// precedence — CODEAF_RELAY, ~/.codeaf/v3/remote/relay, then sync — matching
// what pair.Relay and pair.RelayFor actually resolve.
func TestServeHelpAndRelayPrecedenceIncludeSavedRemoteRelayFile(t *testing.T) {
	var buf bytes.Buffer
	previous := usageOut
	usageOut = &buf
	t.Cleanup(func() { usageOut = previous })

	if err := runServe([]string{"--help"}); err != exitHelped {
		t.Fatalf("runServe(--help) returned %v, want exitHelped", err)
	}
	help := strings.Join(strings.Fields(buf.String()), " ")
	envPos := strings.Index(help, pair.RelayEnv)
	filePos := strings.Index(help, "~/.codeaf/v3/remote/relay")
	syncPos := strings.Index(help, "syncs through")
	if envPos < 0 || filePos < 0 || syncPos < 0 || !(envPos < filePos && filePos < syncPos) {
		t.Fatalf("serve --help did not list %s, ~/.codeaf/v3/remote/relay, then sync in order: %q", pair.RelayEnv, help)
	}

	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv(pair.RelayEnv, "")
	t.Setenv(syncsetup.URLVar, "https://sync.example")
	if err := os.MkdirAll(filepath.Dir(pair.RelayFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pair.RelayFile(), []byte("https://file.example/fabric\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := pair.Relay(syncRelayURL()); got != "https://file.example/fabric" {
		t.Fatalf("serve precedence with v3/remote/relay gave %q, want file override", got)
	}
	if got := pair.RelayFor(pair.Known{}, false, syncRelayURL()); got != "https://file.example/fabric" {
		t.Fatalf("unpaired --at precedence with v3/remote/relay gave %q, want file override", got)
	}
	paired := pair.Known{Service: "https://paired.example/fabric"}
	if got := pair.RelayFor(paired, true, syncRelayURL()); got != "https://paired.example/fabric" {
		t.Fatalf("paired --at precedence gave %q, want paired machine service over file", got)
	}
	t.Setenv(pair.RelayEnv, "https://env.example/fabric")
	if got := pair.RelayFor(paired, true, syncRelayURL()); got != "https://env.example/fabric" {
		t.Fatalf("CODEAF_RELAY precedence gave %q, want env override", got)
	}
}
