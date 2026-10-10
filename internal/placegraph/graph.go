package placegraph

// The graph itself: validation, the read-only Snapshot queries, and every
// mutation of the places and their edges. Memberships live in membership.go.
//
// THE LAWS THIS FILE ENFORCES, each pinned by a test:
//   - Places form a DAG: many parents, never a cycle.
//   - The first parent decides an inherited tint; tints never blend.
//   - Deleting or merging a place moves its children up; no chat is ever deleted.
//   - A name is unique among siblings, so the same two words never sit side by
//     side in one tile grid.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ---- copying ---------------------------------------------------------------

func (p Place) clone() Place {
	p.Parents = append([]string{}, p.Parents...)
	p.Context.Sources = append([]Source(nil), p.Context.Sources...)
	p.Manager = append(json.RawMessage(nil), p.Manager...)
	if p.Decide != nil {
		d := *p.Decide
		p.Decide = &d
	}
	return p
}

func (st *State) clone() *State {
	c := &State{
		Version:     st.Version,
		Revision:    st.Revision,
		Places:      make([]Place, len(st.Places)),
		Memberships: append([]Membership{}, st.Memberships...),
		Pinned:      append([]string{}, st.Pinned...),
		Open:        append([]OpenRow(nil), st.Open...),
	}
	for i, p := range st.Places {
		c.Places[i] = p.clone()
	}
	return c
}

func (st *State) find(id string) *Place {
	for i := range st.Places {
		if st.Places[i].ID == id {
			return &st.Places[i]
		}
	}
	return nil
}

// childrenOf maps a parent id to its child ids in Places order. Top-level places
// are listed under RootID. Archived places are included; callers filter.
func (st *State) childrenOf() map[string][]string {
	m := make(map[string][]string, len(st.Places))
	for _, p := range st.Places {
		if len(p.Parents) == 0 {
			m[RootID] = append(m[RootID], p.ID)
		}
		for _, par := range p.Parents {
			m[par] = append(m[par], p.ID)
		}
	}
	return m
}

// descendantIDs is the set reachable below id, id itself excluded, archived places
// INCLUDED (cycle checks must see every edge). Iterative: depth is not bounded.
func (st *State) descendantIDs(id string) map[string]bool {
	kids := st.childrenOf()
	seen := map[string]bool{}
	stack := []string{id}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range kids[n] {
			if !seen[c] {
				seen[c] = true
				stack = append(stack, c)
			}
		}
	}
	return seen
}

// effectiveTint follows first parents until a place states a tint. A top-level
// place that states none is graphite ("no tint chosen"). The walk is bounded by the
// place count so even a graph that somehow held a cycle could not spin.
func (st *State) effectiveTint(id string) Tint {
	cur := id
	for i := 0; i <= len(st.Places); i++ {
		p := st.find(cur)
		if p == nil {
			return TintGraphite
		}
		if p.Tint != "" {
			return p.Tint
		}
		if len(p.Parents) == 0 {
			return TintGraphite
		}
		cur = p.Parents[0]
	}
	return TintGraphite
}

// ---- validation ------------------------------------------------------------

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("%w: empty id", ErrInvalid)
	case id == RootID || id == NowID:
		return ErrReservedID
	case len(id) > MaxIDBytes || !utf8.ValidString(id) || hasControl(id):
		return fmt.Errorf("%w: bad id %q", ErrInvalid, id)
	}
	return nil
}

func validChatID(id string) error {
	if id == "" || len(id) > MaxChatIDBytes || !utf8.ValidString(id) || hasControl(id) {
		return fmt.Errorf("%w: bad chat id", ErrInvalid)
	}
	return nil
}

// cleanName trims and checks a name. Names are free text but one line.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("%w: empty name", ErrInvalid)
	case !utf8.ValidString(name) || hasControl(name):
		return "", fmt.Errorf("%w: name has control characters", ErrInvalid)
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return "", fmt.Errorf("%w: name over %d characters", ErrTooLarge, MaxNameRunes)
	}
	return name, nil
}

func validateContext(c Context, place string) error {
	if len(c.Instructions) > MaxInstructions {
		return fmt.Errorf("%w: instructions over %d bytes", ErrTooLarge, MaxInstructions)
	}
	if !utf8.ValidString(c.Instructions) {
		return fmt.Errorf("%w: instructions are not UTF-8", ErrInvalid)
	}
	if len(c.Sources) > MaxSources {
		return fmt.Errorf("%w: more than %d sources", ErrTooLarge, MaxSources)
	}
	seen := map[string]bool{}
	for _, s := range c.Sources {
		switch {
		case s.ID == "" || len(s.ID) > MaxIDBytes || seen[s.ID]:
			return fmt.Errorf("%w: source id in %s", ErrInvalid, place)
		case !s.Kind.Valid() || !s.AddedBy.Valid():
			return fmt.Errorf("%w: source kind or addedBy in %s", ErrInvalid, place)
		case s.Ref == "" || !utf8.ValidString(s.Ref) || hasControl(s.Ref):
			return fmt.Errorf("%w: source ref in %s", ErrInvalid, place)
		case len(s.Ref) > MaxSourceRefBytes || utf8.RuneCountInString(s.Label) > MaxSourceLabel || hasControl(s.Label):
			return fmt.Errorf("%w: source ref or label in %s", ErrTooLarge, place)
		}
		seen[s.ID] = true
	}
	return nil
}

