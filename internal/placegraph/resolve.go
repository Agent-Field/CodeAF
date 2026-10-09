package placegraph

// RESOLVING WHAT A CHAT USES (design: Places 6e "Context merges from every
// place", 6f "A chat's Using popover").
//
// ContextPlaces already answers WHICH places reach a chat: its active
// memberships plus their ancestors up to two levels, each once, at the nearest
// level. This file turns that list into what the chat is actually given — the
// instructions, the sources and the decided policy — with every item labelled by
// the place it came from, and the budget that keeps several places from filling
// the prompt.
//
// THERE IS ONE RESOLVER. The engine composes message[0] from a Bundle and the
// Using popover draws the same Bundle, so the popover can never list something
// the model was not given or hide something it was. A second resolver in the
// bridge or the renderer would be a second account of the same prompt, and the
// two would drift.
//
// IT NEVER CALLS A MODEL AND NEVER OPENS A SOURCE. "Contradictory instructions"
// cannot be detected without reading them for meaning, so they are not judged
// here: each place's words are kept apart under its own name and the precedence
// rule is stated to the model, which is already reading them. Sources are
// stat'ed (sources.go) and named; their contents are never read into a prompt by
// this package.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"
)

// The per-chat budget (design 6e risks: "Merging several places can bloat the
// prompt, so cap it with a per-chat source budget and show what was trimmed").
// They are named once here; the manual and the prompt interpolate them.
const (
	// ContextSourceBudget is how many sources ONE chat is given across all its
	// places. Sources past it are listed in Bundle.Trimmed and not given.
	ContextSourceBudget = 12
	// ContextInstructionBudget is how many bytes of place instructions ONE chat
	// is given across all its places, nearest place first.
	ContextInstructionBudget = 12 << 10
)

// PolicyField names one setting that CAN conflict between places.
type PolicyField string

const (
	PolicyModel       PolicyField = "model"
	PolicyPermissions PolicyField = "permissions"
)

// PolicyFields lists the fields in the order a Bundle reports them.
var PolicyFields = []PolicyField{PolicyModel, PolicyPermissions}

// Valid reports whether f is a known field.
func (f PolicyField) Valid() bool { return f == PolicyModel || f == PolicyPermissions }

func (p Policy) value(f PolicyField) string {
	switch f {
	case PolicyModel:
		return strings.TrimSpace(p.Model)
	case PolicyPermissions:
		return strings.TrimSpace(p.Permissions)
	}
	return ""
}

// PolicyOutcome says how a field's value was reached.
type PolicyOutcome string

const (
	// PolicyAgreed: every place that has an opinion says the same thing.
	PolicyAgreed PolicyOutcome = "agreed"
	// PolicyDecided: the places disagree and their nearest common ancestor's own
	// value decides (6e: "Release is under Software and Marketing, so codeaf
	// decides").
	PolicyDecided PolicyOutcome = "decided"
	// PolicyChosen: no common ancestor could decide, and the person picked one
	// place's value for this chat earlier (6e: "the chat asks once and remembers
	// the answer").
	PolicyChosen PolicyOutcome = "chosen"
	// PolicyNeedsPick: nobody can decide yet. Value is empty and NOTHING is
	// applied: guessing between two places' permissions is the one thing that
	// must not happen silently.
	PolicyNeedsPick PolicyOutcome = "needsPick"
)

// DecidedByYou is PolicyDecision.DecidedBy for a remembered pick.
const DecidedByYou = "you"

// Bundle is everything one chat uses, labelled by origin. Every slice is non-nil
// so the wire form of an unplaced chat is empty lists, which a surface draws as
// nothing (the emptiness law).
type Bundle struct {
	ChatID   string `json:"chatId"`
	Revision uint64 `json:"revision"`
	// Places are the context places: level 0 (filed here) in filing order, then
	// level 1, then 2 — exactly Snapshot.ContextPlaces.
	Places []UsedPlace `json:"places"`
	// Instructions are each place's prose, nearest first, cut to
	// ContextInstructionBudget. A place whose words did not fit at all is still
	// listed, Trimmed with empty Text, so the popover can say so.
	Instructions []UsedInstruction `json:"instructions"`
	// Sources are given to the model (Status ok or missing), at most
	// ContextSourceBudget, deduplicated across places.
	Sources []UsedSource `json:"sources"`
	// Trimmed are sources that passed the policy but are past the budget. They
	// are named in the Using popover and NOT given to the model.
	Trimmed []UsedSource `json:"trimmed"`
	// Refused are sources the source policy will not give a chat (sources.go),
	// each with its Reason.
	Refused []UsedSource `json:"refused"`
	// Policy is one decision per field some place has an opinion on.
	Policy []PolicyDecision `json:"policy"`
	Counts UsingCounts      `json:"counts"`
}

