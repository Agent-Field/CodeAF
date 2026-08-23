package session

// The subharness contract, session side: the three doors every surface in the
// subharness v2 wave opens, and the types they hand over
// (docs/SUBHARNESS-PRD.md, docs/SUBHARNESS-CONTRACT.md). It is written by hand
// before the lanes start, in the shape standing_contract.go and
// standing_orders.go already proved.
//
// THE SIGNATURES ARE THE CONTRACT. The surface lanes code against them exactly
// as they stand and the bodies fill in underneath. A door with nothing behind it
// answers the way it does when this build has no subharnesses at all — nothing,
// calmly — so a page built against one draws nothing rather than an error.
//
// WHICH LANE FILLS WHICH is written on each door below. In short: the LIST and
// the INTAKE CARD are answered here already, out of the registry, because
// everything they need is a fact the registry holds and a second reading of it
// would be a second answer to "which subharnesses are there". LAUNCHING is the
// door lane's, because a run is a task node and this file may not decide what a
// task node is.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// errSubharnessUnwired is what the launching door answers on a surface that
// wired no way to run one. It is a fact rather than a refusal: there is
// genuinely nothing here to launch, and saying so is more use than a silence a
// page would have to invent a sentence for.
var errSubharnessUnwired = errors.New("there is nothing here to run")

// SubharnessRow is one line of the `/subharness` list.
//
// It carries the MANIFEST WHOLE rather than a handful of copied fields, and that
// is the one-source-of-truth law reaching a surface: the row's name, its one
// line, its cost shape and its provenance mark are all the manifest's own, so a
// list and a card drawn from the same registry can never disagree about what a
// subharness is. What this type adds is the one thing the manifest cannot know —
// what happened last time.
type SubharnessRow struct {
	Manifest exec.Manifest
	// LastRun is the dim note under the row, in a person's words: when it last
	// ran and how it went. IT IS EMPTY WHEN THERE IS NO HISTORY and the row
	// draws nothing there — never "0 runs", never "never run" (the emptiness
	// law).
	LastRun string
}

// SubharnessField is one line of the intake card: a field of the input schema,
// and what is in it so far.
type SubharnessField struct {
	// Field is the schema's own account of it — name, type, title, description,
	// whether it is required, what it defaults to.
	Field exec.Field
	// Value is what has been filled in, in its own JSON. Nil is a blank, which
	// the card draws as nothing.
	Value json.RawMessage
	// Filled says somebody or something actually put this here. It is separate
	// from a non-nil Value because a field carrying its schema DEFAULT is
	// answered without having been filled, and the card draws the two
	// differently: a default is dim, an answer is not.
	Filled bool
}

// SubharnessCard is the intake card, and it is ONE CARD FOR BOTH INTERACTIVE
// DOORS — the one chat raises when it proposes a match, and the one `/subharness`
// opens on a name. Every input field appears; the filled ones are stated, the
// required blanks are highlighted; the person edits inline or answers chat's
// batched questions, and confirming launches.
//
// GROOMING IS INFER-THEN-CONFIRM, NEVER INTERROGATE. The card is where that law
// becomes visible: what could be derived from the conversation is already in the
// fields, and the only thing anybody is asked about is what is in Missing.
type SubharnessCard struct {
	Manifest exec.Manifest
	Fields   []SubharnessField
	// Missing is the names of the REQUIRED fields still blank, in the card's own
	// field order. An empty Missing is a card that could be confirmed as it
	// stands.
	Missing []string
	// Why is chat's one line about why this subharness was raised — "the brief
	// and a failing test name are both here". It is empty on the `/subharness`
	// path, where the person chose it themselves and needs no reason given back
	// to them.
	Why string
}