func validatePolicy(p Policy) error {
	for _, v := range []string{p.Model, p.Permissions} {
		if utf8.RuneCountInString(v) > MaxPolicyFieldRune || hasControl(v) {
			return fmt.Errorf("%w: policy field", ErrInvalid)
		}
	}
	return nil
}

// validateState checks every invariant. With repair true it fixes what it can by
// DROPPING (dangling parent edges, dangling or duplicate memberships, dangling or
// archived pins, duplicate parents) and lists each drop; everything else (bad ids,
// duplicate places, cycles, bound breaches, bad fields) is an error. With repair
// false, nothing is changed and any violation is an error: that is the check every
// mutation must pass, so a bug in a mutator fails loudly instead of persisting.
func validateState(st *State, repair bool) ([]string, error) {
	var repairs []string
	if st.Version != SchemaVersion {
		return nil, fmt.Errorf("%w: version %d", ErrInvalid, st.Version)
	}
	if st.Places == nil {
		st.Places = []Place{}
	}
	if st.Memberships == nil {
		st.Memberships = []Membership{}
	}
	if st.Pinned == nil {
		st.Pinned = []string{}
	}
	if len(st.Places) > MaxPlaces || len(st.Memberships) > MaxMemberships || len(st.Pinned) > MaxPinned {
		return nil, fmt.Errorf("%w: places, memberships or pins over their bound", ErrTooLarge)
	}
	ids := make(map[string]*Place, len(st.Places))
	for i := range st.Places {
		p := &st.Places[i]
		if err := validID(p.ID); err != nil {
			return nil, err
		}
		if ids[p.ID] != nil {
			return nil, fmt.Errorf("%w: duplicate place id %s", ErrInvalid, p.ID)
		}
		ids[p.ID] = p
		if n, err := cleanName(p.Name); err != nil || n != p.Name {
			return nil, fmt.Errorf("%w: name of %s", ErrInvalid, p.ID)
		}
		if p.Tint != "" && !p.Tint.Valid() {
			return nil, fmt.Errorf("%w: tint of %s", ErrInvalid, p.ID)
		}
		if err := validateContext(p.Context, p.ID); err != nil {
			return nil, err
		}
		if err := validatePolicy(p.Policy); err != nil {
			return nil, err
		}
		if p.Decide != nil {
			if err := validateDecide(*p.Decide); err != nil {
				return nil, fmt.Errorf("%w (place %s)", err, p.ID)
			}
		}
		if len(p.Manager) > 0 && (len(p.Manager) > MaxManagerBytes || !json.Valid(p.Manager)) {
			return nil, fmt.Errorf("%w: manager of %s", ErrInvalid, p.ID)
		}
		if p.Parents == nil {
			p.Parents = []string{}
		}
	}
	for i := range st.Places {
		p := &st.Places[i]
		clean := p.Parents[:0:0]
		seen := map[string]bool{}
		for _, par := range p.Parents {
			switch {
			case par == p.ID:
				return nil, fmt.Errorf("%w: %s -> %s", ErrCycle, p.Name, p.Name)
			case par == RootID && repair:
				repairs = append(repairs, "dropped explicit root parent of "+p.ID)
			case par == RootID || par == NowID:
				return nil, fmt.Errorf("%w: parent %q of %s", ErrReservedID, par, p.ID)
			case seen[par] && repair:
				repairs = append(repairs, "dropped duplicate parent "+par+" of "+p.ID)
			case seen[par]:
				return nil, fmt.Errorf("%w: duplicate parent %s of %s", ErrInvalid, par, p.ID)
			case ids[par] == nil && repair:
				repairs = append(repairs, "dropped missing parent "+par+" of "+p.ID)
			case ids[par] == nil:
				return nil, fmt.Errorf("%w: parent %s of %s", ErrNotFound, par, p.ID)
			default:
				seen[par] = true
				clean = append(clean, par)
			}
		}
		p.Parents = clean
		if len(p.Parents) > MaxParents {
			return nil, fmt.Errorf("%w: %s has more than %d parents", ErrTooLarge, p.ID, MaxParents)
		}
	}
	if names := st.cycleNames(); names != "" {
		return nil, fmt.Errorf("%w: %s", ErrCycle, names)
	}
	// Memberships.
	type pair struct{ c, p string }
	seenM := map[pair]bool{}
	perChat := map[string]int{}
	kept := st.Memberships[:0:0]
	for _, m := range st.Memberships {
		if err := validChatID(m.ChatID); err != nil {
			return nil, err
		}
		if !m.AddedBy.Valid() {
			return nil, fmt.Errorf("%w: addedBy of %s in %s", ErrInvalid, m.ChatID, m.PlaceID)
		}
		switch {
		case ids[m.PlaceID] == nil && repair:
			repairs = append(repairs, "dropped membership of "+m.ChatID+" in missing place "+m.PlaceID)
			continue
		case ids[m.PlaceID] == nil:
			return nil, fmt.Errorf("%w: membership place %s", ErrNotFound, m.PlaceID)
		case seenM[pair{m.ChatID, m.PlaceID}] && repair:
			repairs = append(repairs, "dropped duplicate membership of "+m.ChatID+" in "+m.PlaceID)
			continue
		case seenM[pair{m.ChatID, m.PlaceID}]:
			return nil, fmt.Errorf("%w: duplicate membership %s/%s", ErrInvalid, m.ChatID, m.PlaceID)
		}
		seenM[pair{m.ChatID, m.PlaceID}] = true
		perChat[m.ChatID]++
		if perChat[m.ChatID] > MaxChatPlaces {
			return nil, fmt.Errorf("%w: %s is in more than %d places", ErrTooLarge, m.ChatID, MaxChatPlaces)
		}
		kept = append(kept, m)
	}
	st.Memberships = kept
	// Pins.
	pins := st.Pinned[:0:0]
	seenP := map[string]bool{}
	for _, id := range st.Pinned {
		p := ids[id]
		switch {
		case p == nil && repair:
			repairs = append(repairs, "dropped pin of missing place "+id)
		case p == nil:
			return nil, fmt.Errorf("%w: pinned %s", ErrNotFound, id)
		case seenP[id] && repair:
			repairs = append(repairs, "dropped duplicate pin "+id)
		case seenP[id]:
			return nil, fmt.Errorf("%w: duplicate pin %s", ErrInvalid, id)
		case p.Archived && repair:
			repairs = append(repairs, "dropped pin of archived place "+id)
		case p.Archived:
			return nil, fmt.Errorf("%w: pinned %s", ErrArchived, id)
		default:
			seenP[id] = true
			pins = append(pins, id)
		}
	}
	st.Pinned = pins
	st.pruneOpen(ids)
	return repairs, nil
}

