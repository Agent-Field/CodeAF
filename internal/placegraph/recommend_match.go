package placegraph

// The deterministic half of recommendations: the folder rule, the caps on
// what the AI may create, and same-name merges. Grouping chats into a new
// place moved to the organiser. Nothing here calls a model.
//
// WHY RULES FIRST. A model asked "which of my 200 places is this?" is slow,
// costs a whole prompt of place names per chat, and can answer with a place
// that does not exist. Rules cut the question down to a handful of real places
// (or answer it outright when a chat runs in a folder a place already holds),
// and the model only ever chooses among labels it was handed.

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Lexical evidence alone never reaches the default offer threshold: a shared
// word is a reason to ASK, not an answer. Only a shared folder, or a model's
// pick among rule-chosen candidates, can make an offer at the defaults.
const (
	lexicalConfidenceCap = 70
	workspaceSourceConf  = 95
	workspaceMembersConf = 85
	sameNameMergeConf    = 90
	// minChatWords is the least a chat must say before it is worth placing.
	minChatWords = 3
	// clusterCoreShare is how many of a word-cluster's chats must share a word
	// for the cluster to count. It stops one chain of loosely similar titles
	// (a~b, b~c, c~d) being offered as one place that has nothing in common.
	clusterCoreShare = 0.6
	clusterJaccard   = 0.34
	maxClustersPass  = 3
	maxClusterShown  = 24
)

