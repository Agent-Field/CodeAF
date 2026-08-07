package resident

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	consolidationInterval          = time.Minute
	consolidationThreshold         = 12 // registry default retained for deterministic fixtures
	playbookConsolidationThreshold = 8
	consolidationScanLimit         = 10_000
	consolidationOutputLimit       = 8
	contractPlaybookBytes          = 800
	contractPlaybookHeader         = "Earned method notes for this territory:\n"
)

// knownToolWords is deliberately a list: adding a tool should not require
// changing cue extraction's control flow.
var knownToolWords = []string{
	"sh", "write", "edit", "web", "git", "curl", "python", "pytest", "go", "npm",
	"ffmpeg", "fal", "seedance",
}

// WithConsolidator installs the notebook's sleep pass and returns the
// reconciler for chaining. A nil consolidator leaves the notebook untouched.
func (r *Reconciler) WithConsolidator(consolidate ConsolidateFunc) *Reconciler {
	r.consolidate = consolidate
	return r
}

// ExtractCues turns free text into ordered notebook scopes. Paths lead with
// their nearest repository parents, tools follow, and general scopes close
// every query so stable user and environment facts remain available.
func ExtractCues(text string) []string {
	seen := make(map[string]bool)
	cues := make([]string, 0, 8)
	add := func(cue string) {
		key := strings.ToLower(cue)
		if cue == "" || seen[key] {
			return
		}
		seen[key] = true
		cues = append(cues, cue)
	}

	paths := make([]string, 0, 2)
	for _, field := range strings.Fields(text) {
		if filename, ok := cuePath(field); ok {
			paths = append(paths, filename)
			add("file:" + filename)
		}
	}
	for _, filename := range paths {
		dir := path.Dir(filename)
		for depth := 0; depth < 2 && dir != "" && dir != "." && dir != "/"; depth++ {
			add("repo:" + dir)
			next := path.Dir(dir)
			if next == dir {
				break
			}
			dir = next
		}
	}

	tools := make(map[string]bool, len(knownToolWords))
	for _, tool := range knownToolWords {
		tools[tool] = true
	}
	for _, field := range strings.Fields(text) {
		word := strings.ToLower(strings.Trim(field, "\"'`()[]{}<>,.;:!?"))
		if tools[word] {
			add("tool:" + word)
		}
	}
	add("user")
	add("env")
	return cues
}

// ContractPlaybook builds the optional earned-doctrine lookup used by the
// contract pass. SearchFacts counts every returned bullet as a real use.
func ContractPlaybook(graph *store.Store) plan.ContractPlaybook {
	if graph == nil {
		return nil
	}
	return func(node plan.Node) string {
		work := strings.Join([]string{
			node.Title,
			node.Summary,
			strings.Join(node.Sources, "\n"),
			node.Brief,
		}, "\n")
		facts, err := graph.SearchFacts(store.FactQuery{
			Cues:         ExtractCues(work),
			Terms:        work,
			Kind:         store.FactPlaybook,
			PreferUseful: true,
			MaxBytes:     contractPlaybookBytes - len(contractPlaybookHeader),
			Limit:        consolidationOutputLimit,
		})
		if err != nil || len(facts) == 0 {
			return ""
		}
		var notes strings.Builder
		for _, fact := range facts {
			fmt.Fprintf(&notes, "- [%s] %s\n", fact.Scope, fact.Body)
		}
		return strings.TrimSuffix(notes.String(), "\n")
	}
}

