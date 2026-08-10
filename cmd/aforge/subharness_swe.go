package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// The swe worker's registration: what it is for, how much of a job fits in one
// of them, and how long one may take.
//
// docs/SUBHARNESSES.md, "Canonical choice prompts for `swe`", is the source of
// truth for the two long strings below. They are written there once, in prose,
// beside the reasoning that produced them, and carried here verbatim — the
// test beside this file reads the document and fails if the two ever part
// company. Editing them here alone is how a prompt drifts away from the
// paragraph that explains it.
//
// They are priors and nothing more. The anchors are the *initial* setting of
// this worker's hardness; the first measured leaves start replacing them
// through profile.NeedsRecalibration and plan.Recalibrate, per worker, exactly
// as linear's have always been replaced.

// sweMenuEntry is the compiler-menu paragraph as the document writes it — with
// the worker's name in front, because the document shows the entry as a model
// will read it. MenuText supplies that name itself, so the registration below
// hands it the rest.
const sweMenuEntry = `swe — an end-to-end software-engineering pipeline for changing code in a
real repository. Give it a coding issue whole — a feature, a bug fix, a
refactor with its tests — and it plans internally, edits in parallel git
worktrees, judges each change before merging, and verifies the result
against the repository's own build and tests before calling itself done.
Choose it when the work must be discovered rather than merely made: a bug
whose cause is not yet located, a refactor that crosses the codebase, an
issue that demands substantial new test surface. Do not choose it when the
change is already located and specified — a well-described edit in a
handful of files is the default worker's job even when it is a whole
feature — nor when the deliverable is prose or analysis about code rather
than a change to it, nor when no repository's tests or build could say
whether the job is done.`

// swePriorAnchors is the capacity ruler: comfortably atomic, borderline,
// oversized, in the voice of plan/size.go's own three worked examples.
const swePriorAnchors = `Use these three reference tasks to judge scale against the swe worker. They
are the ruler; place the node against them rather than estimating it on its
own.

TOO SMALL — a change already located and specified, however complete. Fix a
  typo'd flag; correct one function when the failing test names it; add a
  well-described option that touches a handful of files. The default worker
  finishes this in minutes; this pipeline's planning and verification would
  cost more than the change.

RIGHT — one coding issue taken whole, however many files it touches.
  Implement a described feature along with the tests that prove it; hunt
  down and fix a bug whose cause is not yet located; carry a refactor
  through an interface and every call site, keeping the suite green. One
  repository, one coherent goal, verifiable by that repository's own tests
  or build, finished in one run even if that run takes an hour.

TOO BIG — more than one product-scale goal in one instruction. Build the
  whole application from a spec; rewrite a codebase in another language;
  "modernize" a repository with no stated end state. These decompose above
  the leaf: several swe nodes with goals of their own, or a graph mixing
  swe work with research and writing that are not code changes at all.

Judge by the coherence of the goal, not the number of files. A change that
touches forty files in service of one stated behavior is RIGHT. An
instruction hiding three unrelated deliverables is TOO BIG even if each is
small.`

// sweMaxCostEnv is the operator's ceiling on what one swe leaf may spend inside
// the engine. It is plumbing rather than a setting — a number the engine is
// handed, not a preference the product has — which is why it is pinned in
// config.OperatorEnvPins and never becomes a row in the settings sheet.
const sweMaxCostEnv = "AFORGE_SWE_MAX_COST"

// sweMaxCost reads that ceiling. An unset, unparseable or non-positive value is
// the default rather than a refusal: a measurement run that dies at startup
// because somebody typed "ten" has wasted more than the ceiling was worth.
func sweMaxCost(environ func(string) string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(environ(sweMaxCostEnv)), 64)
	if err != nil || value <= 0 {
		return exec.DefaultSWEMaxCost
	}
	return value
}

// sweInfo is the whole registration.
//
// The budget shape is the part of it that is a claim rather than a copy. A
// linear leaf's fifteen-minute floor is a statement about one agent taking one
// small step at a time; this worker plans, dispatches parallel coders into git
// worktrees, judges each one, merges, and then audits — and the shortest honest
// version of that sequence is well past a quarter of an hour. Thirty minutes is
// the floor, and the scaling puts a leaf on the ordinary 150k-token grant at
// fifty: comfortably inside the "finished in one run even if that run takes an
// hour" the anchors promise. The watchdog above it is the same per-worker
// deadline path every leaf already goes through, so nothing here has to know
// that it exists.
func sweInfo() exec.SubharnessInfo {
	return exec.SubharnessInfo{
		Name:              exec.SWESubharness,
		Purpose:           strings.TrimPrefix(sweMenuEntry, exec.SWESubharness+" — "),
		PriorAnchors:      swePriorAnchors,
		DeadlineFloor:     60 * time.Minute,
		DeadlineStep:      time.Minute,
		DeadlinePerTokens: 3_000,
	}
}