// placeName is the words a refusal quotes. An id with no row keeps the id, so a
// damaged edge still names something.
func (st *State) placeName(id string) string {
	if p := st.find(id); p != nil && strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return id
}

// cycleNames is one parent cycle as place names, the first name repeated at the
// end ("Alpha -> Beta -> Alpha"). "" when the graph is a DAG. The walk is
// iterative: a chain as long as MaxPlaces must not grow the call stack. Names,
// not ids, because a refused parent edit has to say which places would loop.
func (st *State) cycleNames() string {
	color := make(map[string]int, len(st.Places)) // 0 unseen, 1 on the stack, 2 done
	type frame struct {
		id string
		pi int
	}
	for _, start := range st.Places {
		if color[start.ID] != 0 {
			continue
		}
		stack := []frame{{id: start.ID}}
		color[start.ID] = 1
		for len(stack) > 0 {
			i := len(stack) - 1
			p := st.find(stack[i].id)
			if p == nil || stack[i].pi >= len(p.Parents) {
				color[stack[i].id] = 2
				stack = stack[:i]
				continue
			}
			par := p.Parents[stack[i].pi]
			stack[i].pi++
			switch color[par] {
			case 1:
				var names []string
				for _, f := range stack {
					if f.id == par || len(names) > 0 {
						names = append(names, st.placeName(f.id))
					}
				}
				names = append(names, st.placeName(par))
				return strings.Join(names, " -> ")
			case 0:
				color[par] = 1
				stack = append(stack, frame{id: par})
			}
		}
	}
	return ""
}

// wouldCycleNames names the loop that making par a parent of id would close.
// par is id, or already reachable below id. The walk is downward through
// children so it follows the edge that actually connects them, not only the
// first parent.
func (st *State) wouldCycleNames(id, par string) string {
	if id == par {
		n := st.placeName(id)
		return n + " -> " + n
	}
	kids := st.childrenOf()
	prev := map[string]string{id: ""}
	queue := []string{id}
	found := false
	for len(queue) > 0 && !found {
		n := queue[0]
		queue = queue[1:]
		for _, c := range kids[n] {
			if _, seen := prev[c]; seen {
				continue
			}
			prev[c] = n
			if c == par {
				found = true
				break
			}
			queue = append(queue, c)
		}
	}
	if !found {
		return st.placeName(id) + " -> " + st.placeName(par) + " -> " + st.placeName(id)
	}
	var chain []string
	for cur := par; ; cur = prev[cur] {
		chain = append(chain, st.placeName(cur))
		if cur == id {
			break
		}
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	chain = append(chain, st.placeName(id))
	return strings.Join(chain, " -> ")
}

// ---- Snapshot --------------------------------------------------------------

// Snapshot is an immutable copy of the graph at one revision, with every read
// query. It does no I/O; hold one for a render and ask it as much as you like.
type Snapshot struct {
	State
	byID map[string]int
}

func newSnapshot(st *State) *Snapshot {
	c := st.clone()
	s := &Snapshot{State: *c, byID: make(map[string]int, len(c.Places))}
	for i, p := range c.Places {
		s.byID[p.ID] = i
	}
	return s
}

// Place returns a copy of one place. RootID and NowID are not places.
func (s *Snapshot) Place(id string) (Place, bool) {
	i, ok := s.byID[id]
	if !ok {
		return Place{}, false
	}
	return s.Places[i].clone(), true
}

func (s *Snapshot) kids() map[string][]string { return s.State.childrenOf() }

// Children lists a place's direct children in creation order; RootID lists the
// top-level places. Archived children are left out unless includeArchived.
func (s *Snapshot) Children(id string, includeArchived bool) []Place {
	var out []Place
	for _, c := range s.kids()[id] {
		p, _ := s.Place(c)
		if includeArchived || !p.Archived {
			out = append(out, p)
		}
	}
	return out
}

// Descendants lists everything below id once each (a diamond's shared child is not
// repeated), nearest first. RootID lists every place. The walk passes THROUGH an
// archived place so its children stay reachable; archived places themselves are
// listed only with includeArchived.
func (s *Snapshot) Descendants(id string, includeArchived bool) []Place {
	kids := s.kids()
	seen := map[string]bool{id: true}
	queue := []string{id}
	var out []Place
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, c := range kids[n] {
			if seen[c] {
				continue
			}
			seen[c] = true
			queue = append(queue, c)
			if p, _ := s.Place(c); includeArchived || !p.Archived {
				out = append(out, p)
			}
		}
	}
	return out
}

