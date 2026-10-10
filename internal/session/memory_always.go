package session

// RULES: MEMORIES MARKED ALWAYS.
//
// "Always use tabs here", "never touch the public API" — a sentence that governs
// work nobody has done yet is kept as a memory with the always flag set, and it
// is put in front of every conversation turn and every task brief at its scope
// rather than recalled when the router judges it relevant
// (docs/design/automations/DESIGN.md, "Rules become memories marked always";
// internal/store's memory_always.go keeps the flag).
//
// THERE ARE TWO MOUTHS AND ONE OF THEM ASKS. The person's own `/always <text>`
// is the confirmation and asks nothing ([Agent.RememberAlways]). The model's
// `remember` with `always` set is a question the consent gate never lets through
// on its own (internal/approval's KeepsARule), because a line about to sit in
// front of every later conversation is the person's to set — and with nobody
// there to answer, it is refused rather than kept.
//
// THE SCOPES ARE MEMORY'S OWNERS AND NOTHING NARROWER: the person, this machine,
// or one project. There is no rule for one conversation only, and no "not here"
// exception, so a conversation with no project of its own — home, or a folder
// nobody could prove the identity of — keeps its rules for the person, and the
// receipt says so rather than letting the reach be a surprise.
//
// A RULE IS READ WHERE WORK IS BORN, with the standing orders it replaces:
//
//   - a conversation's message[0], at the start of every turn ([Agent.refreshAlwaysLocked]);
//   - a task node's brief, once per frontier pass ([TaskGraph.alwaysWorld]);
//   - a plan-born run worker's brief and a headless `codeaf do` run's, through
//     the run spec ([AlwaysWorld]).
//
// MEMORY OFF IS RULES OFF. Every one of those reads goes through the memory store
// the door opened, and a door that opened none has no rules to put anywhere.

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/store"
)

// alwaysBlockMost is how many rules one block or brief section carries. A rule
// rides in front of every request of every turn, and a person with forty rules
// over one place has written a document rather than a set of rules; past the
// cap the newest wait ([store.Store.AlwaysMemories] orders the settled ones
// first) and the section says how many. PERF.md records it with the contextual
// memory bounds; changing it changes that section in the same commit.
const alwaysBlockMost = 12

// alwaysBlockRunes bounds the rules' own lines in one block, for the same bill:
// about six hundred tokens at four characters a token, half the routed memory
// block's [memoryBlockRunes], because this block is paid on every turn whether
// or not the turn needed it. A rule that would carry the section past it waits
// with the rest, whole, and is counted in the "more" line rather than cut.
const alwaysBlockRunes = 2400

// alwaysWorldHeading titles the section in a conversation's block and in a task
// brief alike: ONE CONSTANT, because a heading spelled in two places is a
// heading that will one day be spelled two ways.
const alwaysWorldHeading = "Always"

// alwaysWorldBinding is the one sentence the rules ride under, and it says the
// three things a model has to know about the lines below it: whose they are,
// that they bind, and that they did not arrive with the request in front of it.
const alwaysWorldBinding = "Rules the person asked to always hold here: not suggestions, and they did not arrive with this request."

// alwaysWorldReport is the closing line a TASK's section carries and a
// conversation's does not. A node works with nobody to ask, so a rule it cannot
// honour has exactly one place to be said; a conversation can say so in the
// very next sentence it writes.
const alwaysWorldReport = "If you cannot honour one of these, say so in your report."

// MemoryOwners is the owner list a session at one project may SEE: the person,
// this machine, and the project when its key is known. It is the one builder of
// that list — [Agent.memoryOwnersFor] is it — exported for the doors outside
// this package that read rules for a place with no session open on it
// (cmd/codeaf's `codeaf do`).
func MemoryOwners(projectKey string) []string {
	owners := []string{store.OwnerUser, store.OwnerMachine}
	if key := strings.TrimSpace(projectKey); key != "" {
		owners = append(owners, store.OwnerProject(key))
	}
	return owners
}

