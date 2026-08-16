package resident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	territoryMinJobs = 4
	// One hour keeps a just-finished job individually visible long enough for
	// delivery review and immediate follow-ups, while still letting the next
	// ordinary 30-minute retrospective pack settled history promptly.
	territoryFreshnessWindow = time.Hour
)

// TerritoryDigestJob is the bounded evidence supplied for one member of a
// territory digest. Pointers retain the route to its durable artifacts.
type TerritoryDigestJob struct {
	ID       string
	Title    string
	Ask      string
	Outcome  string
	Pointers []string
}

// TerritoryDigestFunc writes the one model-authored map for a territory.
// Clustering, naming, membership, and hierarchy are all deterministic Go.
//
// The voice contract arrives as an argument rather than being fetched by the
// writer, because the notebook belongs to the resident and this is the only
// surface that speaks without a store handle of its own. Empty on an empty
// notebook, which keeps the prompt byte-identical to what it was before any
// preference was learned.
type TerritoryDigestFunc func(ctx context.Context, title string, jobs []TerritoryDigestJob, voice string) (string, error)

// WithTerritoryDigester enables territory maintenance during a retrospective.
// It is separate from WithReflector so non-chat and headless paths stay inert.
func (r *Reconciler) WithTerritoryDigester(digest TerritoryDigestFunc) *Reconciler {
	r.digestTerritory = digest
	return r
}

// maintainTerritories performs at most one action: grow the oldest matching
// territory first, otherwise form the largest eligible cluster. A failed
// digest or journal batch ends this pass rather than trying a second action.
func (r *Reconciler) maintainTerritories(ctx context.Context, now time.Time) bool {
	if r.digestTerritory == nil {
		return false
	}
	jobs, err := r.store.TerritoryJobs()
	if err != nil {
		return false
	}
	nodes, err := r.store.Nodes()
	if err != nil {
		return false
	}

	eligible := make([]store.TerritoryJob, 0)
	membersByTerritory := make(map[string][]store.TerritoryJob)
	for _, job := range jobs {
		if job.Node.Parent != store.RootID {
			membersByTerritory[job.Node.Parent] = append(membersByTerritory[job.Node.Parent], job)
			continue
		}
		age := now.Sub(job.Node.FinishedAt)
		if !job.Node.FinishedAt.IsZero() && age >= territoryFreshnessWindow {
			eligible = append(eligible, job)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if !eligible[i].Node.FinishedAt.Equal(eligible[j].Node.FinishedAt) {
			return eligible[i].Node.FinishedAt.Before(eligible[j].Node.FinishedAt)
		}
		return eligible[i].Node.ID < eligible[j].Node.ID
	})

	territories := make([]store.Node, 0)
	for _, node := range nodes {
		if node.Group == store.TerritoryGroup && node.Parent == store.RootID && node.FoldRoot {
			territories = append(territories, node)
		}
	}
	sort.SliceStable(territories, func(i, j int) bool {
		if territories[i].CreatedSeq != territories[j].CreatedSeq {
			return territories[i].CreatedSeq < territories[j].CreatedSeq
		}
		return territories[i].ID < territories[j].ID
	})

	for _, territory := range territories {
		members := membersByTerritory[territory.ID]
		for _, candidate := range eligible {
			if !matchesTerritory(candidate, members) {
				continue
			}
			grown := append(append([]store.TerritoryJob(nil), members...), candidate)
			sortTerritoryJobs(grown)
			digestJobs := territoryDigestJobs(grown)
			digest, err := r.digestTerritory(ctx, territoryDisplayTitle(territory), digestJobs,
				VoiceSection(r.store, territoryDisplayTitle(territory)))
			if err != nil || strings.TrimSpace(digest) == "" {
				return false
			}
			if err := r.store.GrowTerritory(territory.ID, candidate.Node.ID, digest,
				territoryPointers(grown)); err != nil {
				return false
			}
			return true
		}
	}

	clusters := clusterTerritoryJobs(eligible)
	for _, cluster := range clusters {
		if len(cluster) < territoryMinJobs {
			continue
		}
		title := territoryTitle(cluster)
		digestJobs := territoryDigestJobs(cluster)
		digest, err := r.digestTerritory(ctx, title, digestJobs, VoiceSection(r.store, title))
		if err != nil || strings.TrimSpace(digest) == "" {
			return false
		}
		memberIDs := make([]string, len(cluster))
		for index := range cluster {
			memberIDs[index] = cluster[index].Node.ID
		}
		if err := r.store.FormTerritory(territoryID(memberIDs), title, digest,
			territoryPointers(cluster), memberIDs); err != nil {
			return false
		}
		return true
	}
	return false
}

func matchesTerritory(candidate store.TerritoryJob, members []store.TerritoryJob) bool {
	for _, member := range members {
		if territoryJobsShareSignal(candidate, member) {
			return true
		}
	}
	return false
}

func territoryJobsShareSignal(first, second store.TerritoryJob) bool {
	if first.Workspace != "" && first.Workspace == second.Workspace {
		return true
	}
	if usableTerritoryScope(first.DominantScope) &&
		first.DominantScope == second.DominantScope {
		return true
	}
	return territoryContains(first.Continuity, second.Node.ID) ||
		territoryContains(second.Continuity, first.Node.ID)
}

func usableTerritoryScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "user", "env":
		return false
	default:
		return true
	}
}

