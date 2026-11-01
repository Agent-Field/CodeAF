package factory

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ── THE JOB'S BRIEF ────────────────────────────────────────────────────────
//
// A brief is the job's current state in one screen: a verdict, a facts row
// and two to four blocks from a fixed palette. NUMBERS COME FROM THE ENGINE,
// WORDS COME FROM THE MODEL, AND EVERY CLAIM POINTS AT EVIDENCE. The facts
// row is computed here from the steps' structured results ([BriefFacts]) and
// never from anything a model wrote; the verdict and the blocks are a
// writer's, and each claim in them names the step and try it rests on.
//
// The job keeps its last [BriefKeep] versions ([Item.KeepBrief]). Before the
// first one, a reader is answered a stub made from engine data alone
// ([BriefStub]), so the page has something true to show in a job's first
// minutes and whenever no writer is configured.

// BriefKeep is how many brief versions a job keeps, newest last.
const BriefKeep = 20

// The palette's limits. They are the schema: a block over one of them is at
// fault ([BriefBlock.Fault]) and is dropped, never cut to fit.
const (
	BriefFactsMost      = 5
	BriefBlocksMost     = 4
	BriefWordsMost      = 14 // a verdict, and each bullet
	BriefBulletsMost    = 4
	BriefFlowNodesMost  = 6 // in one row
	BriefFlowRowsMost   = 2 // before and after
	BriefDiagramMost    = 8
	BriefTableRowsMost  = 5
	BriefTableColsMost  = 3
	BriefSpotsMost      = 3
	BriefQuestionsMost  = 2
	BriefOptionsLeast   = 2
	BriefOptionsMost    = 3
	BriefShotsMost      = 2
	briefRulesFactKey   = "rules"
	briefVerdictKey     = "verdict"
	briefEvidenceRules  = "rule_checks"
	briefEvidenceClaims = "claims"
)

// The block kinds of the palette. A writer may use only these.
const (
	BlockBullets      = "bullets"
	BlockFlow         = "flow"
	BlockDiagram      = "diagram"
	BlockTable        = "table"
	BlockWhereToLook  = "where_to_look"
	BlockProof        = "proof"
	BlockOpenQuestion = "open_question"
	BlockYourCall     = "your_call"
	BlockScreenshots  = "screenshots"
)

// The reader's status of a job's brief ([BriefStatusOf]).
const (
	BriefStatusReady      = "ready"
	BriefStatusGenerating = "generating"
	BriefStatusFailed     = "failed"
	BriefStatusStub       = "stub"
)

// The states of a write on its way ([BriefWriting]).
const (
	BriefGenerating = "generating"
	BriefFailed     = "failed"
)

// BriefEvidence is what one claim or fact rests on: a step, the try of it,
// what kind of result (tests, exit, findings, claims, rule_checks, hunk,
// screenshot) and, where the kind has several, which one.
type BriefEvidence struct {
	Step string `json:"step"`
	Try  int    `json:"try"`
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
}

// BriefFact is one chip of the facts row. Key is stable across versions (the
// step's name, or "rules"), so a reader can say which chips changed.
type BriefFact struct {
	Key      string        `json:"key"`
	Label    string        `json:"label"`
	Value    string        `json:"value"`
	OK       bool          `json:"ok"`
	Evidence BriefEvidence `json:"evidence"`
}

// BriefClaim is one sentence with what shows it.
type BriefClaim struct {
	Text     string          `json:"text"`
	Evidence []BriefEvidence `json:"evidence"`
}

// BriefNode is one chip of a flow row; Mark is the failure point.
type BriefNode struct {
	Text string `json:"text"`
	Mark bool   `json:"mark,omitempty"`
}

// BriefFlowRow is one labelled row of a flow ("Before", "After").
type BriefFlowRow struct {
	Label string      `json:"label"`
	Nodes []BriefNode `json:"nodes"`
}