// UsingCounts is the Using chip ("3 places · 5 sources"). Sources counts what the
// model is given, not what was trimmed or refused.
type UsingCounts struct {
	Places  int `json:"places"`
	Sources int `json:"sources"`
}

// Empty reports whether the chat uses nothing at all.
func (b *Bundle) Empty() bool { return b == nil || len(b.Places) == 0 }

// UsedPlace is one place in the chat's context.
type UsedPlace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tint Tint   `json:"tint"`
	// Level is 0 for a place the chat is filed in, 1 for a parent of one, 2 for
	// a grandparent.
	Level     int  `json:"level"`
	Inherited bool `json:"inherited"`
	// Through names the filed places an inherited place is reached through, in
	// filing order ("codeaf · inherited" through Config parser and Release).
	Through []string `json:"through,omitempty"`
}

// UsedInstruction is one place's instructions as the chat is given them.
type UsedInstruction struct {
	PlaceID string `json:"placeId"`
	// AlsoFrom lists further places whose instructions were the same words, so
	// one text is given once and still credited to every place that says it.
	AlsoFrom []string `json:"alsoFrom,omitempty"`
	Text     string   `json:"text"`
	// Bytes is the full length; Trimmed means Text is shorter than that.
	Bytes   int  `json:"bytes"`
	Trimmed bool `json:"trimmed,omitempty"`
}

// SourceStatus is what a stat of a given source found.
type SourceStatus string

const (
	SourceOK      SourceStatus = "ok"
	SourceMissing SourceStatus = "missing"
	// SourceRefusedStatus marks entries of Bundle.Refused.
	SourceRefusedStatus SourceStatus = "refused"
	// SourceTrimmedStatus marks entries of Bundle.Trimmed.
	SourceTrimmedStatus SourceStatus = "trimmed"
)

// UsedSource is one distinct source and every place that offers it.
type UsedSource struct {
	// Key is the deduplication key (SourceKey).
	Key  string     `json:"key"`
	Kind SourceKind `json:"kind"`
	// Ref is the canonical spelling: a cleaned absolute path, a URL, a chat id.
	Ref   string `json:"ref"`
	Label string `json:"label,omitempty"`
	// RepoRoot is the git work tree a path lies in, when it lies in one.
	RepoRoot string         `json:"repoRoot,omitempty"`
	From     []SourceOrigin `json:"from"`
	Status   SourceStatus   `json:"status"`
	Reason   string         `json:"reason,omitempty"`
}

// SourceOrigin is one place's listing of a source.
type SourceOrigin struct {
	PlaceID  string  `json:"placeId"`
	SourceID string  `json:"sourceId"`
	AddedBy  AddedBy `json:"addedBy"`
	Level    int     `json:"level"`
}

// Want is one place's value for a policy field.
type Want struct {
	PlaceID string `json:"placeId"`
	Value   string `json:"value"`
}

// PolicyDecision is the resolved value of one field and how it was reached.
type PolicyDecision struct {
	Field   PolicyField   `json:"field"`
	Value   string        `json:"value"`
	Outcome PolicyOutcome `json:"outcome"`
	// DecidedBy is a place id (agreed: the nearest place saying it; decided: the
	// common ancestor), DecidedByYou for a pick, or "" when a pick is needed.
	DecidedBy string `json:"decidedBy,omitempty"`
	// Chosen is the place whose value the person picked (PolicyChosen only).
	Chosen string `json:"chosen,omitempty"`
	// Wanted lists every context place with an opinion, nearest first.
	Wanted []Want `json:"wanted"`
}

