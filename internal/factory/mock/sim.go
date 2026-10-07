package mock

// The simulation. A factory is CONTINUOUS: work arrives while nobody looks,
// streams advance through their stages, money accrues, questions surface,
// things land. Nothing moves unless Tick or Sleep is called, so a test drives
// the clock and never sleeps.

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	glyphThought = "✳"
	glyphShell   = "$"
	glyphTest    = "◎"
	glyphWrite   = "✎"
	glyphSaid    = "»"
	glyphAsk     = "?"
	glyphFail    = "✕"
	glyphOK      = "✓"
)

// effortRate is dollars per sim-hour of one task at each effort. Effort is the
// crew's one word, so it is the one thing that moves the meter per stage.
var effortRate = map[string]float64{"cheap": 0.6, "": 1.4, "strong": 3.2}

// baseMinutes is how long a stage takes for a small item before jitter.
var baseMinutes = map[string]int{"plan": 4, "write": 9, "test": 4, "review": 4, "security": 4, "read": 3, "checks": 4, "bisect": 4, "fix": 5}

// step advances the world by one sim-minute.
func (w *World) step() {
	w.now = w.now.Add(time.Minute)
	w.arrive()
	benches := 0
	for _, it := range w.items {
		if it.State == factory.StateRunning && it.Stream != nil && !it.Stream.Paused {
			benches++
		}
	}
	// Queued items take a bench when one frees.
	for _, it := range w.items {
		if benches >= w.benches {
			break
		}
		if it.State == factory.StateQueued {
			w.start(it)
			benches++
		}
	}
	for _, it := range w.items {
		if it.State == factory.StateRunning && it.Stream != nil && !it.Stream.Paused {
			w.advance(it)
		}
		if (it.State == factory.StateRunning || it.State == factory.StateNeedsYou) && it.Stream != nil {
			w.pulse(it.Stream, it.State == factory.StateRunning && !it.Stream.Paused)
		}
	}
}

func (w *World) arrive() {
	// About one arrival every 25 sim-minutes across the fleet, slower at night.
	h := w.now.Hour()
	rate := 1.0 / 25
	if h < 7 || h > 22 {
		rate /= 4
	}
	if w.rng.Float64() < rate {
		r := w.pickRepo()
		kind := factory.KindIssue
		if w.rng.Float64() < 0.3 {
			kind = factory.KindPR
		}
		it := w.genItem(r, kind, w.now)
		w.items = append(w.items, it)
		w.shift.Arrived++
		// A banked habit may pick it up on its own.
		for _, hb := range r.Habits {
			if kind == factory.KindPR && strings.Contains(hb, "review pass") && it.Tier != factory.TierStranger {
				it.Gate = factory.GateShip
				w.launch(it)
			}
			if kind == factory.KindIssue && strings.Contains(hb, "labelled factory") && hasLabel(it, "factory") && it.Tier != factory.TierStranger {
				w.launch(it)
			}
		}
	}
	// Occasionally CI goes red somewhere.
	if w.rng.Float64() < 1.0/600 {
		r := w.pickRepo()
		if !r.CIRed {
			r.CIRed = true
			it := w.genItem(r, factory.KindCI, w.now)
			w.items = append(w.items, it)
			w.shift.Arrived++
			for _, hb := range r.Habits {
				if strings.Contains(hb, "keep main green") {
					w.launch(it)
				}
			}
		}
	}
}

func hasLabel(it *factory.Item, l string) bool {
	for _, x := range it.Labels {
		if x == l {
			return true
		}
	}
	return false
}

// launch puts an item on a bench if one is free, the queue if not.
func (w *World) launch(it *factory.Item) {
	if it.State == factory.StateRunning || it.State == factory.StateQueued {
		return
	}
	running := 0
	for _, x := range w.items {
		if x.State == factory.StateRunning {
			running++
		}
	}
	it.Changed = w.now
	if running >= w.benches {
		it.State = factory.StateQueued
		return
	}
	w.start(it)
}

