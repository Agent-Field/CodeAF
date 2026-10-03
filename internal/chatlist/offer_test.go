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
		Off:     "studio offline",
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
	if got, want := TakeoverLine(r), "last saved turn 42s ago; up to 3 turns may still be on studio"; got != want {
		t.Fatalf("got %q", got)
	}
	r.Pending = 0
	if got, want := TakeoverLine(r), "last saved turn 42s ago"; got != want {
		t.Fatalf("got %q", got)
	}
}

// A chat that runs now says where before how fresh the copy is, because
// continuing it here stops it there.
func TestTakeoverLineNamesTheDeviceARunningChatIsOn(t *testing.T) {
	r := Row{Status: Running, Device: "studio", DurableAgo: 12 * time.Second}
	if got, want := TakeoverLine(r), "running on studio; last saved turn 12s ago"; got != want {
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
		NoIdentity:   "this machine has no identity yet: codeaf identity import",
		Unreachable:  "other machines unreachable",
		ClockOff:     "this computer's clock is off by more than 5 minutes",
		ReplacedGone: "your identity was replaced and sync has deleted the old one; pair this computer again (/pair on a computer that has the new one)",
		Replaced:     "your chats are moving to a new identity; when that is done, pair this computer again (/pair on the computer that moved them)",
		Removed:      "this device was removed by another of your devices, so this chat stays here only — run `codeaf pair` to bring it back",
		SlowDown:     "sync is asking this computer to slow down; new turns stay here and go up as soon as it allows",
		TooManyNew:   "this network has started too many new identities today; sync begins when it allows more",
	} {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestRelayFullNamesTheCeilingOnlyWhenTheRelayDid(t *testing.T) {
	for limit, want := range map[int64]string{
		0:         "your sync space is full, so new turns stay on this computer; free space there and reopen this chat",
		5 << 30:   "your sync space is full (5 GiB), so new turns stay on this computer; free space there and reopen this chat",
		3 << 29:   "your sync space is full (1.5 GiB), so new turns stay on this computer; free space there and reopen this chat",
		512 << 20: "your sync space is full (512 MiB), so new turns stay on this computer; free space there and reopen this chat",
	} {
		if got := RelayFull(limit); got != want {
			t.Errorf("RelayFull(%d) = %q, want %q", limit, got, want)
		}
	}
}

func TestMovedSaysTheMeasuredTime(t *testing.T) {
	got := Moved("spark", 3200*time.Millisecond, true)
	want := "Moved from spark in 3.2s. Everything as you left it. What was running there can start again here."
	if got != want {
		t.Fatalf("got %q", got)
	}
	if got := Moved("", 1500*time.Millisecond, false); got != "Moved here in 1.5s. Everything as you left it." {
		t.Fatalf("got %q", got)
	}
}

// A chat whose holder is offline stopped; its sentence does not say it runs.
func TestTakeoverLineOfAQuietRowDoesNotSayRunning(t *testing.T) {
	r := Row{Status: Running, Device: "build-box", DurableAgo: 15 * time.Second}.Quiet()
	if got, want := TakeoverLine(r), "last saved turn 15s ago"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTakeoverLineSpellsAgeInReadableUnits(t *testing.T) {
	for age, want := range map[time.Duration]string{
		42 * time.Second: "42s", 90 * time.Second: "1m", 3 * time.Hour: "3h", 50 * time.Hour: "2d",
	} {
		if got := TakeoverLine(Row{DurableAgo: age}); got != "last saved turn "+want+" ago" {
			t.Errorf("%v: %q", age, got)
		}
	}
}
