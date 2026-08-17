package orchestrate

import _ "embed"

// PlannerPrompt is the planner's law, held as prose beside the contract it
// describes.
//
// WHY AN ASSET AND NOT A STRING BUILDER: the ten laws are an ARGUMENT — about
// what a node is for, when an edge is real, and why saying nothing is an answer
// — and an argument is written in paragraphs. Held in Go it was held in
// fragments, every line wearing quotes and an escape, and the shape of it was
// invisible to the person changing it. Held here it is a document, diffed as a
// document, editable by somebody who is not going to open a compiler. This is
// internal/subharness/prompts' arrangement, for its reasons.
//
// WHY NO PLACEHOLDERS: the designer's guide is filled from the package that owns
// its numbers, because it describes caps. This one describes a CONTRACT — the
// shapes in orchestrate.go — and there is no number in it to drift. What could
// drift is the Amendment schema quoted in PART THREE, and a test in this package
// holds it against the struct tags rather than a renderer.
//
// It is short on purpose. The designer is called once per design; the planner is
// called once at the start and once on EVERY node completion, so every paragraph
// here is billed against the run's one tank as many times as the run is wide.
//
//go:embed prompt.md
var PlannerPrompt string
