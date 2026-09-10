package session

// THE LEAN PROFILE: THE SAME PRODUCT, SIZED FOR THE WINDOW IT IS RUNNING IN.
//
// Everything in front of a request — the page and the tool block — is paid for
// on every round of every turn (prefixbudget_test.go weighs it). On a frontier
// model with a hundred and twenty-eight thousand tokens of room that bill is a
// few percent of the window and the laws it buys are worth it. On an open-weight
// model with sixteen thousand it is most of the window, and the cost is not only
// money: every extra instruction is one more thing for a small model to get
// wrong, and every schema it is not going to call is attention taken off the ones
// it is.
//
// So there are two shapes of prefix, and this file is the whole of the decision.
//
// ── IT IS DERIVED AND IT IS NOT A NEW DIAL ──
//
// Nobody is asked to choose a profile, because nobody arrives at a settings sheet
// wanting to. The two facts this build already holds answer it:
//
//   - THE MODEL'S WINDOW. [Config.promptWindow] is the same ladder [Agent.window]
//     climbs — the catalog the machine running this session owns, then the figure
//     the session was configured with, then the conservative default — with
//     everything this process has learned about the model applied over it
//     ([TrustedWindowFor]). Under [leanWindowThreshold] the prefix is lean.
//   - THE SEAT. The crew's `worker` row is the open-weight seat that does the
//     work (internal/config's crew.go), and a conversation riding that model IS
//     the case this profile was written for, whatever window its card claims.
//
// [promptProfileEnv] pins it for a test or a bench cell and is not a settings
// row for the same reason: it is how a measurement names the arm it is measuring,
// not how a person configures their machine. It is spelled the way every other
// pin of that kind in this tree is (internal/splitgate's Mode): the words are
// exact, and anything else — a typo, a stale word, nothing at all — is not a pin
// at all rather than a silent move onto the other arm.
//
// ── WHAT LEAN ACTUALLY CHANGES ──
//
// Four things, each in the place that already owns the decision, and none of them
// a second mechanism:
//
//  1. THE PAGE loses the sections [leanPageSections] names, by their `# ` heading
//     (prompt.go's [renderSystemAt]). The discipline stays, byte-identical with
//     the worker's copy of it.
//  2. THE SHELF grows [leanCapabilityGroups], so the schemas a small model will
//     not reach for on most turns wait one `load_capability` call away
//     (tools_capabilities.go) instead of riding in front of every request.
//  3. `ask` IS PRE-ARMED. A model that answers one call per message cannot do
//     load-then-ask inside a turn, so the `questions` group is put on the belt at
//     construction through the one arming door ([Agent.armFamily]) and is not
//     offered by the loading verb.
//  4. THE MEMORY REFLEX DOES NOT RUN and the project's instruction file rides
//     under [leanInstructionLimit]. The reflex is two model calls every turn on
//     top of the one the person is waiting for; on this seat it is the most
//     expensive thing in the turn that nobody asked for.
//
// ── AND FULL IS UNTOUCHED ──
//
// Every predicate here answers `full` unless something says otherwise, and
// prefixbudget_test.go asserts that a frontier shape renders BYTE-IDENTICAL to
// what it rendered before this file existed. A profile that changed the default
// arm would be a diet nobody measured.

