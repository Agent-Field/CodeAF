// Package footer is the contextual line: the ONE row under the composer, in
// the three zones design-law-v2 §7 gives it.
//
//	▐ chat ▌  work  notebook   interrupt esc · 12s · $0.03   ~/a/v2 · ▂ 3% of 262K · $0.31 today
//	▐ chat ▌  work  notebook   ‹ … ‹ Higher-order… · answer 1—3          ~/a/v2 · $0.31 today
//
// # The three zones
//
//	LEFT   PLACES — `chat  work  notebook` as clickable words, the one you are
//	       in wearing a filled pill (`▐ chat ▌`: a raised chip ground with
//	       half-block caps, so the ends read as rounded without a border being
//	       drawn) and the rest bare and dim. This is the homes door, moved off
//	       the sidebar and into the hug. It renders from [FocusContext.Places],
//	       which the host supplies, so nothing about what a place contains is
//	       known here. THE TABS ARE PERMANENT: they do not yield to the trail,
//	       and see [FocusContext.ScopeTail] for the reader-reported reason.
//	       Profiles with no trustworthy raised ground draw no pill and mark the
//	       current word by tier alone.
//
//	MIDDLE ONLY WHAT IS TRUE RIGHT NOW: the breadcrumb when the reader is
//	       inside something (`‹ … ‹ Higher-order…`, eliding ancestors first —
//	       scope.go), `interrupt esc` while a turn is streaming with that
//	       turn's own elapsed and cost beside it, `answer 1—3` while a question
//	       is open, a coral sentence when the last send failed, and whatever
//	       live verbs the host hands in. Chips come through
//	       [registry.ChipOn]/[registry.ChipFor]. OTHERWISE EMPTY. The silence is
//	       the design: every standing legend this row used to carry (`ctrl+c
//	       stop or quit`, `alt+enter insert newline`, `ctrl+r toggle receipts`,
//	       `? help`) belongs to the `?` sheet, which is the surface built to
//	       teach doors.
//
//	RIGHT  STANDING FACTS, dim, right-aligned to the edge (§16: the right edge
//	       is a column): the pending system state, where the work lands on disk
//	       in placeline's abbreviated form, the context gauge (one filling cell,
//	       its percentage, and the window it is a percentage OF — amber past the
//	       warn point, absent entirely when the window is unknown), and `$X.XX
//	       today` — §13 puts the day total in the bottom bar and nowhere else.
//	       Absent spend renders as absence, never `$0.00`. THERE IS NO MODEL
//	       WORD; see [FocusContext] for why an identifier may not be printed
//	       here.
//
// # Degradation
//
// One row, ever, and the ladder is a sentence read straight down: the MIDDLE
// empties first (it is silent most of the time anyway, so losing it costs the
// reader nothing they did not have a moment ago), then the health notice, then
// the gauge's window word, then the directory, then the PLACES collapse to the
// current word alone, then the gauge, then the day's money. The last thing
// standing is where you are.
//
// The right zone is fitted as SEPARATE COLUMNS rather than as a block, which is
// what lets `of 262K` leave while `▂ 3%` stays; the left zone is measured at its
// floor and grows back into whatever the other zones did not need, and the
// trail is measured the same way, so it elides its ancestors before it gives up
// its own name. The drop pass itself is [tokens.FitFooter]: the mechanic lives
// in tokens and is not restated here.
//
// # FocusContext: the host's contract
//
// [FocusContext] is a plain struct the host fills once per frame from whatever
// pane holds focus. It is deliberately NOT state this package remembers between
// renders: focus context changes on nearly every keystroke and every stream
// tick, so a render-time parameter is the honest shape, not a Set call the
// caller would have to keep in sync.
//
// Every field is honest by construction: a field left at its zero value draws
// nothing rather than drawing an empty cell — the affordance never lies, and
// that includes lying by presence. Two fields carry a narrower meaning than
// their names once did, and both are documented where they are declared:
// [FocusContext.Input] reaches this row only as [InputFailed] (§8 gives every
// other input state to the composer's own ghost text), and
// [FocusContext.Attention] no longer draws a badge — it TINTS the answer chip
// amber, because that chip is already the statement that a human is needed.
//
// # Contract with the shell
//
// [Model] is built with [New]; [Model.Render] is a pure function of a
// [FocusContext] and a width, returning at most one row, never panicking and
// never exceeding the width it was given, down to w=1. It paints CONTENT only —
// the ground under this row is the seam's, and the seam belongs to the lane that
// owns the strip.
//
// [Model.Targets] and [Model.TargetAt] answer the pointer from the same layout
// the paint uses (hit.go), so a click can never land on a word the paint had
// dropped or moved.
package footer
