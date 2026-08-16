// Package consentui is the consent question, as a component.
//
// It renders one durable [store.AgentQuestion] at a time, collects an answer,
// and hands it back. It never touches the store, never posts a message, and
// never resolves a question: a dialog that journaled would be a second place
// consent could be recorded, and the whole point of internal/consent is that
// there is exactly one. The wiring above journals through the doors named in
// [Result].
//
// # What it is for
//
// The desk (internal/consent) is the last free moment before money moves. Until
// now the only surface that could stand at it was the old chat window's option
// list. This package is that moment drawn properly, and it carries the six
// mechanics 10.4 asks for:
//
//   - PREVIEW BEFORE CONSEQUENCE (5.20 rule 2). The dialog names the blast
//     radius in words — "cancel 4 running workers, ~$2.10 in flight" — on a row
//     of its own, above the answers. The words are the CALLER's
//     ([Presentation.Consequence]): only the caller knows what is actually in
//     flight, and a component that guessed would be estimating (10.2.8).
//   - LETTER MNEMONICS (10.4.17). Every answer wears a letter derived from the
//     question's OWN option labels — never a letter this package invented, and
//     never an answer kind the durable row does not carry. "yes, start it" is
//     `y`; an allow/session/deny row lands on `a`/`s`/`d` by the same rule, which
//     is where 10.4.17's example letters come from rather than a hardcoded set.
//     Digits work too, because a digit is the durable wire form (see [Result]).
//   - TYPED REJECTION IS STEERING (10.4.16, now law). Choosing the option that
//     means "no" opens "tell it what to do differently"; what is typed rides
//     back on [Result.Steering] as the redirect, and the wiring journals it.
//   - SCOPE SHOWN, SCOPE EDITABLE (10.4.18). An option that would whitelist
//     patterns lists the exact patterns before it is confirmed. EDITING them is
//     behind `f`'s fullscreen escalation and off the fast path — a fast path
//     that can silently widen a grant is not a fast path.
//   - A DENIED ACTION STILL RENDERS WHAT WAS REJECTED (10.4.17). The detail view
//     rides out on [Result.Detail] whatever the answer was, so the transcript
//     can keep the diff the user said no to.
//   - MULTI-QUESTION FLOW. A queue, an amber `?` count, and answering advances.
//     Esc closes the dialog WITHOUT answering and never destroys typed steering
//     text — it is stashed per question (8.2.21's draft law).
//
// # Contract with the shell
//
// [Model] satisfies tui2.Pane, tui2.PaneKeys and tui2.PaneFocus. Render(w, h)
// is a pure function of the queue and the rectangle; the shell mounts it on
// LayerOverlay with Shell.SetOverlay. Fullscreen is forced below
// [tokens.DialogFullscreenBelowWidth] / [tokens.DialogFullscreenBelowHeight]
// (10.4.17's "stated size threshold", stated in the tokens table) — `f` cannot
// turn it off there, because below those numbers there is nothing left to float
// over.
//
// # Styling
//
// Amber owns needs-a-human and nothing else (5.16): the `?` glyph, the waiting
// count, and the steer prompt. Everything else is the three-tier grey ramp.
// Selection is a background band, never a foreground colour. There is no box,
// no border, and no box inside a box — a dialog is a calm block of rows
// separated by whitespace, which is 5.13 applied to the one surface most likely
// to shout.
//
// # The verbatim law (12.5)
//
// The prompt is rendered exactly as the question row carries it, sanitized and
// wrapped and never summarized. The only thing stripped is the fenced JSON
// payload [store.QuestionMessageBody] appends for machines — that is the same
// question in machine form, not a second sentence, and [PromptOf] removes only
// a trailing fence whose contents actually parse as that payload's shape.
//
// Section numbers in comments refer to audit-notes/chat-rebuild.md.
package consentui
