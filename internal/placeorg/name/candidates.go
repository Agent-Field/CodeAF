// Package name proposes member-spelled names without changing the working tree.
package name

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const CandidateCount = 4

var tokenRE = regexp.MustCompile(`[a-z][a-z0-9-]{2,}`)
var spellingRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9-]*`)
var wordRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,14}$`)
var stop = words("the a an and or of to in for on with is are was were be been it its this that these those you your my me i we our")
var generic = words("only task tasks just lane lanes brief do new chat chats group misc things stuff place general other work")

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

// Index holds corpus document frequencies once, rather than rescanning the library per place.
type Index struct {
	chats []placeorg.Chat
	docs  map[string][]string
	df    map[string]int
	n     int
	// owner holds the lowercase words that are the person's own name. A place
	// named for the person tells the person nothing, so Validator refuses them.
	owner map[string]bool
}

// MinTitleChats is how many member chats must carry a name word in their
// titles before it counts as the place's own vocabulary rather than an accident.
const MinTitleChats = 2

// homeUser reads the person's account name from the folders their chats ran
// in: the segment after /home or /Users that most chats share. It is derived
// from the corpus, never written down, so another person's library works alike.
func homeUser(chats []placeorg.Chat) string {
	counts := map[string]int{}
	for _, c := range chats {
		parts := strings.Split(strings.ReplaceAll(c.Workspace, "\\", "/"), "/")
		for i := 0; i+1 < len(parts); i++ {
			if parts[i] == "home" || parts[i] == "Users" {
				counts[strings.ToLower(parts[i+1])]++
				break
			}
		}
	}
	best := ""
	for w, n := range counts {
		if n > counts[best] || (n == counts[best] && w < best) {
			best = w
		}
	}
	return best
}

// WithOwner adds words from the person's profile (their name, handle) to the
// words the corpus already revealed, and returns the index for chaining.
func (idx *Index) WithOwner(words ...string) *Index {
	for _, w := range words {
		for _, part := range tokenRE.FindAllString(strings.ToLower(w), -1) {
			idx.owner[part] = true
		}
	}
	return idx
}

func NewIndex(corpus *placeorg.Corpus) *Index {
	idx := &Index{docs: map[string][]string{}, df: map[string]int{}, owner: map[string]bool{}}
	for _, c := range corpus.Chats {
		if k := corpus.Kinds[c.ID]; k == placeorg.KindProbe || k == placeorg.KindDuplicate {
			continue
		}
		if _, exists := idx.docs[c.ID]; exists {
			continue
		}
		idx.chats = append(idx.chats, c)
		idx.n++
		seen := map[string]bool{}
		for _, w := range tokenRE.FindAllString(strings.ToLower(c.Text), -1) {
			if stop[w] {
				continue
			}
			idx.docs[c.ID] = append(idx.docs[c.ID], w)
			seen[w] = true
		}
		if idx.docs[c.ID] == nil {
			idx.docs[c.ID] = []string{}
		}
		for w := range seen {
			idx.df[w]++
		}
	}
	idx.WithOwner(homeUser(idx.chats))
	return idx
}

// Candidates matches the reference's top four c-TF-IDF hints before its generic-word filter.
// Equal scores retain first occurrence in corpus order, as Python's stable sort does.
func (idx *Index) Candidates(members []string) []string {
	member := map[string]bool{}
	for _, id := range members {
		member[id] = true
	}
	tf, support := map[string]int{}, map[string]int{}
	var order []string
	for _, c := range idx.chats {
		if !member[c.ID] {
			continue
		}
		seen := map[string]bool{}
		for _, w := range idx.docs[c.ID] {
			if tf[w] == 0 {
				order = append(order, w)
			}
			tf[w]++
			seen[w] = true
		}
		for w := range seen {
			support[w]++
		}
	}
	score := map[string]float64{}
	var ranked []string
	for _, w := range order {
		if support[w] < 2 || float64(idx.df[w]) > .6*float64(idx.n) {
			continue
		}
		score[w] = float64(tf[w]) * math.Log(float64(idx.n)/float64(1+idx.df[w]))
		ranked = append(ranked, w)
	}
	sort.SliceStable(ranked, func(i, j int) bool { return score[ranked[i]] > score[ranked[j]] })
	if len(ranked) > CandidateCount {
		ranked = ranked[:CandidateCount]
	}
	return ranked
}

// Validator keeps the dominant spelling in member titles; message spelling is a backstop.
// Semantic picks may combine member words, but cannot introduce an unsupported word.
func (idx *Index) Validator(members []string, taken map[string]bool) placeorg.Validator {
	return idx.ValidatorWithCharter(members, taken, "")
}

// ValidatorWithCharter also refuses a name word that is the person's own name,
// or that fewer than MinTitleChats member titles use unless the charter line
// says it: a rare token names an accident, not a subject.
func (idx *Index) ValidatorWithCharter(members []string, taken map[string]bool, charter string) placeorg.Validator {
	member := map[string]bool{}
	for _, id := range members {
		member[id] = true
	}
	titles, texts := []string{}, []string{}
	for _, c := range idx.chats {
		if member[c.ID] {
			titles = append(titles, c.Title)
			texts = append(texts, c.Text)
		}
	}
	inTitles := map[string]int{}
	for _, title := range titles {
		seen := map[string]bool{}
		for _, w := range tokenRE.FindAllString(strings.ToLower(title), -1) {
			if !seen[w] {
				seen[w] = true
				inTitles[w]++
			}
		}
	}
	meant := map[string]bool{}
	for _, w := range tokenRE.FindAllString(strings.ToLower(charter), -1) {
		meant[w] = true
	}
	spell := dominant(titles)
	for key, value := range dominant(texts) {
		if spell[key] == "" {
			spell[key] = value
		}
	}
	return func(raw string) (string, bool) {
		fields := strings.Fields(strings.Trim(strings.TrimSpace(raw), `"`))
		if len(fields) < 1 || len(fields) > 2 {
			return "", false
		}
		allGeneric := true
		for i, w := range fields {
			if !wordRE.MatchString(w) {
				return "", false
			}
			key := strings.ToLower(w)
			if !generic[key] && !stop[key] {
				allGeneric = false
			}
			if spell[key] == "" || idx.owner[key] {
				return "", false
			}
			if !generic[key] && !stop[key] && inTitles[key] < MinTitleChats && !meant[key] {
				return "", false
			}
			fields[i] = spell[key]
		}
		result := strings.Join(fields, " ")
		return result, !allGeneric && !taken[strings.ToLower(result)]
	}
}

func dominant(texts []string) map[string]string {
	counts := map[string]map[string]int{}
	order := map[string][]string{}
	out := map[string]string{}
	for _, text := range texts {
		for _, spelling := range spellingRE.FindAllString(text, -1) {
			key := strings.ToLower(spelling)
			if counts[key] == nil {
				counts[key] = map[string]int{}
			}
			if counts[key][spelling] == 0 {
				order[key] = append(order[key], spelling)
			}
			counts[key][spelling]++
		}
	}
	for key, spellings := range order {
		for _, spelling := range spellings {
			if counts[key][spelling] > counts[key][out[key]] {
				out[key] = spelling
			}
		}
	}
	return out
}
