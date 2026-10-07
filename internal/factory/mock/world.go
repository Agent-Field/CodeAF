// Package mock is a moving factory floor behind a [factory.Seam]: a generated
// fleet of repos, a stream of arriving work, benches that run it, questions
// that surface, proof sheets that land, and a clock that only moves when the
// surface says Tick or Sleep. Nothing here talks to a forge, a model or
// ~/.codeaf; it exists so the surface can be felt before the engine is wired.
//
// It is the port of cmd/factory-mock onto the factory vocabulary. THE WORLD IS
// DETERMINISTIC FROM ITS SEED: the same seed and the same doors in the same
// order draw the same floor, which is what a test and a second look both want.
//
// THE WHOLE MOCK IS REMOVABLE IN ONE DELETE. REMOVING.md beside this file
// names the paths; the shipped binary carries none of it unless built with
// the factorymock tag.
package mock

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// World is the floor behind the seam. Every door runs off the surface's loop
// and possibly beside another door, so ONE MUTEX GUARDS ALL OF IT and every
// exported door takes it exactly once.
type World struct {
	mu      sync.Mutex
	rng     *rand.Rand
	now     time.Time
	repos   []*factory.Repo
	items   []*factory.Item
	runs    map[int]*run
	nextID  int
	nextNum map[string]int
	benches int
	daily   float64
	rail    float64
	shift   factory.Shift
	habits  int // clean sign-offs in a row, for the banking offer
	speed   time.Duration
	acc     time.Duration // a Tick shorter than a minute is carried, not lost
}

// run is the simulation's private bookkeeping for one stream: which of the
// item's stages each phase came from (-1 for a phase a person added after
// landing), how long each phase takes, and which one-off questions it has
// already asked.
type run struct {
	stage   []int
	dur     []time.Duration
	planned bool   // the plan gate has asked
	scoped  bool   // the rare scope question has been asked
	pending string // what the open question was about: rounds · scope
}

var authors = []struct {
	Name string
	Tier factory.Tier
}{
	{"santosh", factory.TierOwner}, {"abir", factory.TierCollab}, {"priya", factory.TierCollab}, {"mateo", factory.TierCollab},
	{"wenjie", factory.TierStranger}, {"olu", factory.TierStranger}, {"hana", factory.TierStranger}, {"dependabot", factory.TierStranger},
	{"kwame", factory.TierStranger}, {"ines", factory.TierCollab}, {"tomasz", factory.TierStranger}, {"renu", factory.TierStranger},
}

// repoSeed is one generated repository. Its steps are sentences with a time
// word, banked onto the issue recipe exactly as AddStage would bank them.
type repoSeed struct {
	name, team string
	areas      []string
	habits     []string
	policy     []string
	steps      []seedStep
}

type seedStep struct {
	words string
	on    bool
}