// ResolveOptions carries what the graph alone does not know.
type ResolveOptions struct {
	// Choices are the person's remembered picks (ChoiceBook.For). Choices for
	// another chat are ignored.
	Choices []Choice
	// Sources is the source policy. The zero value means
	// DefaultSourcePolicy(os.UserHomeDir(), "").
	Sources SourcePolicy
}

// Resolve answers what chatID uses. It reads the disk only to stat path sources
// (sources.go); it never opens one and never calls a model.
func (s *Snapshot) Resolve(chatID string, opts ResolveOptions) *Bundle {
	pol := opts.Sources
	if pol.zero() {
		home, _ := os.UserHomeDir()
		pol = DefaultSourcePolicy(home, "")
	}
	b := &Bundle{
		ChatID:       chatID,
		Revision:     s.Revision,
		Places:       []UsedPlace{},
		Instructions: []UsedInstruction{},
		Sources:      []UsedSource{},
		Trimmed:      []UsedSource{},
		Refused:      []UsedSource{},
		Policy:       []PolicyDecision{},
	}
	cps := s.ContextPlaces(chatID)
	if len(cps) == 0 {
		return b
	}
	through := s.reachedThrough(cps)
	for _, cp := range cps {
		p := s.Places[s.byID[cp.PlaceID]]
		tint, _ := s.EffectiveTint(p.ID)
		b.Places = append(b.Places, UsedPlace{
			ID: p.ID, Name: p.Name, Tint: tint, Level: cp.Level,
			Inherited: cp.Level > 0, Through: through[p.ID],
		})
	}
	b.Instructions = s.resolveInstructions(cps)
	b.Sources, b.Trimmed, b.Refused = s.resolveSources(chatID, cps, pol)
	b.Policy = s.resolvePolicy(chatID, cps, opts.Choices)
	b.Counts = UsingCounts{Places: len(b.Places), Sources: len(b.Sources)}
	return b
}

