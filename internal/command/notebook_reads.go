package command

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui"
)

// THE NOTEBOOK PAGE'S ONE READ.
//
// The notebook page (internal/tui2/homes) draws three sections — what aforge
// believes, what it knows how to do, what it has been practising — and until
// this file existed nothing filled them: the surface rendered four empty rooms
// and the wiring set three fields (Expanded, Now, Visitor). This is the read
// that fills the rest.
//
// ONE CALL, ONE SNAPSHOT. It is a single method rather than eight because the
// page is drawn from ONE state and the alternative — a surface calling several
// reads and stitching them — is the second opinion problem the chat's scope
// source exists to prevent: two reads taken a poll apart show a belief that a
// craft's survival record already counted. One call means one instant.
//
// EVERY FIGURE IS MEASURED OR ABSENT. Every "Has…" flag here is 12.9.2 taken
// literally: a cost nobody measured is not a cost of zero, a workflow that never
// ran is not a workflow that lost, a trait with no samples is not a trait
// measured zero times. The surface renders absence as absence, and it can only
// do that if this side refuses to pass a zero in place of a fact it does not
// have.
//
// NOTHING HERE IS A RENDERING DECISION. The words the reader sees — "you keep
// correcting", "draft, never run", "practicing" — belong to the page. What this
// file decides is only what is TRUE: which class a belief is, whether a craft
// has a record, which scope is strongest. The one apparent exception is step
// ordinals (see [NotebookCraftStep.Needs]), and it is not a rendering decision
// but the opposite of one: it is how a step id stops being an id before it can
// reach a screen (5.14).

// notebook read windows. None is a policy about what exists; each is what a
// page can show before the reader is scrolling through a table rather than
// reading a notebook.
const (
	notebookBeliefWindow   = 200
	notebookSkillWindow    = 60
	notebookQuestionWindow = 40
	notebookPracticeWindow = 400
	notebookEvidenceCap    = 6
	notebookAttemptCap     = 8
)

// factRetired is the facts table's retired status, spelled here because the
// store exports the word only under its question lifecycle and a skill's
// retirement is the same string for a different kind of row.
const factRetired = "retired"

// NotebookPage is everything the notebook surface draws, read at one instant.
type NotebookPage struct {
	// Beliefs is the window, newest first. Total is how many there are and
	// AtCeiling says Total is a FLOOR rather than a count — there is no count
	// read behind the facts table, so a full window reports itself as one.
	Beliefs   []NotebookBelief
	Total     int
	AtCeiling bool

	Crafts []NotebookCraft
	Skills []NotebookSkill

	Questions  []NotebookQuestion
	Competence NotebookCompetence
	Today      NotebookDay
}

// NotebookBeliefClass is which kind of row a belief is. The strings are the
// classes the page knows and nothing else reaches it.
type NotebookBeliefClass string

const (
	// NotebookBeliefPlain is an ordinary belief about a scope.
	NotebookBeliefPlain NotebookBeliefClass = ""
	// NotebookBeliefTaste is a rule the user keeps correcting toward — a
	// preference fact on a taste shelf (store.TasteScopePrefix).
	NotebookBeliefTaste NotebookBeliefClass = "taste"
	// NotebookBeliefTrait is a measurement about the user.
	NotebookBeliefTrait NotebookBeliefClass = "trait"
	// NotebookBeliefPlaybook is a scoped strategy earned from earlier work.
	NotebookBeliefPlaybook NotebookBeliefClass = "playbook"
)

// NotebookBelief is one row of the beliefs section.
type NotebookBelief struct {
	// Seq is the fact's sequence — the handle a verb carries, never rendered.
	Seq   int64
	Body  string
	Scope string
	Kind  string
	Class NotebookBeliefClass
	// Channel is store.FactChannel verbatim: stated, inferred, distilled, trial.
	Channel string
	// Status is the row's lifecycle where its class has one. A taste rule is
	// "forming" while it is a candidate and "kept" once it stands.
	Status string
	// Trust is store.CredibilityWord's reading of the fact's confidence.
	Trust string
	// Samples is how many measurements are behind a trait.
	Samples    int
	HasSamples bool

	Learned  time.Time
	LastUsed time.Time
	Uses     int
	HasUses  bool

	// Retired is a belief the user let go; Provisional is a candidate or a
	// superseded line — known, not load bearing.
	Retired     bool
	Provisional bool

	// Evidence names the work that taught it, already resolved to titles a
	// person can read (see [NotebookEvidence]).
	Evidence []NotebookEvidence
}

