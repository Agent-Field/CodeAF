// Package placeorg organises a person's chats into places: it builds the tree
// the first time, files each new chat as it arrives, guesses where a draft is
// going while it is typed, learns from the person's corrections, follows the
// person's steering, and proposes changes to the tree over time. It reads and
// proposes; internal/placegraph is the store that holds what the person has.
//
// THE ONE LAW, STATED AS TYPES: CLASSICAL MATH OWNS STRUCTURE, THE MODEL OWNS
// SEMANTICS. Every stage is a deterministic, seeded, cheap operation that
// narrows the world to a few options. A model enters only through the Judge,
// which can do exactly two things: choose among options it was shown, or write
// a short text under a validator. Both take a fallback as a REQUIRED argument
// and neither can fail: when the model is down, slow, over budget or answers
// outside the contract, the fallback is the answer and the journal says so.
// Nothing a model writes becomes an id, a path or a permission.
//
// STAGES PROPOSE, THE RUNNER APPLIES. A stage returns Ops; it cannot touch the
// Tree, whose mutators are unexported. The Runner applies each Op through one
// door that journals it, and checks every Invariant before a Plan may leave
// this package. A Plan is applied to the store in one commit, so one undo
// takes a whole organising run back.
//
// WHAT IS KEPT IN internal/placegraph AND WHAT THIS PACKAGE REPLACES is laid
// out in ARCHITECTURE.md beside this file. The structural test in
// contract_law_test.go keeps every sub-package to this contract: no package
// here reaches a provider, a network or the engine; every package that asks a
// Judge declares its fallbacks; every call to the Judge passes one.
package placeorg
