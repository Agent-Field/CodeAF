package factory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ── THE BRIEF'S WRITER: what it is given, and what of its answer is kept ───
//
// The writer is one small model call, not the manager's chat turn. The
// factory assembles everything it may read ([BriefInputOf]) from the job's
// own records: the issue, the plan, each step's structured result per try,
// the change's stats and a hunk index, the rules that applied and the brief
// before. It is never the full diff.
//
// THE MODEL'S ANSWER IS A DRAFT, NOT A BRIEF. [CheckBriefDraft] keeps only
// what passes: the palette's limits, evidence that names a step and try that
// exist, and every number in a claim's text found in that claim's evidence.
// A claim or block that fails is dropped, and each drop is said; a verdict
// that fails keeps the previous version. The facts row is never the
// model's: it is computed again from the job as the version is kept.

// The writer's cost guard: at most BriefVersionCostMost a version, and the
// brief's spend at most BriefSpendShare of the job's. Over either, the brief
// is written only when an ask opens, at the end, or when a person asks for it.
const (
	BriefVersionCostMost = 0.03
	BriefSpendShare      = 0.05
	// BriefDebounce is how long a job's inputs must stand still before the
	// writer reads them, so a burst of step results is one version.
	BriefDebounce = 10 * time.Second
	// briefOutputMost bounds each step output and the issue body the writer
	// reads, in bytes.
	briefOutputMost = 2 << 10
	briefHunksMost  = 40
)

// The triggers of a write. A person's retry and an ask that opened are
// written whatever the cost guard says; the end of a job is too.
const (
	BriefTriggerStep  = "step"  // a step reported
	BriefTriggerEdit  = "edit"  // the steps or a model changed
	BriefTriggerRead  = "read"  // a GitHub comment was read
	BriefTriggerAsk   = "ask"   // the job began waiting on its person
	BriefTriggerEnd   = "end"   // the job finished
	BriefTriggerRetry = "retry" // a person asked for it again
)

// briefForced are the triggers the cost guard never holds back.
var briefForced = map[string]bool{BriefTriggerAsk: true, BriefTriggerEnd: true, BriefTriggerRetry: true}

// BriefTriggers is every trigger word, for the wire's enum.
var BriefTriggers = []string{BriefTriggerStep, BriefTriggerEdit, BriefTriggerRead, BriefTriggerAsk, BriefTriggerEnd, BriefTriggerRetry}

// BriefStepInput is one try of one step as the writer reads it.
type BriefStepInput struct {
	Step     string      `json:"step"`
	Kind     string      `json:"kind"`
	Until    string      `json:"until,omitempty"`
	State    string      `json:"state"`
	Try      int         `json:"try"`
	Latest   bool        `json:"latest"`
	Summary  string      `json:"summary,omitempty"`
	Rejected bool        `json:"rejected,omitempty"`
	Done     bool        `json:"done,omitempty"`
	Findings int         `json:"findings,omitempty"`
	Exit     int         `json:"exit,omitempty"`
	Tests    *TestCounts `json:"tests,omitempty"`
	Claims   []Claim     `json:"claims,omitempty"`
	Output   string      `json:"output,omitempty"`
}

// BriefInput is everything the writer reads. Previous is not part of the
// input's hash: a new version changes it, and that alone is no reason to
// write another.
type BriefInput struct {
	Issue struct {
		Title  string   `json:"title"`
		Body   string   `json:"body,omitempty"`
		Labels []string `json:"labels,omitempty"`
	} `json:"issue"`
	NeedsYou bool             `json:"needs_you"`
	Question string           `json:"question,omitempty"`
	Model    string           `json:"model,omitempty"`
	Plan     string           `json:"plan,omitempty"`
	Steps    []BriefStepInput `json:"steps"`
	Change   *ChangeStat      `json:"change,omitempty"`
	Hunks    []PatchHunk      `json:"hunks,omitempty"`
	Rules    *RuleChecks      `json:"rules,omitempty"`
	Hold     string           `json:"rule_hold,omitempty"`
	Facts    []BriefFact      `json:"facts"`
	Previous *BriefVersion    `json:"previous,omitempty"`
}