// NotebookEvidence is one piece of work behind a belief, resolved to a NAME.
//
// The surface used to be handed node ids and "#seq" strings and drew them
// verbatim — `taught by  task-5381` — which is a raw id on a screen and banned
// (5.14, design-law-v2 §19). Resolving belongs here because this is the side
// that can read the graph: the page may not, and a page that could would be a
// page that reads a store per frame.
type NotebookEvidence struct {
	// Name is the work's own title, or empty when the graph no longer holds it
	// — a folded node, a task from before a rebuild. The surface says "a past
	// task" for an empty name and never falls back to the handle.
	Name string
	// Room is the node the work ran as, carried so a surface can open it. It is
	// a handle and never rendered; empty means the reference is not work at all
	// but an earlier note in the notebook.
	Room string
	// When the work happened, so an unnamed reference can still be placed in
	// time.
	When time.Time
}

// NotebookCraft is one learned workflow.
type NotebookCraft struct {
	Name        string
	Description string
	Dir         string
	// Proved and Against are the survival record; HasRecord false means the
	// workflow has never run, which is not the same fact as never having won.
	Proved    int
	Against   int
	HasRecord bool
	// LastCostUSD is what the most recent clean run spent, whole subtree.
	LastCostUSD float64
	HasCost     bool
	// Version is how many commits are behind the file today.
	Version int
	Updated time.Time
	// CeilingUSD and Wall are the bounds a run will obey.
	CeilingUSD float64
	HasCeiling bool
	Wall       time.Duration

	Steps   []NotebookCraftStep
	History []NotebookCraftVersion
}

// NotebookCraftStep is one leaf of a workflow in file order.
type NotebookCraftStep struct {
	Brief string
	// Needs are the ORDINALS of the steps this one waits for — "1", "3" — and
	// not their ids. A step id is an id (5.14) and a surface may not render one,
	// so the resolution happens here, where the file order that defines the
	// ordinals is already in hand. A dependency on a step that is not in the
	// file is dropped rather than passed through as a name.
	Needs  []string
	Model  string
	Skill  string
	Verify bool
}

// NotebookCraftVersion is one commit behind a workflow.
type NotebookCraftVersion struct {
	// Version is the `vN` this commit is, counted from the first commit.
	Version int
	Subject string
	When    time.Time
}

// NotebookSkill is one forged tool.
type NotebookSkill struct {
	Seq  int64
	Name string
	Body string
	Path string
	Uses int
	// HasUses is false for a tool nothing has reached for yet, so the row can
	// tell "never used" from "not counted".
	HasUses bool
	Learned time.Time
	Retired bool
	// Note is the store's own sentence about a retirement.
	Note string
}

// NotebookQuestion is one durable knowledge gap and what drilling it cost.
type NotebookQuestion struct {
	Seq  int64
	Body string
	// Scope is what the gap is about.
	Scope string
	// Status is store's question lifecycle: open, practicing, resolved, retired.
	Status  string
	Runs    int
	CostUSD float64
	HasCost bool
	Asked   time.Time
	// Note is the store's status note — why it ended, in its own words.
	Note     string
	Attempts []NotebookAttempt
}

// NotebookAttempt is one practice round.
type NotebookAttempt struct {
	CostUSD float64
	HasCost bool
	// Delta is the surprise the round removed, negative when the world got more
	// predictable. HasDelta false is a round that has not landed an outcome.
	Delta    float64
	HasDelta bool
	When     time.Time
}

// NotebookCompetence is the one measured line about how good aforge is.
type NotebookCompetence struct {
	Strongest string
	Frontier  string
}