// Ancestor is a place above another and how many levels above (1 = a parent).
type Ancestor struct {
	PlaceID string `json:"placeId"`
	Level   int    `json:"level"`
}

// ContextAncestors returns the ancestors that contribute context: parents (level
// 1) and their parents (level 2), each once, at the NEAREST level it appears
// (design 6e: "plus their ancestors, up to 2 levels"). Archived places are skipped
// but still count as a level, so archiving a middle place does not pull a distant
// ancestor closer.
func (s *Snapshot) ContextAncestors(id string) []Ancestor {
	var out []Ancestor
	seen := map[string]bool{id: true}
	frontier := []string{id}
	for level := 1; level <= ContextAncestorLevels; level++ {
		var next []string
		for _, n := range frontier {
			i, ok := s.byID[n]
			if !ok {
				continue
			}
			for _, par := range s.Places[i].Parents {
				if seen[par] {
					continue
				}
				seen[par] = true
				next = append(next, par)
				if pi, ok := s.byID[par]; ok && !s.Places[pi].Archived {
					out = append(out, Ancestor{PlaceID: par, Level: level})
				}
			}
		}
		frontier = next
	}
	return out
}

// Breadcrumb is the first-parent chain from the top down to, not including, id:
// "All places › Software › codeaf". It is what ⌘[ ("up a level") walks.
func (s *Snapshot) Breadcrumb(id string) []Place {
	var rev []Place
	cur := id
	for i := 0; i <= len(s.Places); i++ {
		p, ok := s.Place(cur)
		if !ok || len(p.Parents) == 0 {
			break
		}
		par, ok := s.Place(p.Parents[0])
		if !ok {
			break
		}
		rev = append(rev, par)
		cur = par.ID
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// EffectiveTint is the tint a place shows: its own if it chose one, else its first
// parent's, never a blend. Root and Now are graphite. ok is false for an unknown id.
func (s *Snapshot) EffectiveTint(id string) (Tint, bool) {
	if id == RootID || id == NowID {
		return TintGraphite, true
	}
	if _, ok := s.byID[id]; !ok {
		return "", false
	}
	return s.State.effectiveTint(id), true
}

// SuggestTint picks the least-used pickable tint among top-level places, ties
// broken by palette order. It is deterministic and calls no model (assumption Q-04:
// the design's "AI picks an unused colour" is served by a rule, not a call).
func (s *Snapshot) SuggestTint() Tint { return suggestTint(&s.State) }

// NewTopLevelTint is SuggestTint under the name a new top-level place is given
// when nobody chose a hue: the least-used of the five, graphite excluded.
func (s *Snapshot) NewTopLevelTint() Tint { return s.SuggestTint() }

func suggestTint(st *State) Tint {
	use := map[Tint]int{}
	for _, p := range st.Places {
		if len(p.Parents) == 0 && !p.Archived {
			use[st.effectiveTint(p.ID)]++
		}
	}
	best := PickableTints[0]
	for _, t := range PickableTints {
		if use[t] < use[best] {
			best = t
		}
	}
	return best
}

// Counts are the numbers a tile or a delete confirmation shows.
type Counts struct {
	// Children and Descendants count active (not archived) places.
	Children    int `json:"children"`
	Descendants int `json:"descendants"`
	// Chats are filed directly here; ChatsInclusive adds every descendant's, each
	// chat once ("47 places · 212 chats"). For RootID, Chats is 0.
	Chats          int `json:"chats"`
	ChatsInclusive int `json:"chatsInclusive"`
}

// Counts returns the tile numbers for a place or the root.
func (s *Snapshot) Counts(id string) Counts {
	c := Counts{
		Children:       len(s.Children(id, false)),
		Descendants:    len(s.Descendants(id, false)),
		ChatsInclusive: len(s.ChatsIn(id, true)),
	}
	if id != RootID {
		c.Chats = len(s.ChatsIn(id, false))
	}
	return c
}

// PinnedPlaces lists the pinned places in the person's order.
func (s *Snapshot) PinnedPlaces() []Place {
	var out []Place
	for _, id := range s.Pinned {
		if p, ok := s.Place(id); ok {
			out = append(out, p)
		}
	}
	return out
}

// NameAvailable reports whether name is free among the places that would be its
// siblings under parents (an empty list means top-level), ignoring exceptID.
func (s *Snapshot) NameAvailable(name string, parents []string, exceptID string) bool {
	return !nameClash(&s.State, strings.TrimSpace(name), parents, exceptID)
}

func nameClash(st *State, name string, parents []string, exceptID string) bool {
	under := map[string]bool{}
	for _, p := range parents {
		under[p] = true
	}
	for _, o := range st.Places {
		if o.ID == exceptID || o.Archived || !strings.EqualFold(o.Name, name) {
			continue
		}
		if len(parents) == 0 && len(o.Parents) == 0 {
			return true
		}
		for _, op := range o.Parents {
			if under[op] {
				return true
			}
		}
	}
	return false
}

// ---- mutation helpers ------------------------------------------------------

// normalizeParents drops the virtual root, rejects Now, de-duplicates keeping the
// first occurrence (order matters: the first decides tint) and checks the bound.
func normalizeParents(parents []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range parents {
		switch {
		case p == RootID:
			continue
		case p == NowID:
			return nil, ErrReservedID
		case p == "":
			return nil, fmt.Errorf("%w: empty parent id", ErrInvalid)
		case seen[p]:
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) > MaxParents {
		return nil, fmt.Errorf("%w: more than %d parents", ErrTooLarge, MaxParents)
	}
	return out, nil
}

func (st *State) needPlace(id string) (*Place, error) {
	if id == RootID || id == NowID {
		return nil, ErrReservedID
	}
	p := st.find(id)
	if p == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return p, nil
}

func (st *State) unpin(id string) {
	out := st.Pinned[:0:0]
	for _, p := range st.Pinned {
		if p != id {
			out = append(out, p)
		}
	}
	st.Pinned = out
}

// ---- Store: place mutations ------------------------------------------------

// NewPlace is the input to CreatePlace.
type NewPlace struct {
	Name string
	// Parents, in order; empty or RootID means top-level.
	Parents []string
	// Tint is an explicit choice. Empty: a child inherits; a top-level place gets
	// the least-used pickable tint (Snapshot.SuggestTint).
	Tint    Tint
	Context Context
	Policy  Policy
}

// CreatePlace adds a place with a fresh stable id.
func (s *Store) CreatePlace(in NewPlace) (Place, Receipt, error) {
	var created Place
	rc, err := s.mutate(func(st *State, now time.Time) (*change, error) {
		name, err := cleanName(in.Name)
		if err != nil {
			return nil, err
		}
		parents, err := normalizeParents(in.Parents)
		if err != nil {
			return nil, err
		}
		for _, par := range parents {
			pp, err := st.needPlace(par)
			if err != nil {
				return nil, err
			}
			if pp.Archived {
				return nil, fmt.Errorf("%w: parent %s", ErrArchived, par)
			}
		}
		if in.Tint != "" && !in.Tint.Valid() {
			return nil, fmt.Errorf("%w: tint %q", ErrInvalid, in.Tint)
		}
		if len(st.Places) >= MaxPlaces {
			return nil, fmt.Errorf("%w: %d places", ErrTooLarge, MaxPlaces)
		}
		if nameClash(st, name, parents, "") {
			return nil, ErrNameTaken
		}
		ctx, err := s.prepareContext(in.Context, now)
		if err != nil {
			return nil, err
		}
		if err := validatePolicy(in.Policy); err != nil {
			return nil, err
		}
		tint := in.Tint
		if tint == "" && len(parents) == 0 {
			tint = suggestTint(st)
		}
		id := s.opts.NewID("pl_")
		for st.find(id) != nil {
			id = s.opts.NewID("pl_")
		}
		p := Place{ID: id, Name: name, Parents: parents, Tint: tint, Context: ctx, Policy: in.Policy, CreatedAt: now}
		st.Places = append(st.Places, p)
		created = p.clone()
		return &change{ActionCreate, id}, nil
	})
	if err != nil {
		return Place{}, Receipt{}, err
	}
	return created, rc, nil
}

// prepareContext fills in source ids and stamps, then validates.
func (s *Store) prepareContext(c Context, now time.Time) (Context, error) {
	out := Context{Instructions: c.Instructions, Sources: append([]Source(nil), c.Sources...)}
	for i := range out.Sources {
		if out.Sources[i].ID == "" {
			out.Sources[i].ID = s.opts.NewID("src_")
		}
		if out.Sources[i].AddedBy == "" {
			out.Sources[i].AddedBy = AddedByYou
		}
		if out.Sources[i].At.IsZero() {
			out.Sources[i].At = now
		}
	}
	if err := validateContext(out, "context"); err != nil {
		return Context{}, err
	}
	return out, nil
}

// Rename changes a place's name. The id never changes.
func (s *Store) Rename(id, name string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		name, err := cleanName(name)
		if err != nil {
			return nil, err
		}
		if name == p.Name {
			return nil, nil
		}
		if !p.Archived && nameClash(st, name, p.Parents, id) {
			return nil, ErrNameTaken
		}
		p.Name = name
		return &change{ActionRename, id}, nil
	})
}

// SetTint states a place's tint. The empty Tint clears the choice: a child goes
// back to inheriting its first parent's, a top-level place shows graphite.
func (s *Store) SetTint(id string, t Tint) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		if t != "" && !t.Valid() {
			return nil, fmt.Errorf("%w: tint %q", ErrInvalid, t)
		}
		if p.Tint == t {
			return nil, nil
		}
		p.Tint = t
		return &change{ActionTint, id}, nil
	})
}

