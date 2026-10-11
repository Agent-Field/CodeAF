// Package steer is the steering channel of the Places organiser: a sentence
// the person typed on a place becomes one validated Steer, and the Steer
// re-renders that place as ops. The model only parses. It picks one entry of
// a closed list and a sentence it cannot map changes nothing.
package steer

import (
	"context"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Steerer implements placeorg.Steerer.
type Steerer struct{}

var (
	_ placeorg.Steerer = (*Steerer)(nil)
	_ placeorg.Judged  = (*Steerer)(nil)
)

// New returns the steerer. It holds no state: every figure it needs arrives
// with the tree, the corpus and the Judge.
func New() *Steerer { return &Steerer{} }

// noneKey is the answer that means "did not understand"; it is the fallback.
const noneKey = "none"

// wish is one row of the closed option space the model chooses from.
type wish struct {
	key    string
	action placeorg.SteerAction
	axis   placeorg.AxisKind
	text   string
}

// wishes is the whole grammar of §10 as options. The model cannot add to it.
var wishes = []wish{
	{"month", placeorg.SteerAxis, placeorg.AxisMonth, "organise this place by month"},
	{"project", placeorg.SteerAxis, placeorg.AxisProject, "organise this place by project or repo"},
	{"workspace", placeorg.SteerAxis, placeorg.AxisWorkspace, "organise this place by the folder each chat ran in"},
	{"kind", placeorg.SteerAxis, placeorg.AxisKindOfWork, "organise this place by kind of work"},
	{"separate", placeorg.SteerSeparate, placeorg.AxisCustom, "keep two named groups apart inside this place"},
	{"flat", placeorg.SteerFlat, "", "flatten this place: no sub-places"},
	{noneKey, placeorg.SteerNone, "", "none of these; the sentence is not a wish about organising"},
}

// Fallbacks states what each judgment point does when the model is absent.
func (*Steerer) Fallbacks() []placeorg.Fallback {
	return []placeorg.Fallback{
		{When: "reading a steering sentence", Does: "understands nothing; no action, no ops"},
		{When: "reading the two groups to keep apart", Does: "takes the words either side of 'separate from' in the sentence"},
		{When: "labelling a chat's kind of work", Does: "the verb the title starts with, from a closed list"},
	}
}

// Parse maps a sentence to a Steer with one Choose over the wishes. The scope
// is left empty: it is the place the person is on, the caller sets it, and
// the model has no way to change it.
func (*Steerer) Parse(ctx context.Context, t *placeorg.Tree, sentence string, judge placeorg.Judge) (placeorg.Steer, placeorg.Pick, error) {
	sentence = strings.TrimSpace(sentence)
	if sentence == "" {
		return placeorg.Steer{Action: placeorg.SteerNone}, placeorg.Pick{Key: noneKey, FellBack: true, Why: "empty sentence"}, nil
	}
	opts := make([]placeorg.Option, len(wishes))
	for i, w := range wishes {
		opts[i] = placeorg.Option{Key: w.key, Text: w.text}
	}
	pick := judge.Choose(ctx, placeorg.Question{
		Role:      placeorg.RolePlaceSteer,
		Purpose:   "Turn the person's sentence about one place into one organising action.",
		Context:   "Sentence: " + sentence,
		Options:   opts,
		MaxTokens: 60,
	}, placeorg.Option{Key: noneKey, Text: wishes[len(wishes)-1].text})
	w := wishFor(pick.Key)
	s := placeorg.Steer{Action: w.action, Axis: w.axis}
	if w.action == placeorg.SteerSeparate {
		s.Values = separateValues(ctx, sentence, judge)
		if len(s.Values) < 2 {
			return placeorg.Steer{Action: placeorg.SteerNone}, pick, nil
		}
	}
	return s, pick, nil
}

// wishFor returns the row for a key, and the none row for anything else.
func wishFor(key string) wish {
	for _, w := range wishes {
		if w.key == key {
			return w
		}
	}
	return wishes[len(wishes)-1]
}

// separateRule reads "keep A separate from B" and its near forms.
var separateRule = regexp.MustCompile(`(?i)(?:keep\s+)?(.+?)\s+(?:separate|apart|distinct)\s+from\s+(.+)`)

// valueWords bounds one value: a group a person names has one or two words.
var valueWords = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}'-]*(?: [\p{L}\p{N}][\p{L}\p{N}'-]*)?$`)

const maxValues = 4

// separateValues asks the model to write the groups, comma separated, and
// accepts only a list the sentence's own rule could also have produced in
// shape: two to maxValues short values. The rule reading is the fallback.
func separateValues(ctx context.Context, sentence string, judge placeorg.Judge) []string {
	rule := ruleValues(sentence)
	fallback := strings.Join(rule, ", ")
	if fallback == "" {
		fallback = noneKey
	}
	pick := judge.Write(ctx, placeorg.Question{
		Role:      placeorg.RolePlaceSteer,
		Purpose:   "Name the groups the person wants kept apart.",
		Context:   "Sentence: " + sentence + "\nAnswer with the group names only, comma separated.",
		MaxTokens: 30,
	}, cleanValues, fallback)
	if vals, ok := splitValues(pick.Written); ok {
		return vals
	}
	return rule
}

// ruleValues reads the groups straight from the sentence.
func ruleValues(sentence string) []string {
	m := separateRule.FindStringSubmatch(sentence)
	if m == nil {
		return nil
	}
	vals, ok := splitValues(m[1] + "," + m[2])
	if !ok {
		return nil
	}
	return vals
}

// cleanValues is the Validator for a written list of groups.
func cleanValues(raw string) (string, bool) {
	vals, ok := splitValues(raw)
	if !ok {
		return "", false
	}
	return strings.Join(vals, ", "), true
}

// splitValues splits on commas and "and", lower-cases, drops repeats and
// checks the bounds. A group cannot be called by a whole sentence.
func splitValues(raw string) ([]string, bool) {
	raw = strings.NewReplacer(" and ", ",", " & ", ",", ";", ",").Replace(strings.ToLower(raw))
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.Trim(strings.TrimSpace(p), `."'`)
		p = strings.TrimPrefix(p, "the ")
		if p == "" || seen[p] {
			continue
		}
		if !valueWords.MatchString(p) {
			return nil, false
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, len(out) >= 2 && len(out) <= maxValues
}