func clusterTerritoryJobs(jobs []store.TerritoryJob) [][]store.TerritoryJob {
	if len(jobs) == 0 {
		return nil
	}
	parent := make([]int, len(jobs))
	for index := range parent {
		parent[index] = index
	}
	var find func(int) int
	find = func(index int) int {
		if parent[index] != index {
			parent[index] = find(parent[index])
		}
		return parent[index]
	}
	union := func(first, second int) {
		left, right := find(first), find(second)
		if left != right {
			parent[right] = left
		}
	}

	byID := make(map[string]int, len(jobs))
	byWorkspace := make(map[string]int)
	byScope := make(map[string]int)
	for index, job := range jobs {
		byID[job.Node.ID] = index
		if job.Workspace != "" {
			if previous, ok := byWorkspace[job.Workspace]; ok {
				union(index, previous)
			} else {
				byWorkspace[job.Workspace] = index
			}
		}
		if usableTerritoryScope(job.DominantScope) {
			if previous, ok := byScope[job.DominantScope]; ok {
				union(index, previous)
			} else {
				byScope[job.DominantScope] = index
			}
		}
	}
	for index, job := range jobs {
		for _, linkedID := range job.Continuity {
			if linked, ok := byID[linkedID]; ok {
				union(index, linked)
			}
		}
	}

	grouped := make(map[int][]store.TerritoryJob)
	for index, job := range jobs {
		root := find(index)
		grouped[root] = append(grouped[root], job)
	}
	clusters := make([][]store.TerritoryJob, 0, len(grouped))
	for _, cluster := range grouped {
		sortTerritoryJobs(cluster)
		clusters = append(clusters, cluster)
	}
	sort.SliceStable(clusters, func(i, j int) bool {
		if len(clusters[i]) != len(clusters[j]) {
			return len(clusters[i]) > len(clusters[j])
		}
		left, right := clusters[i][0].Node, clusters[j][0].Node
		if left.CreatedSeq != right.CreatedSeq {
			return left.CreatedSeq < right.CreatedSeq
		}
		return left.ID < right.ID
	})
	return clusters
}

func sortTerritoryJobs(jobs []store.TerritoryJob) {
	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].Node.CreatedSeq != jobs[j].Node.CreatedSeq {
			return jobs[i].Node.CreatedSeq < jobs[j].Node.CreatedSeq
		}
		return jobs[i].Node.ID < jobs[j].Node.ID
	})
}

func territoryTitle(jobs []store.TerritoryJob) string {
	scopeCounts := make(map[string]int)
	workspaceCounts := make(map[string]int)
	for _, job := range jobs {
		if usableTerritoryScope(job.DominantScope) {
			scopeCounts[territoryNoun(job.DominantScope)]++
		}
		if job.Workspace != "" {
			workspaceCounts[territoryNoun(filepath.Base(job.Workspace))]++
		}
	}
	noun := repeatedSignal(scopeCounts)
	if noun == "" {
		noun = repeatedSignal(workspaceCounts)
	}
	if noun == "" {
		noun = commonTerritoryNoun(jobs)
	}
	if noun == "" {
		noun = "related"
	}
	noun = titleWords(noun)
	if strings.HasSuffix(strings.ToLower(noun), " work") {
		return noun
	}
	return noun + " work"
}

func repeatedSignal(counts map[string]int) string {
	best, bestCount := "", 1
	for signal, count := range counts {
		if signal == "" || count < 2 {
			continue
		}
		if count > bestCount || count == bestCount && (best == "" || signal < best) {
			best, bestCount = signal, count
		}
	}
	return best
}

func territoryNoun(value string) string {
	value = strings.TrimSpace(value)
	if cut := strings.LastIndex(value, ":"); cut >= 0 {
		value = value[cut+1:]
	}
	return strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(value))
}

var territoryStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "build": true, "create": true,
	"deliver": true, "do": true, "for": true, "from": true, "in": true,
	"make": true, "of": true, "on": true, "the": true, "to": true,
	"update": true, "with": true, "work": true,
}

func commonTerritoryNoun(jobs []store.TerritoryJob) string {
	counts := make(map[string]int)
	for _, job := range jobs {
		text := job.Node.Title + " " + job.Node.Provenance.Intent
		seen := make(map[string]bool)
		for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if len(token) < 3 || territoryStopWords[token] || seen[token] {
				continue
			}
			seen[token] = true
			counts[token]++
		}
	}
	return repeatedSignal(counts)
}

func titleWords(value string) string {
	words := strings.Fields(value)
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}

func territoryID(memberIDs []string) string {
	ids := append([]string(nil), memberIDs...)
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return "territory-" + hex.EncodeToString(sum[:6])
}

func territoryDigestJobs(jobs []store.TerritoryJob) []TerritoryDigestJob {
	result := make([]TerritoryDigestJob, len(jobs))
	for index, job := range jobs {
		outcome := strings.TrimSpace(job.Node.FoldDigest)
		if outcome == "" {
			outcome = strings.TrimSpace(job.Node.Summary)
		}
		if outcome == "" {
			outcome = strings.TrimSpace(job.Node.Error)
		}
		title := strings.TrimSpace(job.Node.Title)
		if title == "" {
			title = firstLine(job.Node.Provenance.Intent)
		}
		result[index] = TerritoryDigestJob{
			ID: job.Node.ID, Title: title, Ask: job.Node.Provenance.Intent,
			Outcome: outcome, Pointers: append([]string(nil), job.Node.FoldPointers...),
		}
	}
	return result
}

func territoryPointers(jobs []store.TerritoryJob) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)
	for _, job := range jobs {
		for _, pointer := range append([]string{"node:" + job.Node.ID}, job.Node.FoldPointers...) {
			pointer = strings.TrimSpace(pointer)
			if pointer == "" || seen[pointer] {
				continue
			}
			seen[pointer] = true
			result = append(result, pointer)
		}
	}
	return result
}

func territoryDisplayTitle(node store.Node) string {
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	if title := strings.TrimSpace(node.Brief); title != "" {
		return strings.TrimPrefix(title, "Territory: ")
	}
	return "Related work"
}

func territoryContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
