//go:build e2e

package e2e

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Home's navigation and submission choices must work before a provider is
// connected. This drives the shipped binary without sending a model request.
func TestHomeRestoredNavigationNoModel(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH")
	}
	for _, width := range []int{180, 120, 44} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			home := newHome(t, nil)
			seedProject(t, home, "alpha", 0, time.Minute)
			ws := newWorkspace(t, "navigation", false)
			r := startFresh(t, "home_restore", home, ws, width, 40, "chat", "--no-host")
			r.skipSetup(t)
			r.waitFor(20*time.Second, say(t, "placeRestWord"))

			r.keys("Escape")
			r.waitFor(15*time.Second, say(t, "homeDoorWord"))
			r.keys("Space", "Space")
			r.waitFor(15*time.Second, say(t, "placeRestWord"))

			r.lit("Seed")
			screen := r.waitFor(15*time.Second, say(t, "homeAskHereWord"), say(t, "homeStartWord"), "Seed Alpha")
			match := strings.Index(screen, "Seed Alpha")
			ask := strings.Index(screen, say(t, "homeAskHereWord"))
			start := strings.Index(screen, say(t, "homeStartWord"))
			box := strings.LastIndex(screen, "› Seed")
			if !(match < ask && ask < start && start < box) {
				t.Fatalf("search, ask, new conversation and box are out of order:\n%s", screen)
			}
			if width > 44 {
				r.keys("Up")
				r.waitFor(10*time.Second, "enter asks this here")
				r.keys("Down")
				r.waitFor(10*time.Second, "enter starts a new conversation")
			}
			t.Logf("restored choices at %d columns:\n%s", width, screen)

			// Clearing the draft and closing Home are separate Escape presses.
			r.keys("Escape")
			r.waitFor(10*time.Second, say(t, "placeRestWord"))
			r.keys("Escape")
			r.waitFor(10*time.Second, say(t, "homeDoorWord"))
			r.keys("Escape")
			if screen := r.capture(); strings.Contains(screen, say(t, "placeRestWord")) {
				t.Fatalf("Escape reopened Home:\n%s", screen)
			}
			r.keys("Space", "Space")
			r.waitFor(10*time.Second, say(t, "placeRestWord"))
			r.lit("/ask")
			screen = r.waitFor(10*time.Second, "/task")
			if strings.Contains(screen, "ask here on home") {
				t.Fatalf("the removed /ask command is still offered:\n%s", screen)
			}
		})
	}
}

// Both submission rows cross the real surface-to-engine boundary, with a local
// endpoint providing a fixed answer so no provider account is needed.
func TestHomeRestoredSubmissionDoorsWithStub(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH")
	}
	stub := &stopStub{kind: parkOnStream}
	base := serveStopStub(t, stub)
	stub.stop()
	home := newHome(t, map[string]any{"model.talk": "stub/bounded"})
	seedProject(t, home, "alpha", 0, time.Minute)
	ws := newWorkspace(t, "submission", false)
	r := startWithEnv(t,
		[]string{"OPENROUTER_API_KEY=stub-key", "CODEAF_BASE_URL=" + base, "CODEAF_PROFILE_DIR="},
		"home_submission", home, ws, 180, 40, "chat", "--no-host", "--one-model")
	r.skipSetup(t)
	r.waitFor(20*time.Second, say(t, "placeRestWord"))
	r.lit("answer in the home pane")
	r.waitFor(10*time.Second, say(t, "homeStartWord"))
	r.keys("Up")
	r.waitFor(10*time.Second, "enter asks this here")
	r.keys("Enter")
	pane := r.waitFor(20*time.Second, stopStubDone, say(t, "exchangeBack"))
	t.Logf("ask here received the endpoint's answer in its own pane:\n%s", pane)
	r.keys("Escape")
	r.waitFor(10*time.Second, say(t, "placeRestWord"))
	r.lit("answer in a new conversation")
	r.waitFor(10*time.Second, say(t, "homeStartWord"))
	r.keys("Enter")
	conversation := r.waitFor(20*time.Second, stopStubDone, "idle", "› answer in a new conversation")
	if strings.Contains(conversation, say(t, "exchangeBack")) {
		t.Fatalf("the new-conversation row left the answer in an ask pane:\n%s", conversation)
	}
	t.Logf("the default row opened a conversation and received the answer:\n%s", conversation)
	r.keys("Space", "Space")
	r.waitFor(10*time.Second, say(t, "placeRestWord"))
}