// BriefInputOf assembles the writer's input from the job.
func BriefInputOf(it Item) BriefInput {
	var in BriefInput
	in.Issue.Title, in.Issue.Body, in.Issue.Labels = it.Title, clip(it.Body, briefOutputMost), it.Labels
	in.NeedsYou, in.Question, in.Model = it.State == StateNeedsYou, it.Question, it.Model
	in.Rules, in.Hold, in.Facts = it.RuleChecks, it.RuleHold, BriefFacts(it)
	in.Steps = []BriefStepInput{}
	if it.Stream != nil {
		for _, p := range it.Stream.Phases {
			in.Steps = append(in.Steps, stepInputs(briefStageOf(it.Stages, p), p)...)
			if p.Name == "plan" && p.Result != nil {
				in.Plan = clip(p.Result.Output, briefOutputMost)
			}
		}
	}
	if it.Change != nil {
		in.Change = it.Change.Stat
		hunks := PatchHunks(it.Change.Patch)
		in.Hunks = hunks[:min(len(hunks), briefHunksMost)]
	}
	if n := len(it.Briefs); n > 0 {
		prev := it.Briefs[n-1]
		in.Previous = &prev
	}
	return in
}

// stepInputs is one line per finished try of a phase, then its latest
// result, which carries the structured fields.
func stepInputs(st Stage, p Phase) []BriefStepInput {
	base := BriefStepInput{Step: p.Name, Kind: string(st.Kind), Until: st.Until, State: string(p.State)}
	var out []BriefStepInput
	latest := briefTry(p)
	for _, r := range p.Rounds {
		if r.Round == latest {
			continue
		}
		line := base
		line.Try, line.Summary, line.Rejected = r.Round, r.Summary, r.Rejected
		out = append(out, line)
	}
	last := base
	last.Try, last.Latest = latest, true
	if r := p.Result; r != nil {
		last.Done, last.Findings, last.Exit, last.Tests = r.Done, r.Findings, r.Exit, r.Tests
		last.Claims, last.Output = r.Claims, clip(r.Output, briefOutputMost)
	}
	return append(out, last)
}

func clip(s string, most int) string {
	if len(s) <= most {
		return s
	}
	return s[:most]
}

