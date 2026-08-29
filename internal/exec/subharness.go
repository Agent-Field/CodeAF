package exec

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// A subharness is an alternative way to turn the same Task into the same
// Outcome. This file is everything the rest of the system needs to know about
// one before it runs: what it is for, how much it can take, and how long it may
// have. The executor that actually does the work is registered separately, on a
// Registry, because a surface builds its workers with its own clients and
// workspaces while this description is a fact about the process.
//
// The whole file is inert with only linear registered, and that is the law
// rather than a convenience: MenuText returns nothing, the sizing prompt keeps
// its pre-subharness bytes, and every lookup answers linear.

// LinearSubharness is the baseline and the fallback. An empty name resolves to
// it, but the two are not the same fact: empty is a question nobody answered,
// and this name is the answer "the generalist" said out loud. GeneralistSubharness
// is the predicate that keeps them apart.
const LinearSubharness = plan.LinearSubharness

// BareSubharness is the cheap whole-taker for one-sitting work. It is
// repeated here for the same reason LinearSubharness is: the bare executor
// lives in a sub-package that imports this one, so importing it back would
// close a cycle. The string matches bare.BareSubharness exactly.
const BareSubharness = "bare"

// SubharnessInfo is one registration.
//
// Purpose and PriorAnchors are the two halves of teaching a model to choose:
// the purpose says what the work has to be about, the anchors say how much of
// it fits. Both are prompt text, and both are priors — measurement replaces the
// anchors through the profile store, and the measured history is rendered
// beside the purpose through the knowledge hook below.
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
	// PriorAnchors is the initial capacity ruler in the style of plan/size.go's
	// three worked examples: comfortably atomic, borderline, oversized. It is
	// the initial setting of this subharness's hardness and nothing more; the
	// first eight measured leaves start replacing it.
	PriorAnchors string `json:"prior_anchors,omitempty"`

	// DeadlineFloor and the scaling pair are the budget shape. A leaf's hang
	// backstop is not a policy about patience, it is a claim about how long
	// this kind of work legitimately takes, and the claim differs per worker: a
	// linear leaf's fifteen minutes would kill a coding pipeline in its first
	// merge. Zero values fall back to linear's shape, so a registration that
	// says nothing about time is served rather than refused.
	DeadlineFloor     time.Duration `json:"deadline_floor,omitempty"`
	DeadlineStep      time.Duration `json:"deadline_step,omitempty"`
	DeadlinePerTokens int           `json:"deadline_per_tokens,omitempty"`
}

// linearInfo is the shape the whole system ran on before there was a second
// one: fifteen minutes floor, one minute per fifty thousand tokens above it.
// Every surface that grants a leaf its room reads it through [SubharnessFor],
// which answers with this registration for the generalist and for any name
// nobody registered. Its Purpose and PriorAnchors are deliberately empty —
// linear is the baseline every node is already judged against, not an entry on
// a menu.
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
// decided — a retry running on the shape its first attempt was given, a leaf
// whose worker changed under it.
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
// fires when a worker is wedged past every limit it was given.
func (s SubharnessInfo) Watchdog(budgetTokens int) time.Duration {
	return WatchdogAbove(s.Deadline(budgetTokens))
}

// linearManifest is the baseline under the new contract: the same cost shape,
// wearing the typed front door every subharness now has.
//
// ITS PURPOSE STAYS EMPTY, which is the law two lines above linearInfo and not
// an omission here. Linear is the baseline every node is judged against rather
// than an entry on a menu — [Subharnesses] leaves it out, [MenuText] never draws
// it, and the `/subharness` list does not offer it either, because the
// generalist is what you get when you pick nothing, not something you pick.
// [Manifest.Validate] knows about this one exemption by name.
var linearManifest = LeafManifest(linearInfo)

var (
	subharnessMutex sync.RWMutex
	// subharnessBy is the process's one table of what a name means, and it holds
	// MANIFESTS now rather than the bare registration it used to. That is the
	// growth the subharness contract asked for: every existing reader still asks
	// for the same [SubharnessInfo] fields through the projections below, and the
	// schemas, cues, whitelist and guards ride along in the same entry instead of
	// in a second table that could disagree with this one about which
	// subharnesses exist.
	subharnessBy    = map[string]Manifest{LinearSubharness: linearManifest}
	subharnessOrder []string
	// measured is the hook onto self-knowledge: one line per subharness of what
	// its leaves have actually cost. It lives here as a function rather than as
	// data because the profile store belongs to the surface, and the menu must
	// never be the reason a package imports one.
	measured func(subharness string) string
)

