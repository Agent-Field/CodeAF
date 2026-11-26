package factory

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ── A WORKSTREAM'S STRUCTURE: NESTING, ITS STEWARD AND ITS RULES ──────────
//
// Workstreams v5 (SPEC §2.1) gives a workstream three more things, and each
// one is kept on the workstream itself so a save stays one row:
//
//   - Parent: the workstream it sits inside. NESTING IS ONE LEVEL: a
//     workstream with a parent can never be one, and one that has children
//     can never get a parent. A child ALWAYS HAS ITS PARENT'S PEOPLE: the
//     store copies the parent's owner, shares and privacy onto every child in
//     the same save that changed them ([InheritPeople]), so the access core
//     reads a child exactly as it reads any workstream.
//   - Steward: the one person who looks after it, its maker until handed on.
//     Only the steward changes its people and its rules; everyone else in it
//     reads them.
//   - Rules: who owns incoming work, who else is asked when an ask goes
//     unanswered for a day, how much an agent may spend on a task before it
//     must ask, and what keeps running. A child starts with a COPY of its
//     parent's rules and may change its own copy.
//
// People are not a new list. Today's owner and shares ARE the people (a person
// or a team, each with the role access reads), so nothing that already
// decides who reaches a workstream moved.

// The kinds a rules owner may be.
const (
	RulesOwnerPerson   = "person"
	RulesOwnerEveryone = "everyone"
)

