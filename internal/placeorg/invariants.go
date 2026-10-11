package placeorg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The self-check. These are the laws a Plan must satisfy before it may leave
// this package. They are code, not prose: each is a function over the Tree
// and the Plan, and the Runner runs all of them, journals each result, and
// refuses a Plan on any failure. The caps are named here once; the policy
// page interpolates them.

const (
	// MaxDepth is the deepest an emergent split may go.
	MaxDepth = 6
	// MaxFanout is the most children a node may have before an interpose is
	// considered.
	MaxFanout = 9
	// Quorum is the fewest chats that make a place, after duplicates fold.
	Quorum = 4
	// MaxLinks is the most shortcuts a chat may have.
	MaxLinks = 2
	// MaxNameWords is the longest a name may be.
	MaxNameWords = 2
)

// Invariant is one law.
type Invariant struct {
	// Name is the short spelling the journal and the progress view use.
	Name string
	// Check returns nil when the law holds, or an error naming the first
	// node or chat that breaks it.
	Check func(t *Tree, p *Plan, c *Corpus) error
}

// InvariantResult is one line of the self-check a person can read.
type InvariantResult struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Why  string `json:"why,omitempty"`
}

// Invariants is the full list, in the order they are checked. The names are
// the contract; the checks are the audit stage's.
var Invariants = []Invariant{
	{Name: "depth within " + strconv.Itoa(MaxDepth), Check: checkDepth},
	{Name: "fan-out within " + strconv.Itoa(MaxFanout), Check: checkFanout},
	{Name: "every place has at least " + strconv.Itoa(Quorum) + " chats", Check: checkQuorum},
	{Name: "names unique tree-wide, one or two words", Check: checkNames},
	{Name: "at most " + strconv.Itoa(MaxLinks) + " shortcuts per chat", Check: checkLinks},
	{Name: "forest: one parent per place", Check: checkForest},
	{Name: "every op in the plan is in the journal and replays to this tree", Check: checkReplay},
	{Name: "a home for every chat, duplicates beside their representative", Check: checkHomes},
	{Name: "no probe is a place or counts toward one", Check: checkProbes},
	{Name: "the person's own places are intact in add-on mode", Check: checkAddOn},
	{Name: "no model text became an id, a path or a permission", Check: checkIDs},
}

// Check runs every invariant and returns the results; ok is true only when
// all hold. A nil tree, plan or corpus is an empty one, so a caller that has
// not built that piece yet still gets a result per law.
func Check(t *Tree, p *Plan, c *Corpus) (results []InvariantResult, ok bool) {
	if t == nil {
		t = NewTree()
	}
	if p == nil {
		p = &Plan{}
	}
	if c == nil {
		c = &Corpus{}
	}
	results = make([]InvariantResult, len(Invariants))
	ok = true
	for i, inv := range Invariants {
		results[i].Name = inv.Name
		if inv.Check == nil {
			results[i].Why = "not checked"
			ok = false
			continue
		}
		err := inv.Check(t, p, c)
		if err != nil {
			results[i].Why = err.Error()
			ok = false
			continue
		}
		results[i].OK = true
	}
	return results, ok
}

func checkDepth(t *Tree, _ *Plan, _ *Corpus) error {
	for id, n := range t.nodes {
		d, loop := chainDepth(t, id)
		if loop {
			return fmt.Errorf("%s is part of a loop", id)
		}
		if d > MaxDepth || n.Depth > MaxDepth {
			got := d
			if n.Depth > got {
				got = n.Depth
			}
			return fmt.Errorf("%s is %d deep", id, got)
		}
	}
	return nil
}

func checkFanout(t *Tree, _ *Plan, _ *Corpus) error {
	if len(t.roots) > MaxFanout {
		return fmt.Errorf("the top level has %d places", len(t.roots))
	}
	for id, n := range t.nodes {
		if len(n.Children) > MaxFanout {
			return fmt.Errorf("%s has %d places inside it", id, len(n.Children))
		}
	}
	return nil
}