// NotebookDay is the day receipt.
type NotebookDay struct {
	SpendUSD float64
	// HasSpend is false on a day with no self-directed work at all, so the
	// receipt renders nothing rather than $0.00.
	HasSpend  bool
	Learned   int
	Practiced time.Duration
}

// Notebook reads everything the notebook page draws.
//
// A Commander with no store answers with a zero page, which the surface renders
// as three bands teaching what would put something in them — the same picture a
// brand new machine draws, which is the property that lets a visitor window and
// an empty one look honestly alike.
//
// now is the clock the day figures are taken against, passed in rather than read
// here so the surface's frame and its receipts agree about what "today" is.
func (c *Commander) NotebookPage(now time.Time) NotebookPage {
	if c == nil || c.store == nil {
		return NotebookPage{}
	}
	page := NotebookPage{}
	page.Beliefs, page.Total, page.AtCeiling = c.notebookBeliefs()
	page.Crafts = c.notebookCrafts()
	page.Skills = c.notebookSkills()
	page.Questions = c.notebookQuestions()
	page.Competence = c.notebookCompetence(now)
	page.Today = c.notebookDay(now)
	return page
}

// -- beliefs -----------------------------------------------------------------

// notebookBeliefs reads the belief window and sorts each row into its class.
//
// ONE READ FOR FOUR CLASSES. Facts(limit) already returns every kind in journal
// order, so taste rules, traits and playbooks arrive with the plain beliefs and
// the classing is a switch rather than four queries. TasteRules is asked as well
// — but only for its SHELF SET, because that read is the one place that knows
// which line on a shelf is current, and a superseded taste line in the window
// would otherwise be drawn as a rule that stands.
func (c *Commander) notebookBeliefs() ([]NotebookBelief, int, bool) {
	facts, err := c.store.Facts(notebookBeliefWindow)
	if err != nil {
		return nil, 0, false
	}
	current := map[int64]string{}
	if rules, err := c.store.TasteRules(""); err == nil {
		for _, rule := range rules {
			current[rule.Seq] = rule.Status
		}
	}
	evidence := c.notebookEvidence(facts)

	out := make([]NotebookBelief, 0, len(facts))
	for _, fact := range facts {
		row, ok := notebookBelief(fact, current)
		if !ok {
			continue
		}
		row.Evidence = evidence[fact.Seq]
		out = append(out, row)
	}
	// The window IS the count until a count read exists (the surface's state.go
	// records the gap): a full window reports itself as a floor.
	return out, len(out), len(facts) >= notebookBeliefWindow
}

// notebookBelief maps one fact onto a row, or refuses it. Skills, questions and
// unsettled pairs are refused because each is drawn somewhere else — the
// know-how band, the practice band, and (for an unsettled pair) as the question
// it will become.
func notebookBelief(fact store.Fact, tasteShelves map[int64]string) (NotebookBelief, bool) {
	switch fact.Kind {
	case store.FactSkill, store.FactQuestion, store.FactUnsettled:
		return NotebookBelief{}, false
	}
	row := NotebookBelief{
		Seq: fact.Seq, Body: fact.Body, Scope: fact.Scope,
		Kind: string(fact.Kind), Channel: string(fact.Channel),
		Learned: fact.Time, LastUsed: fact.LastUsed,
		Uses: fact.Uses, HasUses: fact.Uses > 0,
		Retired:     fact.Status == store.FactQuarantined,
		Provisional: fact.Status == store.FactCandidate || fact.Status == store.FactSuperseded,
	}
	if fact.Channel != "" {
		row.Trust = store.CredibilityWord(fact.Confidence)
	}
	switch {
	case fact.Kind == store.FactTrait:
		row.Class = NotebookBeliefTrait
		row.Trust = ""
		measurement, ok := notebookTrait(fact)
		if !ok {
			return NotebookBelief{}, false
		}
		row.Body = measurement.body
		row.Samples, row.HasSamples = measurement.samples, true
		if !measurement.updated.IsZero() {
			row.Learned = measurement.updated
		}
	case fact.Kind == store.FactPlaybook:
		row.Class = NotebookBeliefPlaybook
	case strings.HasPrefix(fact.Scope, store.TasteScopePrefix):
		status, current := tasteShelves[fact.Seq]
		if !current {
			// A superseded line on a shelf is history, and the shelf's current
			// line is already in this window.
			return NotebookBelief{}, false
		}
		row.Class = NotebookBeliefTaste
		row.Status = notebookTasteWord(status)
		if subject, ok := store.TasteSubject(fact.Scope); ok {
			row.Scope = subject
		}
	}
	return row, true
}

