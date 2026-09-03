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
	"time"

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
// THE TUI LANE draws it; the LastRun note is read through the STORE LANE's seam
// ([Config.SubharnessLastRun]), which is filled from the run journals that lane
// keeps beside each bundle. A build with no seam wired draws no note at all,
// which is exactly what a subharness nobody has run yet should draw — the
// emptiness law, and never "0 runs".
func (a *Agent) SubharnessList() []SubharnessRow { return a.config.subharnessRows() }

// subharnessRows is that same list asked of a CONFIG, before there is an agent
// to ask, and it is where the body lives because everything it reads is a
// config field. [Config.mayProposeSubharness] is built on it, so the render
// step can answer the propose gate's third question — is there anything on the
// registry — with the very list the belt counts (beltfacts.go).
func (c Config) subharnessRows() []SubharnessRow {
	registry := c.Subharnesses
	if registry == nil {
		return nil
	}
	history := c.SubharnessLastRun
	manifests := registry.Manifests()
	rows := make([]SubharnessRow, 0, len(manifests))
	for _, manifest := range manifests {
		if manifest.Name == exec.LinearSubharness {
			continue
		}
		row := SubharnessRow{Manifest: manifest}
		if history != nil {
			row.LastRun = strings.TrimSpace(history(manifest.Name))
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// SubharnessRunNote is what a finished run tells the store about itself, so the
// next `/subharness` list can draw a note under its row.
//
// IT IS THE FACTS AND NOT THE SENTENCE. When it ran, whether it finished, why
// not where it did not, and what it cost — and no rendering of any of them,
// because the emptiness law, the word for an unfinished run and how a cost is
// drawn are all the SURFACE's to decide. A store that rendered them would be a
// second place those three decisions are made.
type SubharnessRunNote struct {
	At       time.Time
	Finished bool
	// Why is [exec.RunResult.Incomplete] carried verbatim — the sentence was
	// written by whoever knew what ran out, and nothing between there and the row
	// is entitled to rephrase it.
	Why     string
	CostUSD float64
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
// THE BODY IS subharness_run.go's, and this is the door onto it: the name is
// resolved here so that a typo is refused before a node exists to carry it, the
// id is minted before the node is admitted (that file states why), and the pair
// that comes back is the pair a surface already knows how to open a room on.
//
// NOTHING IS ASKED HERE, because the question already happened: every path into
// this door goes through the intake card and somebody confirming it.
func (a *Agent) SubharnessRun(ctx context.Context, name string, input json.RawMessage) (uint64, string, error) {
	return a.startSubharnessRun(ctx, name, input, "")
}

// startSubharnessRun is [Agent.SubharnessRun] with chat's reason carried in. It
// is the second door rather than a fourth argument on the first because the
// signature above is the frozen contract every surface codes against, and the
// reason is filled by exactly one caller — the belt's proposal
// (tools_subharness.go), where a run is raised BY chat rather than chosen by the
// person, and the row and the room are owed one line saying why.
func (a *Agent) startSubharnessRun(ctx context.Context, name string, input json.RawMessage, why string) (uint64, string, error) {
	registry := a.config.Subharnesses
	if registry == nil {
		return 0, "", errSubharnessUnwired
	}
	runner, err := registry.Subharness(strings.TrimSpace(name))
	if err != nil {
		return 0, "", err
	}
	// A CALLER WHOSE CONTEXT IS ALREADY GONE STARTS NOTHING. The node itself runs
	// on the process's own context and outlives this call by design
	// ([Agent.runTaskNode]) — which is exactly why this has to be checked here
	// rather than left to be noticed later: an interrupted turn that admitted a
	// node on its way out would leave real work running for a sentence nobody is
	// waiting on any more.
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	manifest := runner.Manifest()
	spec := &subharnessRunSpec{
		name:  manifest.Name,
		input: input,
		why:   strings.TrimSpace(why),
		// The model is the conversation's own, taken at the moment the run
		// starts. A run that picked its own would be spending the person's money
		// on a choice they never made.
		model: a.Model(),
	}
	id := a.reserveSubharnessRun()
	a.admitSubharnessRun(id, spec, manifest)
	return id, subharnessNodeTitle(manifest.Name), nil
}
