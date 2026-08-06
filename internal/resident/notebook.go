package resident

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	consolidationInterval    = time.Minute
	consolidationThreshold   = 12
	consolidationScanLimit   = 10_000
	consolidationOutputLimit = 8
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

// NotebookDigest retrieves the facts relevant to one piece of work and
// renders the bounded block workers receive with their inputs.
func NotebookDigest(graph *store.Store, brief, goal string, limit int) string {
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
		digest.WriteString("- ")
		digest.WriteString(fact.Body)
		digest.WriteByte('\n')
	}
	return strings.TrimSuffix(digest.String(), "\n")
}

// consolidateNotebook rewrites at most one overgrown scope. Maintenance is
// best effort: model and store failures leave ordinary reconciliation alone.
func (r *Reconciler) consolidateNotebook(ctx context.Context) {
	if r.consolidate == nil {
		return
	}
	now := time.Now()
	if !r.lastConsolidation.IsZero() && now.Sub(r.lastConsolidation) < consolidationInterval {
		return
	}
	r.lastConsolidation = now

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
	blockedScopes := make(map[string]bool)
	var scopeOrder []string
	for _, fact := range facts {
		if _, seen := byScope[fact.Scope]; !seen {
			scopeOrder = append(scopeOrder, fact.Scope)
		}
		byScope[fact.Scope] = append(byScope[fact.Scope], fact)
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
		if count := len(scoped); count > worstCount {
			worstScope = scope
			worstCount = count
		}
	}
	if worstCount <= consolidationThreshold {
		return
	}

	originals := byScope[worstScope]
	rewritten, err := r.consolidate(ctx, worstScope, originals)
	if err != nil {
		return
	}
	if len(rewritten) > consolidationOutputLimit {
		rewritten = rewritten[:consolidationOutputLimit]
	}

	originalBySeq := make(map[int64]store.Fact, len(originals))
	originalByBody := make(map[string]int64, len(originals))
	for _, original := range originals {
		originalBySeq[original.Seq] = original
		originalByBody[strings.ToLower(strings.TrimSpace(original.Body))] = original.Seq
	}
	type plannedRewrite struct {
		learned Learned
		sources []int64
		nodeID  string
	}
	planned := make([]plannedRewrite, 0, len(rewritten))
	claimedSources := make(map[int64]bool, len(originals))
	seenBodies := make(map[string]bool, len(rewritten))
	for _, learned := range rewritten {
		if learned.Kind == store.FactUnsettled {
			if learned.Unsettled == nil || learned.Unsettled.Validate() != nil {
				return
			}
			learned.Body = store.FormatUnsettledPair(*learned.Unsettled)
		}
		bodyKey := strings.ToLower(strings.TrimSpace(learned.Body))
		if bodyKey == "" {
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
		// RecordFact's ordinary duplicate hygiene may supersede an unchanged
		// original while recording. Require that original to belong to this
		// output so its automatic event remains the truthful mapping.
		if duplicateSeq := originalByBody[bodyKey]; duplicateSeq != 0 && !containsFactSeq(sourceSeqs, duplicateSeq) {
			return
		}
		planned = append(planned, plannedRewrite{learned: learned, sources: sourceSeqs, nodeID: nodeID})
	}
	// Mapping validation is all-or-nothing. A malformed model mapping must not
	// add rewrites or leave a source attached to an invented replacement.
	if len(planned) == 0 || len(claimedSources) != len(originals) {
		return
	}

	replacementFor := make(map[int64]int64, len(originals))
	for _, rewrite := range planned {
		fact, err := r.recordLearnedFact(rewrite.nodeID, rewrite.learned)
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