// notebookTasteWord is a shelf's standing in the product's language. It is the
// one word this file spends on presentation, and it is here rather than on the
// page because "candidate" and "active" are the STORE's words for a lifecycle
// only taste has — a page that translated them would be a page that had to know
// what a shelf is.
func notebookTasteWord(status string) string {
	switch status {
	case store.FactCandidate:
		return "forming"
	case store.FactActive:
		return "kept"
	}
	return ""
}

type traitReading struct {
	body    string
	samples int
	updated time.Time
}

// notebookTrait decodes a trait fact's measurement. A trait whose body is not a
// measurement is refused rather than drawn as prose: its body is JSON, and JSON
// on a page is the raw payload 12.5 forbids.
func notebookTrait(fact store.Fact) (traitReading, bool) {
	var measurement store.TraitMeasurement
	if err := json.Unmarshal([]byte(fact.Body), &measurement); err != nil {
		return traitReading{}, false
	}
	body := notebookTraitValue(measurement.Value)
	if body == "" {
		return traitReading{}, false
	}
	name := strings.TrimPrefix(fact.Scope, "trait:")
	if name != "" && name != fact.Scope {
		body = strings.ReplaceAll(name, "-", " ") + " " + body
	}
	return traitReading{body: body, samples: measurement.N, updated: measurement.Updated}, true
}

// notebookTraitValue renders a measurement's value as the short phrase it is.
// A trait's value is a number, a boolean or a word; anything else is refused,
// because a measurement nobody can read is not a row.
func notebookTraitValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case bool:
		if v {
			return "yes"
		}
		return "no"
	case float64:
		return strconv.FormatFloat(v, 'g', 4, 64)
	case int:
		return strconv.Itoa(v)
	}
	return ""
}

// notebookEvidence resolves what taught each belief, in ONE pass over the window
// rather than one pass per belief, and resolves every node reference to the
// TITLE of the work behind it.
//
// (*Commander).NotebookEvidence answers for a single fact and re-reads the whole
// table to do it, which is the right shape for a drill and the wrong shape for a
// list: forty beliefs would be forty full scans of the notebook. The references
// are the same ones it finds — the node a fact was written under, the facts that
// cite it — with one addition this wave: the node is looked up and named. The
// lookup is CACHED across the whole window, because a burst of beliefs learned
// inside one job all name the same node, and it is one indexed point read per
// distinct node rather than a scan.
func (c *Commander) notebookEvidence(facts []store.Fact) map[int64][]NotebookEvidence {
	if len(facts) == 0 {
		return nil
	}
	bySeq := make(map[int64]store.Fact, len(facts))
	for _, fact := range facts {
		bySeq[fact.Seq] = fact
	}
	titles := map[string]string{}
	name := func(nodeID string) string {
		if title, ok := titles[nodeID]; ok {
			return title
		}
		title := ""
		if node, found, err := c.store.Node(nodeID); err == nil && found {
			title = notebookNodeName(node)
		}
		titles[nodeID] = title
		return title
	}

	out := make(map[int64][]NotebookEvidence, len(facts))
	add := func(target int64, fact store.Fact) {
		if len(out[target]) >= notebookEvidenceCap {
			return
		}
		ref := NotebookEvidence{When: fact.Time}
		if node := strings.TrimSpace(fact.NodeID); node != "" && node != store.RootID {
			ref.Room = node
			ref.Name = name(node)
		}
		for _, seen := range out[target] {
			// One work, one line. A job that taught six beliefs in one run is
			// still one thing that taught them, and a reference with no room is
			// deduped on its instant for the same reason.
			if seen.Room == ref.Room && (ref.Room != "" || seen.When.Equal(ref.When)) {
				return
			}
		}
		out[target] = append(out[target], ref)
	}
	for _, fact := range facts {
		if strings.TrimSpace(fact.NodeID) != "" && fact.NodeID != store.RootID {
			add(fact.Seq, fact)
		}
		if fact.EvidenceSeq > 0 {
			add(fact.EvidenceSeq, fact)
		}
		if fact.Unsettled == nil {
			continue
		}
		for _, approach := range fact.Unsettled.Approaches {
			for _, seq := range approach.Evidence {
				if source, ok := bySeq[seq]; ok {
					add(fact.Seq, source)
				}
			}
		}
	}
	return out
}