// start compiles the item's stages into phases, in order, one phase per stage
// that is on. A RUN IS ITS STAGES AT LAUNCH: switching a stage afterwards
// changes the next run, never this one; effort, rounds and cap are read live.
func (w *World) start(it *factory.Item) {
	it.State = factory.StateRunning
	it.Changed = w.now
	s := &factory.Stream{Started: w.now, Activity: make([]int, 12)}
	r := &run{}
	sz := map[string]int{"S": 1, "M": 2, "L": 4}[it.Triage.Size]
	if sz == 0 {
		sz = 2
	}
	jitter := func(base int) time.Duration {
		return time.Duration(base*sz+w.rng.Intn(base*sz/2+1)) * time.Minute
	}
	for i, st := range it.Stages {
		if !st.On {
			continue
		}
		d := 2 * time.Minute
		if st.Name != "proof" {
			base := baseMinutes[st.Name]
			if base == 0 {
				base = 3
			}
			d = jitter(base)
		}
		s.Phases = append(s.Phases, factory.Phase{Name: st.Name, State: factory.PhasePending, Note: st.Ask, Left: d})
		r.stage = append(r.stage, i)
		r.dur = append(r.dur, d)
	}
	if len(s.Phases) == 0 {
		// Every stage switched off still leaves a sheet to sign.
		s.Phases = append(s.Phases, factory.Phase{Name: "proof", State: factory.PhasePending, Left: 2 * time.Minute})
		r.stage = append(r.stage, -1)
		r.dur = append(r.dur, 2*time.Minute)
	}
	used := map[int]bool{}
	for _, x := range w.items {
		if x.Stream != nil && x.State == factory.StateRunning && x != it {
			used[x.Stream.Bench] = true
		}
	}
	bench := 1
	for used[bench] {
		bench++
	}
	s.Bench = bench
	it.Stream = s
	it.Question, it.QKind = "", ""
	it.Proof, it.Policy = nil, nil
	w.runs[it.ID] = r
	w.enter(it, 0)
	w.say(it, glyphThought, "thought", "reading "+it.Ref()+" · "+it.Triage.Read)
}

// stageOf is the item's stage behind phase i, or nil for an added phase.
func (w *World) stageOf(it *factory.Item, i int) *factory.Stage {
	r := w.runs[it.ID]
	if r == nil || i < 0 || i >= len(r.stage) || r.stage[i] < 0 || r.stage[i] >= len(it.Stages) {
		return nil
	}
	return &it.Stages[r.stage[i]]
}

// enter makes phase i the running one: round one, and as many tasks as its
// fanout splits into.
func (w *World) enter(it *factory.Item, i int) {
	s := it.Stream
	s.Cur = i
	ph := &s.Phases[i]
	ph.State = factory.PhaseRunning
	ph.Round = 1
	ph.Tasks = 1
	if st := w.stageOf(it, i); st != nil {
		switch st.Fanout {
		case "per-file":
			ph.Tasks = 2 + w.rng.Intn(3)
		case "per-finding":
			ph.Tasks = max(1, s.Findings)
		case "per-claim":
			ph.Tasks = max(1, strings.Count(it.Body, "\n- "))
		}
	}
}

func (w *World) say(it *factory.Item, glyph, tone, text string) {
	if it.Stream == nil {
		return
	}
	it.Stream.Log = append(it.Stream.Log, factory.LogLine{At: w.now, Glyph: glyph, Tone: tone, Text: text})
	if len(it.Stream.Log) > 400 {
		it.Stream.Log = it.Stream.Log[len(it.Stream.Log)-400:]
	}
}

// ask parks the item on a question for a person.
func (w *World) ask(it *factory.Item, kind, q string) {
	it.State = factory.StateNeedsYou
	it.QKind = kind
	it.Question = q
	it.Stream.Phases[it.Stream.Cur].State = factory.PhaseWaiting
	w.say(it, glyphAsk, "ask", q)
	w.shift.Asked++
	it.Changed = w.now
}

