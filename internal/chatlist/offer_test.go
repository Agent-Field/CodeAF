package chatlist

import (
	"strings"
	"testing"
	"time"
)

func TestOfferCopy(t *testing.T) {
	cases := []struct {
		row  Row
		want Offer
	}{
		{Row{Status: Off, Device: "studio"}, Offer{ContinueHere, "continue here"}},
		{Row{Status: Running, Device: "studio"}, Offer{ContinueHere, "continue here"}},
		{Row{Status: Branch, Device: "studio", OrphanTurns: 3}, Offer{Merge, "3 turns from studio: merge / discard"}},
		{Row{Status: Idle}, Offer{Kind: Open}},
		{Row{Status: Here}, Offer{Kind: Open}},
	}
	for _, tc := range cases {
		if got := OfferFor(tc.row); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.row.Status, got, tc.want)
		}
	}
}

func TestStatusLineCopy(t *testing.T) {
	cases := map[Status]string{
		Running: "running on studio",
		Off:     "studio off",
		Branch:  "2 turns from studio: merge / discard",
		Idle:    "",
		Here:    "",
	}
	for st, want := range cases {
		if got := StatusLine(Row{Status: st, Device: "studio", OrphanTurns: 2}); got != want {
			t.Errorf("%s: got %q, want %q", st, got, want)
		}
	}
}

func TestTakeoverLineOmitsClauseAtZero(t *testing.T) {
	r := Row{Device: "studio", DurableAgo: 42*time.Second + 900*time.Millisecond, Pending: 3}
	if got, want := TakeoverLine(r), "last durable turn 42s ago; up to 3 turns may still be on studio"; got != want {
		t.Fatalf("got %q", got)
	}
	r.Pending = 0
	if got, want := TakeoverLine(r), "last durable turn 42s ago"; got != want {
		t.Fatalf("got %q", got)
	}
}

// A chat that runs now says where before how fresh the copy is, because
// continuing it here stops it there.
func TestTakeoverLineNamesTheDeviceARunningChatIsOn(t *testing.T) {
	r := Row{Status: Running, Device: "studio", DurableAgo: 12 * time.Second}
	if got, want := TakeoverLine(r), "running on studio; last durable turn 12s ago"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCopiesCameSaysNothingForNone(t *testing.T) {
	for names, want := range map[string]string{
		"":      "",
		"n1":    "the working copy of a task came along: n1",
		"n1,n2": "working copies of tasks came along: n1, n2",
	} {
		var in []string
		if names != "" {
			in = strings.Split(names, ",")
		}
		if got := CopiesCame(in); got != want {
			t.Errorf("CopiesCame(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFrozenSentences(t *testing.T) {
	if got := KeptEdits(2, "studio"); got != "your unsaved edits here were kept as 2 turns from studio" {
		t.Error(got)
	}
	if got := Superseded("studio"); got != "studio continued this chat; this window now only shows it" {
		t.Error(got)
	}
	for got, want := range map[string]string{
		NoIdentity:  "this machine has no identity yet: codeaf identity import",
		Unreachable: "other machines unreachable",
		ClockOff:    "this computer's clock is off by more than 5 minutes",
		Replaced:    "your chats are moving to a new identity; when that is done, pair this computer again (/pair on the computer that moved them)",
	} {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