// NotebookDigest retrieves the facts relevant to one piece of work and
// renders the bounded block workers receive with their inputs. A non-empty
// nodeID attributes the whole injected batch to that node in one event.
func NotebookDigest(graph *store.Store, nodeID, brief, goal string, limit int) string {
	if graph == nil {
		return ""
	}
	work := strings.TrimSpace(strings.TrimSpace(brief) + "\n" + strings.TrimSpace(goal))
	facts, err := graph.SearchFacts(store.FactQuery{
		Cues:  ExtractCues(work),
		Terms: work,
		Limit: limit,
	})
	if err != nil || len(facts) == 0 {
		return ""
	}
	if nodeID != "" {
		seqs := make([]int64, 0, len(facts))
		for _, fact := range facts {
			seqs = append(seqs, fact.Seq)
		}
		_ = graph.RecordFactInjection(nodeID, seqs)
	}

	var digest strings.Builder
	digest.WriteString("notebook (lessons from earlier work; if your own experience in this task contradicts one, trust the experience and state the correction explicitly in your final message — that is how the notebook stays true):\n")
	for _, fact := range facts {
		if fact.Kind == store.FactUnsettled && fact.Unsettled != nil {
			digest.WriteString("- ")
			digest.WriteString(store.UnsettledFactFlag)
			digest.WriteString(fmt.Sprintf("%d\n", fact.Seq))
			digest.WriteString("  ")
			digest.WriteString(store.FormatUnsettledPair(*fact.Unsettled))
			digest.WriteByte('\n')
			continue
		}
		if fact.Kind == store.FactSkill {
			fmt.Fprintf(&digest, "- #%d skill: ", fact.Seq)
		} else {
			fmt.Fprintf(&digest, "- #%d ", fact.Seq)
		}
		digest.WriteString(fact.Body)
		digest.WriteByte('\n')
	}
	return strings.TrimSuffix(digest.String(), "\n")
}