func (w *World) advance(it *factory.Item) {
	s := it.Stream
	if s.Cur >= len(s.Phases) {
		return
	}
	ph := &s.Phases[s.Cur]
	st := w.stageOf(it, s.Cur)
	r := w.runs[it.ID]
	// Money: the stage's effort at its rate, per minute, per task.
	effort := ""
	if st != nil {
		effort = st.Effort
	}
	rate := effortRate[effort]
	if effort == "" && (ph.Name == "test" || ph.Name == "proof") {
		rate = effortRate["cheap"]
	}
	cost := rate / 60 * (1 + 0.25*float64(max(ph.Tasks, 1)-1))
	s.Spent += cost
	w.daily += cost
	w.shift.Spent += cost
	if it.Cap > 0 && s.Spent >= it.Cap {
		w.ask(it, "cap", fmt.Sprintf("hit the %s cap in %s · raise to %s, or stop?", usd(it.Cap), ph.Name, usd(it.Cap*2)))
		return
	}
	ph.Left -= time.Minute
	// Chatter, so the stream view has grain.
	if w.rng.Float64() < 0.35 {
		w.chatter(it, ph)
	}
	// A scope question, rarely, in plan or write, once a run.
	if (ph.Name == "plan" || ph.Name == "write") && !r.scoped && w.rng.Float64() < 1.0/90 {
		r.scoped = true
		r.pending = "scope"
		repo := w.repo(it.Repo)
		other := it.Triage.Area
		if repo != nil {
			other = repo.Areas[w.rng.Intn(len(repo.Areas))]
		}
		w.ask(it, "scope", []string{"the fix wants to touch " + it.Triage.Area + " and " + other + " · both, or just the first?", "two ways in: a lock or a queue · the queue is bigger but honest · which?", "the test that covers this is skipped on dev · fix the test too?"}[w.rng.Intn(3)])
		return
	}
	if ph.Left > 0 {
		return
	}
	// The round is over. Clean rounds finish the stage; dirty ones loop while
	// the stage has rounds left, and then stop and ask.
	dirty, retry, note := false, time.Duration(0), ""
	switch {
	case ph.Name == "plan":
		w.say(it, glyphSaid, "said", "plan: "+w.planSummary())
		if it.Gate == factory.GatePlan && !r.planned {
			r.planned = true
			w.ask(it, "plan", "plan is ready · go, or change it?")
			return
		}
	case ph.Name == "test":
		if w.rng.Float64() < 0.3 {
			dirty, retry = true, time.Duration(2+w.rng.Intn(4))*time.Minute
			area := it.Triage.Area
			if area == "" {
				area = "it"
			}
			note = fmt.Sprintf("%d/%d · Test%s failed", 7+w.rng.Intn(5), 12, strings.ToUpper(area[:1])+area[1:])
		} else {
			w.say(it, glyphOK, "ok", "tests 12/12")
		}
	case ph.Name == "review":
		n := w.rng.Intn(4)
		if n > 0 {
			s.Findings += n
			dirty, retry = true, time.Duration(n*2)*time.Minute
			note = fmt.Sprintf("%d findings", n)
			if st != nil && st.Fanout == "per-finding" {
				ph.Tasks = n
			}
		} else {
			w.say(it, glyphOK, "ok", fmt.Sprintf("review %d: clean", ph.Round))
		}
	case ph.Name == "security":
		if w.rng.Float64() < 0.15 {
			dirty, retry, note = true, 3*time.Minute, "a token lands in a log line"
		} else {
			w.say(it, glyphOK, "ok", "security: nothing found")
		}
	}
	if dirty {
		ph.Note = note
		rounds := 1
		if st != nil {
			rounds = max(1, st.Max)
		}
		if ph.Round < rounds {
			w.say(it, glyphFail, "fail", fmt.Sprintf("%s %d/%d: %s · fixing", ph.Name, ph.Round, rounds, note))
			ph.Round++
			ph.Left = retry
			return
		}
		r.pending = "rounds"
		w.ask(it, "scope", fmt.Sprintf("%s is not %s after %d round(s): %s · one more round, or go on as is?", ph.Name, until(st), ph.Round, note))
		return
	}
	w.finish(it)
}

func until(st *factory.Stage) string {
	if st == nil || st.Until == "" {
		return "done"
	}
	return st.Until
}

// finish marks the current phase done and enters the next, or lands.
func (w *World) finish(it *factory.Item) {
	s := it.Stream
	s.Phases[s.Cur].State = factory.PhaseDone
	s.Phases[s.Cur].Left = 0
	w.shift.Hours[w.now.Hour()]++
	if s.Cur+1 >= len(s.Phases) {
		w.land(it)
		return
	}
	w.enter(it, s.Cur+1)
}

