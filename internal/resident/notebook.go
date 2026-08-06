package resident

import (
	"context"
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
	"sh", "web", "git", "curl", "python", "pytest", "go", "npm",
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
	digest.WriteString("notebook:\n")
	for _, fact := range facts {
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
	byScope := make(map[string][]store.Fact)
	worstScope := ""
	worstCount := 0
	for _, fact := range facts {
		byScope[fact.Scope] = append(byScope[fact.Scope], fact)
		if count := len(byScope[fact.Scope]); count > worstCount {
			worstScope = fact.Scope
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

	var replacementSeq int64
	for _, learned := range rewritten {
		if strings.TrimSpace(learned.Body) == "" {
			continue
		}
		fact, err := r.store.RecordFact("", learned.Scope, learned.Kind, clipFactBody(learned.Body))
		if err != nil {
			return
		}
		replacementSeq = fact.Seq
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
		if err := r.store.SupersedeFact(original.Seq, replacementSeq); err != nil {
			return
		}
	}
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
