package tui3

// `thinking` — HOW HARD THE THING UNDER THE CURSOR IS ASKED TO THINK.
//
//	thinking high
//
// One clause, on the two cards where a rung on the effort ladder is a fact about
// the subject rather than about a conversation: THE MACHINE'S OWN CARD, where it
// is the install's default (config's `effort` row, the last scope the resolver
// consults), and A STANDING ITEM'S CARD, where it is that item's own rung.
//
// ── WHY IT IS A BAND OF ITS OWN ──
//
// Every other band on the machine's card answers a question about what has
// HAPPENED: `keeping an eye on` is what will wake, `agents` is the shape of the
// last few minutes, `since you left` is what went on while you were away, and
// `today` is the day summed. A setting is none of those, and putting it inside
// one of them would have been a fact filed under a heading that does not cover
// it — `today` in particular, whose emptiness law drops the whole band on a
// machine nobody has worked on, which is exactly the morning this rung is most
// worth reading.
//
// It sits ABOVE `today` deliberately. `today` is the card's closing arithmetic
// and has been the last band on it since the card existed (homeband_spark.go
// says the whole of why); a glance ends on the day's figures, and a setting
// wedged under them would have moved what the card finishes on.
//
// ── AND IT IS THE FACT, NEVER THE KEY ──
//
// `ctrl+v` moves this rung on both cards and is named in the card's own legend
// (homeband_keys.go), where every other chord on this screen is named. This band
// states what IS; the legend states what the keyboard does. Saying the key here
// as well would be the card teaching one gesture twice, three rows apart.
//
// ── THE EMPTINESS LAW, BOTH WAYS ──
//
// An item nobody has set a rung on draws NOTHING — its firings run on the
// standing role's own floor and "nobody said" is not a rung to print. An install
// whose `thinking` row says `off` draws nothing either, for the same reason and
// with the same honesty: nothing extra is being asked of the model, so there is
// no depth to state. In both cases the legend still names the key, because the
// key still works — it is how a person gets OFF absence.

import (
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/effort"
)

func init() {
	registerHomeBand(homeBand{
		name:  "thinking",
		order: bandOrderThinking,
		kinds: []bandKind{bandKindMachine, bandKindItem},
		draw:  drawThinkingBand,
	})
}

func drawThinkingBand(a *app, ctx bandContext) []string {
	rung, where := a.bandRung(ctx.subject)
	clause := effortClause(rung)
	if clause == "" {
		return nil
	}
	// THE CLAUSE IS FURNITURE UNTIL IT MOVES, and then it is the reading ladder's
	// ramp and nothing else — no second colour and no ground under it
	// (effortscope.go's header).
	return bandClauses(ctx.width, 0, a.effortInk(where, ctx.pal), clause)
}

// bandRung is the rung one card's subject is set to, and the name the flash
// keys on. An empty name is a subject that has no rung to state.
func (a *app) bandRung(subject bandSubject) (effort.Rung, string) {
	switch subject.kind {
	case bandKindMachine:
		dir, ok := a.effortProfile()
		if !ok {
			return effort.None, ""
		}
		return config.DefaultEffortAt(dir), machineSubjectID
	case bandKindItem:
		item := subject.itemOrNil()
		if item == nil {
			return effort.None, ""
		}
		// A word this build cannot parse reads as absence rather than as a
		// refusal, which is the same leniency every other reader of a stored rung
		// keeps (internal/effort's Parse).
		rung, _ := effort.Parse(item.Does.Effort)
		return rung, item.ID
	}
	return effort.None, ""
}