// BriefInputHash is the hash of what the writer reads, without the brief
// before: equal hashes are the same job to write about.
func BriefInputHash(in BriefInput) string {
	in.Previous = nil
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// BriefSpent is what the job's kept brief versions cost.
func BriefSpent(it Item) float64 {
	total := 0.0
	for _, v := range it.Briefs {
		total += v.CostUSD
	}
	return total
}

// The reasons a write is not begun ([BriefBeginSkip]).
const (
	BriefSkipUnchanged = "unchanged"
	BriefSkipCost      = "cost"
	BriefSkipWriting   = "writing"
)

// BriefBeginSkip says why a write should not begin now, "" when it should:
// another write is on its way; the inputs are the ones the newest version (or
// the last failed write) was made from; or the cost guard holds it back. A
// retry skips only for a write on its way.
func BriefBeginSkip(it Item, trigger, hash string, now time.Time) string {
	if w := it.BriefWriting; w != nil && w.State == BriefGenerating && now.Sub(w.At) < briefWritingStale {
		return BriefSkipWriting
	}
	if trigger == BriefTriggerRetry {
		return ""
	}
	if hash == briefLastHash(it) {
		return BriefSkipUnchanged
	}
	if !briefForced[trigger] && !BriefCostAllowed(it) {
		return BriefSkipCost
	}
	return ""
}

// briefWritingStale is how long a write on its way holds the next back; one
// that has not finished by then is taken as lost.
const briefWritingStale = 2 * time.Minute

func briefLastHash(it Item) string {
	if w := it.BriefWriting; w != nil && w.Hash != "" {
		return w.Hash
	}
	if n := len(it.Briefs); n > 0 {
		return it.Briefs[n-1].InputHash
	}
	return ""
}

// BriefCostAllowed is the cost guard for a write no one asked for: the last
// version cost at most [BriefVersionCostMost], and one more like it keeps
// the brief's spend within [BriefSpendShare] of the job's. The first version
// is always allowed, since nothing is known of its cost.
func BriefCostAllowed(it Item) bool {
	n := len(it.Briefs)
	if n == 0 {
		return true
	}
	last := it.Briefs[n-1].CostUSD
	return last <= BriefVersionCostMost && BriefSpent(it)+last <= BriefSpendShare*JobSpent(it)
}

// BriefDraft is the writer's answer: the words, the blocks it picked and the
// evidence of each claim. Its facts are never read; the engine has them.
type BriefDraft struct {
	Verdict         string          `json:"verdict"`
	VerdictEvidence []BriefEvidence `json:"verdict_evidence"`
	Blocks          []BriefBlock    `json:"blocks"`
}

// BriefDrop is one part of a draft that was not kept, and why.
type BriefDrop struct {
	Part   string `json:"part"`
	Reason string `json:"reason"`
}

// CheckBriefDraft reads the writer's JSON against the job as it is now. It
// answers the version to keep (facts computed, model and cost left to the
// caller), each part dropped, and the verdict's fault, "" when the verdict
// passed. An answer that is not one JSON object with a verdict is a verdict
// fault; a block that does not read as a block is a drop.
//
// THE READING IS FORGIVING OF SHAPE, NEVER OF CONTENT. A field the draft does
// not use is ignored, and one evidence object where a list belongs is read as
// a list of one ([briefLists]): neither changes what a claim says or rests
// on, and refusing them would throw away a whole brief over punctuation.
// Everything the claim says is still checked.
func CheckBriefDraft(it Item, raw []byte) (BriefVersion, []BriefDrop, string) {
	var top struct {
		Verdict         string            `json:"verdict"`
		VerdictEvidence json.RawMessage   `json:"verdict_evidence"`
		Blocks          []json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &top); err != nil {
		return BriefVersion{}, nil, "the answer is not a brief: " + err.Error()
	}
	d := BriefDraft{Verdict: top.Verdict}
	if len(top.VerdictEvidence) > 0 && json.Unmarshal(briefLists(top.VerdictEvidence), &d.VerdictEvidence) != nil {
		return BriefVersion{}, nil, "the verdict's evidence is not a list of evidence"
	}
	var unread []BriefDrop
	for i, rawBlock := range top.Blocks {
		var b BriefBlock
		if err := json.Unmarshal(briefLists(rawBlock), &b); err != nil {
			unread = append(unread, BriefDrop{Part: fmt.Sprintf("block %d", i+1), Reason: "not a block: " + err.Error()})
			continue
		}
		d.Blocks = append(d.Blocks, b)
	}
	ev := briefEvidenceOf(it)
	if fault := firstFault(
		func() string { return BriefVerdictFault(d.Verdict) },
		func() string { return ev.claimFault(d.Verdict, d.VerdictEvidence) },
	); fault != "" {
		return BriefVersion{}, nil, fault
	}
	done, total := briefProgress(it)
	v := BriefVersion{
		Issue: it.Title, StepsDone: done, StepsTotal: total, Verdict: d.Verdict, VerdictEvidence: d.VerdictEvidence,
		Facts: BriefFacts(it), Blocks: []BriefBlock{},
	}
	drops := unread
	seen := map[string]bool{}
	for i, b := range d.Blocks {
		part := fmt.Sprintf("block %d (%s)", i+1, b.Kind)
		kept, bd := ev.checkBlock(b, part, it.State == StateNeedsYou)
		drops = append(drops, bd...)
		switch {
		case kept == nil:
		case seen[kept.Key()]:
			drops = append(drops, BriefDrop{Part: part, Reason: "another block has the same heading"})
		case len(v.Blocks) >= BriefBlocksMost:
			drops = append(drops, BriefDrop{Part: part, Reason: fmt.Sprintf("more than %d blocks", BriefBlocksMost)})
		default:
			seen[kept.Key()] = true
			v.Blocks = append(v.Blocks, *kept)
		}
	}
	return v, drops, ""
}

// briefEvidence is what the job can show: for each step, its tries and the
// numbers each kind of its result says; and the rules and changed files.
type briefEvidence struct {
	it    Item
	tries map[string]int
	files map[string]ChangedFile
	hunks []PatchHunk
}

func briefEvidenceOf(it Item) briefEvidence {
	e := briefEvidence{it: it, tries: map[string]int{}, files: map[string]ChangedFile{}}
	if it.Stream != nil {
		for _, p := range it.Stream.Phases {
			if p.Result != nil || len(p.Rounds) > 0 {
				e.tries[p.Name] = briefTry(p)
			}
		}
	}
	if it.Change != nil {
		for _, f := range it.Change.Files {
			e.files[f.Path] = f
		}
		e.hunks = PatchHunks(it.Change.Patch)
	}
	return e
}

// briefEvidenceKinds says, for each kind of evidence, the numbers it shows,
// and false when the evidence names nothing the job has.
var briefEvidenceKinds = map[string]func(e briefEvidence, ref BriefEvidence) ([]int, bool){
	"tests":       stepNumbers(func(r StageResult) []int { return testNumbers(r.Tests) }),
	"exit":        stepNumbers(func(r StageResult) []int { return []int{r.Exit} }),
	"findings":    stepNumbers(func(r StageResult) []int { return []int{r.Findings} }),
	"claims":      stepNumbers(claimNumbers),
	"output":      stepNumbers(func(r StageResult) []int { return textNumbers(r.Output) }),
	"summary":     summaryNumbers,
	"rule_checks": ruleNumbers,
	"file":        fileNumbers,
	"change":      changeNumbers,
	"hunk":        hunkNumbers,
}

// BriefEvidenceKinds is every kind of evidence a claim may name.
var BriefEvidenceKinds = []string{"tests", "exit", "findings", "claims", "output", "summary", "rule_checks", "file", "change", "hunk"}

// phaseOf is the job's phase of a step's name.
func (e briefEvidence) phaseOf(step string) (Phase, bool) {
	if e.it.Stream == nil {
		return Phase{}, false
	}
	for _, p := range e.it.Stream.Phases {
		if p.Name == step {
			return p, true
		}
	}
	return Phase{}, false
}

// stepNumbers reads a kind from the step's latest result: only the latest
// try keeps its structured result, so evidence of an earlier try names its
// summary instead.
func stepNumbers(read func(StageResult) []int) func(briefEvidence, BriefEvidence) ([]int, bool) {
	return func(e briefEvidence, ref BriefEvidence) ([]int, bool) {
		p, ok := e.phaseOf(ref.Step)
		if !ok || p.Result == nil || ref.Try != briefTry(p) {
			return nil, false
		}
		return append(read(*p.Result), ref.Try), true
	}
}

func testNumbers(t *TestCounts) []int {
	if t == nil {
		return nil
	}
	return []int{t.Passed, t.Failed, t.Skipped, t.Passed + t.Failed + t.Skipped}
}

func claimNumbers(r StageResult) []int {
	shown := 0
	for _, c := range r.Claims {
		if c.OK {
			shown++
		}
	}
	return []int{shown, len(r.Claims), len(r.Claims) - shown}
}

func summaryNumbers(e briefEvidence, ref BriefEvidence) ([]int, bool) {
	p, ok := e.phaseOf(ref.Step)
	if !ok {
		return nil, false
	}
	for _, r := range p.Rounds {
		if r.Round == ref.Try {
			return append(textNumbers(r.Summary), ref.Try), true
		}
	}
	return nil, false
}

func ruleNumbers(e briefEvidence, ref BriefEvidence) ([]int, bool) {
	return nil, e.it.RuleChecks != nil
}

func fileNumbers(e briefEvidence, ref BriefEvidence) ([]int, bool) {
	f, ok := e.files[ref.Ref]
	if !ok {
		return nil, false
	}
	stat := []int{f.Added, f.Removed, f.Added + f.Removed}
	if s := e.it.Change.Stat; s != nil {
		stat = append(stat, s.Files, s.Added, s.Removed, s.Added+s.Removed)
	}
	return stat, true
}

// changeNumbers reads the whole change's stat: files, lines added, removed
// and both.
func changeNumbers(e briefEvidence, _ BriefEvidence) ([]int, bool) {
	if e.it.Change == nil || e.it.Change.Stat == nil {
		return nil, false
	}
	s := e.it.Change.Stat
	return []int{s.Files, s.Added, s.Removed, s.Added + s.Removed}, true
}

// hunkNumbers reads a hunk named "file:start".
func hunkNumbers(e briefEvidence, ref BriefEvidence) ([]int, bool) {
	file, start, _ := strings.Cut(ref.Ref, ":")
	for _, h := range e.hunks {
		if h.File == file && strconv.Itoa(h.Start) == start {
			return []int{h.Start, h.Added, h.Removed, h.Lines()}, true
		}
	}
	return nil, false
}

// numbersIn finds the numbers a text says, a thousands comma read through
// ("1,069" is 1069); digits inside a word ("v2", "a1.go") are not numbers.
var numbersIn = regexp.MustCompile(`(?:^|[^\w.])(\d{1,3}(?:,\d{3})+|\d+)(?:\.\d+)?\b`)

func textNumbers(s string) []int {
	var out []int
	for _, m := range numbersIn.FindAllStringSubmatch(s, -1) {
		if n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", "")); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// numbersOf is every number the evidence shows, and the first reference
// that names nothing the job has.
func (e briefEvidence) numbersOf(refs []BriefEvidence) ([]int, string) {
	var all []int
	for _, ref := range refs {
		read, ok := briefEvidenceKinds[ref.Kind]
		if !ok {
			return nil, fmt.Sprintf("evidence of kind %q is not one the job keeps", ref.Kind)
		}
		nums, ok := read(e, ref)
		if !ok {
			return nil, fmt.Sprintf("evidence %s try %d %s %s names nothing the job has", ref.Step, ref.Try, ref.Kind, ref.Ref)
		}
		all = append(all, nums...)
	}
	return all, ""
}

// claimFault is what is wrong with a text and its evidence, "" for nothing:
// a reference names nothing the job has, or the text says a number none of
// its evidence shows.
func (e briefEvidence) claimFault(text string, refs []BriefEvidence) string {
	nums, fault := e.numbersOf(refs)
	if fault != "" {
		return fault
	}
	for _, n := range textNumbers(text) {
		if !slices.Contains(nums, n) {
			return fmt.Sprintf("%d in %q is not in its evidence", n, text)
		}
	}
	return ""
}

// checkBlock drops each claim of a block that fails, then the block itself
// when it fails the palette or its own evidence. It answers the block kept,
// nil for none, and the drops.
func (e briefEvidence) checkBlock(b BriefBlock, part string, needsYou bool) (*BriefBlock, []BriefDrop) {
	var drops []BriefDrop
	drop := func(what, reason string) { drops = append(drops, BriefDrop{Part: part + " " + what, Reason: reason}) }
	b.Bullets = keepClaims(b.Bullets, func(c BriefClaim) string { return e.claimFault(c.Text, c.Evidence) }, func(c BriefClaim) string { return c.Text }, drop)
	b.Spots = keepClaims(b.Spots, func(s BriefSpot) string { return e.claimFault(s.Why+" "+strconv.Itoa(s.Lines), s.Evidence) }, func(s BriefSpot) string { return s.File }, drop)
	b.Questions = keepClaims(b.Questions, func(q BriefQuestion) string { return e.questionFault(q) }, func(q BriefQuestion) string { return q.Text }, drop)
	b.Shots = keepClaims(b.Shots, func(s BriefShot) string { return e.claimFault(s.Label, s.Evidence) }, func(s BriefShot) string { return s.Ref }, drop)
	if fault := firstFault(
		func() string { return b.Fault(needsYou) },
		func() string { return e.blockFault(b) },
		func() string { return e.restFault(b.Rest) },
	); fault != "" {
		drops = append(drops, BriefDrop{Part: part, Reason: fault})
		return nil, drops
	}
	return &b, drops
}

// keepClaims keeps the items whose fault is "", saying each one dropped.
func keepClaims[T any](items []T, fault func(T) string, name func(T) string, drop func(what, reason string)) []T {
	var out []T
	for _, x := range items {
		if f := fault(x); f != "" {
			drop(fmt.Sprintf("%q", name(x)), f)
			continue
		}
		out = append(out, x)
	}
	return out
}

func (e briefEvidence) questionFault(q BriefQuestion) string {
	if q.Action != nil {
		if _, ok := e.phaseOf(q.Action.Step); !ok {
			return fmt.Sprintf("the action asks %q, which is not a step of the job", q.Action.Step)
		}
	}
	return e.claimFault(q.Text, q.Evidence)
}

// blockFault checks the words of a block whose evidence is its own: every
// number they say is in that evidence, and a proof's numbers are its step's.
func (e briefEvidence) blockFault(b BriefBlock) string {
	if len(b.Evidence) == 0 {
		return ""
	}
	if fault := e.claimFault(strings.Join(blockWords(b), " "), b.Evidence); fault != "" {
		return fault
	}
	if b.Proof != nil {
		return e.proofFault(b)
	}
	return ""
}

func (e briefEvidence) proofFault(b BriefBlock) string {
	for _, ref := range b.Evidence {
		p, ok := e.phaseOf(ref.Step)
		if ref.Kind != briefEvidenceClaims || !ok || p.Result == nil {
			continue
		}
		n := claimNumbers(*p.Result)
		if b.Proof.Shown != n[0] || b.Proof.Total != n[1] {
			return fmt.Sprintf("the proof says %d of %d, and %s showed %d of %d", b.Proof.Shown, b.Proof.Total, ref.Step, n[0], n[1])
		}
		return ""
	}
	return "the proof block names no proof step's claims"
}

// blockWords is the text a block draws outside its claims.
func blockWords(b BriefBlock) []string {
	words := []string{b.Mermaid}
	for _, row := range b.Flow {
		for _, n := range row.Nodes {
			words = append(words, n.Text)
		}
	}
	if b.Table != nil {
		for _, row := range b.Table.Rows {
			words = append(words, row...)
		}
	}
	words = append(words, b.Options...)
	return append(words, b.Nodes...)
}

// restFault checks the numbers of where-to-look's rest line against the
// change: its totals, each file's, and the lines of its generated files and
// its tests taken together.
func (e briefEvidence) restFault(rest string) string {
	var nums []int
	generated, tests := 0, 0
	for _, f := range e.files {
		lines := f.Added + f.Removed
		nums = append(nums, f.Added, f.Removed, lines)
		if GeneratedPath(f.Path) {
			generated += lines
		}
		if strings.Contains(f.Path, "_test.") || strings.Contains(f.Path, "test/") {
			tests += lines
		}
	}
	nums = append(nums, generated, tests, len(e.files))
	if e.it.Change != nil && e.it.Change.Stat != nil {
		s := e.it.Change.Stat
		nums = append(nums, s.Files, s.Added, s.Removed, s.Added+s.Removed)
	}
	for _, n := range textNumbers(rest) {
		if !slices.Contains(nums, n) {
			return fmt.Sprintf("%d in the rest line is not in the change", n)
		}
	}
	return ""
}

// briefListKeys are the fields that hold a list of evidence.
var briefListKeys = map[string]bool{"evidence": true, "verdict_evidence": true}

// briefLists reads raw JSON with every evidence field that holds one object
// made a list of that one object, at any depth. Anything else is as it was.
func briefLists(raw json.RawMessage) json.RawMessage {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	out, err := json.Marshal(wrapEvidence(v, false))
	if err != nil {
		return raw
	}
	return out
}

// wrapEvidence walks v; under an evidence field, a lone object becomes a
// list of one.
func wrapEvidence(v any, evidence bool) any {
	switch x := v.(type) {
	case map[string]any:
		if evidence {
			return []any{wrapEvidence(x, false)}
		}
		for k, child := range x {
			x[k] = wrapEvidence(child, briefListKeys[k])
		}
		return x
	case []any:
		for i, child := range x {
			x[i] = wrapEvidence(child, false)
		}
		return x
	}
	return v
}