// consolidateNotebook rewrites at most one overgrown scope. Maintenance is
// best effort: model and store failures leave ordinary reconciliation alone.
func (r *Reconciler) consolidateNotebook(ctx context.Context) {
	now := r.now()
	if !r.lastConsolidation.IsZero() && now.Sub(r.lastConsolidation) < consolidationInterval {
		return
	}
	r.lastConsolidation = now
	_, _ = r.store.AgeFacts(now)
	if r.consolidate == nil {
		return
	}

	facts, err := r.store.ActiveFacts("", consolidationScanLimit)
	if err != nil {
		return
	}
	pendingTrials := make(map[int64]bool)
	if stats, err := r.store.TrialStats(); err == nil {
		for _, outcome := range stats.Outcomes {
			if outcome.Status == store.TrialPending {
				pendingTrials[outcome.TrialOf] = true
			}
		}
	}
	byScope := make(map[string][]store.Fact)
	playbooksByScope := make(map[string]int)
	blockedScopes := make(map[string]bool)
	var scopeOrder []string
	for _, fact := range facts {
		if _, seen := byScope[fact.Scope]; !seen {
			scopeOrder = append(scopeOrder, fact.Scope)
		}
		byScope[fact.Scope] = append(byScope[fact.Scope], fact)
		if fact.Kind == store.FactPlaybook {
			playbooksByScope[fact.Scope]++
		}
		if pendingTrials[fact.Seq] {
			blockedScopes[fact.Scope] = true
		}
	}
	worstScope := ""
	worstCount := 0
	for _, scope := range scopeOrder {
		if blockedScopes[scope] {
			continue
		}
		scoped := byScope[scope]
		if len(scoped) <= int(r.store.Parameter(store.ParameterConsolidationThreshold)) &&
			playbooksByScope[scope] <= playbookConsolidationThreshold {
			continue
		}
		if count := len(scoped); count > worstCount {
			worstScope = scope
			worstCount = count
		}
	}
	candidates := scopeAliasCandidates(scopeOrder)
	var candidate *ScopePair
	if len(candidates) > 0 {
		candidate = &candidates[0]
	}
	if worstScope == "" && candidate == nil {
		return
	}

	originals := byScope[worstScope]
	consolidated, err := r.consolidate(ctx, worstScope, originals, candidate)
	if err != nil {
		return
	}
	defer r.applyScopeAliasJudgment(candidate, consolidated.ScopeAlias)
	if worstScope == "" {
		return
	}
	rewritten := consolidated.Facts
	if len(rewritten) > consolidationOutputLimit {
		rewritten = rewritten[:consolidationOutputLimit]
	}
	outcomes, err := r.store.FactOutcomes()
	if err != nil {
		return
	}

	originalBySeq := make(map[int64]store.Fact, len(originals))
	originalByBody := make(map[string]int64, len(originals))
	for _, original := range originals {
		originalBySeq[original.Seq] = original
		originalByBody[strings.ToLower(strings.TrimSpace(original.Body))] = original.Seq
	}
	type plannedRewrite struct {
		learned     Learned
		sources     []int64
		nodeID      string
		skillSource int64
	}
	type plannedQuarantine struct {
		seq         int64
		evidenceSeq int64
	}
	planned := make([]plannedRewrite, 0, len(rewritten))
	quarantines := make([]plannedQuarantine, 0)
	claimedSources := make(map[int64]bool, len(originals))
	seenBodies := make(map[string]bool, len(rewritten))
	for _, learned := range rewritten {
		quarantineSeqs := uniqueFactSeqs(learned.Quarantines)
		for _, quarantineSeq := range quarantineSeqs {
			if _, ok := originalBySeq[quarantineSeq]; !ok || claimedSources[quarantineSeq] {
				return
			}
			outcome := outcomes[quarantineSeq]
			// One bad job is an anecdote. The store enforces the same pattern bar
			// the prompt teaches before accepting a model-requested quarantine.
			if outcome.Bad < 2 || outcome.LatestBadSeq == 0 {
				return
			}
			claimedSources[quarantineSeq] = true
			quarantines = append(quarantines, plannedQuarantine{
				seq: quarantineSeq, evidenceSeq: outcome.LatestBadSeq,
			})
		}
		if learned.Kind == store.FactUnsettled {
			if learned.Unsettled == nil || learned.Unsettled.Validate() != nil {
				return
			}
			learned.Body = store.FormatUnsettledPair(*learned.Unsettled)
		}
		bodyKey := strings.ToLower(strings.TrimSpace(learned.Body))
		if bodyKey == "" {
			if len(quarantineSeqs) == 0 || len(learned.Sources) > 0 || learned.Replaces > 0 {
				return
			}
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(learned.Scope), worstScope) || seenBodies[bodyKey] {
			return
		}
		seenBodies[bodyKey] = true
		sourceSeqs := append([]int64(nil), learned.Sources...)
		if learned.Replaces > 0 {
			sourceSeqs = append(sourceSeqs, learned.Replaces)
		}
		sourceSeqs = uniqueFactSeqs(sourceSeqs)
		if len(sourceSeqs) == 0 {
			return
		}
		nodeID := ""
		for _, sourceSeq := range sourceSeqs {
			original, ok := originalBySeq[sourceSeq]
			if !ok || claimedSources[sourceSeq] {
				return
			}
			claimedSources[sourceSeq] = true
			if nodeID == "" && original.NodeID != "" {
				nodeID = original.NodeID
			}
		}
		var skillSource int64
		if learned.Kind == store.FactSkill {
			for _, sourceSeq := range sourceSeqs {
				source := originalBySeq[sourceSeq]
				if source.Kind == store.FactSkill && source.Artifact != "" {
					skillSource = source.Seq
					break
				}
			}
			if skillSource == 0 {
				return
			}
		}
		// RecordFact's ordinary duplicate hygiene may supersede an unchanged
		// original while recording. Require that original to belong to this
		// output so its automatic event remains the truthful mapping.
		if duplicateSeq := originalByBody[bodyKey]; duplicateSeq != 0 && !containsFactSeq(sourceSeqs, duplicateSeq) {
			return
		}
		planned = append(planned, plannedRewrite{learned: learned, sources: sourceSeqs, nodeID: nodeID, skillSource: skillSource})
	}
	// Mapping validation is all-or-nothing. A malformed model mapping must not
	// add rewrites or leave a source attached to an invented replacement.
	if len(planned)+len(quarantines) == 0 || len(claimedSources) != len(originals) {
		return
	}

	replacementFor := make(map[int64]int64, len(originals))
	for _, rewrite := range planned {
		var fact store.Fact
		var err error
		if rewrite.learned.Kind == store.FactSkill {
			fact, err = r.store.RewriteActiveSkillFrom(store.FactWriterDistiller, rewrite.nodeID, rewrite.learned.Scope, clipFactBody(rewrite.learned.Body), rewrite.skillSource)
		} else {
			fact, err = r.recordLearnedFact(rewrite.nodeID, rewrite.learned)
		}
		if err != nil {
			return
		}
		for _, sourceSeq := range rewrite.sources {
			replacementFor[sourceSeq] = fact.Seq
		}
	}

	active, err := r.store.ActiveFacts(worstScope, len(originals)+consolidationOutputLimit)
	if err != nil {
		return
	}
	stillActive := make(map[int64]bool, len(active))
	for _, fact := range active {
		stillActive[fact.Seq] = true
	}
	for _, original := range originals {
		// Recording an unchanged line already supersedes its prior copy. Only
		// originals still active need an explicit consolidation event.
		if !stillActive[original.Seq] {
			continue
		}
		replacementSeq, mapped := replacementFor[original.Seq]
		if !mapped {
			continue
		}
		if err := r.store.SupersedeFact(original.Seq, replacementSeq); err != nil {
			return
		}
	}
	for _, quarantine := range quarantines {
		if err := r.store.QuarantineFact(quarantine.seq, quarantine.evidenceSeq,
			store.FactOriginConsolidator); err != nil {
			return
		}
	}
}