func (w *World) planSummary() string {
	return []string{
		"one measure at the seam, a test that pins the width, no new state",
		"queue the writes behind one owner; three files; the migration is reversible",
		"a new verb on the existing grammar; manual page in the same change",
		"split: parser first, then the renderer; the second half can run beside the first",
	}[w.rng.Intn(4)]
}

func (w *World) chatter(it *factory.Item, ph *factory.Phase) {
	a := it.Triage.Area
	lines := map[string][]string{
		"plan":     {"reading internal/" + a, "the test that should have caught this is skipped", "two callers; one of them is the bug", "checking what dev does today"},
		"read":     {"reading the diff · " + it.Diff, "the body claims three things", "one claim has no test near it"},
		"write":    {"editing internal/" + a + "/" + a + ".go", "adding a test for the ordering", "the rename touches 4 files", "go build ./... ok"},
		"test":     {"go test ./internal/" + a + "/", "race detector on", "one flake, re-running once"},
		"checks":   {"running the test the claim implies", "screenshot at 80 and 100 cols", "benchmark: main vs this PR, 4 runs"},
		"review":   {"reading the diff as a stranger would", "a nil check is missing on the cold path", "naming: this is a queue, not a cache"},
		"security": {"grepping for tokens in log lines", "checking the stranger path writes nothing", "authz on the new route"},
		"bisect":   {"git bisect run · 6 steps", "found: the merge at 02:14", "the test is right, the code is wrong"},
		"fix":      {"editing the ordering", "go build ./... ok"},
		"proof":    {"writing the sheet", "one claim has no evidence; saying so"},
	}
	ls := lines[ph.Name]
	if len(ls) == 0 {
		return
	}
	g, tone := glyphThought, "thought"
	switch ph.Name {
	case "write", "fix":
		g, tone = glyphWrite, "write"
	case "test", "checks":
		g, tone = glyphTest, "test"
	case "bisect":
		g, tone = glyphShell, "shell"
	}
	w.say(it, g, tone, ls[w.rng.Intn(len(ls))])
}

// land writes the proof sheet and parks the item for sign-off. Gate none
// with an all-green sheet ships straight away.
func (w *World) land(it *factory.Item) {
	s := it.Stream
	s.Ended = w.now
	it.Proof = w.proof(it)
	it.Policy = w.policy(it)
	it.Changed = w.now
	if it.Kind == factory.KindCI {
		if r := w.repo(it.Repo); r != nil {
			r.CIRed = false
		}
	}
	allOK := true
	for _, c := range it.Proof {
		allOK = allOK && c.OK
	}
	for _, c := range it.Policy {
		allOK = allOK && c.OK
	}
	if it.Gate == factory.GateNone && allOK {
		w.ship(it)
		w.say(it, glyphOK, "ok", "shipped · habit · proof all green")
		return
	}
	it.State = factory.StateLanded
	w.say(it, glyphSaid, "said", "landed · proof sheet ready · your sign-off")
}

func (w *World) ship(it *factory.Item) {
	it.State = factory.StateShipped
	it.Changed = w.now
	w.shift.Shipped++
	w.shift.Shipping = append(w.shift.Shipping, it.Ref())
}

