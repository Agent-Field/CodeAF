//go:build e2e

package e2e

import (
	"os/exec"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// A FRESH INSTALL THAT WAS ONLY STARTED. The person ran codeaf, looked at the
// start screen and quit: nothing they made is on the computer, so it can still
// join another computer's chats. The first half of the test is that fact read
// off the real binary's own files; the second is that a pair link typed at the
// start screen goes to the approve screen and not to a model.
func TestFreshStartIsPristineAndAPairLinkIsNotAChatMessage(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}
	env := []string{config.APIKeyEnv + "=not-a-real-key", "CODEAF_CELLS=1", "CODEAF_SYNC_URL=" + startRelay(t), "CODEAF_TELEMETRY=off"}
	home := emptyHome(t)
	ws := newWorkspace(t, "fresh", false)
	r := startWithEnv(t, env, "fresh", home, ws, 120, 40)
	r.skipSetup(t)

	r.paste("https://codeaf.agentfield.ai/p/k7m2q9xd#Qm9v")
	r.keys("Enter")
	// Whichever way the answer goes (a request to approve, one that ran out, or
	// no way to approve here) it is a pairing answer; a model's turn is not.
	var screen string
	for _, answer := range []string{"wants to pair", "request has run out", "pairing is not available"} {
		if got, ok := waitPlain(r, 5*time.Second, answer); ok {
			screen = got
			break
		}
	}
	if screen == "" {
		t.Fatalf("the link got no pairing answer:\n%s", plain(r))
	}
	r.keys("Escape")
	r.quit()

	if ok, err := identity.Pristine(home); err != nil || !ok {
		t.Fatalf("a computer that was only started is not pristine: %v %v", ok, err)
	}
}