// BriefTable is a before/after table: a header and rows of the same width.
type BriefTable struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// BriefSpot is one place to look in the change. ID is what
// factory.job_brief_spot is asked for.
type BriefSpot struct {
	ID       string          `json:"id,omitempty"`
	File     string          `json:"file"`
	Symbol   string          `json:"symbol,omitempty"`
	Lines    int             `json:"lines"`
	Why      string          `json:"why"`
	Evidence []BriefEvidence `json:"evidence"`
}

// BriefProof is the proof step's command and its numbers.
type BriefProof struct {
	Command string `json:"command,omitempty"`
	Shown   int    `json:"shown"`
	Total   int    `json:"total"`
}

// BriefAction is the one request an open question may offer: words for a
// step to act on.
type BriefAction struct {
	Label string `json:"label"`
	Step  string `json:"step"`
	Ask   string `json:"ask,omitempty"`
}

// BriefQuestion is something the evidence does not cover.
type BriefQuestion struct {
	Text     string          `json:"text"`
	Action   *BriefAction    `json:"action,omitempty"`
	Evidence []BriefEvidence `json:"evidence"`
}

// BriefShot is one screenshot a proof step took.
type BriefShot struct {
	Label    string          `json:"label"`
	Ref      string          `json:"ref"`
	Evidence []BriefEvidence `json:"evidence"`
}

// BriefBlock is one block of the palette. Kind says which fields it fills;
// the others stay empty. Evidence is the block's own, for the kinds whose
// content is one claim (flow, diagram, table, proof, your_call).
type BriefBlock struct {
	Kind      string          `json:"kind"`
	Heading   string          `json:"heading"`
	Bullets   []BriefClaim    `json:"bullets,omitempty"`
	Flow      []BriefFlowRow  `json:"flow,omitempty"`
	Mermaid   string          `json:"mermaid,omitempty"`
	Nodes     []string        `json:"nodes,omitempty"`
	Table     *BriefTable     `json:"table,omitempty"`
	Spots     []BriefSpot     `json:"spots,omitempty"`
	Rest      string          `json:"rest,omitempty"`
	Proof     *BriefProof     `json:"proof,omitempty"`
	Questions []BriefQuestion `json:"questions,omitempty"`
	Options   []string        `json:"options,omitempty"`
	Shots     []BriefShot     `json:"shots,omitempty"`
	Evidence  []BriefEvidence `json:"evidence,omitempty"`
}

// Key names the block across versions: its kind and its heading.
func (b BriefBlock) Key() string {
	return b.Kind + "/" + strings.ToLower(strings.TrimSpace(b.Heading))
}

// BriefVersion is one brief as it was written. Rev counts up from 1 on the
// job; a stub has Rev 0 and Stub set and is never kept. ChangedKeys are the
// keys (verdict, a fact's, a block's) that differ from the version before.
type BriefVersion struct {
	Rev             int             `json:"rev"`
	Stub            bool            `json:"stub,omitempty"`
	AfterStep       string          `json:"after_step,omitempty"`
	MadeAt          time.Time       `json:"made_at,omitzero"`
	Model           string          `json:"model,omitempty"`
	CostUSD         float64         `json:"cost_usd,omitempty"`
	Issue           string          `json:"issue"`
	StepsDone       int             `json:"steps_done"`
	StepsTotal      int             `json:"steps_total"`
	Verdict         string          `json:"verdict"`
	VerdictEvidence []BriefEvidence `json:"verdict_evidence,omitempty"`
	Facts           []BriefFact     `json:"facts"`
	Blocks          []BriefBlock    `json:"blocks"`
	ChangedKeys     []string        `json:"changed_keys"`
}

// BriefWriting is a write of the next version on its way, or the last one
// that failed. Nil is neither: the kept versions are current.
type BriefWriting struct {
	State     string    `json:",omitempty"`
	AfterStep string    `json:",omitempty"`
	Failure   string    `json:",omitempty"`
	At        time.Time `json:",omitzero"`
}