func (w *World) proof(it *factory.Item) []factory.Claim {
	a := it.Triage.Area
	var cs []factory.Claim
	switch it.Kind {
	case factory.KindPR:
		cs = []factory.Claim{
			{Text: "rails follow the tree on narrow widths", OK: true, Evidence: "screenshot 80→100 cols", Medium: "screenshot"},
			{Text: "no behaviour change on wide", OK: true, Evidence: "test · 9/9", Medium: "test"},
			{Text: "no perf impact", OK: w.rng.Float64() < 0.6, Evidence: "+40ms/frame @ 200 rows · benchmark", Medium: "benchmark"},
		}
		if cs[2].OK {
			cs[2].Evidence = "12ms → 12ms/frame · benchmark"
		}
	case factory.KindCI:
		cs = []factory.Claim{
			{Text: "the three red runs pass", OK: true, Evidence: "test · 3/3 runs", Medium: "test"},
			{Text: "nothing else moved", OK: true, Evidence: "diff +6 −2 · one file", Medium: "test"},
		}
	default:
		cs = []factory.Claim{
			{Text: "fires on first true, never again", OK: true, Evidence: "test · 0.3s", Medium: "test"},
			{Text: "the " + a + " survives a compact", OK: true, Evidence: "e2e · 1.8s", Medium: "test"},
			{Text: "no row left on home when retired", OK: true, Evidence: "screenshot", Medium: "screenshot"},
			{Text: "survives a codeaf restart", OK: w.rng.Float64() < 0.65, Evidence: "no check covers it", Medium: ""},
		}
		if cs[3].OK {
			cs[3].Evidence = "e2e restart · 4.1s"
			cs[3].Medium = "test"
		}
	}
	// Each stage's own proof lines are claims too.
	for _, st := range it.Stages {
		if !st.On {
			continue
		}
		for _, p := range st.Proof {
			cs = append(cs, factory.Claim{Text: p, OK: true, Evidence: "checked in " + st.Name, Medium: "policy"})
		}
	}
	return cs
}

func (w *World) policy(it *factory.Item) []factory.Claim {
	r := w.repo(it.Repo)
	if r == nil {
		return nil
	}
	var cs []factory.Claim
	for _, p := range r.Recipe.Policy {
		c := factory.Claim{Text: p, OK: true, Medium: "policy"}
		switch {
		case strings.Contains(p, "complexity"):
			d := w.rng.Intn(14) - 2
			c.OK = d <= 10
			c.Evidence = fmt.Sprintf("complexity %+d%%", d)
		case strings.Contains(p, "dependencies"):
			c.Evidence = "go.mod unchanged"
		case strings.Contains(p, "labels"):
			c.Evidence = "bug · 1.4 · agentfield-bot"
		case strings.Contains(p, "migrations"):
			c.Evidence = "down migration present"
		case strings.Contains(p, "lighthouse"):
			c.Evidence = "lighthouse 94"
		default:
			c.Evidence = "checked"
		}
		cs = append(cs, c)
	}
	return cs
}

// answer resolves a waiting question with yes, no, or words.
func (w *World) answer(it *factory.Item, yes bool, words string) error {
	if it.State != factory.StateNeedsYou || it.Stream == nil {
		return fmt.Errorf("%s is not waiting on you", it.Ref())
	}
	s := it.Stream
	ph := &s.Phases[s.Cur]
	r := w.runs[it.ID]
	kind, pending := it.QKind, r.pending
	it.Question, it.QKind, r.pending = "", "", ""
	w.shift.Handled++
	it.Changed = w.now
	it.State = factory.StateRunning
	ph.State = factory.PhaseRunning
	switch {
	case kind == "cap":
		if !yes {
			w.stop(it)
			return nil
		}
		it.Cap *= 2
		w.say(it, glyphSaid, "said", fmt.Sprintf("cap raised to %s · carrying on", usd(it.Cap)))
	case kind == "plan":
		if yes {
			w.say(it, glyphSaid, "said", "go")
			w.finish(it)
			return nil
		}
		w.say(it, glyphSaid, "said", "replanning with: "+words)
		ph.Left = 3 * time.Minute
		r.planned = false
	case pending == "rounds":
		if yes {
			if st := w.stageOf(it, s.Cur); st != nil {
				st.Max = ph.Round + 1
			}
			ph.Round++
			ph.Left = 3 * time.Minute
			w.say(it, glyphSaid, "said", fmt.Sprintf("one more round · %s %d", ph.Name, ph.Round))
			return nil
		}
		w.say(it, glyphSaid, "said", "going on as is · "+ph.Note)
		w.finish(it)
	default:
		switch {
		case words != "":
			w.say(it, glyphSaid, "said", "noted: "+words)
		case yes:
			w.say(it, glyphSaid, "said", "yes · carrying on")
		default:
			w.say(it, glyphSaid, "said", "no · the narrow fix")
		}
	}
	return nil
}

