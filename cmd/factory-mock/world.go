package main

// The world: repos, items, work orders, streams. Everything here is mock data,
// generated deterministically from a seed so a screen can be looked at twice.

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

type Kind uint8

const (
	KindIssue Kind = iota
	KindPR
	KindCI
)

func (k Kind) String() string {
	switch k {
	case KindPR:
		return "pr"
	case KindCI:
		return "ci"
	}
	return "issue"
}

type Tier uint8

const (
	TierOwner Tier = iota
	TierCollab
	TierStranger
)

func (t Tier) String() string {
	switch t {
	case TierOwner:
		return "owner"
	case TierCollab:
		return "collaborator"
	}
	return "stranger"
}

type Origin uint8

const (
	OriginGitHub Origin = iota
	OriginTerminal
	OriginChat
)

// A Step is a sentence the factory runs at a named moment. It is the whole of
// what "custom pipeline" means here: no graph, a time word and words.
type Step struct {
	When string // after plan · after write · after review · before proof
	Text string
	On   bool
}

func (st Step) Short() string {
	f := strings.Fields(st.Text)
	if len(f) > 2 {
		f = f[:2]
	}
	return strings.Join(f, " ")
}

type State uint8

const (
	StNew State = iota
	StQueued
	StRunning
	StNeedsYou
	StLanded // proof sheet waits for sign-off
	StMerged
	StDismissed
)

func (s State) String() string {
	return [...]string{"new", "queued", "running", "needs you", "landed", "merged", "dismissed"}[s]
}

type Gate uint8

const (
	GatePlan Gate = iota // comes back with the plan first
	GateShip             // runs to a PR, you sign off
	GateNone             // banked habit, self-shipping
)

func (g Gate) String() string {
	return [...]string{"plan", "ship", "none"}[g]
}

// WorkOrder is the STRUCTURED half of an item: everything the factory needs
// stated, as chips. Words edit chips (parse.go); chips never need words.
type WorkOrder struct {
	PlanModel   string
	WriteModel  string
	ReviewModel string
	Rounds      int
	Security    bool
	Cap         float64
	Gate        Gate
	Constraints []string
	Steps       []Step
}

func (o WorkOrder) Recipe() string {
	parts := []string{
		"plan " + o.PlanModel,
		"write " + o.WriteModel,
		fmt.Sprintf("review %s ×%d", o.ReviewModel, o.Rounds),
	}
	if o.Security {
		parts = append(parts, "security")
	}
	parts = append(parts, fmt.Sprintf("cap $%.0f", o.Cap), "gate "+o.Gate.String())
	return strings.Join(parts, " · ")
}

type Triage struct {
	Type      string // bug feat chore question
	Size      string // S M L
	Area      string
	Readiness int // 0..100
	Est       float64
	DupOf     int
	Risk      string // low mid high
	Read      string // the factory's one-line read
	Questions []string
}

type Repo struct {
	Name   string
	Team   string
	Areas  []string
	Habits []string // banked sentences
	Steps  []Step   // banked steps every item here carries, toggled per item
	Policy []string
	CIRed  bool
	Hue    int
}

type PhaseState uint8

const (
	PhPending PhaseState = iota
	PhRunning
	PhDone
	PhFailed
	PhWaiting // waiting on you
)

type Phase struct {
	Name  string
	State PhaseState
	Note  string
	Dur   time.Duration // sim duration this phase takes
	Left  time.Duration
}

type LogLine struct {
	At    time.Time
	Glyph string
	Tone  string // thought shell test write said ask fail ok
	Text  string
}

type Stream struct {
	Phases   []Phase
	Cur      int
	Spent    float64
	Started  time.Time
	Ended    time.Time
	Activity []int // sparkline ring, 0..7
	Log      []LogLine
	Paused   bool
	Findings int
	Bench    int
}

type Claim struct {
	Text     string
	OK       bool
	Evidence string
	Medium   string // test screenshot transcript benchmark policy
}

type Item struct {
	ID       int
	Repo     *Repo
	Num      int
	Kind     Kind
	Title    string
	Body     string
	Author   string
	Tier     Tier
	Origin   Origin
	Synced   bool
	Created  time.Time
	Changed  time.Time
	State    State
	Triage   Triage
	Order    WorkOrder
	Stream   *Stream
	Question string
	QKind    string // plan cap scope
	Proof    []Claim
	Policy   []Claim
	Marked   bool
	Labels   []string
	Checks   string // for PRs: ci state text
	Diff     string
}