// RegisterSubharness makes one available to the whole process: to the menu the
// compiler chooses from, to the sizing pass's rulers, and to the budget shape a
// leaf is granted. It is process-global for the same reason the anchors are —
// one process is one set of workers — and it is deliberately the only door:
// registering here is what puts a subharness in front of every model that could
// choose it, so a new one is never half-installed.
func RegisterSubharness(info SubharnessInfo) {
	// A registration that says nothing about schemas, cues or guards is a
	// manifest with none, which is exactly what every one of these callers has
	// always meant. The error is dropped here and only here: this door has never
	// had one, and every caller of it is a leaf worker whose name and purpose
	// were written in Go beside the executor they describe.
	_ = RegisterManifest(LeafManifest(info))
}

// RegisterManifest is the same door with the whole contract carried through it,
// and it is what a Go-native subharness under docs/SUBHARNESS-PRD.md registers
// with. [RegisterSubharness] above is this function with the six new fields left
// empty, kept because every existing caller in the tree is describing a leaf
// worker and has nothing to say about them.
//
// IT ANSWERS THE VALIDATION IN PROSE rather than swallowing it, because the
// callers that will use this one are a bundle being loaded off disk and a model
// iterating against what it got back — and neither is served by a silent refusal.
func RegisterManifest(manifest Manifest) error {
	name := strings.TrimSpace(manifest.Name)
	if name == "" || name == LinearSubharness {
		// The baseline is already in the table and may not be re-registered. It
		// is not an error to try: a build enumerating its workers should not have
		// to know which one of them is the one nobody may describe.
		return nil
	}
	manifest.Name = name
	if err := manifest.Validate(); err != nil {
		return err
	}
	remember(manifest)
	// The sizing pass reads its rulers out of plan, which cannot import this
	// package. One registration, both readers.
	plan.UseSubharness(plan.Subharness{Name: name, Purpose: manifest.Purpose}, manifest.PriorAnchors)
	return nil
}

// remember is the guarded half of registration, split out so the lock it takes
// is released by a defer under the line that took it.
func remember(manifest Manifest) {
	subharnessMutex.Lock()
	defer subharnessMutex.Unlock()
	if _, known := subharnessBy[manifest.Name]; !known {
		subharnessOrder = append(subharnessOrder, manifest.Name)
		sort.Strings(subharnessOrder)
	}
	subharnessBy[manifest.Name] = manifest
}

// Subharnesses returns the registered specialists in a stable order. Linear is
// never among them: a menu with one entry is no menu.
func Subharnesses() []SubharnessInfo {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	list := make([]SubharnessInfo, 0, len(subharnessOrder))
	for _, name := range subharnessOrder {
		list = append(list, subharnessBy[name].SubharnessInfo)
	}
	return list
}

// RegisteredManifests is [Subharnesses] with the whole contract carried through
// instead of only the registration half — the same names, the same order, the
// same exclusion of the baseline. It is what a surface asks when it wants the
// schemas and the cues; a surface that only wants a menu keeps asking the
// projection above, and neither one is a second enumeration.
func RegisteredManifests() []Manifest {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	list := make([]Manifest, 0, len(subharnessOrder))
	for _, name := range subharnessOrder {
		list = append(list, subharnessBy[name])
	}
	return list
}

// ManifestFor resolves a name to the whole manifest, and says whether anything
// was there. It is the honest half of [SubharnessFor], which degrades an unknown
// name to the baseline: a caller reading a schema or a whitelist has to know it
// got the thing it asked for, because linear's front door is not the front door
// of the subharness somebody named.
func ManifestFor(name string) (Manifest, bool) {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	manifest, ok := subharnessBy[strings.TrimSpace(name)]
	return manifest, ok
}

// SubharnessFor resolves a name to what will actually run it. An unknown or
// empty name is linear rather than an error — the same degradation Registry.For
// promises, said one layer up so a budget shape can be read before dispatch.
func SubharnessFor(name string) SubharnessInfo {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	if manifest, ok := subharnessBy[strings.TrimSpace(name)]; ok {
		return manifest.SubharnessInfo
	}
	return linearInfo
}

// KnownSubharness reports whether a name reaches a registered specialist. It is
// the validator every surface uses on a name that came from a model or a flag,
// and it answers no for "linear" and for empty: those are the baseline, and
// nothing needs to say so out loud.
func KnownSubharness(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == LinearSubharness {
		return false
	}
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	_, ok := subharnessBy[name]
	return ok
}