// BriefStatusOf is what a reader says about the job's brief.
func BriefStatusOf(it Item) string {
	if w := it.BriefWriting; w != nil && (w.State == BriefGenerating || w.State == BriefFailed) {
		return w.State
	}
	if len(it.Briefs) == 0 {
		return BriefStatusStub
	}
	return BriefStatusReady
}

// LatestBrief is the job's newest kept version, or its stub when it has none.
func LatestBrief(it Item) BriefVersion {
	if n := len(it.Briefs); n > 0 {
		return it.Briefs[n-1]
	}
	return BriefStub(it)
}

// BriefStub is the brief made from engine data alone: the issue's line, the
// steps done of total and the facts, with no blocks and no model.
func BriefStub(it Item) BriefVersion {
	done, total := briefProgress(it)
	return BriefVersion{
		Stub: true, Issue: it.Title, Verdict: it.Title, StepsDone: done, StepsTotal: total,
		Facts: BriefFacts(it), Blocks: []BriefBlock{}, ChangedKeys: []string{},
	}
}

// briefProgress counts a run's phases, or the switched-on steps of a job
// that has not run.
func briefProgress(it Item) (done, total int) {
	if it.Stream != nil && len(it.Stream.Phases) > 0 {
		for _, p := range it.Stream.Phases {
			if p.State == PhaseDone {
				done++
			}
		}
		return done, len(it.Stream.Phases)
	}
	for _, s := range it.Stages {
		if s.On {
			total++
		}
	}
	return 0, total
}

// KeepBrief keeps v as the job's newest version: it numbers it, says what
// changed from the one before, drops all but the last [BriefKeep], and ends
// any write in progress or failed. It answers the version as kept.
func (it *Item) KeepBrief(v BriefVersion) BriefVersion {
	v.Stub = false
	var prev *BriefVersion
	if n := len(it.Briefs); n > 0 {
		prev = &it.Briefs[n-1]
		v.Rev = prev.Rev + 1
	} else {
		v.Rev = 1
	}
	v.ChangedKeys = briefChanged(prev, v)
	if v.Facts == nil {
		v.Facts = []BriefFact{}
	}
	if v.Blocks == nil {
		v.Blocks = []BriefBlock{}
	}
	it.Briefs = append(it.Briefs, v)
	if over := len(it.Briefs) - BriefKeep; over > 0 {
		it.Briefs = slices.Clone(it.Briefs[over:])
	}
	it.BriefWriting = nil
	return v
}

// briefParts is a version as its keyed parts, in reading order.
func briefParts(v BriefVersion) (keys []string, parts map[string]string) {
	parts = map[string]string{briefVerdictKey: v.Verdict}
	keys = []string{briefVerdictKey}
	add := func(key string, part any) {
		raw, _ := json.Marshal(part)
		keys, parts[key] = append(keys, key), string(raw)
	}
	for _, f := range v.Facts {
		add(f.Key, f)
	}
	for _, b := range v.Blocks {
		add(b.Key(), b)
	}
	return keys, parts
}

// briefChanged is every key of v whose part is new or differs from prev's.
func briefChanged(prev *BriefVersion, v BriefVersion) []string {
	keys, parts := briefParts(v)
	if prev == nil {
		return keys
	}
	_, old := briefParts(*prev)
	out := []string{}
	for _, k := range keys {
		if was, ok := old[k]; !ok || was != parts[k] {
			out = append(out, k)
		}
	}
	return out
}

// BriefChangedSince is what changed after the version a person last saw, as
// keys the newest version still has. since 0 is not known, and answers the
// newest version's own change; since older than every kept version answers
// every key of the newest.
func BriefChangedSince(versions []BriefVersion, since int) []string {
	n := len(versions)
	if n == 0 || since >= versions[n-1].Rev {
		return []string{}
	}
	latest := versions[n-1]
	if since <= 0 {
		return slices.Clone(latest.ChangedKeys)
	}
	keys, _ := briefParts(latest)
	if since < versions[0].Rev-1 {
		return keys
	}
	changed := map[string]bool{}
	for _, v := range versions {
		if v.Rev > since {
			for _, k := range v.ChangedKeys {
				changed[k] = true
			}
		}
	}
	out := []string{}
	for _, k := range keys {
		if changed[k] {
			out = append(out, k)
		}
	}
	return out
}