// Reparent replaces a place's whole parent list (order is meaningful: the first
// parent decides inherited tint). It refuses a cycle, an unknown or archived NEW
// parent, and a name that would then collide with a sibling.
//
// A place that ends up top-level with no tint of its own keeps the tint it was
// showing, so moving a family member out never silently turns it graphite.
func (s *Store) Reparent(id string, parents []string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) { return reparent(st, id, parents) })
}

func reparent(st *State, id string, parents []string) (*change, error) {
	p, err := st.needPlace(id)
	if err != nil {
		return nil, err
	}
	parents, err = normalizeParents(parents)
	if err != nil {
		return nil, err
	}
	if equalStrings(parents, p.Parents) {
		return nil, nil
	}
	had := map[string]bool{}
	for _, o := range p.Parents {
		had[o] = true
	}
	below := st.descendantIDs(id)
	for _, par := range parents {
		pp, err := st.needPlace(par)
		if err != nil {
			return nil, err
		}
		if par == id || below[par] {
			return nil, fmt.Errorf("%w: %s", ErrCycle, st.wouldCycleNames(id, par))
		}
		if pp.Archived && !had[par] {
			return nil, fmt.Errorf("%w: parent %s", ErrArchived, par)
		}
	}
	if !p.Archived && nameClash(st, p.Name, parents, id) {
		return nil, ErrNameTaken
	}
	shown := st.effectiveTint(id)
	p.Parents = parents
	if len(parents) == 0 && p.Tint == "" {
		p.Tint = shown
	}
	return &change{ActionReparent, id}, nil
}

