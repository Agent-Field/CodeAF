package placegraph

// The deterministic half of recommendations: reading words out of chats and
// places, scoring a chat against a place, grouping chats in no place, and the
// caps on what the AI may create. Nothing here calls a model, and everything
// here is ordered so the same graph and the same chats always give the same
// answer.
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
// singular so "parsers" meets "parser".
func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) < 3 || stopWords[w] {
			continue
		}
		if len(w) > 4 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			w = strings.TrimSuffix(w, "s")
		}
		out[w] = true
	}
	return out
}

func chatWords(c ChatEvidence) map[string]bool {
	return words(clipRunes(c.Title, 200) + " " + clipRunes(c.Summary, 600) + " " + clipRunes(c.FirstMessage, 600))
}

// cleanFolder is a caller-supplied folder in comparable form, or "" when it is
// not an absolute path. It is only ever compared, never opened or written.
func cleanFolder(p string) string {
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

// placeWords is what a place says about itself, weighted: its own name most,
// its sources' labels next, then its instructions, its ancestors' names and the
// titles of chats already filed there.
func placeWords(s *Snapshot, p Place, library map[string]ChatEvidence) map[string]int {
	w := map[string]int{}
	add := func(text string, weight int) {
		for k := range words(text) {
			if w[k] < weight {
				w[k] = weight
			}
		}
	}
	add(p.Name, 3)
	for _, src := range p.Context.Sources {
		add(src.Label, 2)
		if src.Kind == SourceFolder || src.Kind == SourceRepo || src.Kind == SourceFile {
			add(filepath.Base(src.Ref), 2)
		}
	}
	add(clipRunes(p.Context.Instructions, 2000), 1)
	for _, a := range s.ContextAncestors(p.ID) {
		if ap, ok := s.Place(a.PlaceID); ok {
			add(ap.Name, 1)
		}
	}
	for i, id := range s.ChatsIn(p.ID, false) {
		if i >= 50 {
			break
		}
		if c, ok := library[id]; ok {
			add(c.Title, 1)
		}
	}
	return w
}

// candidate is one place scored for one chat or one group of chats.
type candidate struct {
	place      Place
	confidence int
	basis      string
	shared     int
}

// Bases: why an offer was made, in the person's own terms on the reason line.
const (
	BasisFolder   = "folder"
	BasisWords    = "words"
	BasisModel    = "model"
	BasisSameName = "same-name"
)

// scorePlace scores what a chat (or a group's shared words and folder) says
// against one place. Zero means no reason at all to consider it.
func scorePlace(s *Snapshot, p Place, folder string, said map[string]bool, library map[string]ChatEvidence) candidate {
	c := candidate{place: p}
	if folder != "" {
		for _, src := range p.Context.Sources {
			if (src.Kind == SourceFolder || src.Kind == SourceRepo) && folderHolds(src.Ref, folder) {
				c.confidence, c.basis = workspaceSourceConf, BasisFolder
				return c
			}
		}
		members, same := 0, 0
		for _, id := range s.ChatsIn(p.ID, false) {
			if m, ok := library[id]; ok {
				members++
				if cleanFolder(m.Workspace) == cleanFolder(folder) {
					same++
				}
			}
		}
		if same >= 2 && same*2 > members {
			c.confidence, c.basis = workspaceMembersConf, BasisFolder
			return c
		}
	}
	pw := placeWords(s, p, library)
	score := 0
	for w := range said {
		if pw[w] > 0 {
			c.shared++
			score += pw[w]
		}
	}
	if c.shared == 0 {
		return c
	}
	c.confidence, c.basis = min(lexicalConfidenceCap, 20+8*score), BasisWords
	return c
}

// rankCandidates scores every active place the chats are not already in and
// returns the ones with any reason, best first, ties by name then id.
func rankCandidates(s *Snapshot, folder string, said map[string]bool, exclude map[string]bool, library map[string]ChatEvidence) []candidate {
	var out []candidate
	for _, p := range s.Places {
		if p.Archived || exclude[p.ID] {
			continue
		}
		if c := scorePlace(s, p, folder, said, library); c.confidence > 0 {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.confidence != b.confidence {
			return a.confidence > b.confidence
		}
		if a.shared != b.shared {
			return a.shared > b.shared
		}
		if a.place.Name != b.place.Name {
			return a.place.Name < b.place.Name
		}
		return a.place.ID < b.place.ID
	})
	return out
}

// cluster is a group of chats in no place that rules found to belong together.
type cluster struct {
	chats  []ChatEvidence
	folder string          // shared folder, when that is why they belong
	core   map[string]bool // words most of them share
}

func (c cluster) ids() []string {
	out := make([]string, len(c.chats))
	for i, ch := range c.chats {
		out[i] = ch.ChatID
	}
	sort.Strings(out)
	return out
}

// findClusters groups chats in no place. First by shared folder, which is the
// strongest signal there is; then the rest by shared words, single-link with a
// core-word check. Groups smaller than minSize are dropped. Largest first.
func findClusters(chats []ChatEvidence, minSize int) []cluster {
	byFolder := map[string][]ChatEvidence{}
	var loose []ChatEvidence
	for _, c := range chats {
		if f := cleanFolder(c.Workspace); f != "" {
			byFolder[f] = append(byFolder[f], c)
		} else {
			loose = append(loose, c)
		}
	}
	var out []cluster
	folders := make([]string, 0, len(byFolder))
	for f := range byFolder {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	for _, f := range folders {
		group := byFolder[f]
		if len(group) >= minSize {
			out = append(out, cluster{chats: group, folder: f, core: coreWords(group)})
		} else {
			loose = append(loose, group...)
		}
	}
	out = append(out, wordClusters(loose, minSize)...)
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].chats) != len(out[j].chats) {
			return len(out[i].chats) > len(out[j].chats)
		}
		return out[i].ids()[0] < out[j].ids()[0]
	})
	return out
}

func wordClusters(chats []ChatEvidence, minSize int) []cluster {
	sort.SliceStable(chats, func(i, j int) bool { return chats[i].ChatID < chats[j].ChatID })
	sets := make([]map[string]bool, len(chats))
	for i, c := range chats {
		sets[i] = words(clipRunes(c.Title, 200) + " " + clipRunes(c.Summary, 600))
	}
	parent := make([]int, len(chats))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range chats {
		for j := i + 1; j < len(chats); j++ {
			inter, union := 0, len(sets[i])
			for w := range sets[j] {
				if sets[i][w] {
					inter++
				} else {
					union++
				}
			}
			if inter >= 2 && union > 0 && float64(inter)/float64(union) >= clusterJaccard {
				parent[find(j)] = find(i)
			}
		}
	}
	groups := map[int][]ChatEvidence{}
	var roots []int
	for i, c := range chats {
		r := find(i)
		if _, ok := groups[r]; !ok {
			roots = append(roots, r)
		}
		groups[r] = append(groups[r], c)
	}
	var out []cluster
	for _, r := range roots {
		g := groups[r]
		if len(g) < minSize {
			continue
		}
		core := coreWords(g)
		if len(core) == 0 {
			continue
		}
		out = append(out, cluster{chats: g, core: core})
	}
	return out
}

// coreWords are the words at least clusterCoreShare of the chats share.
func coreWords(chats []ChatEvidence) map[string]bool {
	count := map[string]int{}
	for _, c := range chats {
		for w := range chatWords(c) {
			count[w]++
		}
	}
	core := map[string]bool{}
	need := int(float64(len(chats))*clusterCoreShare + 0.999)
	for w, n := range count {
		if n >= need {
			core[w] = true
		}
	}
	return core
}

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