// ── FACTS ───────────────────────────────────────────────────────────────────

// briefFactMaker reads one fact from a step that reported, or says it has
// none. They are tried in order and the first that answers wins, so a step
// is one chip.
type briefFactMaker func(st Stage, p Phase) (BriefFact, bool)

var briefFactMakers = []briefFactMaker{proofFact, checkFact, cleanFact}

// BriefFacts is the facts row: one fact for each step that reported a
// structured result a chip can say (a proof's claims, a check's tests or
// exit, a clean-until step's findings), in the run's order, then the
// factory's rule checks. At most [BriefFactsMost]; the rules fact is kept.
func BriefFacts(it Item) []BriefFact {
	out := []BriefFact{}
	if it.Stream != nil {
		for _, p := range it.Stream.Phases {
			if p.Result == nil {
				continue
			}
			if f, ok := phaseFact(briefStageOf(it.Stages, p), p); ok {
				out = append(out, f)
			}
		}
	}
	rules, hasRules := rulesFact(it)
	most := BriefFactsMost
	if hasRules {
		most--
	}
	out = out[:min(len(out), most)]
	if hasRules {
		out = append(out, rules)
	}
	return out
}

func phaseFact(st Stage, p Phase) (BriefFact, bool) {
	for _, maker := range briefFactMakers {
		if f, ok := maker(st, p); ok {
			f.Key, f.Label = p.Name, briefLabel(p.Name)
			f.Evidence.Step, f.Evidence.Try = p.Name, briefTry(p)
			return f, true
		}
	}
	return BriefFact{}, false
}

// briefStageOf is the job's step a phase runs, or one made from the phase when
// the job's steps no longer name it.
func briefStageOf(stages []Stage, p Phase) Stage {
	for _, s := range stages {
		if s.Name == p.Name {
			return s
		}
	}
	return Stage{Name: p.Name, Kind: p.Kind}
}

// briefTry is which try a phase's result came from: its round, or its
// finished tries when no round was recorded, and one for a step that ran once.
func briefTry(p Phase) int {
	return max(p.Round, len(p.Rounds), 1)
}