// reachedThrough maps each inherited place to the filed places it is reached
// through, in filing order.
func (s *Snapshot) reachedThrough(cps []ContextPlace) map[string][]string {
	out := map[string][]string{}
	for _, cp := range cps {
		if cp.Level != 0 {
			continue
		}
		for _, a := range s.ContextAncestors(cp.PlaceID) {
			if !contains(out[a.PlaceID], cp.PlaceID) {
				out[a.PlaceID] = append(out[a.PlaceID], cp.PlaceID)
			}
		}
	}
	// A place that is filed directly is not "through" anything, even when it is
	// also an ancestor of another filed place: it arrives at level 0.
	for _, cp := range cps {
		if cp.Level == 0 {
			delete(out, cp.PlaceID)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// resolveInstructions spends the instruction budget nearest place first.
//
// THE SAME WORDS ARE GIVEN ONCE. A child created by copying its parent's
// instructions says the same thing twice; giving it twice would spend the budget
// on a repeat and suggest to the model that it matters twice as much. The one
// entry is credited to every place that says it.
func (s *Snapshot) resolveInstructions(cps []ContextPlace) []UsedInstruction {
	out := []UsedInstruction{}
	byText := map[string]int{}
	for _, cp := range cps {
		text := strings.TrimSpace(s.Places[s.byID[cp.PlaceID]].Context.Instructions)
		if text == "" {
			continue
		}
		if i, ok := byText[text]; ok {
			out[i].AlsoFrom = append(out[i].AlsoFrom, cp.PlaceID)
			continue
		}
		byText[text] = len(out)
		out = append(out, UsedInstruction{PlaceID: cp.PlaceID, Text: text, Bytes: len(text)})
	}
	left := ContextInstructionBudget
	for i := range out {
		if len(out[i].Text) <= left {
			left -= len(out[i].Text)
			continue
		}
		out[i].Text = cutAtRune(out[i].Text, left)
		out[i].Trimmed = true
		left -= len(out[i].Text)
	}
	return out
}

// cutAtRune shortens text to at most n bytes without splitting a character,
// preferring to end at a line break in the last quarter so a cut does not land
// mid-sentence when it need not.
func cutAtRune(text string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(text) <= n {
		return text
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	text = text[:cut]
	if nl := strings.LastIndexByte(text, '\n'); nl >= len(text)*3/4 {
		text = text[:nl]
	}
	return strings.TrimRight(text, " \t\n")
}

// resolveSources deduplicates every place's sources by canonical key, applies the
// source policy, and spends the source budget nearest place first.
func (s *Snapshot) resolveSources(chatID string, cps []ContextPlace, pol SourcePolicy) (given, trimmed, refused []UsedSource) {
	var all []UsedSource
	byKey := map[string]int{}
	for _, cp := range cps {
		for _, src := range s.Places[s.byID[cp.PlaceID]].Context.Sources {
			origin := SourceOrigin{PlaceID: cp.PlaceID, SourceID: src.ID, AddedBy: src.AddedBy, Level: cp.Level}
			key := SourceKey(src)
			if i, ok := byKey[key]; ok {
				all[i].From = append(all[i].From, origin)
				if all[i].Label == "" {
					all[i].Label = strings.TrimSpace(src.Label)
				}
				continue
			}
			byKey[key] = len(all)
			all = append(all, UsedSource{
				Key: key, Kind: src.Kind, Ref: canonicalRef(src.Kind, src.Ref),
				Label: strings.TrimSpace(src.Label), From: []SourceOrigin{origin},
			})
		}
	}
	given, trimmed, refused = []UsedSource{}, []UsedSource{}, []UsedSource{}
	for _, u := range all {
		// ONCE THE BUDGET IS SPENT NOTHING MORE IS STAT'ED. What is past it is not
		// given either way, and a chat in many large places would otherwise pay a
		// stat per listed path at every turn that re-resolves, under the turn's
		// lock. A trimmed entry is therefore not checked against the policy; it
		// is named in the popover and never reaches a prompt.
		if len(given) >= ContextSourceBudget {
			u.Status, u.Reason = SourceTrimmedStatus, fmt.Sprintf("past the budget of %d sources per chat", ContextSourceBudget)
			trimmed = append(trimmed, u)
			continue
		}
		pol.inspect(chatID, &u)
		if u.Status == SourceRefusedStatus {
			refused = append(refused, u)
			continue
		}
		given = append(given, u)
	}
	return given, trimmed, refused
}

// resolvePolicy decides each field (design 6e "Conflicts").
func (s *Snapshot) resolvePolicy(chatID string, cps []ContextPlace, choices []Choice) []PolicyDecision {
	out := []PolicyDecision{}
	for _, f := range PolicyFields {
		var wanted []Want
		values := map[string]bool{}
		for _, cp := range cps {
			if v := s.Places[s.byID[cp.PlaceID]].Policy.value(f); v != "" {
				wanted = append(wanted, Want{PlaceID: cp.PlaceID, Value: v})
				values[v] = true
			}
		}
		if len(wanted) == 0 {
			continue
		}
		d := PolicyDecision{Field: f, Wanted: wanted}
		if len(values) == 1 {
			d.Value, d.Outcome, d.DecidedBy = wanted[0].Value, PolicyAgreed, wanted[0].PlaceID
			out = append(out, d)
			continue
		}
		ids := make([]string, len(wanted))
		for i, w := range wanted {
			ids[i] = w.PlaceID
		}
		if id, v, ok := s.commonDecider(ids, f); ok {
			d.Value, d.Outcome, d.DecidedBy = v, PolicyDecided, id
			out = append(out, d)
			continue
		}
		d.Outcome = PolicyNeedsPick
		for _, c := range choices {
			if c.ChatID != chatID || c.Field != f {
				continue
			}
			for _, w := range wanted {
				if w.PlaceID == c.PlaceID {
					d.Value, d.Outcome, d.DecidedBy, d.Chosen = w.Value, PolicyChosen, DecidedByYou, w.PlaceID
				}
			}
		}
		out = append(out, d)
	}
	return out
}

// commonDecider finds the place that settles a disagreement among ids: the
// nearest common ancestor that has an opinion of its own on f. A place counts as
// its own ancestor, so a parent that disagrees with its child decides over it
// (6f: "Release wanted Flash · codeaf decided"), and a common ancestor with no
// opinion is passed over for the next one up rather than blocking the decision —
// a plain grouping place has nothing to say about a model. Archived places
// never decide.
//
// IN A GRAPH WITH MANY PARENTS THERE CAN BE SEVERAL NEAREST COMMON ANCESTORS.
// When they say different things there is no honest single decider and the
// person is asked; when they agree, the nearest of them (then the lowest id) is
// named.
func (s *Snapshot) commonDecider(ids []string, f PolicyField) (string, string, bool) {
	dist := make([]map[string]int, len(ids))
	for i, id := range ids {
		dist[i] = s.ancestorsOrSelf(id)
	}
	var common []string
	for id := range dist[0] {
		ok := true
		for _, d := range dist[1:] {
			if _, in := d[id]; !in {
				ok = false
				break
			}
		}
		if ok && s.active(id) && s.Places[s.byID[id]].Policy.value(f) != "" {
			common = append(common, id)
		}
	}
	// Minimal: a common ancestor with no other common ancestor below it.
	var nearest []string
	for _, c := range common {
		below := false
		for _, other := range common {
			if other == c {
				continue
			}
			if _, isAnc := s.ancestorsOrSelf(other)[c]; isAnc {
				below = true
				break
			}
		}
		if !below {
			nearest = append(nearest, c)
		}
	}
	far := func(id string) (worst, sum int) {
		for _, d := range dist {
			worst = max(worst, d[id])
			sum += d[id]
		}
		return
	}
	sort.Slice(nearest, func(i, j int) bool {
		wi, si := far(nearest[i])
		wj, sj := far(nearest[j])
		if wi != wj {
			return wi < wj
		}
		if si != sj {
			return si < sj
		}
		return nearest[i] < nearest[j]
	})
	decider, value := "", ""
	for _, id := range nearest {
		v := s.Places[s.byID[id]].Policy.value(f)
		if decider == "" {
			decider, value = id, v
		} else if v != value {
			return "", "", false
		}
	}
	return decider, value, decider != ""
}

// ancestorsOrSelf maps id and every place above it, at any depth, to its
// distance from id (shortest path). Archived places are walked through, so an
// archived middle place does not cut the line to the places above it.
func (s *Snapshot) ancestorsOrSelf(id string) map[string]int {
	out := map[string]int{id: 0}
	frontier := []string{id}
	for depth := 1; len(frontier) > 0 && depth <= MaxPlaces; depth++ {
		var next []string
		for _, n := range frontier {
			i, ok := s.byID[n]
			if !ok {
				continue
			}
			for _, par := range s.Places[i].Parents {
				if _, seen := out[par]; seen {
					continue
				}
				out[par] = depth
				next = append(next, par)
			}
		}
		frontier = next
	}
	return out
}

// ReadSnapshot reads the graph file WITHOUT the store's lock, for a reader that
// must never wait on another process and never writes: the engine composing a
// prompt at the start of a turn, with the turn's own lock held.
//
// IT IS SAFE BECAUSE EVERY WRITE IS A WHOLE-FILE RENAME (Store.writeLocked): a
// reader sees the previous document or the next one, never half of one. What it
// gives up is recovery — a damaged or newer file is an error here and is left
// exactly where it is for the Store, which owns setting it aside. Dangling
// references are dropped in memory the way Store would drop them, so both read
// the same graph. A missing file is an empty graph.
func ReadSnapshot(path string) (*Snapshot, error) {
	data, err := readCapped(path)
	if errors.Is(err, os.ErrNotExist) {
		return newSnapshot(&State{Version: SchemaVersion, Places: []Place{}, Memberships: []Membership{}, Pinned: []string{}}), nil
	}
	if errors.Is(err, errOversize) {
		return nil, fmt.Errorf("%w: places file over %d bytes", ErrTooLarge, MaxFileBytes)
	}
	if err != nil {
		return nil, err
	}
	var st State
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&st); err != nil {
		return nil, fmt.Errorf("%w: places file is not valid JSON: %v", ErrInvalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: unexpected data after the places document", ErrInvalid)
	}
	if st.Version > SchemaVersion {
		return nil, fmt.Errorf("%w (file version %d, this build %d)", ErrUnsupportedVersion, st.Version, SchemaVersion)
	}
	if _, err := validateState(&st, true); err != nil {
		return nil, err
	}
	return newSnapshot(&st), nil
}