// RulesOwner is who owns work that nobody asked for directly: one person, or
// everyone in the workstream (the first to answer decides).
type RulesOwner struct {
	Kind string `json:"kind" jsonschema:"enum=person,enum=everyone"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// RulesPerson is one person a rule names.
type RulesPerson struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// KeepsRunning is what runs in a workstream without anyone asking: the
// Factory turning new issues into tasks here, and the standing orders, by id,
// that are switched on for it.
type KeepsRunning struct {
	Factory bool     `json:"factory"`
	Orders  []string `json:"orders"`
	// FactorySince is when the Factory switch was last turned on. THE
	// SERVER STAMPS IT, never the person: whatever a writer sends is
	// replaced ([StampFactorySince]). Only an issue opened after it comes
	// into the workstream ([FactoryWorkstreamFor]), so turning the switch on
	// never sweeps in the backlog the repository already had.
	FactorySince *time.Time `json:"factory_since,omitempty"`
}

// WorkstreamRules are how a workstream runs (SPEC §2.1).
//
// StartWithoutAskingUSD is the most an agent may spend on one task before it
// must ask; ZERO IS NEVER START WITHOUT ASKING. It is honoured through the
// workstream's go-ahead policy ([ApplyStartWithoutAsking]), so the Factory
// follows it by itself with no app attached.
type WorkstreamRules struct {
	DefaultOwner          RulesOwner   `json:"default_owner"`
	Fallback              *RulesPerson `json:"fallback,omitempty"`
	StartWithoutAskingUSD float64      `json:"start_without_asking_usd"`
	KeepsRunning          KeepsRunning `json:"keeps_running"`
	// PublishesWithoutAsking turns the ask rule off: an agent publishes,
	// sends, posts and pays without asking first. Its zero value is the
	// default, "Afi asks before publishing or sending" (workstream_asks_first.go),
	// so every workstream kept before the rule existed asks too.
	PublishesWithoutAsking bool `json:"publishes_without_asking,omitempty"`
	// AsksBefore is how much the ask rule stops, when it is on: "" asks
	// before anything is published, sent or paid for (AsksBeforeOutward, the
	// default every kept workstream reads as), AsksBeforeSharedFile also
	// before anything that changes a shared file, AsksBeforeEveryTask before
	// every task starts (People and rules, screen 16).
	AsksBefore string `json:"asks_before,omitempty" jsonschema:"enum=,enum=outward,enum=shared_file,enum=every_task"`
	// FallbackAfterHours is how long an Ask waits on its decider before
	// Fallback may answer it too ("Santosh answers what waits unanswered for
	// 1 day"). ZERO IS THE DEFAULT DAY ([TaskFallbackAfter]); a value is one
	// of [FallbackAfterChoices].
	FallbackAfterHours int `json:"fallback_after_hours,omitempty"`
	// Exceptions are the named kinds of question Afi decides without asking
	// here, each listed under Assumed on its task instead of raised as an Ask
	// (workstream_ask_exceptions.go). They loosen the rule; nothing else may.
	Exceptions []AskException `json:"exceptions,omitempty"`
	// NotNow is each suggested exception the steward turned down, and when,
	// so Afi does not suggest it again for [SuggestionRestFor].
	NotNow []AskNotNow `json:"not_now,omitempty"`
	// Said are the workstream's rules in plain words, as people wrote them
	// (a new workstream's "Nothing ships without Priya's OK"): shown in People
	// and rules and given to every task. They were kept as decisions, decided
	// by whoever made the workstream (live test 2026-11-23 #14).
	Said []string `json:"said,omitempty"`
}

// StampFactorySince sets next's [KeepsRunning.FactorySince] from before: kept
// while the Factory switch stays on, now when it was just turned on, and
// gone when it is off.
func StampFactorySince(before WorkstreamRules, next *WorkstreamRules, now time.Time) {
	switch {
	case !next.KeepsRunning.Factory:
		next.KeepsRunning.FactorySince = nil
	case before.KeepsRunning.Factory && before.KeepsRunning.FactorySince != nil:
		at := *before.KeepsRunning.FactorySince
		next.KeepsRunning.FactorySince = &at
	default:
		at := now.UTC()
		next.KeepsRunning.FactorySince = &at
	}
}

// FactoryWorkstreamFor is the workstream whose Factory switch takes the job
// in, and false when none does. SPEC: "The factory turns each new issue in a
// connected repo into a task" in the workstream whose switch is on.
//
// Only an issue from GitHub that was opened after the switch was turned on
// is taken, and only by a workstream that is not archived. A workstream that
// names the issue's repository among its sources takes it before one that
// names no repository at all; one that names only other repositories never
// does. When several are equally near, the oldest takes it, because a job
// belongs to one workstream at most.
func FactoryWorkstreamFor(all OrgWorkstreams, it Item) (string, bool) {
	repo := strings.ToLower(RepoOf(it))
	if !IsIssueJob(it) || it.Kind != KindIssue || it.Num <= 0 || repo == "" || it.Created.IsZero() {
		return "", false
	}
	best, bestNamed := OrgWorkstream{}, false
	found := false
	for _, w := range all {
		r := w.RulesOrDefault()
		if w.ArchivedAt != nil || !r.KeepsRunning.Factory || r.KeepsRunning.FactorySince == nil || it.Created.Before(*r.KeepsRunning.FactorySince) {
			continue
		}
		named, any := false, false
		for _, src := range w.Sources {
			if src.Kind != "repo" {
				continue
			}
			any = true
			if strings.EqualFold(strings.TrimSpace(src.Ref), repo) {
				named = true
			}
		}
		if any && !named {
			continue
		}
		switch {
		case !found, named && !bestNamed:
		case named != bestNamed:
			continue
		case w.CreatedAt.Before(best.CreatedAt), w.CreatedAt.Equal(best.CreatedAt) && w.ID < best.ID:
		default:
			continue
		}
		best, bestNamed, found = w, named, true
	}
	return best.ID, found
}

// The bounds a workstream's rules are kept within.
const (
	// RulesOrdersMost is the most standing orders one workstream keeps
	// switched on.
	RulesOrdersMost = 50
	// RulesOrderIDMost is the longest order id a rule keeps.
	RulesOrderIDMost = 128
	// RulesSaidMost is the most rules in words a workstream keeps, and
	// RulesSaidLongest the longest one, in characters.
	RulesSaidMost    = 20
	RulesSaidLongest = 400
)

// ErrNestedTooDeep is a workstream asked to sit inside one that is itself
// inside another.
var ErrNestedTooDeep = errors.New("a workstream can sit inside a top-level workstream only; this one is already inside another")

// ErrSpaceHoldsNoWorkstreams is a workstream asked to sit inside a person's
// own space. My space holds pages, canvases and chats; shared work belongs in
// a workstream of its own.
var ErrSpaceHoldsNoWorkstreams = errors.New("My space holds no workstreams; make it a workstream of its own")

// ErrHasChildren is a workstream with workstreams inside it asked to move
// inside another.
var ErrHasChildren = errors.New("this workstream has workstreams inside it, so it cannot move inside another")

// ErrChildPeople is a change to a child's people, which are always its
// parent's.
var ErrChildPeople = errors.New("a workstream inside another always has the same people; change them on the workstream it is inside")

// StewardHandle is who looks after w: its steward, else its maker, else its
// person owner.
func (w OrgWorkstream) StewardHandle() string {
	switch {
	case w.Steward != "":
		return NormalizeHandle(w.Steward)
	case w.CreatedBy != "":
		return NormalizeHandle(w.CreatedBy)
	case w.Owner.Kind == ShareToPerson:
		return NormalizeHandle(w.Owner.ID)
	}
	return ""
}

// RulesOrDefault is w's rules, or the rules a workstream that never set any
// runs by: incoming work is its steward's, nobody else is asked, nothing
// starts without asking and nothing keeps running.
func (w OrgWorkstream) RulesOrDefault() WorkstreamRules {
	if w.Rules != nil {
		r := *w.Rules
		r.KeepsRunning.Orders = slices.Clone(r.KeepsRunning.Orders)
		if r.KeepsRunning.Orders == nil {
			r.KeepsRunning.Orders = []string{}
		}
		if r.Fallback != nil {
			f := *r.Fallback
			r.Fallback = &f
		}
		r.Exceptions, r.NotNow = slices.Clone(r.Exceptions), slices.Clone(r.NotNow)
		return r
	}
	return DefaultRules(w.StewardHandle())
}

// DefaultRules are the rules a workstream starts with when nothing else is
// given: incoming work is steward's.
func DefaultRules(steward string) WorkstreamRules {
	owner := RulesOwner{Kind: RulesOwnerEveryone}
	if steward != "" {
		owner = RulesOwner{Kind: RulesOwnerPerson, ID: steward}
	}
	return WorkstreamRules{DefaultOwner: owner, KeepsRunning: KeepsRunning{Orders: []string{}}}
}

// CleanRules checks r and puts it in the shape it is kept in: handles
// normalized, names dropped (a name is answered fresh, never kept), orders
// trimmed and without repeats.
func CleanRules(r *WorkstreamRules) error {
	switch r.DefaultOwner.Kind {
	case RulesOwnerPerson:
		r.DefaultOwner.ID = NormalizeHandle(r.DefaultOwner.ID)
		if r.DefaultOwner.ID == "" {
			return errors.New("the owner of incoming work needs a person")
		}
	case RulesOwnerEveryone:
		r.DefaultOwner.ID = ""
	default:
		return errors.New(`the owner of incoming work is a person or "everyone"`)
	}
	r.DefaultOwner.Name = ""
	if r.Fallback != nil {
		r.Fallback.ID, r.Fallback.Name = NormalizeHandle(r.Fallback.ID), ""
		if r.Fallback.ID == "" {
			r.Fallback = nil
		}
	}
	usd := r.StartWithoutAskingUSD
	if math.IsNaN(usd) || math.IsInf(usd, 0) || usd < 0 || usd > PolicyCapMostUSD {
		return fmt.Errorf("start without asking is $0 (never) up to $%d a task", PolicyCapMostUSD)
	}
	r.StartWithoutAskingUSD = math.Round(usd*100) / 100
	orders := make([]string, 0, len(r.KeepsRunning.Orders))
	for _, id := range r.KeepsRunning.Orders {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(orders, id) {
			continue
		}
		if len(id) > RulesOrderIDMost || strings.IndexFunc(id, unicode.IsControl) >= 0 || !utf8.ValidString(id) {
			return errors.New("a standing order's id is 1–128 plain characters")
		}
		orders = append(orders, id)
	}
	if len(orders) > RulesOrdersMost {
		return fmt.Errorf("a workstream keeps at most %d standing orders running", RulesOrdersMost)
	}
	r.KeepsRunning.Orders = orders
	switch r.AsksBefore {
	case "", AsksBeforeOutward:
		r.AsksBefore = ""
	case AsksBeforeSharedFile, AsksBeforeEveryTask:
	default:
		return errors.New(`the ask rule is "outward", "shared_file" or "every_task"`)
	}
	if r.FallbackAfterHours == int(TaskFallbackAfter/time.Hour) {
		r.FallbackAfterHours = 0
	}
	if r.FallbackAfterHours != 0 && !slices.Contains(FallbackAfterChoices, r.FallbackAfterHours) {
		return fmt.Errorf("the wait before the fallback answers is one of %v hours", FallbackAfterChoices)
	}
	said := make([]string, 0, len(r.Said))
	for _, line := range r.Said {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" || slices.Contains(said, line) {
			continue
		}
		if utf8.RuneCountInString(line) > RulesSaidLongest || !utf8.ValidString(line) {
			return fmt.Errorf("a rule is at most %d characters", RulesSaidLongest)
		}
		said = append(said, line)
	}
	if len(said) > RulesSaidMost {
		return fmt.Errorf("a workstream keeps at most %d rules in words", RulesSaidMost)
	}
	r.Said = said
	if len(said) == 0 {
		r.Said = nil
	}
	return cleanExceptions(r)
}

// ChildrenOf is the ids of the workstreams inside id, archived ones included,
// in id order.
func ChildrenOf(all OrgWorkstreams, id string) []string {
	var out []string
	for cid, w := range all {
		if w.Parent == id && cid != id {
			out = append(out, cid)
		}
	}
	sort.Strings(out)
	return out
}

// CheckParent is whether id may sit inside parent: parent is another
// workstream that is open and top-level, and id has nothing inside it. An
// empty parent is always allowed (top-level).
func CheckParent(all OrgWorkstreams, id, parent string) error {
	if parent == "" {
		return nil
	}
	if parent == id {
		return errors.New("a workstream cannot sit inside itself")
	}
	p, ok := all[parent]
	if !ok {
		return errors.New("the workstream to put it inside is not one this organization keeps")
	}
	if p.ArchivedAt != nil {
		return errors.New("the workstream to put it inside is archived")
	}
	if p.Space {
		return ErrSpaceHoldsNoWorkstreams
	}
	if p.Parent != "" {
		return ErrNestedTooDeep
	}
	if id != "" && len(ChildrenOf(all, id)) > 0 {
		return ErrHasChildren
	}
	return nil
}

// InheritPeople copies each changed top-level workstream's people (owner,
// shares, privacy) onto the workstreams inside it, and each changed child's
// parent's people onto that child, answering the ids it changed besides
// those. THIS IS THE ONE PLACE A CHILD'S PEOPLE ARE WRITTEN, and it runs inside
// every save, so no path that changes a parent's people can leave a child
// behind.
func InheritPeople(all OrgWorkstreams, changed []string) []string {
	var more []string
	seen := map[string]bool{}
	for _, id := range changed {
		seen[id] = true
	}
	mirror := func(childID string, parent OrgWorkstream) {
		c, ok := all[childID]
		if !ok || c.Parent != parent.ID {
			return
		}
		if c.Owner == parent.Owner && c.Private == parent.Private && slices.Equal(c.Shares, parent.Shares) {
			return
		}
		c.Owner, c.Private, c.Shares, c.SharedWith = parent.Owner, parent.Private, slices.Clone(parent.Shares), nil
		all[childID] = c
		if !seen[childID] {
			seen[childID] = true
			more = append(more, childID)
		}
	}
	for _, id := range changed {
		w, ok := all[id]
		if !ok {
			continue
		}
		if w.Parent != "" {
			if p, ok := all[w.Parent]; ok {
				mirror(id, p)
			}
			continue
		}
		for _, cid := range ChildrenOf(all, id) {
			mirror(cid, w)
		}
	}
	return more
}

// ReadsFilesOf is the workstreams whose files w's people and agents read, in
// order: w itself, then the workstream it is inside, whose files it shows as
// "From <parent>". Files asks this rather than walking parents itself, so
// the one-level rule is spelled once.
func ReadsFilesOf(w OrgWorkstream) []string {
	if w.Parent == "" {
		return []string{w.ID}
	}
	return []string{w.ID, w.Parent}
}

// ── start without asking, through the go-ahead policy ──

// RuleAllowanceID is the id of the one allowance a workstream's "start
// without asking" rule keeps in its go-ahead policy. Allowances a person made
// by hand keep their own ids and are never touched.
const RuleAllowanceID = "start-without-asking"

// ruleAllowanceYears is how far ahead the rule's allowance ends: a rule has no
// end, and every change to it or to its jobs writes it again.
const ruleAllowanceYears = 1

// ApplyStartWithoutAsking writes a workstream's "start without asking" rule
// into its go-ahead policy p: ONE allowance covering the workstream's jobs, at
// most usd on each job ([Allowance.PerJobUSD]), and the rule's owner as the
// policy's owner when it has none. Zero takes the allowance away, so nothing
// starts without asking. It answers whether p changed.
func ApplyStartWithoutAsking(p *GroupPolicy, jobs []int, usd float64, owner string, now time.Time) bool {
	before := slices.Clone(p.Allowances)
	beforeOwner := p.Owner
	kept := p.Allowances[:0:0]
	for _, a := range p.Allowances {
		if a.ID != RuleAllowanceID {
			kept = append(kept, a)
		}
	}
	cover := make([]int, 0, len(jobs))
	for _, j := range jobs {
		if j > 0 && !slices.Contains(cover, j) {
			cover = append(cover, j)
		}
	}
	if len(cover) > PolicyJobsMost {
		// THE NEWEST JOBS ARE THE ONES STILL WAITING TO START; the oldest
		// ones the allowance could not hold have long started or ended.
		cover = cover[len(cover)-PolicyJobsMost:]
	}
	if usd > 0 && len(cover) > 0 {
		capUSD := math.Min(PolicyCapMostUSD, usd*float64(len(cover)))
		kept = append(kept, Allowance{
			ID: RuleAllowanceID, Jobs: cover, CapUSD: capUSD, PerJobUSD: math.Min(usd, capUSD),
			EndsAt: now.UTC().AddDate(ruleAllowanceYears, 0, 0).Truncate(time.Hour).Format(time.RFC3339),
		})
		if p.Owner == "" && owner != "" {
			p.Owner = owner
		}
	}
	p.Allowances = kept
	// An allowance written again with the same jobs and cap is the same rule:
	// only its end moved, and a write for that alone would bump the policy's
	// version on every save of the workstream.
	if p.Owner == beforeOwner && sameAllowancesButEnd(before, kept) {
		p.Allowances = before
		return false
	}
	return true
}

func sameAllowancesButEnd(a, b []Allowance) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.ID != y.ID || x.CapUSD != y.CapUSD || x.PerJobUSD != y.PerJobUSD || !slices.Equal(x.Jobs, y.Jobs) {
			return false
		}
		if x.ID != RuleAllowanceID && x.EndsAt != y.EndsAt {
			return false
		}
	}
	return true
}

// ── archiving and folding a child back ──

// KeepCandidate is one thing in a child workstream that could stay in its
// parent when the child is archived: the agent asks which of these to keep.
type KeepCandidate struct {
	Kind  string `json:"kind" jsonschema:"enum=page,enum=source"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ArchiveKeep is what a child workstream could leave in its parent when it is
// archived: its pages and its sources, in its own order. A top-level
// workstream answers nothing, because there is no parent to keep them in.
// Files moves them; this only says what there is.
func ArchiveKeep(w OrgWorkstream) []KeepCandidate {
	out := []KeepCandidate{}
	if w.Parent == "" {
		return out
	}
	for _, p := range w.Pages {
		if strings.TrimSpace(p.BodyMD) == "" && p.Kind == "overview" {
			continue
		}
		out = append(out, KeepCandidate{Kind: "page", ID: p.ID, Title: p.Title})
	}
	for _, s := range w.Sources {
		title := s.Title
		if title == "" {
			title = s.Ref
		}
		out = append(out, KeepCandidate{Kind: "source", ID: s.ID, Title: title})
	}
	return out
}

// FoldIntoParent turns the child childID back into a heading in its parent:
// its jobs move into the parent (a job belongs to one workstream), and the
// child is archived at now. It answers the heading's name, which is the
// child's. Tasks and files fold through their own lanes, keyed by the
// heading this answers.
func FoldIntoParent(all OrgWorkstreams, childID string, now time.Time) (heading string, changed []string, err error) {
	c, ok := all[childID]
	if !ok {
		return "", nil, errors.New("not found")
	}
	if c.Parent == "" {
		return "", nil, errors.New("only a workstream inside another can fold back into it")
	}
	if c.ArchivedAt != nil {
		return "", nil, errors.New("this workstream is archived")
	}
	p, ok := all[c.Parent]
	if !ok || p.ArchivedAt != nil {
		return "", nil, errors.New("the workstream it is inside is gone or archived")
	}
	for _, j := range c.Jobs {
		if !slices.Contains(p.Jobs, j) {
			p.Jobs = append(p.Jobs, j)
		}
	}
	c.Jobs = []int{}
	at := now.UTC()
	c.ArchivedAt, c.UpdatedAt, p.UpdatedAt = &at, at, at
	all[c.ID], all[p.ID] = c, p
	return c.Name, []string{c.ID, p.ID}, nil
}