func briefLabel(name string) string {
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

func proofFact(st Stage, p Phase) (BriefFact, bool) {
	if !IsProofStage(st) {
		return BriefFact{}, false
	}
	shown := 0
	for _, c := range p.Result.Claims {
		if c.OK {
			shown++
		}
	}
	total := len(p.Result.Claims)
	return BriefFact{
		Value: fmt.Sprintf("%d of %d", shown, total), OK: total > 0 && shown == total,
		Evidence: BriefEvidence{Kind: briefEvidenceClaims},
	}, true
}

func checkFact(st Stage, p Phase) (BriefFact, bool) {
	if st.Kind != StageCheck {
		return BriefFact{}, false
	}
	r := p.Result
	if r.Tests == nil {
		return BriefFact{Value: fmt.Sprintf("exit %d", r.Exit), OK: r.Done && r.Exit == 0, Evidence: BriefEvidence{Kind: "exit"}}, true
	}
	value := fmt.Sprintf("%d passed", r.Tests.Passed)
	for _, part := range []struct {
		n    int
		word string
	}{{r.Tests.Failed, "failed"}, {r.Tests.Skipped, "skipped"}} {
		if part.n > 0 {
			value += fmt.Sprintf(" · %d %s", part.n, part.word)
		}
	}
	return BriefFact{Value: value, OK: r.Tests.Failed == 0 && r.Exit == 0, Evidence: BriefEvidence{Kind: "tests", Ref: r.Tests.Command}}, true
}

func cleanFact(st Stage, p Phase) (BriefFact, bool) {
	if strings.TrimSpace(st.Until) != UntilClean {
		return BriefFact{}, false
	}
	value := "clean"
	switch n := p.Result.Findings; {
	case n == 1:
		value = "1 finding"
	case n > 1:
		value = fmt.Sprintf("%d findings", n)
	}
	return BriefFact{Value: value, OK: p.Result.Done && p.Result.Findings == 0, Evidence: BriefEvidence{Kind: "findings"}}, true
}

// rulesFact is the factory's own rule checks: the change's finding while it
// is still on the job's head, the start rules' otherwise.
func rulesFact(it Item) (BriefFact, bool) {
	rc := it.RuleChecks
	if rc == nil {
		return BriefFact{}, false
	}
	found, ref := rc.Start, "start"
	if rc.Publish != "" && rc.PublishHead == it.HeadCommit {
		found, ref = rc.Publish, "publish"
	}
	if found == "" {
		return BriefFact{}, false
	}
	return BriefFact{
		Key: briefRulesFactKey, Label: "Rules", Value: found, OK: found == CheckPassed,
		Evidence: BriefEvidence{Kind: briefEvidenceRules, Ref: ref},
	}, true
}

// ── THE PALETTE'S LIMITS ────────────────────────────────────────────────────

// BriefWords counts a text's words; a bold lead word is one word.
func BriefWords(s string) int { return len(strings.Fields(s)) }

// BriefVerdictFault says what is wrong with a verdict, "" for nothing.
func BriefVerdictFault(v string) string {
	switch n := BriefWords(v); {
	case n == 0:
		return "the verdict is empty"
	case n > BriefWordsMost:
		return fmt.Sprintf("the verdict has %d words, more than %d", n, BriefWordsMost)
	}
	return ""
}

// briefKindFault checks one kind's own limits.
type briefKindFault func(b BriefBlock, needsYou bool) string

var briefKinds = map[string]briefKindFault{
	BlockBullets:      bulletsFault,
	BlockFlow:         withEvidence(flowFault),
	BlockDiagram:      withEvidence(diagramFault),
	BlockTable:        withEvidence(tableFault),
	BlockWhereToLook:  spotsFault,
	BlockProof:        withEvidence(proofFault),
	BlockOpenQuestion: questionsFault,
	BlockYourCall:     withEvidence(yourCallFault),
	BlockScreenshots:  shotsFault,
}

// BriefBlockKinds is the palette, in the order the handoff names it.
var BriefBlockKinds = []string{BlockBullets, BlockFlow, BlockDiagram, BlockTable, BlockWhereToLook, BlockProof, BlockOpenQuestion, BlockYourCall, BlockScreenshots}

// Fault says what is wrong with the block under the palette's limits, ""
// for nothing. needsYou is whether the job waits on its person, the only
// time a your_call block may stand.
func (b BriefBlock) Fault(needsYou bool) string {
	check, ok := briefKinds[b.Kind]
	if !ok {
		return fmt.Sprintf("%q is not a block of the palette", b.Kind)
	}
	if strings.TrimSpace(b.Heading) == "" {
		return b.Kind + " has no heading"
	}
	return check(b, needsYou)
}

func withEvidence(f func(BriefBlock, bool) string) briefKindFault {
	return func(b BriefBlock, needsYou bool) string {
		if len(b.Evidence) == 0 {
			return b.Kind + " has no evidence"
		}
		return f(b, needsYou)
	}
}

func countFault(what string, n, least, most int) string {
	if n < least || n > most {
		return fmt.Sprintf("%d %s, not %d to %d", n, what, least, most)
	}
	return ""
}

func claimFault(text string, evidence []BriefEvidence, words int) string {
	switch n := BriefWords(text); {
	case n == 0:
		return "a claim is empty"
	case words > 0 && n > words:
		return fmt.Sprintf("a claim has %d words, more than %d", n, words)
	case len(evidence) == 0:
		return fmt.Sprintf("%q has no evidence", text)
	}
	return ""
}

func firstFault(faults ...func() string) string {
	for _, f := range faults {
		if s := f(); s != "" {
			return s
		}
	}
	return ""
}

func bulletsFault(b BriefBlock, _ bool) string {
	if s := countFault("bullets", len(b.Bullets), 1, BriefBulletsMost); s != "" {
		return s
	}
	for _, c := range b.Bullets {
		if s := claimFault(c.Text, c.Evidence, BriefWordsMost); s != "" {
			return s
		}
	}
	return ""
}

func flowFault(b BriefBlock, _ bool) string {
	if s := countFault("flow rows", len(b.Flow), 1, BriefFlowRowsMost); s != "" {
		return s
	}
	for _, row := range b.Flow {
		marks := 0
		for _, n := range row.Nodes {
			if n.Mark {
				marks++
			}
		}
		if s := firstFault(
			func() string { return countFault("nodes in "+row.Label, len(row.Nodes), 1, BriefFlowNodesMost) },
			func() string { return countFault("marked nodes in "+row.Label, marks, 0, 1) },
		); s != "" {
			return s
		}
	}
	return ""
}

func diagramFault(b BriefBlock, _ bool) string {
	if s := countFault("diagram nodes", len(b.Nodes), 2, BriefDiagramMost); s != "" {
		return s
	}
	for _, n := range b.Nodes {
		if !strings.Contains(b.Mermaid, n) {
			return fmt.Sprintf("the diagram does not draw %q", n)
		}
	}
	return ""
}

func tableFault(b BriefBlock, _ bool) string {
	t := b.Table
	if t == nil {
		return "the table is empty"
	}
	if s := firstFault(
		func() string { return countFault("columns", len(t.Columns), 2, BriefTableColsMost) },
		func() string { return countFault("rows", len(t.Rows), 1, BriefTableRowsMost) },
	); s != "" {
		return s
	}
	for _, row := range t.Rows {
		if len(row) != len(t.Columns) {
			return fmt.Sprintf("a row has %d cells under %d columns", len(row), len(t.Columns))
		}
	}
	return ""
}

func spotsFault(b BriefBlock, _ bool) string {
	if s := countFault("spots", len(b.Spots), 1, BriefSpotsMost); s != "" {
		return s
	}
	if strings.TrimSpace(b.Rest) == "" {
		return "where to look does not say what the rest is"
	}
	for _, sp := range b.Spots {
		if strings.TrimSpace(sp.File) == "" {
			return "a spot names no file"
		}
		if s := claimFault(sp.Why, sp.Evidence, 0); s != "" {
			return s
		}
	}
	return ""
}

func proofFault(b BriefBlock, _ bool) string {
	p := b.Proof
	if p == nil || p.Total < 0 || p.Shown < 0 || p.Shown > p.Total {
		return "the proof's numbers do not add up"
	}
	return ""
}

func questionsFault(b BriefBlock, _ bool) string {
	if s := countFault("questions", len(b.Questions), 1, BriefQuestionsMost); s != "" {
		return s
	}
	for _, q := range b.Questions {
		if s := claimFault(q.Text, q.Evidence, 0); s != "" {
			return s
		}
		if q.Action != nil && (strings.TrimSpace(q.Action.Label) == "" || strings.TrimSpace(q.Action.Step) == "") {
			return "a question's action names no step"
		}
	}
	return ""
}

func yourCallFault(b BriefBlock, needsYou bool) string {
	if !needsYou {
		return "your call stands only while the job waits on you"
	}
	return countFault("options", len(b.Options), BriefOptionsLeast, BriefOptionsMost)
}

func shotsFault(b BriefBlock, _ bool) string {
	if s := countFault("screenshots", len(b.Shots), 1, BriefShotsMost); s != "" {
		return s
	}
	for _, sh := range b.Shots {
		if strings.TrimSpace(sh.Ref) == "" || len(sh.Evidence) == 0 {
			return "a screenshot names no image or no evidence"
		}
	}
	return ""
}