import (
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// promptProfile is which of the two prefixes this agent sends. It is a word
// rather than a bool because it is read by people typing it in front of a
// command, and because a third shape would be a third word here rather than a
// second bool everywhere.
type promptProfile string

const (
	// profileFull is the shipped prefix: every law on the page, every ordinary
	// verb in the tool block. It is what a frontier window gets and what every
	// shape got before this file existed.
	profileFull promptProfile = "full"
	// profileLean is the Pi-sized prefix.
	profileLean promptProfile = "lean"
)

// lean is the one question the rest of the package asks of a profile.
func (p promptProfile) lean() bool { return p == profileLean }

const (
	// leanWindowThreshold is the window under which the prefix goes lean, in
	// tokens.
	//
	// THIRTY-TWO THOUSAND IS WHERE THE PREFIX STOPS BEING A ROUNDING ERROR. The
	// full prefix measures about eleven thousand tokens before anybody has
	// typed; that is nine percent of a 128k window and thirty-four percent of a
	// 32k one, and by 16k it is most of what the model has to think in. The line
	// is drawn at the last window where the full prefix still leaves the larger
	// half of the room for the actual conversation — and it is drawn on the
	// window rather than on a model name because a name is a list somebody has
	// to maintain and a window is a fact the catalog already carries.
	leanWindowThreshold = 32_000

	// promptProfileEnv pins the profile for a test or a bench cell.
	promptProfileEnv = "AFORGE_PROMPT_PROFILE"

	// leanInstructionLimit bounds the project's own instruction file on a lean
	// prefix, where [agentsFileLimit] bounds it on a full one.
	//
	// EIGHT KIB OF HOUSE RULES IS TWO THOUSAND TOKENS, which is an eighth of a
	// 16k window spent before the person has typed. Two KiB is still a page of
	// rules; what it cannot be is documentation. The model is told the file was
	// cut and where the rest is, exactly as it is on a full prefix, so nothing
	// is hidden — it is one `read` away.
	leanInstructionLimit = 2 << 10
)

// ── the decision ────────────────────────────────────────────────────────────

// promptProfile is this session's profile, SETTLED ONCE at construction
// (agent.go's newAgent) and read from the config everywhere after.
//
// It is a method on [Config] and not on [Agent] because every reader of it is a
// reader the agent does not exist for yet: the page is rendered before the agent
// is built, and the belt is built from the same config a moment later. That is
// the same law beltfacts.go's predicates are written under — every predicate is
// answerable from the config alone — and it is what makes it impossible for the
// page and the belt to disagree about which profile this is.
//
// A config nobody settled derives the answer live, which is what a test asking
// the question of a bare [Config] wants.
func (c Config) promptProfile() promptProfile {
	if c.profile != "" {
		return c.profile
	}
	return resolvePromptProfile(c)
}

// settlePromptProfile is what newAgent calls: the derivation, run once, written
// back into the config the agent will keep.
func settlePromptProfile(c Config) promptProfile { return resolvePromptProfile(c) }

// resolvePromptProfile is the derivation itself, most specific answer first.
func resolvePromptProfile(c Config) promptProfile {
	if pinned, ok := pinnedPromptProfile(); ok {
		return pinned
	}
	// THE SEAT BEFORE THE WINDOW. An open-weight worker model that claims a
	// large window still reasons like a worker, and the claim is the half of the
	// pair we trust least — a card's figure is a marketing number until an
	// endpoint refuses one (loop.go's [Agent.learnServedWindow]).
	if c.atTheWorkerSeat() {
		return profileLean
	}
	if c.promptWindow() < leanWindowThreshold {
		return profileLean
	}
	return profileFull
}

// pinnedPromptProfile reads [promptProfileEnv].
//
// AN UNRECOGNISED PIN IS NOT A PIN. The two words are exact and everything else
// leaves the derivation to decide, so a stale or mistyped variable in somebody's
// shell cannot quietly move a conversation onto the other arm — the reversal
// internal/splitgate's Mode states at length, for the same reason.
func pinnedPromptProfile() (promptProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(promptProfileEnv))) {
	case string(profileLean):
		return profileLean, true
	case string(profileFull):
		return profileFull, true
	}
	return "", false
}

// promptWindow is the model's window as this config knows it, in tokens.
//
// IT IS [Agent.window]'S LADDER ASKED OF A CONFIG. The agent's own version
// prefers the figure a surface set for the model actually in use, which is a
// fact that does not exist yet at construction; everything below that is the
// same two steps in the same order, with [TrustedWindowFor] applied over the
// answer exactly as [Agent.trustedWindow] applies it.
func (c Config) promptWindow() int {
	window := 0
	if c.ContextWindowFor != nil {
		window = c.ContextWindowFor(c.Model)
	}
	if window <= 0 {
		window = c.ContextWindow
	}
	if window <= 0 {
		window = defaultContextWindow
	}
	return TrustedWindowFor(c.Model, window)
}

