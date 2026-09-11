package session

// ── WHAT A RETRY IS ABOUT ───────────────────────────────────────────────────
//
// [EventRetrying] has always carried one sentence and nothing else, which is
// enough to draw a line and not enough to draw a ROW. A surface reading it could
// not say which model was being asked, how far into its patience the step was, or
// whether the next request goes to the same model or a different one — so it drew
// the same dim line for "asking the same model again, second of four" and for
// "that model is done, the rest of this reply arrives from another one", which
// are not the same news.
//
// So the sentence keeps its job — [Event.Text] is still the whole line, and a
// surface that reads nothing else is unchanged — and these fields say the same
// thing in parts, for a surface that wants to draw them.

// RetryNews is one attempt being replaced, in parts.
//
// It rides on every [EventRetrying] this engine sends and on nothing else. Every
// field is a short fact a surface may independently ignore, and NONE of them is
// the sentence: a surface that wants words uses [Event.Text], which is written by
// the same code path and says the same thing.
type RetryNews struct {
	// Model is the model whose attempt just failed — the one that was being
	// asked, never the one about to be.
	Model string

	// Attempt and Attempts are how far into this model's patience the step is:
	// `2` of `4`. Attempts is the POLICY's number, resolved from the person's
	// settings and from the shape of the failure (internal/taxonomy), never a
	// constant a surface holds — which is the only way the row a person reads
	// and the budget the loop actually walks stay the same number.
	Attempt  int
	Attempts int

	// Reason is why, in the person's own words and never the journal's — "the
	// model would not take the request", "the model went quiet mid-reply". The
	// shapes are named once in internal/taxonomy and spelled for a person once
	// in taxonomy_boundary.go's transportWords; nothing here is a machinery
	// word, and a surface may show it as it stands.
	Reason string

	// Next is the model the step is MOVING TO, and it is empty when the step is
	// asking the same model again.
	//
	// THAT EMPTINESS IS THE WHOLE DISTINCTION a surface could not draw before.
	// A hop changes whose weights finish a reply somebody is already reading, at
	// a different price and in a different voice; a retry changes nothing except
	// that the last attempt is void. Both arrive on this one kind, and this is
	// what tells them apart.
	Next string
}