// stop ends a stream and keeps its branch; the item returns to new.
func (w *World) stop(it *factory.Item) {
	if s := it.Stream; s != nil && s.Cur < len(s.Phases) && it.State != factory.StateLanded && it.State != factory.StateShipped {
		s.Phases[s.Cur].State = factory.PhaseFailed
		s.Ended = w.now
		s.Paused = false
		w.say(it, glyphFail, "fail", "stopped · branch kept")
	}
	it.State = factory.StateNew
	it.Question, it.QKind = "", ""
	it.Changed = w.now
}

// steer lifts an effort word, a gate, a round count or a cap out of the words
// and folds the rest into the log. The effort lands on the running stage, and
// "redo" starts that stage's round over at the new effort.
func (w *World) steer(it *factory.Item, words string) error {
	s := it.Stream
	if s == nil {
		return fmt.Errorf("%s has no stream to steer", it.Ref())
	}
	c := lift(words)
	at := -1
	if r := w.runs[it.ID]; r != nil && s.Cur < len(r.stage) {
		at = r.stage[s.Cur]
	}
	for _, said := range c.apply(it, at) {
		w.say(it, glyphSaid, "said", "steer: "+said)
	}
	if c.redo && s.Cur < len(s.Phases) {
		if r := w.runs[it.ID]; r != nil && s.Cur < len(r.dur) {
			s.Phases[s.Cur].Left = r.dur[s.Cur]
		}
		w.say(it, glyphSaid, "said", "redoing "+s.Phases[s.Cur].Name)
	}
	if c.rest != "" {
		w.say(it, glyphSaid, "said", "steer: "+c.rest+" · folding it in")
	}
	it.Changed = w.now
	return nil
}

// signOff ships a landed item and says whether a habit is due: three clean
// sign-offs in a row offer one, and the count starts over once offered.
func (w *World) signOff(it *factory.Item, edited bool) (bool, error) {
	if it.State != factory.StateLanded {
		return false, fmt.Errorf("%s has not landed", it.Ref())
	}
	w.ship(it)
	w.say(it, glyphOK, "ok", "shipped · your sign-off")
	if edited {
		w.habits = 0
		return false, nil
	}
	w.habits++
	if w.habits >= 3 {
		w.habits = 0
		return true, nil
	}
	return false, nil
}

// reopen loops a landed item back onto its bench with one more phase before
// a fresh proof. No new brief.
func (w *World) reopen(it *factory.Item, name, note string, d time.Duration) error {
	if it.Stream == nil || (it.State != factory.StateLanded && it.State != factory.StateShipped) {
		return fmt.Errorf("%s has not landed", it.Ref())
	}
	s := it.Stream
	r := w.runs[it.ID]
	if r == nil {
		r = &run{stage: make([]int, len(s.Phases)), dur: make([]time.Duration, len(s.Phases))}
		for i := range r.stage {
			r.stage[i] = -1
		}
		w.runs[it.ID] = r
	}
	s.Phases = append(s.Phases, factory.Phase{Name: name, State: factory.PhasePending, Note: note, Left: d}, factory.Phase{Name: "proof", State: factory.PhasePending, Left: time.Minute})
	r.stage = append(r.stage, -1, -1)
	r.dur = append(r.dur, d, time.Minute)
	s.Ended = time.Time{}
	it.State = factory.StateRunning
	it.Changed = w.now
	w.enter(it, len(s.Phases)-2)
	return nil
}

func (w *World) pulse(s *factory.Stream, live bool) {
	if len(s.Activity) == 0 {
		return
	}
	v := w.rng.Intn(2)
	if live {
		v = 2 + w.rng.Intn(6)
	}
	copy(s.Activity, s.Activity[1:])
	s.Activity[len(s.Activity)-1] = v
}

// tick advances the clock by d in one-minute steps. A remainder under a
// minute is carried to the next tick, so a fast surface ticking thirty
// seconds at a time still moves the floor.
func (w *World) tick(d time.Duration) {
	w.acc += d
	for w.acc >= time.Minute {
		w.acc -= time.Minute
		w.step()
	}
}

// sleep starts a new shift and then advances, so the handover shows what
// happened while nobody looked.
func (w *World) sleep(d time.Duration) {
	w.shift = factory.Shift{Since: w.now}
	w.tick(d)
}
