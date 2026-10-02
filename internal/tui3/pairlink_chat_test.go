package tui3

import (
	"strings"
	"testing"
	"time"
)

// A pair link carries the key that opens a fleet, so no road a person types
// into may hand it to a model. These hold the two boxes that are not home: the
// start page's first send, and a conversation that is already running.

func sentAnywhere(lab *startLab) []string { return append(lab.agent.sent, lab.next.sent...) }

// THE START PAGE IS WHERE A FRESH INSTALL LANDS, and the link pasted there is
// the approve screen's. It makes no conversation and sends nothing.
func TestPairLinkOnTheStartPageShowsTheApproveScreenAndMakesNoChat(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.approvals = &fakeApprovals{pending: spark(time.Now())}
	r := newPairRig(t, a)
	t.Cleanup(a.pair.close)
	openStart(t, a)
	typeText(t, a, testLink)
	r.feed(key("enter"))
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })

	if got := sentAnywhere(lab); len(got) != 0 {
		t.Fatalf("the link reached a model: %v", got)
	}
	if lab.made != 0 {
		t.Fatalf("the link made %d conversations", lab.made)
	}
	if got := plain(frame(a)); !strings.Contains(got, "wants to join your fleet") {
		t.Fatalf("no approve screen on the frame:\n%s", got)
	}
}

// A COMPUTER WITH NO WAY TO APPROVE ANSWERS PLAINLY, and still sends nothing.
func TestPairLinkOnTheStartPageWithNoDoorSendsNothing(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	typeText(t, a, testLink)
	drive(t, a, key("enter"))

	if got := sentAnywhere(lab); len(got) != 0 || lab.made != 0 {
		t.Fatalf("the link left the box: sent %v, made %d", got, lab.made)
	}
	if got := plain(frame(a)); !strings.Contains(got, pairUnavailableWord) {
		t.Fatalf("no plain answer on the frame:\n%s", got)
	}
}

// A RUNNING CONVERSATION'S BOX IS NO DIFFERENT.
func TestPairLinkInARunningChatShowsTheApproveScreenAndIsNotSent(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	agent := a.agent.(*fakeAgent)
	typeText(t, a, testLink)
	r.feed(key("enter"))
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	if len(agent.sent) != 0 {
		t.Fatalf("the link reached the model: %v", agent.sent)
	}
	if a.input.String() != "" {
		t.Fatalf("the link stayed in the box: %q", a.input.String())
	}
}