func (r *Reconciler) applyScopeAliasJudgment(candidate *ScopePair, judgment *ScopeAliasJudgment) {
	if candidate == nil || judgment == nil || !judgment.Merge {
		return
	}
	canonical := strings.ToLower(strings.TrimSpace(judgment.Canonical))
	first := strings.ToLower(strings.TrimSpace(candidate.First))
	second := strings.ToLower(strings.TrimSpace(candidate.Second))
	from := ""
	switch canonical {
	case first:
		from = second
	case second:
		from = first
	default:
		return
	}
	_ = r.store.AliasScope(from, canonical)
}

type scoredScopePair struct {
	ScopePair
	score int
}

// scopeAliasCandidates compares only canonical domain, repo, and tool shelves.
// Token normalization makes punctuation and simple inflection differences
// cheap to spot; the model still decides whether similar names mean one thing.
func scopeAliasCandidates(scopes []string) []ScopePair {
	unique := make(map[string]bool, len(scopes))
	canonical := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if _, _, ok := gardenScopeParts(scope); !ok || unique[scope] {
			continue
		}
		unique[scope] = true
		canonical = append(canonical, scope)
	}
	sort.Strings(canonical)
	var scored []scoredScopePair
	for first := 0; first < len(canonical); first++ {
		firstPrefix, firstTokens, _ := gardenScopeParts(canonical[first])
		for second := first + 1; second < len(canonical); second++ {
			secondPrefix, secondTokens, _ := gardenScopeParts(canonical[second])
			if firstPrefix != secondPrefix {
				continue
			}
			score := normalizedTokenSimilarity(firstTokens, secondTokens)
			if score == 0 {
				continue
			}
			scored = append(scored, scoredScopePair{
				ScopePair: ScopePair{First: canonical[first], Second: canonical[second]},
				score:     score,
			})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].First != scored[j].First {
			return scored[i].First < scored[j].First
		}
		return scored[i].Second < scored[j].Second
	})
	result := make([]ScopePair, len(scored))
	for index := range scored {
		result[index] = scored[index].ScopePair
	}
	return result
}

func gardenScopeParts(scope string) (string, []string, bool) {
	for _, prefix := range []string{"domain:", "repo:", "tool:"} {
		if !strings.HasPrefix(scope, prefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(scope, prefix))
		if name == "" {
			return "", nil, false
		}
		fields := strings.FieldsFunc(name, func(char rune) bool {
			return !unicode.IsLetter(char) && !unicode.IsDigit(char)
		})
		seen := make(map[string]bool, len(fields))
		tokens := make([]string, 0, len(fields))
		for _, field := range fields {
			token := singularScopeToken(strings.ToLower(field))
			if token == "" || seen[token] {
				continue
			}
			seen[token] = true
			tokens = append(tokens, token)
		}
		sort.Strings(tokens)
		return prefix, tokens, len(tokens) > 0
	}
	return "", nil, false
}

