package teams

import (
	"errors"
	"strings"
	"time"
)

// ── WRAP UP FIRST, AND THE CLOSE THAT FOLLOWS A CLOSING REPORT ──────────────
//
// The ruling (c-9): when a team with running work is closed, the person's
// default is `Wrap up first`. The manager tells its members to finish and
// commit, answers what it can, and brings the person a closing report as a
// [PacketClosing] packet; the team closes only when the person accepts it.
//
// THE REQUEST IS ONE LINE OF TRAFFIC, because Traffic is the only channel
// between the interface and the session (section 5 of the design): a
// [KindDirective] from [FromYou] to [ToManager] whose State is [StateWrapUp].
// State, not the words, is the marker, so the interface can say whatever it
// likes in Text and a reader never matches prose. [WrapUpRequest] builds it
// and [IsWrapUp] reads it; both sides use these two and nothing else.
//
// ACCEPTING THE REPORT CLOSES THE TEAM, through [AcceptClosing], which both
// the interface (on the person's click) and the manager's session (on reading
// the decision) may call: it is idempotent, so whichever runs second finds the
// team closed and does nothing.

// StateWrapUp marks a [KindDirective] as the person's `Wrap up first`.
const StateWrapUp = "wrap-up"

// WrapUpText is the words a wrap-up request carries when the interface gives
// none.
const WrapUpText = "Wrap up first: ask everyone to finish and commit, then bring me a closing report."

// WrapUpRequest is the Traffic entry the interface appends to a team's log to
// ask its manager to wrap up. text may be "" for [WrapUpText].
func WrapUpRequest(text string) Entry {
	if strings.TrimSpace(text) == "" {
		text = WrapUpText
	}
	return Entry{Kind: KindDirective, From: FromYou, To: ToManager, State: StateWrapUp, Text: text}
}

// IsWrapUp reports whether e is the person's wrap-up request.
func IsWrapUp(e Entry) bool {
	return e.Kind == KindDirective && e.From == FromYou && e.State == StateWrapUp
}

// ErrNotClosing is [AcceptClosing] handed a packet that is not a closing
// report.
var ErrNotClosing = errors.New("teams: that packet is not a closing report")

// AcceptClosing closes the team a decided closing packet was raised from when
// its decision is [OptionClose] or [OptionCloseNow], records the packet as the
// team's report, and appends a [KindClose] line to its Traffic. It reports
// whether this call closed the team: false for a decision to keep going, for
// a team already closed (by the other side, or by hand), and for a packet
// still waiting.
func AcceptClosing(profileDir string, p Packet) (bool, error) {
	if p.Kind != PacketClosing {
		return false, ErrNotClosing
	}
	if p.State != PacketDecided || (p.Decision != OptionClose && p.Decision != OptionCloseNow) {
		return false, nil
	}
	closed := false
	name := ""
	err := Update(profileDir, func(f *File) error {
		t, ok := f.Team(p.Origin)
		if !ok || t.Closed() {
			return nil
		}
		if err := f.Close(p.Origin, time.Now(), p.ID); err != nil {
			return err
		}
		closed, name = true, t.Name
		return nil
	})
	if err != nil || !closed {
		return false, err
	}
	by := FromYou
	if p.DecidedBy != Person && p.DecidedBy != "" {
		by = FromManager
	}
	_ = AppendTraffic(profileDir, p.Origin, Entry{Kind: KindClose, From: by, To: ToEveryone,
		Packet: p.ID, Text: "closed " + name + " on its closing report"})
	return true, nil
}