func (it *Item) Ref() string {
	switch it.Kind {
	case KindCI:
		return "ci"
	}
	return fmt.Sprintf("#%d", it.Num)
}

type Shift struct {
	Since    time.Time
	Shipped  int
	Arrived  int
	Asked    int
	Handled  int
	Spent    float64
	Hours    [24]int
	Shipping []string
}

type World struct {
	rng     *rand.Rand
	Now     time.Time
	Repos   []*Repo
	Items   []*Item
	nextID  int
	nextNum map[string]int
	Benches int
	Daily   float64
	Rail    float64
	Shift   Shift
	Habits  int // sign-offs without edits, for the banking prompt
	Speed   time.Duration
	Models  []Model
}

type Model struct {
	Name  string
	Rate  float64 // $ per sim-hour of work
	Grade string  // cheap mid best
}

var models = []Model{
	{"deepseek-v4.1-flash", 0.6, "cheap"},
	{"qwen3.5-coder", 1.8, "mid"},
	{"kimi-k3", 3.2, "best"},
	{"glm-5", 1.4, "mid"},
	{"minimax-m3", 1.1, "mid"},
}

var authors = []struct {
	Name string
	Tier Tier
}{
	{"santosh", TierOwner}, {"abir", TierCollab}, {"priya", TierCollab}, {"mateo", TierCollab},
	{"wenjie", TierStranger}, {"olu", TierStranger}, {"hana", TierStranger}, {"dependabot", TierStranger},
	{"kwame", TierStranger}, {"ines", TierCollab}, {"tomasz", TierStranger}, {"renu", TierStranger},
}

type repoSeed struct {
	name, team string
	areas      []string
	habits     []string
	policy     []string
	steps      []Step
}

var repoSeeds = []repoSeed{
	{"agentfield/codeaf", "codeaf-core", []string{"tui", "standing", "plandb", "spend", "relay", "skills", "router"},
		[]string{"every PR on dev gets a review pass", "issues labelled factory are briefs", "keep main green"},
		[]string{"complexity within +10% of main", "no new dependencies without asking", "labels · milestone · attribution"},
		[]Step{{"after review", "make the code neater, same behaviour", true}, {"before proof", "screenshot when the UI moves", true}, {"after write", "update the manual page in the same change", false}}},
	{"agentfield/agentfield", "platform", []string{"api", "auth", "billing", "sdk", "docs"},
		[]string{"review every PR, comment-only"}, []string{"migrations reversible", "no secrets in logs"},
		[]Step{{"after review", "security pass on anything touching auth", true}}},
	{"santoshkumar/whisper", "", []string{"messages", "sync", "ui", "crypto"}, nil, nil, nil},
	{"agentfield/relay", "platform", []string{"kv", "dns", "worker", "auth"}, []string{"keep main green"}, []string{"no new dependencies without asking"}, nil},
	{"agentfield/furrow", "platform", []string{"snapshot", "sync", "cli"}, nil, []string{"tests pass"}, nil},
	{"agentfield/fleet", "ops", []string{"ssh", "jobs", "hosts", "cli"}, []string{"triage new issues each morning"}, nil, nil},
	{"agentfield/website", "growth", []string{"pages", "copy", "build", "seo"}, []string{"screenshot when the page moves"}, []string{"lighthouse ≥ 90"}, nil},
	{"agentfield/docs", "growth", []string{"guide", "reference", "examples"}, nil, nil, nil},
	{"agentfield/wisp", "research", []string{"browser", "dom", "net", "render"}, nil, []string{"no unsafe without a comment"}, nil},
	{"agentfield/harness", "codeaf-core", []string{"loop", "tools", "eval", "replay"}, []string{"every PR on dev gets a review pass"}, []string{"complexity within +10% of main"}, nil},
	{"agentfield/training", "research", []string{"data", "sft", "eval", "infra"}, nil, nil, nil},
	{"agentfield/skills", "codeaf-core", []string{"catalog", "install", "tests"}, nil, []string{"tests pass"}, nil},
	{"acme/billing-api", "contract", []string{"invoices", "tax", "webhooks", "ledger"}, []string{"review every PR, comment-only"}, []string{"migrations reversible"}, nil},
	{"acme/mobile", "contract", []string{"ios", "android", "sync", "push"}, nil, nil, nil},
	{"oss/httpx-go", "", []string{"client", "h2", "retry", "tls"}, nil, []string{"tests pass"}, nil},
	{"oss/tinyvec", "", []string{"index", "quant", "io"}, nil, nil, nil},
}

