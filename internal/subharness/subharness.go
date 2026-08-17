// Package subharness is the sub-harness registry as far as anything outside
// the engine has to know it: what one entry IS, and how a person's turn is
// matched against the entries this build holds (docs/SUBHARNESS.md).
//
// The engine — the node kinds, the program over them, the verify ladder, the
// run traces — lands beside this. What is here is the half a CONVERSATION
// needs, and it is deliberately the smaller half: a name, a sentence, the words
// a designer said this harness answers to, and a pure function from a turn to a
// number. Nothing in this package runs anything, reads a file, or calls a model.
package subharness

// Entry is one registry entry, in the fields detection reads. The rest of an
// entry — the program, the tool whitelist, the verify ladder, the dynamism cap,
// the run history — is the engine's and lands beside these without touching
// them.
//
// DESCRIPTION AND CUES ARE WRITTEN AT BUILD TIME, by whoever designs the
// harness, and they are what makes it findable without a slash command. That is
// the whole bargain of this package: a person says what they want in their own
// words, and a harness that was described honestly is offered. A harness
// described as "does stuff" is never offered, and that is the designer's
// answer to receive, not a defect for the matcher to paper over.
type Entry struct {
	// Name is the harness's id, its filename, and the word the offer card says
	// out loud: "research".
	Name string

	// Description is one sentence saying what this harness does, in the words a
	// person would use for it — not in the words the program uses for itself.
	// It is shown under the offer, and its content words are the second of the
	// two matching signals (detect.go).
	Description string

	// Cues are the words and phrases this harness answers to: ["research",
	// "find out", "dig into"]. They are the designer's own trigger vocabulary,
	// frozen at build time, which is what makes detection a table lookup rather
	// than a judgement — see detect.go for why that matters more than recall.
	//
	// A cue of several words matches only as a PHRASE, and counts for more than
	// a single word does: "find out" in a sentence is evidence, "find" on its
	// own is a coincidence waiting to happen.
	Cues []string

	// Revision pins the version this entry is: v1, v2, an integer that only
	// ever goes up (docs/SUBHARNESS.md). Detection does not read it — it is
	// here because an entry without it is not an entry, and a surface that
	// offers a harness should be able to say which one it offered.
	Revision int
}
