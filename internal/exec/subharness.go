package exec

import (
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// A subharness is a named way of turning a Task into an Outcome. There is ONE
// worker, the generalist, and nothing chooses it because there is nothing to
// choose. What the rest of the system still needs from this file is the fact it
// has always needed before a leaf runs: how much room the leaf gets. The
// executor that does the work is registered separately, on a Registry, because
// a surface builds its worker with its own clients and workspaces while this
// description is a fact about the process.
//
// The manifest table below is the SAVED-PROGRAM contract (docs/SUBHARNESS-PRD.md):
// a bundle or a Go-native program registers its typed front door through
// [RegisterManifest]. A program is a named piece of work somebody saved, not a
// second worker, and nothing here puts a name in front of a model to pick.

// LinearSubharness is the worker. An empty name resolves to it, but the two are
// not the same fact: empty is a question nobody answered, and this name is the
// answer said out loud. GeneralistSubharness is the predicate that keeps them
// apart, and old graphs are full of both.
const LinearSubharness = plan.LinearSubharness

// SubharnessInfo is one registration: what it is for, and how much room its
// work is given.
//
// THE TAGS ARE THE ON-DISK SPELLING OF HALF A MANIFEST. [Manifest] embeds this
// struct and is the document a bundle's manifest.json actually is (PRD §6) — a
// file a person writes by hand, a model iterates on, and a pull request reviews.
// Untagged, this half of it would come out spelled in Go field names next to the
// tagged half's lowercase ones, and the format would be two conventions in one
// object. They are named the way the fields beside them are, and everything but
// the name is omitempty, so a manifest that says nothing about its budget shape
// carries nothing about it.
type SubharnessInfo struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose,omitempty"`
	// PriorAnchors is the capacity ruler in the style of plan/size.go's three
	// worked examples: comfortably atomic, borderline, oversized. A saved
	// program that measures differently from the generalist says so here.
	PriorAnchors string `json:"prior_anchors,omitempty"`

	// DeadlineFloor and the scaling pair are the budget shape. A leaf's hang
	// backstop is not a policy about patience, it is a claim about how long
	// this kind of work legitimately takes. Zero values fall back to the
	// generalist's shape, so a registration that says nothing about time is
	// served rather than refused.
	DeadlineFloor     time.Duration `json:"deadline_floor,omitempty"`
	DeadlineStep      time.Duration `json:"deadline_step,omitempty"`
	DeadlinePerTokens int           `json:"deadline_per_tokens,omitempty"`
}

// linearInfo is the shape the whole system runs on: fifteen minutes floor, one
// minute per fifty thousand tokens above it. Every surface that grants a leaf
// its room reads it through [SubharnessFor]. Its Purpose and PriorAnchors are
// deliberately empty — the generalist is the baseline every node is already
// judged against, and there is nothing beside it to tell it apart from.
var linearInfo = SubharnessInfo{
	Name:              LinearSubharness,
	DeadlineFloor:     15 * time.Minute,
	DeadlineStep:      time.Minute,
	DeadlinePerTokens: 50_000,
}

// Deadline is the budget shape applied to one leaf's token grant.
//
// THIS IS THE ONLY PLACE IN THE PROCESS THAT DOES THIS ARITHMETIC, and that is
// the law rather than a tidiness. The same fifteen-minute floor and the same
// minute per fifty thousand tokens were written out longhand in four places —
// the chat surface, the headless runner, the linear loop's own fallback and the
// claim reaper's window — and the four then had to be kept in step by hand
// across a change none of them could see. The reaper's window is derived from
// this figure two additions along, so a floor that moved here and nowhere else
// put the backstop BELOW the deadline it is meant to sit above, which is not a
// backstop but the thing that fires first. `TestOnlyTheSubharnessTableSizesALeafsRoom`
// fails the build on a fifth copy.
func (s SubharnessInfo) Deadline(budgetTokens int) time.Duration {
	floor, step, per := s.DeadlineFloor, s.DeadlineStep, s.DeadlinePerTokens
	if floor <= 0 {
		floor = linearInfo.DeadlineFloor
	}
	if step <= 0 || per <= 0 {
		step, per = linearInfo.DeadlineStep, linearInfo.DeadlinePerTokens
	}
	if scaled := time.Duration(budgetTokens/per) * step; scaled > floor {
		return scaled
	}
	return floor
}

// watchdogPad is how far above a leaf's own deadline the node watchdog sits.
//
// It is the room a leaf told to land needs to notice and finish: one more model
// call and one more transcript flush. Two minutes, unchanged from the figure
// every surface wrote out for itself, and it lives beside the deadline it is
// added to because the two are one bound in two parts — the worker's own clock,
// and the backstop that must never fire below it.
const watchdogPad = 2 * time.Minute