// AddParent gives a place one more parent, last in order (so it never changes the
// tint). Already a parent: nothing happens.
func (s *Store) AddParent(id, parent string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		return reparent(st, id, append(append([]string{}, p.Parents...), parent))
	})
}

// RemoveParent drops one parent edge. Not a parent: nothing happens. Removing the
// last parent makes the place top-level.
func (s *Store) RemoveParent(id, parent string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		var rest []string
		for _, o := range p.Parents {
			if o != parent {
				rest = append(rest, o)
			}
		}
		return reparent(st, id, rest)
	})
}

// Archive hides a place from lists and counts and unpins it. Its chats keep their
// membership (restoring brings them back), its children stay where they are, and
// it stops contributing context. Already archived: nothing happens.
func (s *Store) Archive(id string) (Receipt, error) {
	return s.mutate(func(st *State, now time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		if p.Archived {
			return nil, nil
		}
		p.Archived, p.ArchivedAt = true, now
		st.unpin(id)
		return &change{ActionArchive, id}, nil
	})
}

// Restore reverses Archive. It is not re-pinned. Not archived: nothing happens.
func (s *Store) Restore(id string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		if !p.Archived {
			return nil, nil
		}
		if nameClash(st, p.Name, p.Parents, id) {
			return nil, ErrNameTaken
		}
		p.Archived, p.ArchivedAt = false, time.Time{}
		return &change{ActionRestore, id}, nil
	})
}

// SetContext replaces a place's instructions and sources. Sources with no id get
// one; an empty AddedBy is "you"; a zero At is now.
func (s *Store) SetContext(id string, c Context) (Receipt, error) {
	return s.mutate(func(st *State, now time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		c, err := s.prepareContext(c, now)
		if err != nil {
			return nil, err
		}
		if contextEqual(p.Context, c) {
			return nil, nil
		}
		p.Context = c
		return &change{ActionContext, id}, nil
	})
}

// SetPolicy replaces a place's policy.
func (s *Store) SetPolicy(id string, pol Policy) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		if err := validatePolicy(pol); err != nil {
			return nil, err
		}
		if p.Policy == pol {
			return nil, nil
		}
		p.Policy = pol
		return &change{ActionPolicy, id}, nil
	})
}

// Pin puts a place in the rail's Pinned section at index (clamped; negative means
// last). Already pinned: it moves, which is how the rail reorders.
func (s *Store) Pin(id string, index int) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		if p.Archived {
			return nil, ErrArchived
		}
		before := append([]string{}, st.Pinned...)
		st.unpin(id)
		if index < 0 || index > len(st.Pinned) {
			index = len(st.Pinned)
		}
		st.Pinned = append(st.Pinned[:index], append([]string{id}, st.Pinned[index:]...)...)
		if len(st.Pinned) > MaxPinned {
			return nil, fmt.Errorf("%w: more than %d pinned", ErrTooLarge, MaxPinned)
		}
		if equalStrings(before, st.Pinned) {
			return nil, nil
		}
		return &change{ActionPin, id}, nil
	})
}

// Unpin removes a place from Pinned. Not pinned: nothing happens.
func (s *Store) Unpin(id string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		before := len(st.Pinned)
		st.unpin(id)
		if len(st.Pinned) == before {
			return nil, nil
		}
		return &change{ActionUnpin, id}, nil
	})
}