// renderAlwaysRules is the section itself: the heading, the binding sentence,
// one line per rule, how many more there are, and the caller's closing line.
//
// THE EMPTINESS LAW. No rules is NO SECTION — not an empty heading, not "none".
//
// NO AGES, AND THE ORDER IS THE STORE'S. A conversation's block rides in
// message[0], whose bytes are the provider's cached prefix (prefixcache_test.go
// pins that it only moves when somebody did something), so nothing here may
// depend on a clock: a rule is no less binding for being old, and a "learned 3h
// ago" that ticked over to "4h" would re-price the whole conversation for a
// change nobody made.
//
// EACH RULE IS ONE PHYSICAL LINE OF ESCAPED TEXT ([contextualMemoryField]), so a
// rule's own words can never close the block or open a second one.
func renderAlwaysRules(rules []store.Memory, closing string) string {
	var lines strings.Builder
	used, shown := 0, 0
	for _, rule := range rules {
		if shown == alwaysBlockMost {
			break
		}
		line := "- " + contextualMemoryField(rule.Text) + "\n"
		length := utf8.RuneCountInString(line)
		if used+length > alwaysBlockRunes {
			break
		}
		used += length
		shown++
		lines.WriteString(line)
	}
	if shown == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(alwaysWorldHeading + ":\n\n" + alwaysWorldBinding + "\n\n")
	out.WriteString(lines.String())
	if more := len(rules) - shown; more > 0 {
		out.WriteString("…" + strconv.Itoa(more) + " more\n")
	}
	if closing != "" {
		out.WriteString("\n" + closing + "\n")
	}
	return out.String()
}

// AlwaysWorld renders the rules section a starting worker's brief closes on, or
// "" when no rule holds or there is no memory store. It is the exported door for
// the callers outside this package seated at the same moment a node is — a run
// handing its workers a brief, `codeaf do` handing its own — and it reads one
// store for one owner list, which is the whole of "which rules hold here".
//
// A READ THAT FAILS IS NO SECTION. A worker is not where a person learns their
// disk is gone, and a brief that could not say its rules is the brief it would
// have been with none.
func AlwaysWorld(st *store.Store, owners []string) string {
	if st == nil || len(owners) == 0 {
		return ""
	}
	rules, err := st.AlwaysMemories(owners, 0)
	if err != nil {
		return ""
	}
	return renderAlwaysRules(rules, alwaysWorldReport)
}

// joinWorldSections joins the closing sections of a brief — the rules and the
// standing orders — into the one string the brief closes on, empty ones left
// out. Each section ends on its own newline, so one more puts a blank line
// between them and the second reads as an opening rather than a row of the
// first.
func joinWorldSections(sections ...string) string {
	kept := make([]string, 0, len(sections))
	for _, section := range sections {
		if strings.TrimSpace(section) != "" {
			kept = append(kept, section)
		}
	}
	return strings.Join(kept, "\n")
}

// alwaysWorld is the conversation answering for a piece of its own work: its
// rules, read from the brain a binding read may use ([Config.bindingBrain]) for
// the owners this conversation sees. A node starting here ([TaskGraph.alwaysWorld])
// and a plan-born run worker (RunSpec.Always) both read THIS answer, so a
// conversation and the work it hands out cannot disagree about which rules hold.
func (a *Agent) alwaysWorld() string {
	return AlwaysWorld(a.config.bindingBrain(), a.memoryOwners())
}

// alwaysWorld is the section a starting node's brief closes on, asked once per
// frontier pass for the reason the standing section is: every node in one graph
// sits in one place, so the answer is one answer.
func (g *TaskGraph) alwaysWorld() string {
	if g.home == nil {
		return ""
	}
	return g.home.alwaysWorld()
}

// refreshAlwaysLocked puts the rules that hold over this conversation in front
// of the model. It is called with a.mu held at the start of every turn, beside
// the standing orders' refresh and for their reason: a rule set a moment ago, in
// this window or another, holds over the very next turn.
//
// IT IS NOT GATED ON [Agent.remembers]. That answers whether this conversation
// does memory WORK — the router, the post-turn pass, `remember` on the belt —
// and a lean profile does none of it; but the person's rules hold over a small
// model exactly as they hold over a large one, and reading them is one indexed
// read, not a model call. The store the door opened is the whole condition.
//
// A TASK WORKER RENDERS NOTHING HERE, and not by a check: its config carries no
// brain of its own, only a lent one ([Config.bindingStore]), so its rules arrive
// once, in its brief, where the rest of its world arrives.
//
// A READ THAT FAILS KEEPS THE LAST BLOCK. The rules did not stop holding because
// one read of them did not answer, and a block that vanished for a turn would
// re-price the prefix twice to say nothing true.
func (a *Agent) refreshAlwaysLocked() {
	st := a.config.Memory
	if st == nil {
		a.alwaysText = ""
		return
	}
	rules, err := st.AlwaysMemories(a.memoryOwners(), 0)
	if err != nil {
		return
	}
	a.alwaysText = alwaysBlock(rules)
}