func singularScopeToken(token string) string {
	switch {
	case len(token) > 4 && strings.HasSuffix(token, "ies"):
		return strings.TrimSuffix(token, "ies") + "y"
	case len(token) > 4 && strings.HasSuffix(token, "sses"):
		return strings.TrimSuffix(token, "es")
	case len(token) > 4 && (strings.HasSuffix(token, "xes") ||
		strings.HasSuffix(token, "ches") || strings.HasSuffix(token, "shes") ||
		strings.HasSuffix(token, "zes")):
		return strings.TrimSuffix(token, "es")
	case len(token) > 3 && strings.HasSuffix(token, "s") && !strings.HasSuffix(token, "ss"):
		return strings.TrimSuffix(token, "s")
	default:
		return token
	}
}

func normalizedTokenSimilarity(first, second []string) int {
	if len(first) == 0 || len(second) == 0 {
		return 0
	}
	firstSet := make(map[string]bool, len(first))
	for _, token := range first {
		firstSet[token] = true
	}
	overlap := 0
	for _, token := range second {
		if firstSet[token] {
			overlap++
		}
	}
	if overlap == len(first) && overlap == len(second) {
		return 1000 + overlap
	}
	if overlap == min(len(first), len(second)) {
		return 800 + overlap
	}
	union := len(first) + len(second) - overlap
	if overlap*5 >= union*3 {
		return overlap * 100 / union
	}
	return 0
}

func uniqueFactSeqs(seqs []int64) []int64 {
	seen := make(map[int64]bool, len(seqs))
	result := make([]int64, 0, len(seqs))
	for _, seq := range seqs {
		if seq <= 0 || seen[seq] {
			continue
		}
		seen[seq] = true
		result = append(result, seq)
	}
	return result
}

func containsFactSeq(seqs []int64, want int64) bool {
	for _, seq := range seqs {
		if seq == want {
			return true
		}
	}
	return false
}

func cuePath(field string) (string, bool) {
	filename := strings.Trim(field, "\"'`()[]{}<>,;!?")
	filename = strings.TrimRight(filename, ".")
	if strings.HasPrefix(strings.ToLower(filename), "file:") {
		filename = filename[len("file:"):]
	}
	if fragment := strings.Index(filename, "#L"); fragment >= 0 {
		filename = filename[:fragment]
	}
	filename = trimPosition(filename)
	filename = strings.ReplaceAll(filename, "\\", "/")
	if filename == "" || strings.Contains(filename, "://") {
		return "", false
	}
	if !strings.Contains(filename, "/") && !looksLikeFilename(filename) {
		return "", false
	}
	filename = path.Clean(filename)
	if filename == "." || filename == "/" {
		return "", false
	}
	return filename, true
}

func trimPosition(filename string) string {
	for range 2 {
		colon := strings.LastIndexByte(filename, ':')
		if colon < 0 || colon == len(filename)-1 || !decimal(filename[colon+1:]) {
			break
		}
		filename = filename[:colon]
	}
	return filename
}

func decimal(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != ""
}

func looksLikeFilename(value string) bool {
	base := path.Base(value)
	if strings.HasPrefix(base, ".") && len(base) > 1 {
		return true
	}
	extension := strings.TrimPrefix(path.Ext(base), ".")
	if extension == "" || len(extension) > 12 {
		return false
	}
	hasLetter := false
	for _, char := range extension {
		switch {
		case 'a' <= char && char <= 'z', 'A' <= char && char <= 'Z':
			hasLetter = true
		case '0' <= char && char <= '9':
		default:
			return false
		}
	}
	return hasLetter
}

func clipFactBody(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= store.MaxFactBytes {
		return body
	}
	cut := store.MaxFactBytes - len("...")
	for cut > 0 && !utf8.ValidString(body[:cut]) {
		cut--
	}
	return strings.TrimSpace(body[:cut]) + "..."
}