// TouchOpened records that a place was gone to (drives ⌘P's Recent). It is not a
// structural change: no revision bump, no receipt, so it never breaks an undo.
func (s *Store) TouchOpened(id string, at time.Time) error {
	release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	st, err := s.loadLocked()
	if err != nil {
		return err
	}
	p, err := st.needPlace(id)
	if err != nil {
		return err
	}
	if at.IsZero() {
		at = s.opts.Now()
	}
	p.LastOpenedAt = at.UTC()
	return s.writeLocked(st)
}

// ---- delete and merge ------------------------------------------------------

// DeleteImpact is what a delete confirmation names ("Delete 14 chats?" in the
// design's words; here the chats are only ever UNFILED, never deleted).
type DeleteImpact struct {
	Children  int `json:"children"`
	ChatsHere int `json:"chatsHere"`
	// WouldBeUnplaced are the chats whose only active place is this one.
	WouldBeUnplaced []string `json:"wouldBeUnplaced"`
}

// PreviewDelete says what DeletePlace(id) would do, without doing it.
func (s *Snapshot) PreviewDelete(id string) (DeleteImpact, error) {
	if _, ok := s.byID[id]; !ok {
		return DeleteImpact{}, ErrNotFound
	}
	imp := DeleteImpact{Children: len(s.Children(id, true)), WouldBeUnplaced: []string{}}
	for _, m := range s.Memberships {
		if m.PlaceID != id {
			continue
		}
		imp.ChatsHere++
		other := false
		for _, o := range s.PlacesOf(m.ChatID) {
			if o.PlaceID != id {
				if p, ok := s.Place(o.PlaceID); ok && !p.Archived {
					other = true
				}
			}
		}
		if !other {
			imp.WouldBeUnplaced = append(imp.WouldBeUnplaced, m.ChatID)
		}
	}
	return imp, nil
}

// DeleteResult reports what DeletePlace did.
type DeleteResult struct {
	ChildrenMoved int      `json:"childrenMoved"`
	Unfiled       int      `json:"unfiled"`
	NowUnplaced   []string `json:"nowUnplaced"`
}

// DeletePlace removes a place. Its children move up to ITS parents (in place, so
// order and first-parent tint are kept; a child that would change colour pins the
// colour it was showing). Its memberships are dropped: each chat keeps its other
// places or becomes unplaced. NO CHAT IS DELETED. Undo restores everything.
func (s *Store) DeletePlace(id string) (DeleteResult, Receipt, error) {
	var res DeleteResult
	rc, err := s.mutate(func(st *State, _ time.Time) (*change, error) {
		victim, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		snap := newSnapshot(st)
		imp, _ := snap.PreviewDelete(id)
		res = DeleteResult{Unfiled: imp.ChatsHere, NowUnplaced: imp.WouldBeUnplaced}
		parents := append([]string{}, victim.Parents...)
		moved, err := detach(st, id, func(*Place) []string { return parents })
		if err != nil {
			return nil, err
		}
		res.ChildrenMoved = moved
		return &change{ActionDelete, id}, nil
	})
	if err != nil {
		return DeleteResult{}, Receipt{}, err
	}
	return res, rc, nil
}

// detach removes place id from the graph: every place that listed it as a parent
// has it replaced, at the same position, by repl(thatPlace) (duplicates and
// self-edges dropped); memberships and the pin go; colours that would change are
// pinned. It returns how many places had their parents rewritten.
func detach(st *State, id string, repl func(child *Place) []string) (int, error) {
	shown := map[string]Tint{}
	for _, p := range st.Places {
		if p.ID != id && containsString(p.Parents, id) {
			shown[p.ID] = st.effectiveTint(p.ID)
		}
	}
	moved := 0
	for i := range st.Places {
		p := &st.Places[i]
		if p.ID == id || !containsString(p.Parents, id) {
			continue
		}
		var next []string
		add := func(x string) {
			if x != p.ID && x != id && !containsString(next, x) {
				next = append(next, x)
			}
		}
		for _, par := range p.Parents {
			if par == id {
				for _, r := range repl(p) {
					add(r)
				}
			} else {
				add(par)
			}
		}
		if len(next) > MaxParents {
			return 0, fmt.Errorf("%w: more than %d parents after reparenting %s", ErrTooLarge, MaxParents, p.ID)
		}
		if next == nil {
			next = []string{}
		}
		p.Parents = next
		moved++
	}
	// Drop the place, its memberships and its pin, then pin changed colours.
	places := st.Places[:0:0]
	for _, p := range st.Places {
		if p.ID != id {
			places = append(places, p)
		}
	}
	st.Places = places
	ms := st.Memberships[:0:0]
	for _, m := range st.Memberships {
		if m.PlaceID != id {
			ms = append(ms, m)
		}
	}
	st.Memberships = ms
	st.unpin(id)
	for cid, was := range shown {
		if p := st.find(cid); p != nil && p.Tint == "" && st.effectiveTint(cid) != was {
			p.Tint = was
		}
	}
	return moved, nil
}

