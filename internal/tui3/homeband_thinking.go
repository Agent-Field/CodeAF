package tui3

// `thinking` — HOW HARD THE THING UNDER THE CURSOR IS ASKED TO THINK.
//
//	thinking high
//
// One clause, on the one card where a rung on the effort ladder is a fact about
// the subject rather than about a conversation: A STANDING ITEM'S CARD, where it
// is that item's own rung.
//
// IT USED TO DRAW ON A SECOND CARD. Home had a resting state — the cursor walked
// up off the top of the list onto no row at all and the column became a card
// about the machine — and this band stated the install's own default there. That
// state is retired: `↑` off the top row reaches the tab bar now (pages.go's
// [barCursor]), so there is no card about the machine for a band to draw on. The
// install's default is still a fact and is still moved from the one place that
// has always owned it, the `thinking` row of the settings panel.
//
// ── WHY IT IS A BAND OF ITS OWN ──
//
// A card's other bands answer a question about what has HAPPENED — what a
// standing item last did, when it will wake, what it has cost. A setting is none
// of those, and putting it inside one of them would be a fact filed under a
// heading that does not cover it.
//
// ── AND IT IS THE FACT, NEVER THE KEY ──
//
// `ctrl+v` moves this rung and is named in the card's own legend
// (homeband_keys.go), where every other chord on this screen is named. This band
// states what IS; the legend states what the keyboard does. Saying the key here
// as well would be the card teaching one gesture twice, three rows apart.
//
// ── THE EMPTINESS LAW ──
//
// An item nobody has set a rung on draws NOTHING — its firings run on the
// standing role's own floor and "nobody said" is not a rung to print. The legend
// still names the key, because the key still works: it is how a person gets OFF
// absence.

import "github.com/Agent-Field/aforge-v2/internal/effort"

func init() {
	registerHomeBand(homeBand{
		name:  "thinking",
		order: bandOrderThinking,
		kinds: []bandKind{bandKindItem},
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