// GeneralistSubharness reports whether a name is the generalist, named. It is
// the other half of KnownSubharness rather than its negation: KnownSubharness
// answers "is this a registered specialist", and answers no to both the
// generalist and to nothing at all, which are two different things to every
// reader that would otherwise fill a blank in from somewhere else.
//
// It is the registry's answer for the same reason KnownSubharness is: a surface
// that spelled the comparison itself would be one rename away from being wrong.
func GeneralistSubharness(name string) bool { return plan.GeneralistSubharness(name) }

// SubharnessChosen reports whether a worker was chosen at all — a specialist or
// the generalist. Only the empty string is no choice.
func SubharnessChosen(name string) bool { return plan.SubharnessChosen(name) }

// UseSubharnessKnowledge installs the measured-history hook the menu renders
// under each purpose. Nil, and a hook that returns nothing, leave the menu
// exactly as the registrations wrote it — which is what every process has
// before its first specialist leaf has ever run.
func UseSubharnessKnowledge(knowledge func(subharness string) string) {
	subharnessMutex.Lock()
	defer subharnessMutex.Unlock()
	measured = knowledge
}

// knowledgeHook takes one stable reference to the hook. The hook itself reads
// files, so it is called outside the lock: the menu is rendered on the compile
// path and holding a process-wide lock across disk work is how a registry
// becomes a bottleneck nobody can see.
func knowledgeHook() func(string) string {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	return measured
}

// MenuText is the choice context, and it is empty until there is a choice.
//
// It is built from the registrations rather than written anywhere, so adding a
// subharness adds it to every prompt that chooses one; and it states the rule
// in the same breath as the options, because a menu without a rule for reading
// it is how a model talks itself into the interesting answer.
func MenuText() string { return MenuTextExcept("") }

// MenuTextExcept is the same menu with one worker struck off it, and it exists
// for one situation: a job that has already been tried by that worker and
// failed.
//
// Offering the failed worker its own job back is a rung that goes nowhere. Model
// escalation earns its second attempt by changing something — a stronger model
// on the same worker — while re-choosing the same specialist changes nothing at
// all, and a menu that leaves the option on the table is a menu inviting a loop.
// So the exclusion is structural rather than a sentence in the prompt: a model
// cannot pick what it was never shown.
//
// With one specialist registered and that one excluded, the menu is empty and
// every caller is back to the byte-identical baseline — which is the additive
// law arriving at exactly the right answer without being asked.
func MenuTextExcept(exclude string) string {
	exclude = strings.TrimSpace(exclude)
	specialists := make([]SubharnessInfo, 0, 2)
	for _, info := range Subharnesses() {
		if info.Name != exclude {
			specialists = append(specialists, info)
		}
	}
	if len(specialists) == 0 {
		return ""
	}
	knowledge := knowledgeHook()

	var menu strings.Builder
	menu.WriteString("Subharnesses. A job is normally taken by the default worker: one agent, " +
		"alone and in order, with tools. These specialists sit beside it, each a whole " +
		"different way of doing one job:\n")
	measuredAny := false
	for _, info := range specialists {
		fmt.Fprintf(&menu, "\n- %s — %s\n", info.Name, strings.TrimSpace(info.Purpose))
		if knowledge == nil {
			continue
		}
		if line := strings.TrimSpace(knowledge(info.Name)); line != "" {
			fmt.Fprintf(&menu, "  measured here so far: %s\n", line)
			measuredAny = true
		}
	}
	// W6: the measured line was decoration nobody was told what to do with. The
	// sentence below is the instruction for reading it, and it appears only when
	// there is something to read — a rule about figures that were never printed
	// is prompt the model pays for and cannot use, and its absence keeps the
	// pre-evidence menu byte-identical to what it always was.
	if measuredAny {
		menu.WriteString("\nThe figures beside each worker are what work of this kind has really cost " +
			"here. Read them as evidence about this machine, not as a target: prefer the " +
			"worker whose purpose fits, and among workers that fit, prefer the one the " +
			"evidence says finishes this kind of work.\n")
	}
	menu.WriteString("\nChoose a specialist subharness only when the job's essence matches its " +
		"purpose. When in doubt, or for mixed or non-matching work, leave it unset " +
		"(the default worker).")
	return menu.String()
}

// ForgetSubharnesses restores the process to its linear-only state. Tests own
// it: registration is global by design, and a test that adds one must be able
// to put the process back for every test that asserts the baseline.
func ForgetSubharnesses() {
	forget()
	plan.ForgetSubharnesses()
}

func forget() {
	subharnessMutex.Lock()
	defer subharnessMutex.Unlock()
	subharnessOrder = nil
	subharnessBy = map[string]Manifest{LinearSubharness: linearManifest}
	measured = nil
}