// MergeResult reports what MergePlaces did.
type MergeResult struct {
	Into             Place `json:"into"`
	MembershipsMoved int   `json:"membershipsMoved"`
	ChildrenMoved    int   `json:"childrenMoved"`
	// SkippedParents are parents of the merged place that were NOT added to the
	// survivor because they sit below it and would have made a cycle.
	SkippedParents []string `json:"skippedParents"`
}

// MergePlaces folds `from` into `into` and removes `from`.
//
//   - Memberships union into `into`, de-duplicated (an existing row wins).
//   - Parent edges union: each parent of `from` becomes a parent of `into` unless
//     that would make a cycle (then it is listed in SkippedParents).
//   - Children of `from` become children of `into` (or of `from`'s parents if that
//     would make a cycle), keeping the colour they showed.
//   - Context adds up: instructions are joined by a blank line, sources are united by
//     (kind, ref), and `into`'s policy wins field by field, filled from `from`.
//   - `from`'s pin moves to `into` if `into` is not pinned.
//
// No chat is touched beyond its membership row. Undo restores both places.
func (s *Store) MergePlaces(from, into string) (MergeResult, Receipt, error) {
	var res MergeResult
	rc, err := s.mutate(func(st *State, _ time.Time) (*change, error) {
		if from == into {
			return nil, fmt.Errorf("%w: cannot merge a place into itself", ErrInvalid)
		}
		src, err := st.needPlace(from)
		if err != nil {
			return nil, err
		}
		dst, err := st.needPlace(into)
		if err != nil {
			return nil, err
		}
		if dst.Archived {
			return nil, fmt.Errorf("%w: %s", ErrArchived, into)
		}
		// Context first, so a size failure aborts before anything moves.
		merged := dst.Context
		merged.Sources = append([]Source(nil), dst.Context.Sources...)
		if src.Context.Instructions != "" && src.Context.Instructions != dst.Context.Instructions {
			if merged.Instructions != "" {
				merged.Instructions += "\n\n"
			}
			merged.Instructions += src.Context.Instructions
		}
		for _, so := range src.Context.Sources {
			dup := false
			for _, d := range merged.Sources {
				if d.Kind == so.Kind && d.Ref == so.Ref {
					dup = true
				}
			}
			if !dup {
				for containsSourceID(merged.Sources, so.ID) {
					so.ID = s.opts.NewID("src_")
				}
				merged.Sources = append(merged.Sources, so)
			}
		}
		if err := validateContext(merged, into); err != nil {
			return nil, err
		}
		below := st.descendantIDs(into)
		srcParents := append([]string{}, src.Parents...)
		// A child of `from` that is itself an ancestor of `into` cannot take `into`
		// as a parent without a cycle; it is decided now, before any edge moves.
		aboveInto := map[string]bool{}
		for _, c := range st.childrenOf()[from] {
			if c != into && st.descendantIDs(c)[into] {
				aboveInto[c] = true
			}
		}
		// Parent union.
		for _, par := range srcParents {
			if par == into || below[par] {
				res.SkippedParents = append(res.SkippedParents, par)
				continue
			}
			if !containsString(dst.Parents, par) {
				dst.Parents = append(dst.Parents, par)
			}
		}
		// Memberships union.
		have := map[string]bool{}
		for _, m := range st.Memberships {
			if m.PlaceID == into {
				have[m.ChatID] = true
			}
		}
		for i := range st.Memberships {
			m := &st.Memberships[i]
			if m.PlaceID != from {
				continue
			}
			res.MembershipsMoved++
			if have[m.ChatID] {
				continue // the dropped row is removed by detach; the existing one stays
			}
			have[m.ChatID] = true
			m.PlaceID = into
		}
		dst.Context = merged
		if dst.Policy.Model == "" {
			dst.Policy.Model = src.Policy.Model
		}
		if dst.Policy.Permissions == "" {
			dst.Policy.Permissions = src.Policy.Permissions
		}
		if len(dst.Manager) == 0 {
			dst.Manager = append(json.RawMessage(nil), src.Manager...)
		}
		if i := indexOf(st.Pinned, from); i >= 0 && !containsString(st.Pinned, into) {
			st.Pinned[i] = into
		}
		moved, err := detach(st, from, func(child *Place) []string {
			if child.ID == into {
				return nil // the survivor already took the merged place's parents above
			}
			if !aboveInto[child.ID] {
				return []string{into}
			}
			return srcParents // `into` lives below this child: attaching it there would cycle
		})
		if err != nil {
			return nil, err
		}
		res.ChildrenMoved = moved
		if names := st.cycleNames(); names != "" {
			return nil, fmt.Errorf("%w: %s", ErrCycle, names)
		}
		res.Into = st.find(into).clone()
		if res.SkippedParents == nil {
			res.SkippedParents = []string{}
		}
		return &change{ActionMerge, into}, nil
	})
	if err != nil {
		return MergeResult{}, Receipt{}, err
	}
	return res, rc, nil
}

// ---- small helpers ---------------------------------------------------------

func containsString(xs []string, x string) bool { return indexOf(xs, x) >= 0 }

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsSourceID(ss []Source, id string) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

func contextEqual(a, b Context) bool {
	if a.Instructions != b.Instructions || len(a.Sources) != len(b.Sources) {
		return false
	}
	for i := range a.Sources {
		if a.Sources[i] != b.Sources[i] {
			return false
		}
	}
	return true
}