// atTheWorkerSeat says whether this session is riding the crew's `worker` model
// — the open-weight seat that does the work (internal/config's crew.go, whose
// worker column is the dial the three presets move).
//
// IT IS THE PROFILE'S OWN ROW AND NOT A LIST OF MODEL NAMES. A table of
// open-weight slugs here would be a second copy of the crew, free to drift the
// first time somebody pins their own worker; asking the crew is asking the one
// place that answers. A config with no profile directory reads this build's
// default worker, which is the honest answer for a machine nobody has configured.
func (c Config) atTheWorkerSeat() bool {
	model := strings.TrimSpace(c.Model)
	if model == "" {
		return false
	}
	seat := strings.TrimSpace(config.TierModelAt(c.ProfileDir, config.ModelTierWorker))
	return seat != "" && strings.EqualFold(seat, model)
}

// ── the page ────────────────────────────────────────────────────────────────

// leanSection is one ruling about one `# ` section of the page: whether a lean
// prefix keeps it, and WHY. The reason is the column that matters — a list of
// headings with no reasons beside them is a list nobody can review, and the day
// C's law registry lands, the rows marked kept here are the CORE set.
type leanSection struct {
	// heading is the section's own `# ` line, without the hash.
	heading string
	// keeps says whether a lean prefix carries it.
	keeps bool
	// why is the ruling, in a sentence.
	why string
}

// leanPageSections is the table, and it is deliberately not the whole page:
// a section nobody has ruled on is KEPT, so a law added to prompts/system.md
// tomorrow reaches both arms until somebody decides otherwise. Dropping by
// default would make the lean arm quietly lose every law written after this
// file.
//
// THE HEADINGS ARE MATCHED AGAINST THE COMPOSED PAGE, which is why
// `Things that keep working after this window` is here at all: it is not a
// heading in prompts/system.md but one composed into it from beltfacts.go's
// [standingFacts]. promptprofile_test.go fails if a row names a heading the
// widest page does not have, so a rename cannot silently stop a drop.
var leanPageSections = []leanSection{{
	heading: "How you spend the time",
	keeps:   true,
	why: "THE ONE SECTION WITH ABLATION EVIDENCE BEHIND IT. Twelve unattended runs " +
		"of the same brief: three of three reached a real result with it and none of " +
		"five without (prompt.go's [disciplinePrompt] carries the measurement). It is " +
		"also the one page a worker and a conversation read byte-identically " +
		"(taskprompt_test.go), so a profile that trimmed it here would break that pin " +
		"as well as the outcome.",
}, {
	heading: "Things that keep working after this window",
	keeps:   false,
	why: "It is `stand`'s section from its heading down, and a lean belt is a belt " +
		"whose rare verbs wait on a shelf: the mechanics of `when.in` and `when.at` " +
		"ride WITH the verb, in its own description, and what to do when one fires " +
		"rides with the message that says one fired. What is left on the page is the " +
		"third copy.",
}, {
	heading: "Interrupts and steering",
	keeps:   false,
	why: "Every message it explains already carries its own instruction inline — " +
		"`[carry on]` has checkpoint.go's lead, a landing note has taskdelta.go's, a " +
		"job exit has jobs.go's. A page that explains a message the model has not " +
		"received is a law paid for on every request to be read on almost none.",
}}

// leanPage is prompt.go's one door into all of that: the composed page with the
// sections [leanPageSections] drops taken out, and the one line a shelved verb
// needs put in.
//
// IT CUTS ON `# ` LINES AND NOTHING ELSE. The page's own convention is that a
// topic is a top-level heading, so the heading IS the unit — cutting on anything
// finer would be this file editing another lane's sentences, and cutting on
// anything coarser is not a cut.
func leanPage(page string) string {
	kept := make([]string, 0, 64)
	dropping := false
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, "# ") {
			dropping = leanDropsSection(strings.TrimSpace(strings.TrimPrefix(line, "# ")))
		}
		if !dropping {
			kept = append(kept, line)
		}
	}
	page = strings.TrimRight(strings.Join(kept, "\n"), "\n")
	// AND THE HOLE A DROPPED SECTION LEAVES IS CLOSED, exactly as
	// [promptWithBeltFacts] closes the hole a rendered-nothing token leaves, and
	// for the same reason: prompts/system.md has no triple newline of its own.
	for strings.Contains(page, "\n\n\n") {
		page = strings.ReplaceAll(page, "\n\n\n", "\n\n")
	}
	return page
}