// WatchdogAbove is the node watchdog over a deadline that has already been
// decided — a retry running on the shape its first attempt was given.
//
// It is derived here rather than at each dispatch site for the reason stated on
// [SubharnessInfo.Deadline]: `deadline + 2*time.Minute`, written out by hand in
// seven places across three files, is a pad that disagrees with itself the first
// time one of them is edited.
func WatchdogAbove(deadline time.Duration) time.Duration {
	return deadline + watchdogPad
}

// Watchdog is the node watchdog above one leaf's token grant: its own deadline
// plus the landing pad. The executor has a deadline of its own, so this only
// fires when a leaf is wedged past every limit it was given.
func (s SubharnessInfo) Watchdog(budgetTokens int) time.Duration {
	return WatchdogAbove(s.Deadline(budgetTokens))
}

// linearManifest is the worker wearing the typed front door every saved program
// has.
//
// ITS PURPOSE STAYS EMPTY, which is the law two lines above linearInfo and not
// an omission here. The generalist is what you get when you pick nothing, not
// something you pick, so it is never listed anywhere.
// [Manifest.Validate] knows about this one exemption by name.
var linearManifest = LeafManifest(linearInfo)

var (
	subharnessMutex sync.RWMutex
	// subharnessBy is the process's one table of what a name means, and it
	// holds MANIFESTS: the schemas, cues, whitelist and guards ride along in
	// the same entry instead of in a second table that could disagree with this
	// one about which programs exist.
	subharnessBy = map[string]Manifest{LinearSubharness: linearManifest}
)

// RegisterManifest is the door a saved program comes through, and what a
// Go-native subharness under docs/SUBHARNESS-PRD.md registers with.
//
// IT ANSWERS THE VALIDATION IN PROSE rather than swallowing it, because the
// callers are a bundle being loaded off disk and a model iterating against what
// it got back — and neither is served by a silent refusal.
func RegisterManifest(manifest Manifest) error {
	name := strings.TrimSpace(manifest.Name)
	if name == "" || name == LinearSubharness {
		// The worker is already in the table and may not be re-registered. It
		// is not an error to try: a build enumerating what it can run should
		// not have to know which one of them is the one nobody may describe.
		return nil
	}
	manifest.Name = name
	if err := manifest.Validate(); err != nil {
		return err
	}
	remember(manifest)
	return nil
}

// remember is the guarded half of registration, split out so the lock it takes
// is released by a defer under the line that took it.
func remember(manifest Manifest) {
	subharnessMutex.Lock()
	defer subharnessMutex.Unlock()
	subharnessBy[manifest.Name] = manifest
}

// ManifestFor resolves a name to the whole manifest, and says whether anything
// was there. It is the honest half of [SubharnessFor], which degrades an
// unknown name to the worker: a caller reading a schema or a whitelist has to
// know it got the thing it asked for.
func ManifestFor(name string) (Manifest, bool) {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	manifest, ok := subharnessBy[strings.TrimSpace(name)]
	return manifest, ok
}

// SubharnessFor resolves a name to what will actually run it, which is the one
// worker. An unknown or empty name is it rather than an error — the same
// degradation Registry.For promises, said one layer up so a budget shape can be
// read before dispatch.
func SubharnessFor(string) SubharnessInfo { return linearInfo }

// KnownSubharness reports whether this build can run a name as written. There
// is one worker, so the answer is yes for the generalist — named, or left blank
// by a plan that never asked — and no for everything else.
//
// It has one reader left, and it is about old graphs: a node stored by a build
// that had a second worker still names it, and this is how a surface tells that
// the name it is holding is not a worker this build has. Such a node runs
// linear and says so once.
func KnownSubharness(name string) bool {
	name = strings.TrimSpace(name)
	return name == "" || strings.EqualFold(name, LinearSubharness)
}

// GeneralistSubharness reports whether a name is the generalist, named. It is
// deliberately not KnownSubharness: this one answers no to the empty string,
// which is the difference between "the worker, said out loud" and "nobody said
// anything" — two different things to every reader that would otherwise fill a
// blank in from somewhere else.
//
// It is the registry's answer rather than a comparison each surface spells for
// itself, which would be one rename away from being wrong.
func GeneralistSubharness(name string) bool { return plan.GeneralistSubharness(name) }

// SubharnessChosen reports whether a node's worker column was ever written at
// all. Only the empty string is nothing.
func SubharnessChosen(name string) bool { return plan.SubharnessChosen(name) }

// ForgetSubharnesses restores the process to the state it boots in: the one
// worker and no saved programs. Tests own it — registration is process-global
// by design, and a test that loads a bundle must be able to put the process
// back for every test that asserts the baseline.
func ForgetSubharnesses() {
	subharnessMutex.Lock()
	defer subharnessMutex.Unlock()
	subharnessBy = map[string]Manifest{LinearSubharness: linearManifest}
}