var repoSeeds = []repoSeed{
	{"agentfield/codeaf", "codeaf-core", []string{"tui", "standing", "plandb", "spend", "relay", "skills", "router"},
		[]string{"every PR on dev gets a review pass", "issues labelled factory are briefs", "keep main green"},
		[]string{"complexity within +10% of main", "no new dependencies without asking", "labels · milestone · attribution"},
		[]seedStep{{"after review, make the code neater, same behaviour", true}, {"before proof, screenshot when the UI moves", true}, {"after write, update the manual page in the same change", false}}},
	{"agentfield/agentfield", "platform", []string{"api", "auth", "billing", "sdk", "docs"},
		[]string{"review every PR, comment-only"}, []string{"migrations reversible", "no secrets in logs"},
		[]seedStep{{"after review, security pass on anything touching auth", true}}},
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

// issueRecipe is the recipe every generated repo starts from. Security is
// banked OFF and switched on per item, so a risky item gains the stage
// without the recipe changing.
func issueRecipe() []factory.Stage {
	return []factory.Stage{
		{Name: "plan", Ask: "read the issue and say how", Until: "done", On: true},
		{Name: "write", Ask: "do it in a worktree", Fanout: "per-file", Until: "done", On: true},
		{Name: "test", Ask: "run what the change implies", Until: "green", Max: 2, On: true},
		{Name: "review", Ask: "read it as a stranger would", Fanout: "per-finding", Until: "clean", Max: 1, On: true},
		{Name: "security", Ask: "secrets, injection and authz", Until: "clean", On: false},
		{Name: "proof", Ask: "show each claim in its own medium", Until: "proven", Gate: factory.GateShip, On: true},
	}
}

// prStages and ciStages are the fixed shapes for the two kinds a repo's issue
// recipe does not describe: a pull request is read and checked, a red main is
// bisected and fixed.
func prStages() []factory.Stage {
	return []factory.Stage{
		{Name: "read", Ask: "the diff and its claims", Until: "done", On: true},
		{Name: "checks", Ask: "run what the claims imply", Fanout: "per-claim", Until: "done", On: true},
		{Name: "review", Ask: "findings as a comment", Fanout: "per-finding", Until: "clean", Max: 1, On: true},
		{Name: "security", Ask: "secrets, injection and authz", Until: "clean", On: false},
		{Name: "proof", Ask: "the sheet", Until: "proven", Gate: factory.GateShip, On: true},
	}
}

func ciStages() []factory.Stage {
	return []factory.Stage{
		{Name: "bisect", Ask: "the three red runs", Until: "done", On: true},
		{Name: "fix", Ask: "the smallest change that turns them green", Until: "done", On: true},
		{Name: "test", Ask: "the red test and its neighbours", Until: "green", Max: 2, On: true},
		{Name: "proof", Ask: "the sheet", Until: "proven", Gate: factory.GateShip, On: true},
	}
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

// NewWorld generates a floor: repos (at most sixteen), issues spread over the
// last fourteen days, three items a chat split off, one red CI per red repo,
// and most of the old ones dismissed so the floor shows a delta.
func NewWorld(seed int64, repos, issues, benches int, start time.Time) *World {
	w := &World{rng: rand.New(rand.NewSource(seed)), now: start, runs: map[int]*run{}, nextNum: map[string]int{}, benches: benches, rail: 60, speed: 30 * time.Second}
	w.shift.Since = start
	if repos > len(repoSeeds) {
		repos = len(repoSeeds)
	}
	if repos < 3 {
		repos = 3
	}
	for i := 0; i < repos; i++ {
		s := repoSeeds[i]
		r := &factory.Repo{Name: s.name, Team: s.team, Areas: s.areas, Habits: append([]string(nil), s.habits...), Hue: i % 8}
		r.Recipe.Stages = issueRecipe()
		r.Recipe.Policy = append([]string(nil), s.policy...)
		for _, st := range s.steps {
			r.Recipe.Stages = addStage(r.Recipe.Stages, st.words)
			r.Recipe.Stages[stageAt(r.Recipe.Stages, st.words)].On = st.on
		}
		w.repos = append(w.repos, r)
		w.nextNum[r.Name] = 20 + w.rng.Intn(1800)
	}
	w.repos[2].CIRed = true
	for i := 0; i < issues; i++ {
		r := w.pickRepo()
		age := time.Duration(w.rng.ExpFloat64()*4*24) * time.Hour
		if age > 14*24*time.Hour {
			age = 14 * 24 * time.Hour
		}
		kind := factory.KindIssue
		if w.rng.Float64() < 0.22 {
			kind = factory.KindPR
		}
		w.items = append(w.items, w.genItem(r, kind, start.Add(-age)))
	}
	// A few items a chat split off: the plan store is the same floor.
	for i := 0; i < 3; i++ {
		r := w.repos[i%2]
		it := w.genItem(r, factory.KindIssue, start.Add(-time.Duration(1+i)*time.Hour))
		it.Origin = factory.OriginChat
		it.Author = "santosh"
		it.Tier = factory.TierOwner
		it.Title = []string{"split from chat: spend ledger store, the write half", "split from chat: tree rails on narrow widths", "split from chat: settings search, the index"}[i]
		it.Triage.Read = "your own chat wrote the brief; sized from it"
		it.Triage.Readiness = 90
		it.Triage.Questions = nil
		w.items = append(w.items, it)
	}
	for _, r := range w.repos {
		if r.CIRed {
			w.items = append(w.items, w.genItem(r, factory.KindCI, start.Add(-6*time.Hour)))
		}
	}
	for _, it := range w.items {
		if start.Sub(it.Created) > 3*24*time.Hour && w.rng.Float64() < 0.8 {
			it.State = factory.StateDismissed
		}
	}
	return w
}

// stageAt finds where addStage put the stage these words describe.
func stageAt(stages []factory.Stage, words string) int {
	st := parseStage(words)
	for i, x := range stages {
		if x.Name == st.Name && x.Ask == st.Ask {
			return i
		}
	}
	return len(stages) - 1
}

func (w *World) pickRepo() *factory.Repo {
	// Zipf-ish: the first repos get most of the traffic.
	n := len(w.repos)
	i := int(w.rng.ExpFloat64() * float64(n) / 3)
	if i >= n {
		i = n - 1
	}
	return w.repos[i]
}

func (w *World) repo(name string) *factory.Repo {
	for _, r := range w.repos {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func (w *World) item(id int) *factory.Item {
	for _, it := range w.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (w *World) genItem(r *factory.Repo, kind factory.Kind, created time.Time) *factory.Item {
	w.nextID++
	w.nextNum[r.Name]++
	a := authors[w.rng.Intn(len(authors))]
	if r.Team == "" && a.Tier == factory.TierOwner {
		a = authors[1+w.rng.Intn(len(authors)-1)]
	}
	area := r.Areas[w.rng.Intn(len(r.Areas))]
	it := &factory.Item{ID: w.nextID, Repo: r.Name, Num: w.nextNum[r.Name], Kind: kind, Author: a.Name, Tier: a.Tier, Origin: factory.OriginForge, Synced: true, Created: created, Changed: created, State: factory.StateNew}
	n1, n2 := nouns[w.rng.Intn(len(nouns))], conds[w.rng.Intn(len(conds))]
	switch kind {
	case factory.KindIssue:
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
	case factory.KindPR:
		it.Title = fmt.Sprintf("%s(%s): %s", pickWeighted(w.rng, []string{"fix", "feat", "refactor", "docs", "chore"}, []float64{0.45, 0.25, 0.15, 0.1, 0.05}), area, fmt.Sprintf(titleVerbs["feat"][w.rng.Intn(len(titleVerbs["feat"]))], n1, n2))
		it.Body = fmt.Sprintf(prBodies[w.rng.Intn(len(prBodies))], n1, n2)
		if strings.Contains(it.Body, "%!d") {
			it.Body = fmt.Sprintf(prBodies[1], w.nextNum[r.Name]-w.rng.Intn(40)-1, n1, n2)
		}
		it.Triage = w.triage("review", area)
		it.Diff = fmt.Sprintf("+%d −%d · %d files", 40+w.rng.Intn(400), 5+w.rng.Intn(120), 1+w.rng.Intn(9))
		it.Checks = pickWeighted(w.rng, []string{"ci ✓", "ci ✓", "ci running", "ci ✕"}, []float64{0.5, 0.2, 0.15, 0.15})
	case factory.KindCI:
		it.Title = fmt.Sprintf("main is red · %s · %s", area, []string{"TestCompactKeepsFilters", "TestRelayHandshake", "lint: unused import", "race: probe fire"}[w.rng.Intn(4)])
		it.Body = "The last three runs on main failed in the same test. Started after the merge at 02:14."
		it.Author = "ci"
		it.Tier = factory.TierOwner
		it.Triage = factory.Triage{Type: "ci", Size: "S", Area: area, Readiness: 95, Est: 1.5, Risk: "mid", Read: "same test, three runs in a row; a real regression, not a flake"}
	}
	w.defaultOrder(it, r)
	return it
}

func (w *World) triage(typ, area string) factory.Triage {
	t := factory.Triage{Type: typ, Area: area}
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
		"bug":      {"the %s is read before the %s exists; ordering, not state", "two writers, one row; a lock would hide it, a queue would fix it", "a width law broken at the seam; the fix is one measure", "probably the cache; a repro should take ten minutes", "stale read after %s; looks like a missing invalidate"},
		"feat":     {"small surface, one new verb; fits the existing grammar", "touches three packages; wants a plan first", "mostly plumbing; the hard part is the name", "needs a decision on where it lives before any code"},
		"chore":    {"mechanical; safe to run unattended", "a rename across %d files; one PR, no behaviour change"},
		"question": {"underspecified; two questions for the author first", "answerable from the docs; a reply, not a PR"},
		"review":   {"claims are testable; three checks cover them", "no claims in the body; the review has to infer what it fixes", "big diff for the title; worth asking what else moved"},
	}
	rs := reads[typ]
	r := rs[w.rng.Intn(len(rs))]
	r = strings.ReplaceAll(r, "%d", fmt.Sprint(3+w.rng.Intn(40)))
	switch strings.Count(r, "%s") {
	case 1:
		r = fmt.Sprintf(r, area)
	case 2:
		r = fmt.Sprintf(r, area, nouns[w.rng.Intn(len(nouns))])
	}
	t.Read = r
	if t.Readiness < 55 {
		t.Questions = []string{"which terminal and width were you on?", "does it happen on a fresh profile too?"}
	}
	return t
}

// defaultOrder writes the item's work order: its own copy of the stages for
// its kind, its cap and its gate. There is NO MODEL ON A STAGE; what used to
// be "write with the best model" is the write stage's strong effort.
func (w *World) defaultOrder(it *factory.Item, r *factory.Repo) {
	switch it.Kind {
	case factory.KindPR:
		it.Stages = prStages()
	case factory.KindCI:
		it.Stages = ciStages()
	default:
		it.Stages = copyStages(r.Recipe.Stages)
	}
	it.Cap, it.Gate = 5, factory.GateShip
	rounds, security := 1, false
	switch it.Triage.Size {
	case "L":
		it.Gate, it.Cap, rounds = factory.GatePlan, 15, 2
		if i := stageIndex(it.Stages, "write"); i >= 0 {
			it.Stages[i].Effort = "strong"
		}
	case "M":
		it.Cap = 8
	}
	if it.Kind == factory.KindPR {
		it.Gate, it.Cap = factory.GateShip, 3
	}
	switch {
	case it.Triage.Risk == "high", it.Triage.Area == "auth", it.Triage.Area == "crypto", it.Triage.Area == "billing":
		security = true
		rounds = max(rounds, 2)
	}
	for _, h := range r.Habits {
		if strings.Contains(h, "comment-only") && it.Kind == factory.KindPR {
			if i := stageIndex(it.Stages, "proof"); i >= 0 {
				it.Stages[i].Proof = append(it.Stages[i].Proof, "comment-only")
			}
		}
	}
	if it.Triage.Type == "chore" && it.Triage.Size == "S" {
		it.Gate = factory.GateNone
	}
	if i := stageIndex(it.Stages, "review"); i >= 0 {
		it.Stages[i].Max = rounds
	}
	if i := stageIndex(it.Stages, "security"); i >= 0 && security {
		it.Stages[i].On = true
	}
}

// stageIndex is the first stage with that name, or -1.
func stageIndex(stages []factory.Stage, name string) int {
	for i, s := range stages {
		if s.Name == name {
			return i
		}
	}
	return -1
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

// newFromWords makes a terminal-origin item from a sentence typed into the
// composer. What parse.go lifts becomes chips; what is left is the title.
func (w *World) newFromWords(r *factory.Repo, words string) (*factory.Item, error) {
	c := lift(words)
	if c.rest == "" {
		return nil, fmt.Errorf("say what the work is, not only how")
	}
	w.nextID++
	w.nextNum[r.Name]++
	it := &factory.Item{ID: w.nextID, Repo: r.Name, Num: w.nextNum[r.Name], Kind: factory.KindIssue, Title: c.rest, Body: "Written in the terminal. Not on github yet.", Author: "santosh", Tier: factory.TierOwner, Origin: factory.OriginTerminal, Created: w.now, Changed: w.now, State: factory.StateNew}
	it.Triage = factory.Triage{Type: "feat", Size: "M", Area: r.Areas[0], Readiness: 70, Est: 4, Risk: "low", Read: "your own words; no triage needed beyond sizing"}
	low := strings.ToLower(c.rest)
	if strings.Contains(low, "fix") || strings.Contains(low, "bug") {
		it.Triage.Type = "bug"
	}
	w.defaultOrder(it, r)
	c.apply(it, stageIndex(it.Stages, "write"))
	w.items = append(w.items, it)
	return it, nil
}