var titleVerbs = map[string][]string{
	"bug":      {"%s lost on %s", "%s crashes when %s", "%s shows stale %s", "%s double-fires after %s", "%s ignores %s", "%s leaks on %s", "%s misaligned at %s", "%s returns empty %s"},
	"feat":     {"add %s to %s", "let %s %s", "%s should remember %s", "expose %s in %s", "batch %s for %s", "%s needs a %s"},
	"chore":    {"bump %s in %s", "remove dead %s from %s", "rename %s across %s", "document %s for %s", "flake: %s in %s"},
	"question": {"how does %s handle %s?", "is %s supposed to %s?", "why does %s %s?"},
}

var nouns = []string{"filters", "the tree", "spend ledger", "probe", "standing order", "composer", "cursor", "cache", "retry", "session", "wall tile", "team rail", "token count", "cap", "receipt", "draft", "hug", "attachment", "paste", "scroll", "meter", "sparkline", "worktree", "merge", "label", "milestone", "webhook", "invoice", "tax row", "push token", "handshake", "snapshot", "rsync", "job id", "lighthouse", "sitemap", "DOM diff", "frame", "eval set", "checkpoint"}
var conds = []string{"compact", "restart", "resize", "a second window", "narrow widths", "a cold start", "2k rows", "an empty repo", "ctrl+c twice", "a slow network", "dark terminals", "the 16-colour profile", "a paste with newlines", "midnight rollover", "a renamed branch", "a deleted team", "ssh drop", "100 repos", "a stranger's comment", "zero budget"}

var bodies = []string{
	"Repro: %s, then %s. Expected the %s to stay put. It did not.\n\nSeen on dev as of this morning. Happy to pair.",
	"We keep hitting this in the field. %s under %s and the %s is wrong until a restart.\n\nNot blocking, but it erodes trust in the surface.",
	"Proposal: %s. Today %s forces people to %s by hand. One line in the right place would do.",
	"Small one. %s after %s. Logs attached in the gist. %s looks like the culprit.",
	"Opening this so it is written down. %s · %s · %s. No repro yet.",
}

var prBodies = []string{
	"Claims:\n- %s follow the tree on narrow widths\n- no behaviour change on wide\n- no perf impact\n\nTested by hand at 80 and 100 cols.",
	"Fixes #%d.\n\nThe %s was read before the %s existed. Now it waits. Added a test for the ordering.",
	"Refactor only. Moves %s under %s. No behaviour change intended.",
	"Adds %s. Screenshot in the PR. Also bumps %s because the old one was pinned to a dead release.",
}

func newWorld(seed int64, repos, issues, benches int, start time.Time) *World {
	w := &World{rng: rand.New(rand.NewSource(seed)), Now: start, nextNum: map[string]int{}, Benches: benches, Rail: 60, Speed: 30 * time.Second, Models: models}
	w.Shift.Since = start
	if repos > len(repoSeeds) {
		repos = len(repoSeeds)
	}
	for i := 0; i < repos; i++ {
		s := repoSeeds[i]
		r := &Repo{Name: s.name, Team: s.team, Areas: s.areas, Habits: s.habits, Policy: s.policy, Steps: s.steps, Hue: i % 8}
		w.Repos = append(w.Repos, r)
		w.nextNum[r.Name] = 20 + w.rng.Intn(1800)
	}
	w.Repos[2].CIRed = true
	// Items spread over the last 14 days, heavier on the first repos.
	for i := 0; i < issues; i++ {
		r := w.pickRepo()
		age := time.Duration(w.rng.ExpFloat64()*4*24) * time.Hour
		if age > 14*24*time.Hour {
			age = 14 * 24 * time.Hour
		}
		kind := KindIssue
		if w.rng.Float64() < 0.22 {
			kind = KindPR
		}
		it := w.genItem(r, kind, start.Add(-age))
		w.Items = append(w.Items, it)
	}
	// A few items that a chat split off: the plan store is the same floor.
	for i := 0; i < 3; i++ {
		r := w.Repos[i%2]
		it := w.genItem(r, KindIssue, start.Add(-time.Duration(1+i)*time.Hour))
		it.Origin = OriginChat
		it.Author = "santosh"
		it.Tier = TierOwner
		it.Title = []string{"split from chat: spend ledger store, the write half", "split from chat: tree rails on narrow widths", "split from chat: settings search, the index"}[i]
		it.Triage.Read = "your own chat wrote the brief; sized from it"
		it.Triage.Readiness = 90
		w.Items = append(w.Items, it)
	}
	// One red CI per red repo.
	for _, r := range w.Repos {
		if r.CIRed {
			it := w.genItem(r, KindCI, start.Add(-6*time.Hour))
			w.Items = append(w.Items, it)
		}
	}
	// Dismiss most of the old ones so the floor shows a delta, not a backlog.
	for _, it := range w.Items {
		if start.Sub(it.Created) > 3*24*time.Hour && w.rng.Float64() < 0.8 {
			it.State = StDismissed
		}
	}
	return w
}