// notebookNodeName is the work's own words: its title where the planner gave it
// one, its brief otherwise, flattened to a line. A node with neither is left
// unnamed rather than named after its id.
func notebookNodeName(node store.Node) string {
	for _, candidate := range []string{node.Title, node.Brief} {
		if line := strings.TrimSpace(strings.SplitN(candidate, "\n", 2)[0]); line != "" {
			return line
		}
	}
	return ""
}

// -- know-how ----------------------------------------------------------------

// notebookCrafts reads the workflow catalogue and, for each, the survival record
// the resident keeps as a trait.
//
// The record is a TRAIT and not a table: internal/resident writes one under
// resident.CraftSurvivalKey(name) after every run, which is where this system's
// second-order facts about itself already live. Its absence is the honest signal
// that a workflow has never run — see [NotebookCraft.HasRecord].
func (c *Commander) notebookCrafts() []NotebookCraft {
	summaries, err := c.Crafts()
	if err != nil && len(summaries) == 0 {
		return nil
	}
	out := make([]NotebookCraft, 0, len(summaries))
	for _, summary := range summaries {
		craft := NotebookCraft{
			Name: summary.Name, Description: summary.Description, Updated: summary.When,
		}
		if detail, ok := c.CraftDetail(summary.Name); ok {
			craft.Description = detail.Description
			craft.Dir = detail.Dir
			craft.CeilingUSD, craft.HasCeiling = detail.CostUSD, detail.CostUSD > 0
			craft.Wall = detail.WallClock
			craft.Steps = notebookSteps(detail)
			craft.History = notebookHistory(detail)
			craft.Version = len(detail.History)
			if len(detail.History) > 0 {
				craft.Updated = detail.History[0].When
			}
		}
		if survival, ok := c.craftSurvival(summary.Name); ok {
			craft.Proved, craft.Against, craft.HasRecord = survival.For, survival.Against, true
			craft.LastCostUSD, craft.HasCost = survival.LastCost, survival.LastCost > 0
		}
		out = append(out, craft)
	}
	return out
}

// craftSurvival decodes one workflow's record. A store that refuses, or a
// workflow with no record, answers false — never a zeroed record, which would
// draw as a workflow that has run and never won.
func (c *Commander) craftSurvival(name string) (resident.CraftSurvival, bool) {
	measurement, _, found, err := c.store.Trait(resident.CraftSurvivalKey(name))
	if err != nil || !found {
		return resident.CraftSurvival{}, false
	}
	// The measured value is the record, and it arrives through JSON as a generic
	// map; re-marshalling it is the one honest way to get the struct back
	// without teaching this file the payload's field names twice.
	raw, err := json.Marshal(measurement.Value)
	if err != nil {
		return resident.CraftSurvival{}, false
	}
	var survival resident.CraftSurvival
	if err := json.Unmarshal(raw, &survival); err != nil {
		return resident.CraftSurvival{}, false
	}
	return survival, true
}