var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the and for with that this from into onto have has had was were are not but you your our
	its it's can could would should will just about what when where which who how why then than them they their there
	here also some any all one two new use using used make made get got does did done let lets please help want need
	add fix chat chats thing things file files code work task tasks question ask asked`) {
		stopWords[w] = true
	}
}

// words is the set of meaningful lowercase words in s. Plurals fold onto the
// singular so "parsers" meets "parser". A hyphenated word also counts joined,
// because its halves are often too short to count at all: "Wi-Fi" was two
// two-letter fragments and said nothing, where "wifi" is the topic.
func words(s string) map[string]bool {
	out := map[string]bool{}
	add := func(w string) {
		if len([]rune(w)) < 3 || stopWords[w] {
			return
		}
		if len(w) > 4 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			w = strings.TrimSuffix(w, "s")
		}
		out[w] = true
	}
	notWord := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	lower := strings.ToLower(s)
	for _, w := range strings.FieldsFunc(lower, notWord) {
		add(w)
	}
	for _, run := range strings.FieldsFunc(lower, func(r rune) bool { return r != '-' && notWord(r) }) {
		if parts := strings.FieldsFunc(run, notWord); len(parts) > 1 {
			add(strings.Join(parts, ""))
		}
	}
	return out
}

func chatWords(c ChatEvidence) map[string]bool {
	return words(clipRunes(c.Title, 200) + " " + clipRunes(c.Summary, 600) + " " + clipRunes(c.FirstMessage, 600))
}

// CleanFolder is a caller-supplied folder in comparable form, or "" when it is
// not an absolute path. It is only ever compared, never opened or written.
func CleanFolder(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Clean(p)
}

// folderHolds reports whether a place's folder source covers a chat's folder:
// the same folder, or one inside it.
func folderHolds(source, chat string) bool {
	source, chat = cleanFolder(source), cleanFolder(chat)
	if source == "" || chat == "" {
		return false
	}
	return chat == source || strings.HasPrefix(chat, source+string(filepath.Separator))
}

// folderMatch is the one rule still answered here: the deepest place whose
// folder source covers the chat, or, failing that, a place where most filed
// chats already ran in that same folder. Shared words are not a reason.
func folderMatch(s *Snapshot, folder string, exclude map[string]bool, library map[string]ChatEvidence) (Place, int, bool) {
	if folder == "" {
		return Place{}, 0, false
	}
	var best Place
	bestDepth, conf := -1, 0
	found := false
	for _, p := range s.Places {
		if p.Archived || exclude[p.ID] {
			continue
		}
		hit, c := false, 0
		for _, src := range p.Context.Sources {
			if (src.Kind == SourceFolder || src.Kind == SourceRepo) && folderHolds(src.Ref, folder) {
				hit, c = true, workspaceSourceConf
				break
			}
		}
		if !hit {
			members, same := 0, 0
			for _, id := range s.ChatsIn(p.ID, false) {
				if ev, ok := library[id]; ok {
					members++
					if cleanFolder(ev.Workspace) == cleanFolder(folder) {
						same++
					}
				}
			}
			if same >= 2 && same*2 > members {
				hit, c = true, workspaceMembersConf
			}
		}
		if !hit {
			continue
		}
		d := depthOf(s, p.ID)
		if !found || d > bestDepth || (d == bestDepth && (p.Name < best.Name || (p.Name == best.Name && p.ID < best.ID))) {
			best, bestDepth, conf, found = p, d, c, true
		}
	}
	return best, conf, found
}

// Bases: why an offer was made, in the person's own terms on the reason line.
const (
	BasisFolder   = "folder"
	BasisWords    = "words"
	BasisModel    = "model"
	BasisSameName = "same-name"
	// BasisOrganizer is a filing the organiser filer decided.
	BasisOrganizer = "organizer"
)

// ---- caps on what the AI creates ---------------------------------------------

// depthOf is a place's level: a top-level place is 1, its child 2. With many
// parents the DEEPEST path counts, so a cap can never be stepped around by
// choosing the shallower parent.
func depthOf(s *Snapshot, id string) int {
	memo := map[string]int{}
	var walk func(string, int) int
	walk = func(id string, guard int) int {
		if d, ok := memo[id]; ok {
			return d
		}
		p, ok := s.Place(id)
		if !ok || guard > len(s.Places) {
			return 0
		}
		d := 1
		for _, par := range p.Parents {
			if pd := walk(par, guard+1) + 1; pd > d {
				d = pd
			}
		}
		memo[id] = d
		return d
	}
	return walk(id, 0)
}

// aiCounts is how many ACTIVE places created from the AI's offers exist in all,
// at the top level, and under each parent.
type aiCounts struct {
	total, top int
	under      map[string]int
}

func countAI(s *Snapshot, aiPlaces []string) aiCounts {
	c := aiCounts{under: map[string]int{}}
	for _, id := range aiPlaces {
		p, ok := s.Place(id)
		if !ok || p.Archived {
			continue
		}
		c.total++
		if len(p.Parents) == 0 {
			c.top++
		}
		for _, par := range p.Parents {
			c.under[par]++
		}
	}
	return c
}

// canCreateUnder reports whether the policy lets one more AI-created place sit
// under parent ("" is the top level), and if not, why, in plain words.
func canCreateUnder(s *Snapshot, pol RecommendPolicy, counts aiCounts, parent string) (bool, string) {
	if counts.total >= pol.MaxAIPlaces {
		return false, "codeaf has already created as many places as it may"
	}
	if parent == "" {
		if counts.top >= pol.MaxAITopLevel {
			return false, "codeaf has already created as many top-level places as it may"
		}
		return true, ""
	}
	p, ok := s.Place(parent)
	if !ok || p.Archived {
		return false, "that parent is gone"
	}
	if counts.under[parent] >= pol.MaxAISiblings {
		return false, "codeaf has already created as many places there as it may"
	}
	if depthOf(s, parent)+1 > pol.MaxAIDepth {
		return false, "a new place there would be too deep"
	}
	return true, ""
}

// sameNameKey folds a name to what it SAYS: case, spacing, punctuation and a
// plural ending do not make two places different.
func sameNameKey(name string) string {
	var b strings.Builder
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			w = strings.TrimSuffix(w, "s")
		}
		b.WriteString(w)
	}
	return b.String()
}

func parentKey(parents []string) string {
	ps := append([]string(nil), parents...)
	sort.Strings(ps)
	return strings.Join(ps, "\x00")
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func cleanFolder(p string) string { return CleanFolder(p) }

// SameNameMerges finds active sibling pairs, preferring a person's place over
// an AI-created one and then the older place. Snoozes belong to the caller's ledger.
func SameNameMerges(snap *Snapshot, aiPlaces []string) []Proposal {
	r := &Recommender{Now: func() time.Time { return time.Time{} }}
	return r.mergeOffers(snap, &ledgerState{AIPlaces: aiPlaces}, DefaultRecommendPolicy())
}
