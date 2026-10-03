package tui3

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// While an answer is on its way the card says so and stops offering a and d,
// which a press cannot use; it used to show a bare `esc close` over a card that
// still looked undecided.
func TestApproveSaysItIsWorkingAndOffersNoKeysMeanwhile(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	r.slash("/pair " + testLink)
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	c := a.pair.card.(*approveCard)
	c.key(a, "d")
	if got := plain(frame(a)); !strings.Contains(got, approveDeclining) || strings.Contains(got, "a approve") {
		t.Fatalf("no working line:\n%s", got)
	}
}

// A request that has run out offers no a or d, and says why.
func TestApproveOffersNoKeysOnceTheRequestRanOut(t *testing.T) {
	old := spark(time.Now())
	old.ExpiresAt = time.Now().Add(-time.Minute)
	door := &fakeApprovals{pending: old}
	a, r := approveApp(t, door)
	r.slash("/pair " + testLink)
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	got := plain(frame(a))
	if !strings.Contains(got, approveGone) || strings.Contains(got, "a approve") {
		t.Fatalf("expired card:\n%s", got)
	}
	r.press("a")
	if door.did() != "pending:"+testLink {
		t.Fatalf("a decided an expired request: %s", door.did())
	}
}

// The list's keys are the ones the row under the cursor has, and the columns line up.
func TestDeviceListKeysFollowTheRowAndColumnsAlign(t *testing.T) {
	a, r := renameApp(t, devicesFake())
	c := a.pair.card.(*deviceCard)
	if got := c.hint(); strings.Contains(got, "remove") {
		t.Fatalf("this device's row offers remove: %q", got)
	}
	r.press("down")
	if got := c.hint(); !strings.Contains(got, "r remove") {
		t.Fatalf("another row lacks remove: %q", got)
	}
	var col int
	for _, line := range strings.Split(plain(frame(a)), "\n") {
		for _, word := range []string{"Mac", "Linux"} {
			if i := strings.Index(line, "  "+word); i >= 0 && (strings.Contains(line, "this mac") || strings.Contains(line, "dumb") || strings.Contains(line, "spark")) {
				if col == 0 {
					col = utf8.RuneCountInString(line[:i])
				} else if utf8.RuneCountInString(line[:i]) != col {
					t.Fatalf("system column ragged (%d vs %d):\n%s", i, col, plain(frame(a)))
				}
			}
		}
	}
	if got := plain(frame(a)); !strings.Contains(got, "seen 3h ago") || !strings.Contains(got, "spark     Linux  offline") {
		t.Fatalf("away rows do not say offline:\n%s", got)
	}
}

// A pair link in the box names what enter does.
func TestPairLinkInTheBoxHintsEnter(t *testing.T) {
	a, _ := approveApp(t, &fakeApprovals{pending: spark(time.Now())})
	typeText(t, a, testLink)
	if got := a.hintWord(); got != "enter opens the join request" {
		t.Fatalf("hint = %q", got)
	}
}