// SubharnessList answers every subharness visible to this conversation, across
// every layer, in one list with the registry's own precedence already applied.
//
// IT IS THE REGISTRY'S ANSWER AND NOT A SECOND ENUMERATION
// ([exec.Registry.Manifests]). Compiled-in Go programs and bundles out of a
// store arrive here indistinguishable, which is the whole contract in one line:
// the person, the model and this list cannot tell which is which, because there
// is nothing here that says.
//
// THE GENERALIST IS NOT ON IT. `linear` is registered as a runner — the
// deoptimization path resolves it by name — but it is what you get when you pick
// nothing, not something you pick, and a list that offered it would be offering
// the absence of a choice as a choice.
//
// Nil when this build has no registry, which draws as nothing.
//
// THE TUI LANE draws it; the LastRun note is the STORE LANE's to fill, from the
// run journals it keeps beside each bundle. Until it does, every row's note is
// empty, which is exactly what a subharness nobody has run yet should draw.
func (a *Agent) SubharnessList() []SubharnessRow {
	registry := a.config.Subharnesses
	if registry == nil {
		return nil
	}
	manifests := registry.Manifests()
	rows := make([]SubharnessRow, 0, len(manifests))
	for _, manifest := range manifests {
		if manifest.Name == exec.LinearSubharness {
			continue
		}
		rows = append(rows, SubharnessRow{Manifest: manifest})
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// SubharnessIntake is the card's data for one subharness: every field of its
// input schema, what is filled, and which required ones are still blank.
//
// A NAME NOTHING HAS IS AN ERROR AND NOT AN EMPTY CARD, which is the difference
// between this door and the list beside it. Somebody typed a name; getting a
// blank card for a subharness that does not exist would send them looking for
// the fields rather than for the typo.
//
// WHAT IT DOES NOT DO YET is fill anything in. Filling the schema from the
// conversation is chat's duty (PRD §4) and it is the SESSION LANE's to build:
// one model call over the turn's material, batched down to as few questions as
// the missing fields allow. Until that lands, every field comes back blank and
// every required one comes back in Missing — which is an honest card for a
// conversation nothing has been read out of, and exactly what `/subharness
// <name>` typed cold should show.
func (a *Agent) SubharnessIntake(name string) (SubharnessCard, error) {
	registry := a.config.Subharnesses
	if registry == nil {
		return SubharnessCard{}, errSubharnessUnwired
	}
	runner, err := registry.Subharness(strings.TrimSpace(name))
	if err != nil {
		return SubharnessCard{}, err
	}
	manifest := runner.Manifest()
	card := SubharnessCard{Manifest: manifest}
	for _, field := range manifest.Input.Fields() {
		card.Fields = append(card.Fields, SubharnessField{Field: field})
		if field.Required {
			card.Missing = append(card.Missing, field.Name)
		}
	}
	return card, nil
}

// SubharnessRun launches one subharness on the input the card settled, as a task
// node, and answers the node's id and its title — the same pair
// [Agent.StartTask] answers, so a surface that already knows how to open a room
// on a started task needs no second call site.
//
// A RUN IS A TASK NODE: a roster row, a room, a journal fed by the program's own
// log() and by the host journal underneath it, an id, and a ✕ that cancels it
// through the route every other task uses. That fixes today's asymmetry, where
// DESIGNING a harness is a task and RUNNING one blocks the conversation as a
// turn (harness.go) — the thing a person most wants to walk away from is the
// one thing they cannot.
//
// THE TASK SURFACE IS A PRESENTATION OF A RUN AND NOT ITS DEFINITION. The
// headless path runs the same program through the same runner with no task
// system in the process at all, which is why the contract this door sits on
// ([exec.Runner]) knows nothing about tasks and this door knows everything.
//
// THE DOOR LANE FILLS THIS ONE — reserving the node, spinning the run against
// the registry's runner with the session's Env behind it, wiring the journal to
// the room and the cancel route to the run's context. It answers
// errSubharnessUnwired until then, which is what a surface with no registry
// wired would answer forever.
func (a *Agent) SubharnessRun(ctx context.Context, name string, input json.RawMessage) (uint64, string, error) {
	registry := a.config.Subharnesses
	if registry == nil {
		return 0, "", errSubharnessUnwired
	}
	if _, err := registry.Subharness(strings.TrimSpace(name)); err != nil {
		return 0, "", err
	}
	// The name resolves and the input is in hand; what is missing is the half
	// that turns a run into a node, and it is not this lane's to write.
	return 0, "", errSubharnessUnwired
}