func (w *World) pickRepo() *Repo {
	// Zipf-ish: first repos get most of the traffic.
	n := len(w.Repos)
	x := w.rng.ExpFloat64() * float64(n) / 3
	i := int(x)
	if i >= n {
		i = n - 1
	}
	return w.Repos[i]
}

func (w *World) genItem(r *Repo, kind Kind, created time.Time) *Item {
	w.nextID++
	w.nextNum[r.Name]++
	a := authors[w.rng.Intn(len(authors))]
	if r.Team == "" && a.Tier == TierOwner {
		a = authors[1+w.rng.Intn(len(authors)-1)]
	}
	area := r.Areas[w.rng.Intn(len(r.Areas))]
	it := &Item{ID: w.nextID, Repo: r, Num: w.nextNum[r.Name], Kind: kind, Author: a.Name, Tier: a.Tier, Created: created, Changed: created}
	n1, n2 := nouns[w.rng.Intn(len(nouns))], conds[w.rng.Intn(len(conds))]
	switch kind {
	case KindIssue:
		typ := pickWeighted(w.rng, []string{"bug", "feat", "chore", "question"}, []float64{0.5, 0.28, 0.14, 0.08})
		tmpl := titleVerbs[typ][w.rng.Intn(len(titleVerbs[typ]))]
		it.Title = fmt.Sprintf(tmpl, n1, n2)
		it.Body = fmt.Sprintf(bodies[w.rng.Intn(len(bodies))], n1, n2, nouns[w.rng.Intn(len(nouns))])
		it.Triage = w.triage(typ, area)
		if w.rng.Float64() < 0.3 {
			it.Labels = append(it.Labels, typ)
		}
		if w.rng.Float64() < 0.12 {
			it.Labels = append(it.Labels, "factory")
		}
	case KindPR:
		it.Title = fmt.Sprintf("%s(%s): %s", pickWeighted(w.rng, []string{"fix", "feat", "refactor", "docs", "chore"}, []float64{0.45, 0.25, 0.15, 0.1, 0.05}), area, fmt.Sprintf(titleVerbs["feat"][w.rng.Intn(len(titleVerbs["feat"]))], n1, n2))
		it.Body = fmt.Sprintf(prBodies[w.rng.Intn(len(prBodies))], n1, n2)
		if strings.Contains(it.Body, "%!d") {
			it.Body = fmt.Sprintf(prBodies[1], w.nextNum[r.Name]-w.rng.Intn(40)-1, n1, n2)
		}
		it.Triage = w.triage("review", area)
		it.Diff = fmt.Sprintf("+%d −%d · %d files", 40+w.rng.Intn(400), 5+w.rng.Intn(120), 1+w.rng.Intn(9))
		it.Checks = pickWeighted(w.rng, []string{"ci ✓", "ci ✓", "ci running", "ci ✕"}, []float64{0.5, 0.2, 0.15, 0.15})
	case KindCI:
		it.Title = fmt.Sprintf("main is red · %s · %s", area, []string{"TestCompactKeepsFilters", "TestRelayHandshake", "lint: unused import", "race: probe fire"}[w.rng.Intn(4)])
		it.Body = "The last three runs on main failed in the same test. Started after the merge at 02:14."
		it.Author = "ci"
		it.Tier = TierOwner
		it.Triage = Triage{Type: "ci", Size: "S", Area: area, Readiness: 95, Est: 1.5, Risk: "mid", Read: "same test, three runs in a row — a real regression, not a flake"}
	}
	it.Order = w.defaultOrder(it)
	return it
}