func checkQuorum(t *Tree, _ *Plan, c *Corpus) error {
	for id := range t.nodes {
		n := placeChatCount(t, id, c)
		if n < Quorum {
			return fmt.Errorf("%s has %d chats", id, n)
		}
	}
	return nil
}

func checkNames(t *Tree, _ *Plan, _ *Corpus) error {
	seen := map[string]string{}
	for id, n := range t.nodes {
		words := strings.Fields(n.Name)
		if len(words) < 1 || len(words) > MaxNameWords {
			return fmt.Errorf("%s is named %q", id, n.Name)
		}
		if prev, ok := seen[n.Name]; ok {
			return fmt.Errorf("%q names both %s and %s", n.Name, prev, id)
		}
		seen[n.Name] = id
	}
	return nil
}

func checkLinks(t *Tree, _ *Plan, _ *Corpus) error {
	for chat, links := range t.links {
		if len(links) > MaxLinks {
			return fmt.Errorf("%s is also in %d places", chat, len(links))
		}
	}
	return nil
}

// checkForest requires one parent per place: roots are exactly the nodes
// with no parent, every child pointer agrees with that child's parent, and
// nothing sits outside the top level or in a loop.
func checkForest(t *Tree, _ *Plan, _ *Corpus) error {
	childOf := map[string]string{}
	for id, n := range t.nodes {
		for _, c := range n.Children {
			if prev, ok := childOf[c]; ok {
				return fmt.Errorf("%s is inside %s and %s", c, prev, id)
			}
			childOf[c] = id
			cn, ok := t.nodes[c]
			if !ok {
				return fmt.Errorf("%s lists %s, which is missing", id, c)
			}
			if cn.Parent != id {
				return fmt.Errorf("%s is inside %s but names %s", c, id, cn.Parent)
			}
		}
	}
	roots := map[string]bool{}
	for _, id := range t.roots {
		if roots[id] {
			return fmt.Errorf("%s is listed twice at the top", id)
		}
		roots[id] = true
		n, ok := t.nodes[id]
		if !ok {
			return fmt.Errorf("%s is at the top and missing", id)
		}
		if n.Parent != "" {
			return fmt.Errorf("%s is at the top and inside %s", id, n.Parent)
		}
	}
	for id, n := range t.nodes {
		if _, loop := chainDepth(t, id); loop {
			return fmt.Errorf("%s is part of a loop", id)
		}
		if n.Parent == "" {
			if !roots[id] {
				return fmt.Errorf("%s has no parent and is not at the top", id)
			}
			continue
		}
		if _, ok := t.nodes[n.Parent]; !ok {
			return fmt.Errorf("%s names %s, which is missing", id, n.Parent)
		}
		if childOf[id] != n.Parent {
			return fmt.Errorf("%s names %s but is not listed there", id, n.Parent)
		}
	}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		n := t.nodes[id]
		if n == nil {
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, id := range t.roots {
		walk(id)
	}
	for id := range t.nodes {
		if !seen[id] {
			return fmt.Errorf("%s is not under a top-level place", id)
		}
	}
	return nil
}

// checkReplay treats the plan's ops as the journal: Check is not handed a
// journal, and the plan is the record that leaves this package. An op with
// no stage was never written down. An applied op must already be true of
// the tree; a proposed one must not have been applied yet.
func checkReplay(t *Tree, p *Plan, _ *Corpus) error {
	if p == nil {
		return nil
	}
	for i, op := range p.Ops {
		if op.Stage == "" {
			return fmt.Errorf("op %d is not in the journal", i)
		}
		if !knownKind(op.Kind) {
			return fmt.Errorf("op %d has an unknown kind %q", i, op.Kind)
		}
		if err := replayOne(t, op); err != nil {
			return fmt.Errorf("op %d: %w", i, err)
		}
	}
	return nil
}

func replayOne(t *Tree, op Op) error {
	switch op.Kind {
	case OpFile, OpPark:
		return expectHome(t, op, true)
	case OpInbox:
		return expectHome(t, op, false)
	case OpLink:
		if op.Proposed {
			return nil
		}
		for _, id := range t.links[op.Chat] {
			if id == op.Place {
				return nil
			}
		}
		return fmt.Errorf("%s is not also in %s", op.Chat, op.Place)
	case OpName, OpRename:
		if op.Proposed {
			return nil
		}
		n, ok := t.nodes[op.Place]
		if !ok || n.Name != op.Name {
			return fmt.Errorf("%s is not named %q", op.Place, op.Name)
		}
		return nil
	case OpCollapse, OpDissolve, OpDemote:
		return expectGone(t, op)
	case OpBirth, OpSplit, OpInterpose:
		// A proposed birth, split or interpose has not created its place yet.
		// An applied one has.
		return expectPresent(t, op, !op.Proposed)
	case OpMerge:
		if op.Proposed {
			if _, ok := t.nodes[op.Place]; !ok {
				return fmt.Errorf("%s is already gone", op.Place)
			}
			return nil
		}
		return expectGone(t, op)
	case OpLens, OpExemplar, OpAxis, OpConstraint:
		return nil
	default:
		return fmt.Errorf("unknown kind %q", op.Kind)
	}
}

func expectHome(t *Tree, op Op, inPlace bool) error {
	if op.Proposed {
		return nil
	}
	home, ok := t.home[op.Chat]
	inInbox := t.inbox[op.Chat]
	if inPlace {
		if !ok || home != op.Place || inInbox {
			return fmt.Errorf("%s is not in %s", op.Chat, op.Place)
		}
		return nil
	}
	if ok || !inInbox {
		return fmt.Errorf("%s is not in Now", op.Chat)
	}
	return nil
}

// expectGone: an applied removal has happened; a proposed one has not.
func expectGone(t *Tree, op Op) error {
	_, ok := t.nodes[op.Place]
	if op.Proposed {
		if !ok {
			return fmt.Errorf("%s is already gone", op.Place)
		}
		return nil
	}
	if ok {
		return fmt.Errorf("%s is still there", op.Place)
	}
	return nil
}

// expectPresent: want says the place should exist.
func expectPresent(t *Tree, op Op, want bool) error {
	_, ok := t.nodes[op.Place]
	if want && !ok {
		return fmt.Errorf("%s is missing", op.Place)
	}
	if !want && ok {
		return fmt.Errorf("%s is already there", op.Place)
	}
	return nil
}

func knownKind(k OpKind) bool {
	switch k {
	case OpBirth, OpCollapse, OpDissolve, OpInterpose, OpDemote, OpSplit,
		OpFile, OpPark, OpInbox, OpLink, OpName, OpRename, OpMerge, OpLens,
		OpExemplar, OpConstraint, OpAxis:
		return true
	default:
		return false
	}
}

func checkHomes(t *Tree, _ *Plan, c *Corpus) error {
	if c == nil {
		return nil
	}
	for _, ch := range c.Chats {
		kind := c.Kinds[ch.ID]
		if kind == KindProbe {
			continue
		}
		home, ok := t.home[ch.ID]
		inInbox := t.inbox[ch.ID]
		if kind == KindDuplicate {
			rep := ""
			if c.Representative != nil {
				rep = c.Representative[ch.ID]
			}
			if rep == "" || rep == ch.ID {
				return fmt.Errorf("%s is a copy of nothing", ch.ID)
			}
			repHome, repOK := t.home[rep]
			repInbox := t.inbox[rep]
			if ok != repOK || home != repHome || inInbox != repInbox {
				return fmt.Errorf("%s is not beside %s", ch.ID, rep)
			}
			continue
		}
		if ok == inInbox {
			// Both or neither: a chat is in a place or in Now, not both
			// and not floating.
			if !ok {
				return fmt.Errorf("%s has no place", ch.ID)
			}
			return fmt.Errorf("%s is in a place and in Now", ch.ID)
		}
	}
	return nil
}

func checkProbes(t *Tree, _ *Plan, c *Corpus) error {
	if c == nil {
		return nil
	}
	probes := map[string]bool{}
	for _, ch := range c.Chats {
		if c.Kinds[ch.ID] == KindProbe {
			probes[ch.ID] = true
		}
	}
	for id := range probes {
		if _, ok := t.home[id]; ok || t.inbox[id] {
			return fmt.Errorf("%s is placed", id)
		}
		if _, ok := t.nodes[id]; ok {
			return fmt.Errorf("%s is a place", id)
		}
	}
	for id, n := range t.nodes {
		for _, m := range n.Members {
			if probes[m] {
				return fmt.Errorf("%s is in %s", m, id)
			}
		}
	}
	return nil
}

// checkAddOn refuses an applied structural change. Add-on keeps every
// existing place; a refresh may rebuild, so this law is silent there.
// A proposed op is an offer, and the place is still where it was.
func checkAddOn(_ *Tree, p *Plan, _ *Corpus) error {
	if p == nil || p.Mode != ModeAddOn {
		return nil
	}
	for _, op := range p.Ops {
		if op.Proposed || !structural(op.Kind) {
			continue
		}
		return fmt.Errorf("%s would change %s", op.Kind, op.Place)
	}
	return nil
}

func structural(k OpKind) bool {
	switch k {
	case OpCollapse, OpDissolve, OpDemote, OpRename, OpMerge, OpSplit, OpInterpose, OpBirth:
		return true
	default:
		return false
	}
}

// idPattern is the shape of an id this package mints or copies from a chat.
// A space, a slash or ".." is model text or a path, and neither may be an id.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9_.:@+-]+$`)

func checkIDs(t *Tree, p *Plan, c *Corpus) error {
	names := map[string]bool{}
	if p != nil {
		for _, op := range p.Ops {
			if op.Name != "" {
				names[op.Name] = true
			}
		}
	}
	check := func(id string) error {
		if id == "" {
			return nil
		}
		if !idPattern.MatchString(id) || strings.Contains(id, "..") {
			return fmt.Errorf("%q is not an id", id)
		}
		if names[id] {
			return fmt.Errorf("%q was written as a name and used as an id", id)
		}
		return nil
	}
	for id, n := range t.nodes {
		if err := check(id); err != nil {
			return err
		}
		if err := check(n.Parent); err != nil {
			return err
		}
		for _, c := range n.Children {
			if err := check(c); err != nil {
				return err
			}
		}
		for _, m := range n.Members {
			if err := check(m); err != nil {
				return err
			}
		}
	}
	for chat, links := range t.links {
		if err := check(chat); err != nil {
			return err
		}
		for _, id := range links {
			if err := check(id); err != nil {
				return err
			}
		}
	}
	for chat := range t.home {
		if err := check(chat); err != nil {
			return err
		}
	}
	if p == nil {
		return nil
	}
	for _, op := range p.Ops {
		for _, id := range []string{op.Place, op.Parent, op.Into, op.Chat} {
			if err := check(id); err != nil {
				return err
			}
		}
		for _, id := range op.Members {
			if err := check(id); err != nil {
				return err
			}
		}
		for _, id := range op.Children {
			if err := check(id); err != nil {
				return err
			}
		}
	}
	if c != nil {
		for _, ch := range c.Chats {
			if err := check(ch.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// chainDepth counts steps from id up to a root. loop is true when a parent
// pointer repeats, which is also a broken forest.
func chainDepth(t *Tree, id string) (int, bool) {
	d := 0
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return d, true
		}
		seen[id] = true
		n, ok := t.nodes[id]
		if !ok {
			break
		}
		d++
		id = n.Parent
		if d > len(t.nodes)+1 {
			return d, true
		}
	}
	return d, false
}

// placeChatCount is the chats that belong to a place or a place inside it,
// not counting a probe or a duplicate. Duplicates are filed beside their
// representative and must not make a place on their own.
func placeChatCount(t *Tree, id string, c *Corpus) int {
	seen := map[string]struct{}{}
	visited := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		n := t.nodes[id]
		if n == nil {
			return
		}
		for _, m := range n.Members {
			if !countsTowardPlace(c, m) {
				continue
			}
			seen[m] = struct{}{}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(id)
	return len(seen)
}

func countsTowardPlace(c *Corpus, id string) bool {
	if c == nil || c.Kinds == nil {
		return true
	}
	switch c.Kinds[id] {
	case KindProbe, KindDuplicate:
		return false
	default:
		return true
	}
}
