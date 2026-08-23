package orchestrate

import (
	_ "embed"
	"strconv"
	"strings"
)

// plannerLaw is the document as it is written and diffed. [PlannerPrompt] is
// this with its one figure filled in.
//
//go:embed prompt.md
var plannerLaw string

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
// ONE PLACEHOLDER, AND IT IS A CAP THE CODE ENFORCES. This document describes a
// CONTRACT — the shapes in orchestrate.go — and a contract has no numbers to
// drift, with one exception: [NameWords], the length a node's title may run to,
// which is also the length the column drawing it cuts to ([NodeTitle] holds a
// planner to it). A figure a model reasons with must be the figure the code
// applies, so it is filled from the constant rather than typed into the prose,
// exactly as the designer's guide fills its caps. What could also drift is the
// Amendment schema quoted in PART THREE, and a test in this package holds that
// against the struct tags rather than a renderer.
//
// It is short on purpose. The designer is called once per design; the planner is
// called once at the start and once on EVERY node completion, so every paragraph
// here is billed against the run's one tank as many times as the run is wide.
var PlannerPrompt = strings.ReplaceAll(plannerLaw, "{{NAME_WORDS}}", strconv.Itoa(NameWords))