func (w *World) triage(typ, area string) Triage {
	t := Triage{Type: typ, Area: area}
	t.Size = pickWeighted(w.rng, []string{"S", "M", "L"}, []float64{0.45, 0.38, 0.17})
	t.Readiness = 35 + w.rng.Intn(65)
	if typ == "question" {
		t.Readiness = 20 + w.rng.Intn(30)
	}
	switch t.Size {
	case "S":
		t.Est = 0.8 + w.rng.Float64()*2
	case "M":
		t.Est = 2.5 + w.rng.Float64()*4
	default:
		t.Est = 6 + w.rng.Float64()*10
	}
	t.Risk = pickWeighted(w.rng, []string{"low", "mid", "high"}, []float64{0.55, 0.33, 0.12})
	if w.rng.Float64() < 0.08 {
		t.DupOf = 100 + w.rng.Intn(1500)
	}
	reads := map[string][]string{
		"bug":      {"the %s is read before the %s exists; ordering, not state", "two writers, one row; a lock would hide it, a queue would fix it", "a width law broken at the seam; the fix is one measure", "probably the cache; a repro should take ten minutes", "stale read after %s — looks like a missing invalidate"},
		"feat":     {"small surface, one new verb; fits the existing grammar", "touches three packages; wants a plan first", "mostly plumbing; the hard part is the name", "needs a decision on where it lives before any code"},
		"chore":    {"mechanical; safe to run unattended", "a rename across %d files; one PR, no behaviour change"},
		"question": {"underspecified; two questions for the author first", "answerable from the docs; a reply, not a PR"},
		"review":   {"claims are testable; three checks cover them", "no claims in the body; the review has to infer what it fixes", "big diff for the title; worth asking what else moved"},
	}
	rs := reads[typ]
	r := rs[w.rng.Intn(len(rs))]
	r = strings.ReplaceAll(r, "%d", fmt.Sprint(3+w.rng.Intn(40)))
	if strings.Contains(r, "%s") {
		r = fmt.Sprintf(r, area, nouns[w.rng.Intn(len(nouns))])
	}
	t.Read = r
	if t.Readiness < 55 {
		t.Questions = []string{"which terminal and width were you on?", "does it happen on a fresh profile too?"}
	}
	return t
}

func (w *World) defaultOrder(it *Item) WorkOrder {
	o := WorkOrder{PlanModel: "kimi-k3", WriteModel: "qwen3.5-coder", ReviewModel: "deepseek-v4.1-flash", Rounds: 1, Cap: 5, Gate: GateShip}
	switch it.Triage.Size {
	case "L":
		o.Gate = GatePlan
		o.Cap = 15
		o.Rounds = 2
		o.WriteModel = "kimi-k3"
	case "M":
		o.Cap = 8
	}
	if it.Kind == KindPR {
		o.Gate = GateShip
		o.Cap = 3
	}
	if it.Triage.Risk == "high" || it.Triage.Area == "auth" || it.Triage.Area == "crypto" || it.Triage.Area == "billing" {
		o.Security = true
		o.Rounds = max(o.Rounds, 2)
	}
	for _, h := range it.Repo.Habits {
		if strings.Contains(h, "comment-only") && it.Kind == KindPR {
			o.Constraints = append(o.Constraints, "comment-only")
		}
	}
	if it.Triage.Type == "chore" && it.Triage.Size == "S" {
		o.Gate = GateNone
	}
	o.Steps = append([]Step{}, it.Repo.Steps...)
	return o
}

func pickWeighted(r *rand.Rand, xs []string, ws []float64) string {
	t := 0.0
	for _, x := range ws {
		t += x
	}
	f := r.Float64() * t
	for i, x := range ws {
		f -= x
		if f <= 0 {
			return xs[i]
		}
	}
	return xs[len(xs)-1]
}

func (w *World) modelRate(name string) float64 {
	for _, m := range w.Models {
		if m.Name == name {
			return m.Rate
		}
	}
	return 1
}

// NewFromWords creates a terminal-origin item from a sentence typed into the
// composer. The sentence is the title; chips come out of parse.go.
func (w *World) NewFromWords(r *Repo, words string) *Item {
	w.nextID++
	w.nextNum[r.Name]++
	title, order := parseOrder(words, w.defaultOrder(&Item{Repo: r, Triage: Triage{Size: "M"}}))
	it := &Item{ID: w.nextID, Repo: r, Num: w.nextNum[r.Name], Kind: KindIssue, Title: title, Body: "Written in the terminal. Not on GitHub yet.", Author: "santosh", Tier: TierOwner, Origin: OriginTerminal, Created: w.Now, Changed: w.Now}
	it.Triage = Triage{Type: "feat", Size: "M", Area: r.Areas[0], Readiness: 70, Est: 4, Risk: "low", Read: "your own words; no triage needed beyond sizing"}
	if strings.Contains(strings.ToLower(title), "fix") || strings.Contains(strings.ToLower(title), "bug") {
		it.Triage.Type = "bug"
	}
	it.Order = order
	w.Items = append(w.Items, it)
	return it
}