// leanDropsSection answers the table, defaulting to kept.
func leanDropsSection(heading string) bool {
	for _, section := range leanPageSections {
		if section.heading == heading {
			return !section.keeps
		}
	}
	return false
}

// ── the shelf ───────────────────────────────────────────────────────────────

// questionsGroup is the group `ask` is in, spelled once because two places need
// it: the table in tools_capabilities.go that owns it, and the pre-arming below.
const questionsGroup = "questions"

// leanCapabilityGroups is what a LEAN belt shelves on top of
// [capabilityGroups], built exactly the way that table is — names, no schemas,
// no gates — so a tool this machine does not build is missing from the shelf for
// free and a group with no surviving member does not exist.
//
// THESE ARE NOT RARE VERBS. They are the verbs a small model on a small window
// should not be paying for a schema it will not call on most turns:
// `propose_task` alone is the heaviest schema on the belt. Every one of them is
// still HERE — one call away, with the loading verb where they were — which is
// the difference between a profile and a crippled build.
//
// holds is the belt's own predicate for the group, verbatim, for beltfacts.go's
// reason: the sentence that sends the model to `load_capability` must not name a
// group this shape was never going to have.
var leanCapabilityGroups = []struct {
	capabilityGroup
	holds func(Config) bool
}{{
	// The two heaviest schemas in the block, and the pair that only matters on
	// the turn a piece of work actually leaves this one.
	capabilityGroup: capabilityGroup{name: "tasks", members: []string{"propose_task", "tasks"}},
	holds:           Config.mayProposeTask,
}, {
	// A watch is set up once and reports itself thereafter; nothing about it is
	// needed on the turns in between.
	capabilityGroup: capabilityGroup{name: "watching", members: []string{"watch"}},
	holds:           Config.mayWatch,
}, {
	// The working-state trio (state.go). A model small enough for this profile
	// keeps its state in the transcript it can see.
	capabilityGroup: capabilityGroup{name: "memory", members: []string{"track", "commit", "recall"}},
	holds:           func(Config) bool { return true },
}, {
	// `read` answers most files; this is the one that opens a PDF or a
	// spreadsheet, and it is reached for on the turn there is one.
	capabilityGroup: capabilityGroup{name: "documents", members: []string{"read_document"}},
	holds:           func(Config) bool { return true },
}}

// capabilityShelf is the whole partition for THIS shape: the shipped table, and
// the lean additions where the profile is lean and their own predicate holds.
func (c Config) capabilityShelf() []capabilityGroup {
	if !c.promptProfile().lean() {
		return capabilityGroups
	}
	shelf := make([]capabilityGroup, 0, len(capabilityGroups)+len(leanCapabilityGroups))
	shelf = append(shelf, capabilityGroups...)
	for _, extra := range leanCapabilityGroups {
		if extra.holds(c) {
			shelf = append(shelf, extra.capabilityGroup)
		}
	}
	return shelf
}

// prearmedGroups are the groups a lean belt is handed at construction rather
// than being asked to fetch.
//
// A ONE-CALL-PER-MESSAGE MODEL CANNOT DO LOAD-THEN-ASK. The shelf's whole
// contract is that a loaded group arrives on the NEXT model request and the
// model carries on in the same turn — which is a two-step a frontier model does
// without noticing and a small local model cannot do at all, because its turn is
// one call. `ask` is the one shelved verb where that costs the person the
// answer: the decision ladder reaches its last rung, the model has no `ask`, and
// what it does instead is type the question into prose, where it has no keys and
// no record. So the group is armed before the first turn.
func (c Config) prearmedGroups() []string {
	if !c.promptProfile().lean() {
		return nil
	}
	return []string{questionsGroup}
}