// alwaysBlock is the <always> block message[0] carries, or "" when no rule
// holds. It is tagged the way <memory> and <standing> are tagged, and joined on
// the same way.
func alwaysBlock(rules []store.Memory) string {
	section := renderAlwaysRules(rules, "")
	if section == "" {
		return ""
	}
	return "\n<always>\n" + section + "</always>\n"
}

// ── the two doors that keep one ─────────────────────────────────────────────

// keptRule is a rule as it landed: the row, and where it holds.
type keptRule struct {
	memory store.Memory
	owner  string
	// widened says the rule asked to hold in this project and this conversation
	// has none, so it holds for the person everywhere instead — said in the
	// receipt, never left to surprise somebody in another repository.
	widened bool
}

// alwaysOwnerFor answers the owner a RULE written under a scope word lands in,
// and whether the scope had to be widened to reach one.
//
// IT DIFFERS FROM [Agent.ownerForScope] IN ONE PLACE, ON PURPOSE. An ordinary
// line in a conversation with no provable project goes to the quarantine to wait
// for the person, and a line in a conversation that owns its own folder is
// scoped to that folder — both honest for a line that is only recalled. Neither
// is honest for a rule: the quarantine is never put in front of anything, and a
// folder that belongs to one conversation would make a rule for one
// conversation, which is a scope rules do not have. So both hold for the person.
func (a *Agent) alwaysOwnerFor(scope string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case store.MemoryScopeEnv:
		return store.OwnerMachine, false
	case store.MemoryScopeUser:
		return store.OwnerUser, false
	}
	if key := strings.TrimSpace(a.config.MemoryProjectKey); key != "" && !a.config.Place.Owned {
		return store.OwnerProject(key), false
	}
	return store.OwnerUser, true
}

// keepRule writes one rule through the one write path ([Agent.writeLine]) and
// answers where it landed. Both mouths come here: the person's /always and the
// model's confirmed `remember`; the difference between them is only whether the
// consent gate asked first, which is decided before either reaches this line.
func (a *Agent) keepRule(text, scope string) (keptRule, error) {
	owner, widened := a.alwaysOwnerFor(scope)
	memory, err := a.writeLine(text, store.OwnerScopeOf(owner), true)
	if err != nil {
		return keptRule{}, err
	}
	return keptRule{memory: memory, owner: owner, widened: widened}, nil
}

// ruleReach is where a kept rule holds, in the words both receipts use.
func ruleReach(kept keptRule) string {
	switch {
	case kept.widened:
		return "in every project, since this conversation has no project of its own"
	case kept.owner == store.OwnerMachine:
		return "on this machine"
	case kept.owner == store.OwnerUser:
		return "in every project"
	}
	return "in this project"
}

// personRuleReceipt is the line /always leaves in the transcript.
func personRuleReceipt(kept keptRule) string {
	return "always · " + kept.memory.Title + " · " + ruleReach(kept)
}

// modelRuleReceipt is what the `remember` tool hands the model for a rule, so
// what it tells the person next is where the rule holds and not a guess.
func modelRuleReceipt(kept keptRule) string {
	return "remembered as a rule: " + kept.memory.Title + " (" + ruleReach(kept) + ")"
}

// RememberAlways keeps one rule the PERSON typed — `/always <text>` — and
// answers the receipt the surface prints. It asks nothing: the person typing it
// is the confirmation the model's own `remember` has to ask for.
//
// everywhere is the surface saying the person typed it where no conversation
// is in front of them (home), which keeps it for the person in every project;
// otherwise it holds in this conversation's project, or for the person when the
// conversation has none ([Agent.alwaysOwnerFor]).
func (a *Agent) RememberAlways(text string, everywhere bool) (string, error) {
	scope := store.MemoryScopeProject
	if everywhere {
		scope = store.MemoryScopeUser
	}
	kept, err := a.keepRule(text, scope)
	if err != nil {
		return "", err
	}
	return personRuleReceipt(kept), nil
}

// AlwaysMemories lists the rules in force here — the ones the next turn carries,
// in the order it carries them — or, with everywhere, the ones that hold for the
// person and this machine wherever they work (what home asks for).
func (a *Agent) AlwaysMemories(everywhere bool) ([]MemoryLine, error) {
	if !a.remembers() {
		return nil, errors.New("this build is not remembering anything")
	}
	owners := a.memoryOwners()
	if everywhere {
		owners = MemoryOwners("")
	}
	rules, err := a.memory.store.AlwaysMemories(owners, 0)
	if err != nil {
		return nil, err
	}
	return memoryLines(rules), nil
}
