package tui3

// ── ONE SURFACE OWNS THE KEYSTROKES ─────────────────────────────────────────
//
// A sentence typed at the message box came out as `riting the port`, `orktree`,
// `las`. The roster had been handed the keyboard (ctrl+t) and never given it
// back, and its widen binding is the bare letter `w`, read before the box —
// so every w of every sentence went to the column instead of into the words.
//
// THE LAW, WHICH THIS FILE IS THE ONE STATEMENT OF: while the message box holds
// words, every BARE PRINTABLE key on this surface stands down and the box gets
// it. Not "most of them", and not "the ones whose author remembered": a letter
// that acts while somebody is writing is a surface that decided their sentence
// was a command on the strength of its first word.
//
// It was already the house rule and it was already written down four times, by
// hand, in four files — the stop card's `x` (stop.go), the settle answers
// (tasksettle.go), the design card's `e` (harnesscard.go) and the proposal's
// former answer letters (task.go). Four copies of a rule is four chances for the
// fifth binding to be written without it, which is exactly what happened to
// `w`. So the rule is a function now and the remaining copies call it; a new
// bare letter that does not is a binding that will eat somebody's prose.
//
// ── WHAT MAY BE A BARE LETTER AT ALL ──
//
// Two kinds of key, and nothing else:
//
//  1. A key on a MODAL surface with no box on the frame — the home sheet says
//     it in as many words ("it is modal and has no box, so a letter here cannot
//     be the start of anybody's sentence"), and copy mode and the record card
//     are the same shape of thing.
//
//  2. An ANSWER TO A QUESTION THE SURFACE IS BLOCKED ON, drawn on screen with
//     its answers named, pressed over an empty box. The stop card, the settle
//     answers, the design card and the proposal are all this.
//
// A VIEW TOGGLE IS NEITHER, and `w` was the one bare letter on this surface
// that was neither: it widened a column. Nothing is blocked on it, nothing on
// screen is asking, and there is no moment at which pressing it is the only
// thing a person could have meant. So it is spelled as a chord where a box is on
// the frame ([railWidenChord]) and keeps its bare spelling only on the
// full-frame roster, where there is no box for it to steal from.
//
// THE FIRST KEYPRESS IS COVERED BY THAT SPLIT AND NOT BY THIS PREDICATE, which
// is worth saying plainly: with an empty box the guard below is false, so a
// leading `a` still answers a standing question. That is the trade rule 2 makes
// on purpose — the question is on screen, its letters are drawn, and the person
// pressing one is answering it. A leading `w` had no such question behind it,
// and that is why `w` is the binding that moved rather than the binding that
// gained a guard.

// chordsStandDown reports whether the surface's bare printable chords must stand
// down because the message box is holding words. It is asked by every bare
// letter on this surface, and it is the whole of what "the box wins" means.
func (a *app) chordsStandDown() bool { return !a.input.empty() }