// shelvesTool says whether THIS shape holds one named tool back — the question
// the page's own sentences turn on, asked of a config before there is a belt.
func (c Config) shelvesTool(name string) bool {
	if !c.shelvesCapabilities() {
		return false
	}
	group := capabilityGroupOf(name)
	if group == "" {
		return false
	}
	for _, prearmed := range c.prearmedGroups() {
		if prearmed == group {
			return false
		}
	}
	for _, candidate := range c.capabilityShelf() {
		if candidate.name == group {
			return true
		}
	}
	return false
}

// shelvesFact is what beltfacts.go's composer asks: does this fact's model have
// to be sent to the loading verb for these tools?
//
// IT IS PER FACT AND NOT PER SHAPE, and that is the change the pre-armed group
// forced. `ask` is shelved on a full conversation and carried on a lean one, so
// "does this shape shelve anything" is no longer the same question as "does this
// shape shelve THIS" — and a page that told a lean model to go and load a verb
// already in its tool list would be the defect beltfacts.go exists to prevent,
// written the other way round.
func (c Config) shelvesFact(fact beltFact) bool {
	for _, tool := range fact.tools {
		if c.shelvesTool(tool) {
			return true
		}
	}
	return false
}

// leanShelfPointer is the one sentence a lean page owes: the verbs it names that
// are NOT in the tool list yet, and the group each is in.
//
// THE PAGE MAY NAME A SHELVED VERB ONLY WHERE IT ALSO SAYS HOW IT ARRIVES
// (prompt_belt_test.go holds that both ways). The shipped rows say it inline —
// "`build_harness`, in the `harnesses` group" — because on a full belt exactly
// three families are shelved and each has a sentence of its own. A lean belt
// shelves four more, whose sentences belong to other lanes and are right as they
// stand for every other shape, so the lean arm pays for one composed line
// instead of four rewritten ones. It is composed from the table above and from
// [Agent.loadCapabilityTool]'s own group words, so it cannot name a group this
// machine does not have.
func (c Config) leanShelfPointer() string {
	if !c.promptProfile().lean() {
		return ""
	}
	groups := make([]string, 0, len(leanCapabilityGroups))
	for _, extra := range leanCapabilityGroups {
		if !extra.holds(c) {
			continue
		}
		names := make([]string, 0, len(extra.members))
		for _, member := range extra.members {
			names = append(names, "`"+member+"`")
		}
		groups = append(groups, "`"+extra.name+"` ("+strings.Join(names, ", ")+")")
	}
	if len(groups) == 0 {
		return ""
	}
	return "- VERBS NOT IN YOUR TOOL LIST ARE ONE `" + loadCapabilityToolName +
		"` CALL AWAY: " + strings.Join(groups, ", ") + "."
}

// ── the numbers ─────────────────────────────────────────────────────────────

// instructionLimit is how much of the project's own instruction file rides in
// the prefix (prompt.go's [renderSystemAt]).
func (c Config) instructionLimit() int {
	if c.promptProfile().lean() {
		return leanInstructionLimit
	}
	return agentsFileLimit
}

// onlyOneInstructionFile says whether the prefix stops after the FIRST project
// instruction file it finds.
//
// AGENTS.md AND CLAUDE.md ARE TWO SPELLINGS OF ONE THING, and a repository that
// carries both is carrying two copies of one set of house rules. On a full
// prefix both are quoted, because a frontier model can hold them and the rules
// the person actually edits are the highest-value text in the prefix
// (docs/design/prompt-diet/DESIGN.md §7 says so in as many words). On a lean one
// the second copy is the first thing to go — and it is the second FOUND rather
// than a named file, so a project that has only CLAUDE.md still gets its rules.
func (c Config) onlyOneInstructionFile() bool { return c.promptProfile().lean() }