// notebookSteps flattens a workflow's steps and resolves every dependency from a
// step ID to the ORDINAL a reader can see (see [NotebookCraftStep.Needs]).
func notebookSteps(detail tui.CraftDetail) []NotebookCraftStep {
	ordinal := make(map[string]string, len(detail.Steps))
	for i, step := range detail.Steps {
		if step.ID != "" {
			ordinal[step.ID] = strconv.Itoa(i + 1)
		}
	}
	out := make([]NotebookCraftStep, 0, len(detail.Steps))
	for _, step := range detail.Steps {
		entry := NotebookCraftStep{
			Brief: step.Brief, Model: step.Model, Skill: step.Skill,
			Verify: strings.TrimSpace(step.Verify) != "",
		}
		for _, need := range step.Needs {
			if n, ok := ordinal[need]; ok {
				entry.Needs = append(entry.Needs, n)
			}
		}
		out = append(out, entry)
	}
	return out
}

// notebookHistory numbers a workflow's commits. git log is newest first and the
// version numbers count from the FIRST commit, so the newest entry is vN.
func notebookHistory(detail tui.CraftDetail) []NotebookCraftVersion {
	out := make([]NotebookCraftVersion, 0, len(detail.History))
	for i, version := range detail.History {
		out = append(out, NotebookCraftVersion{
			Version: len(detail.History) - i, Subject: version.Subject, When: version.When,
		})
	}
	return out
}

// notebookSkills reads the forged tools. Every status is read, retired included:
// a tool that vanished is a tool nobody can tell you stopped using, so the row
// stays and says so.
func (c *Commander) notebookSkills() []NotebookSkill {
	facts, err := c.store.SkillFacts("", notebookSkillWindow)
	if err != nil {
		return nil
	}
	out := make([]NotebookSkill, 0, len(facts))
	for _, fact := range facts {
		if fact.Status == store.FactSuperseded {
			continue
		}
		out = append(out, NotebookSkill{
			Seq:  fact.Seq,
			Name: notebookSkillName(fact),
			Body: fact.Body,
			Path: fact.Artifact,
			Uses: fact.Uses, HasUses: fact.Uses > 0,
			Learned: fact.Time,
			Retired: fact.Status == factRetired || fact.Status == store.FactQuarantined,
			Note:    fact.StatusNote,
		})
	}
	return out
}

// notebookSkillName is the tool's word: the last element of its installed path,
// which is the name work reaches for it by. A skill with no artifact yet — a
// candidate that has not been promoted — is named by the first words of what it
// does, because a nameless row is a row nobody can talk about.
func notebookSkillName(fact store.Fact) string {
	if artifact := strings.TrimRight(strings.TrimSpace(fact.Artifact), "/"); artifact != "" {
		if cut := strings.LastIndexByte(artifact, '/'); cut >= 0 && cut+1 < len(artifact) {
			return artifact[cut+1:]
		}
		return artifact
	}
	body := strings.TrimSpace(strings.SplitN(fact.Body, "\n", 2)[0])
	if len(body) > 48 {
		body = strings.TrimSpace(body[:48])
	}
	return body
}

// -- practice ----------------------------------------------------------------

// notebookQuestions reads the knowledge gaps, their rounds and what the rounds
// cost.
//
// THE MONEY COMES FROM THE SELF RECEIPTS, not from a new join. A practice round
// is journaled as a self receipt whose target is the question (TargetKind
// "fact", TargetID the question's seq — internal/store/self_receipt.go's
// selfReceiptIdentity), so the receipts already answer "what has this gap cost"
// and "how much less surprising did each round leave the world".
func (c *Commander) notebookQuestions() []NotebookQuestion {
	facts, err := c.store.Questions("", notebookQuestionWindow)
	if err != nil {
		return nil
	}
	rounds := map[int64]int{}
	if practices, err := c.store.RecentQuestionPractices(notebookPracticeWindow); err == nil {
		for _, practice := range practices {
			rounds[practice.QuestionSeq]++
		}
	}
	attempts := map[int64][]NotebookAttempt{}
	spend := map[int64]float64{}
	if receipts, err := c.store.SelfReceipts(time.Time{}); err == nil {
		for _, receipt := range receipts {
			if receipt.TargetKind != "fact" {
				continue
			}
			seq, err := strconv.ParseInt(receipt.TargetID, 10, 64)
			if err != nil {
				continue
			}
			spend[seq] += receipt.Cost
			attempt := NotebookAttempt{
				CostUSD: receipt.Cost, HasCost: receipt.Cost > 0, When: receipt.Time,
			}
			if receipt.SurpriseDelta != nil {
				attempt.Delta, attempt.HasDelta = *receipt.SurpriseDelta, true
			}
			attempts[seq] = append(attempts[seq], attempt)
		}
	}

	out := make([]NotebookQuestion, 0, len(facts))
	for _, fact := range facts {
		question := NotebookQuestion{
			Seq: fact.Seq, Body: fact.Body, Scope: fact.Scope,
			Status: fact.Status, Asked: fact.Time, Note: fact.StatusNote,
			Runs: rounds[fact.Seq],
		}
		if cost := spend[fact.Seq]; cost > 0 {
			question.CostUSD, question.HasCost = cost, true
		}
		question.Attempts = notebookTail(attempts[fact.Seq])
		// A gap with receipts but no admitted round still ran: the receipts are
		// the evidence, and a run count that ignored them would be lower than
		// the list of attempts under it.
		if question.Runs < len(attempts[fact.Seq]) {
			question.Runs = len(attempts[fact.Seq])
		}
		out = append(out, question)
	}
	return out
}

// notebookTail keeps the newest attempts, oldest first, so a long-running gap
// shows what it has been doing lately rather than what it did first.
func notebookTail(attempts []NotebookAttempt) []NotebookAttempt {
	sort.SliceStable(attempts, func(i, j int) bool { return attempts[i].When.Before(attempts[j].When) })
	if len(attempts) > notebookAttemptCap {
		attempts = attempts[len(attempts)-notebookAttemptCap:]
	}
	return attempts
}

// notebookCompetence is the one measured line about how good aforge is: the
// best-established strong scope and the best-established frontier one.
//
// BEST-ESTABLISHED, not best-scoring: the map's own classes already decide what
// strong and frontier mean, and among equals the scope with the most samples is
// the one the claim is safest about. A machine with nothing measured says
// nothing, which is why both halves may come back empty.
func (c *Commander) notebookCompetence(now time.Time) NotebookCompetence {
	measured, err := c.store.CompetenceMap(store.CompetenceOptions{Now: now})
	if err != nil {
		return NotebookCompetence{}
	}
	out := NotebookCompetence{}
	strongest, frontier := 0, 0
	for _, scope := range measured.Scopes {
		switch scope.Class {
		case store.CompetenceStrong:
			if scope.Samples > strongest {
				out.Strongest, strongest = scope.Scope, scope.Samples
			}
		case store.CompetenceFrontier:
			if scope.Samples > frontier {
				out.Frontier, frontier = scope.Scope, scope.Samples
			}
		}
	}
	return out
}

// notebookDay is the day receipt: what self-directed work cost since local
// midnight, how much of the day was practice, and how much was learned.
//
// LEARNED IS A UNION, not a row count. A receipt may name the same fact twice
// and the person is being told how much was learned, not how many rows were
// written.
func (c *Commander) notebookDay(now time.Time) NotebookDay {
	day := NotebookDay{}
	if spend, err := c.store.SelfSpendToday(); err == nil && spend > 0 {
		day.SpendUSD, day.HasSpend = spend, true
	}
	if practiced, err := c.store.PracticedToday(now); err == nil && practiced > 0 {
		day.Practiced = practiced
	}
	start, _ := notebookDayBounds(now)
	if receipts, err := c.store.SelfReceipts(start); err == nil {
		learned := make(map[int64]bool, len(receipts))
		for _, receipt := range receipts {
			for _, seq := range receipt.FactIDs {
				learned[seq] = true
			}
			for _, seq := range receipt.SkillIDs {
				learned[seq] = true
			}
		}
		day.Learned = len(learned)
	}
	return day
}

// notebookDayBounds is local midnight either side of now. It is spelled here
// rather than reached for in the store because store's own bounds helper is
// unexported, and one four-line function is a smaller thing to own than an
// exported time helper nobody else asked for.
func notebookDayBounds(now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	local := now.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}
