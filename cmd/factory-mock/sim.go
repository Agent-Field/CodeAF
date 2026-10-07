package main

// The simulation. A factory is CONTINUOUS: work arrives while nobody looks,
// streams advance through phases, money accrues, questions surface, things
// land. The mock exists so that continuity can be felt by hand — speed it up,
// sleep a shift, come back to a handover.

import (
	"fmt"
	"strings"
	"time"
)

// Step advances the world by one sim-minute. The app calls it Speed/minute
// times per real tick.
func (w *World) Step() {
	w.Now = w.Now.Add(time.Minute)
	w.arrive()
	benches := 0
	for _, it := range w.Items {
		if it.State == StRunning && it.Stream != nil && !it.Stream.Paused {
			benches++
		}
	}
	// Queued items take a bench when one frees.
	for _, it := range w.Items {
		if benches >= w.Benches {
			break
		}
		if it.State == StQueued {
			w.start(it)
			benches++
		}
	}
	for _, it := range w.Items {
		if it.State == StRunning && it.Stream != nil && !it.Stream.Paused {
			w.advance(it)
		}
		if it.State == StRunning || it.State == StNeedsYou {
			if it.Stream != nil {
				w.pulse(it.Stream, it.State == StRunning && !it.Stream.Paused)
			}
		}
	}
}

func (w *World) arrive() {
	// About one arrival every 25 sim-minutes across the fleet, bursty by hour.
	h := w.Now.Hour()
	rate := 1.0 / 25
	if h < 7 || h > 22 {
		rate /= 4
	}
	if w.rng.Float64() < rate {
		r := w.pickRepo()
		kind := KindIssue
		if w.rng.Float64() < 0.3 {
			kind = KindPR
		}
		it := w.genItem(r, kind, w.Now)
		w.Items = append(w.Items, it)
		w.Shift.Arrived++
		// A banked habit may pick it up on its own.
		for _, hb := range r.Habits {
			if kind == KindPR && strings.Contains(hb, "review pass") && it.Tier != TierStranger {
				it.Order.Gate = GateShip
				w.Launch(it)
			}
			if kind == KindIssue && strings.Contains(hb, "labelled factory") && hasLabel(it, "factory") && it.Tier != TierStranger {
				w.Launch(it)
			}
		}
	}
	// Occasionally CI goes red somewhere.
	if w.rng.Float64() < 1.0/600 {
		r := w.pickRepo()
		if !r.CIRed {
			r.CIRed = true
			it := w.genItem(r, KindCI, w.Now)
			w.Items = append(w.Items, it)
			w.Shift.Arrived++
			for _, hb := range r.Habits {
				if strings.Contains(hb, "keep main green") {
					w.Launch(it)
				}
			}
		}
	}
}

func hasLabel(it *Item, l string) bool {
	for _, x := range it.Labels {
		if x == l {
			return true
		}
	}
	return false
}

// Launch puts an item on the floor: a bench if one is free, the queue if not.
func (w *World) Launch(it *Item) {
	if it.State == StRunning || it.State == StQueued {
		return
	}
	running := 0
	for _, x := range w.Items {
		if x.State == StRunning {
			running++
		}
	}
	it.Changed = w.Now
	if running >= w.Benches {
		it.State = StQueued
		return
	}
	w.start(it)
}

func (w *World) start(it *Item) {
	it.State = StRunning
	it.Changed = w.Now
	s := &Stream{Started: w.Now, Activity: make([]int, 12)}
	o := it.Order
	add := func(name string, mins int, note string) {
		s.Phases = append(s.Phases, Phase{Name: name, Dur: time.Duration(mins) * time.Minute, Left: time.Duration(mins) * time.Minute, Note: note})
	}
	sz := map[string]int{"S": 1, "M": 2, "L": 4}[it.Triage.Size]
	if sz == 0 {
		sz = 2
	}
	j := func(base int) int { return base*sz + w.rng.Intn(base*sz/2+1) }
	switch it.Kind {
	case KindPR:
		add("read", j(3), "the diff and the claims")
		add("checks", j(4), "run what the claims imply")
		for i := 1; i <= o.Rounds; i++ {
			add(fmt.Sprintf("review %d/%d", i, o.Rounds), j(5), o.ReviewModel)
		}
		if o.Security {
			add("security", j(5), "secrets · injection · authz")
		}
		add("proof", 2, "the sheet")
	case KindCI:
		add("bisect", j(4), "the three red runs")
		add("fix", j(5), o.WriteModel)
		add("test", j(3), "")
		add("proof", 1, "")
	default:
		steps := func(when string) {
			for _, st := range o.Steps {
				if st.On && st.When == when {
					add(st.Short(), j(3), st.Text)
				}
			}
		}
		add("plan", j(4), o.PlanModel)
		steps("after plan")
		add("write", j(9), o.WriteModel)
		steps("after write")
		add("test", j(4), "")
		steps("after test")
		for i := 1; i <= o.Rounds; i++ {
			add(fmt.Sprintf("review %d/%d", i, o.Rounds), j(4), o.ReviewModel)
		}
		if o.Security {
			add("security", j(4), "secrets · injection · authz")
		}
		steps("after review")
		steps("before proof")
		add("proof", 2, "the sheet")
	}
	s.Phases[0].State = PhRunning
	bench := 1
	used := map[int]bool{}
	for _, x := range w.Items {
		if x.Stream != nil && x.State == StRunning && x != it {
			used[x.Stream.Bench] = true
		}
	}
	for used[bench] {
		bench++
	}
	s.Bench = bench
	it.Stream = s
	w.say(it, tokens_thought, "thought", "reading "+it.Ref()+" · "+it.Triage.Read)
}

const (
	tokens_thought = "✳"
	tokens_shell   = "$"
	tokens_test    = "◎"
	tokens_write   = "✎"
	tokens_said    = "»"
	tokens_ask     = "?"
	tokens_fail    = "✕"
	tokens_ok      = "✓"
)

func (w *World) say(it *Item, glyph, tone, text string) {
	it.Stream.Log = append(it.Stream.Log, LogLine{At: w.Now, Glyph: glyph, Tone: tone, Text: text})
	if len(it.Stream.Log) > 400 {
		it.Stream.Log = it.Stream.Log[len(it.Stream.Log)-400:]
	}
}

func (w *World) advance(it *Item) {
	s := it.Stream
	if s.Cur >= len(s.Phases) {
		return
	}
	ph := &s.Phases[s.Cur]
	// Money: the phase's model at its rate, per minute.
	model := it.Order.WriteModel
	switch {
	case strings.HasPrefix(ph.Name, "plan"), ph.Name == "read", ph.Name == "bisect":
		model = it.Order.PlanModel
	case strings.HasPrefix(ph.Name, "review"), ph.Name == "security", ph.Name == "checks":
		model = it.Order.ReviewModel
	case ph.Name == "test", ph.Name == "proof":
		model = "deepseek-v4.1-flash"
	}
	cost := w.modelRate(model) / 60
	s.Spent += cost
	w.Daily += cost
	w.Shift.Spent += cost
	if s.Spent >= it.Order.Cap {
		it.State = StNeedsYou
		it.QKind = "cap"
		it.Question = fmt.Sprintf("hit the $%.0f cap in %s · raise to $%.0f, or stop?", it.Order.Cap, ph.Name, it.Order.Cap*2)
		ph.State = PhWaiting
		w.say(it, tokens_ask, "ask", it.Question)
		w.Shift.Asked++
		it.Changed = w.Now
		return
	}
	ph.Left -= time.Minute
	// Chatter, so the stream view has grain.
	if w.rng.Float64() < 0.35 {
		w.chatter(it, ph)
	}
	// A scope question, rarely, in plan or write.
	if (ph.Name == "plan" || ph.Name == "write") && w.rng.Float64() < 1.0/90 && it.QKind == "" {
		it.State = StNeedsYou
		it.QKind = "scope"
		it.Question = []string{"the fix wants to touch " + it.Triage.Area + " and " + it.Repo.Areas[w.rng.Intn(len(it.Repo.Areas))] + " · both, or just the first?", "two ways in: a lock or a queue · the queue is bigger but honest · which?", "the test that covers this is skipped on dev · fix the test too?"}[w.rng.Intn(3)]
		ph.State = PhWaiting
		w.say(it, tokens_ask, "ask", it.Question)
		w.Shift.Asked++
		it.Changed = w.Now
		return
	}
	if ph.Left > 0 {
		return
	}
	// Phase done.
	ph.State = PhDone
	w.Shift.Hours[w.Now.Hour()]++
	switch {
	case ph.Name == "plan":
		w.say(it, tokens_said, "said", "plan: "+w.planSummary(it))
		if it.Order.Gate == GatePlan && it.QKind != "plan" {
			it.State = StNeedsYou
			it.QKind = "plan"
			it.Question = "plan is ready · go, or change it?"
			w.Shift.Asked++
			it.Changed = w.Now
			return
		}
	case ph.Name == "test":
		if w.rng.Float64() < 0.3 {
			w.say(it, tokens_fail, "fail", fmt.Sprintf("%d/%d · Test%s failed · fixing", 7+w.rng.Intn(5), 12, strings.ToUpper(it.Triage.Area[:1])+it.Triage.Area[1:]))
			ph.Left = time.Duration(2+w.rng.Intn(4)) * time.Minute
			ph.State = PhRunning
			return
		}
		w.say(it, tokens_ok, "ok", "tests 12/12")
	case strings.HasPrefix(ph.Name, "review"):
		n := w.rng.Intn(4)
		s.Findings += n
		if n > 0 {
			w.say(it, tokens_said, "said", fmt.Sprintf("%s: %d findings · fixing", ph.Name, n))
			ph.Left = time.Duration(n*2) * time.Minute
			ph.State = PhRunning
			ph.Note = fmt.Sprintf("%d findings", n)
			return
		}
		w.say(it, tokens_ok, "ok", ph.Name+": clean")
	case ph.Name == "security":
		if w.rng.Float64() < 0.15 {
			w.say(it, tokens_fail, "fail", "security: a token lands in a log line · redacting")
			ph.Left = 3 * time.Minute
			ph.State = PhRunning
			return
		}
		w.say(it, tokens_ok, "ok", "security: nothing found")
	case ph.Name == "proof":
		w.land(it)
		return
	}
	s.Cur++
	if s.Cur < len(s.Phases) {
		s.Phases[s.Cur].State = PhRunning
	}
}

func (w *World) planSummary(it *Item) string {
	return []string{
		"one measure at the seam, a test that pins the width, no new state",
		"queue the writes behind one owner; three files; the migration is reversible",
		"a new verb on the existing grammar; manual page in the same change",
		"split: parser first, then the renderer; the second half can run beside the first",
	}[w.rng.Intn(4)]
}

func (w *World) chatter(it *Item, ph *Phase) {
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
	key := ph.Name
	if strings.HasPrefix(key, "review") {
		key = "review"
	}
	ls := lines[key]
	if len(ls) == 0 {
		return
	}
	g := tokens_thought
	tone := "thought"
	switch key {
	case "write", "fix":
		g, tone = tokens_write, "write"
	case "test", "checks":
		g, tone = tokens_test, "test"
	case "bisect":
		g, tone = tokens_shell, "shell"
	}
	w.say(it, g, tone, ls[w.rng.Intn(len(ls))])
}

// land writes the proof sheet and parks the item for sign-off. A habit with
// gate none merges it straight away.
func (w *World) land(it *Item) {
	s := it.Stream
	s.Ended = w.Now
	s.Phases[s.Cur].State = PhDone
	it.Proof = w.proof(it)
	it.Policy = w.policy(it)
	it.Changed = w.Now
	if it.Kind == KindCI {
		it.Repo.CIRed = false
	}
	allOK := true
	for _, c := range it.Proof {
		allOK = allOK && c.OK
	}
	for _, c := range it.Policy {
		allOK = allOK && c.OK
	}
	if it.Order.Gate == GateNone && allOK {
		it.State = StMerged
		w.Shift.Shipped++
		w.Shift.Shipping = append(w.Shift.Shipping, it.Ref())
		w.say(it, tokens_ok, "ok", "merged · habit · proof all green")
		return
	}
	it.State = StLanded
	w.say(it, tokens_said, "said", "landed · proof sheet ready · your sign-off")
}

func (w *World) proof(it *Item) []Claim {
	a := it.Triage.Area
	var cs []Claim
	switch it.Kind {
	case KindPR:
		cs = []Claim{
			{Text: "rails follow the tree on narrow widths", OK: true, Evidence: "screenshot 80→100 cols", Medium: "screenshot"},
			{Text: "no behaviour change on wide", OK: true, Evidence: "test · 9/9", Medium: "test"},
			{Text: "no perf impact", OK: w.rng.Float64() < 0.6, Evidence: "+40ms/frame @ 200 rows · benchmark", Medium: "benchmark"},
		}
		if cs[2].OK {
			cs[2].Evidence = "12ms → 12ms/frame · benchmark"
		}
	case KindCI:
		cs = []Claim{
			{Text: "the three red runs pass", OK: true, Evidence: "test · 3/3 runs", Medium: "test"},
			{Text: "nothing else moved", OK: true, Evidence: "diff +6 −2 · one file", Medium: "test"},
		}
	default:
		cs = []Claim{
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
	return cs
}

func (w *World) policy(it *Item) []Claim {
	var cs []Claim
	for _, p := range it.Repo.Policy {
		c := Claim{Text: p, OK: true, Medium: "policy"}
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

// Answer resolves a needs-you question with y/n or words.
func (w *World) Answer(it *Item, yes bool, words string) {
	if it.State != StNeedsYou || it.Stream == nil {
		return
	}
	s := it.Stream
	ph := &s.Phases[s.Cur]
	switch it.QKind {
	case "cap":
		if yes {
			it.Order.Cap *= 2
			w.say(it, tokens_said, "said", fmt.Sprintf("cap raised to $%.0f · carrying on", it.Order.Cap))
			ph.State = PhRunning
			it.State = StRunning
		} else {
			w.Stop(it)
			return
		}
	case "plan":
		if yes {
			w.say(it, tokens_said, "said", "go · writing")
			s.Cur++
			s.Phases[s.Cur].State = PhRunning
			it.State = StRunning
		} else {
			w.say(it, tokens_said, "said", "replanning with: "+words)
			ph.State = PhRunning
			ph.Left = 3 * time.Minute
			it.State = StRunning
			it.QKind = ""
		}
	default:
		if words != "" {
			w.say(it, tokens_said, "said", "noted: "+words)
		} else if yes {
			w.say(it, tokens_said, "said", "yes · carrying on")
		} else {
			w.say(it, tokens_said, "said", "no · the narrow fix")
		}
		ph.State = PhRunning
		it.State = StRunning
	}
	it.Question = ""
	w.Shift.Handled++
	it.Changed = w.Now
}

func (w *World) Stop(it *Item) {
	if it.Stream != nil && it.Stream.Cur < len(it.Stream.Phases) {
		it.Stream.Phases[it.Stream.Cur].State = PhFailed
		it.Stream.Ended = w.Now
		w.say(it, tokens_fail, "fail", "stopped · branch kept")
	}
	it.State = StNew
	it.Question = ""
	it.QKind = ""
	it.Changed = w.Now
}

func (w *World) Steer(it *Item, words string) {
	if it.Stream == nil {
		return
	}
	title, o := parseOrder(words, it.Order)
	changed := o.Recipe() != it.Order.Recipe()
	it.Order = o
	if changed {
		w.say(it, tokens_said, "said", "recipe now: "+o.Recipe())
	}
	if title != "" {
		w.say(it, tokens_said, "said", "steer: "+title+" · folding it in")
	}
}

// SignOff merges a landed item. Returns whether a habit prompt is due.
func (w *World) SignOff(it *Item, edited bool) bool {
	it.State = StMerged
	it.Changed = w.Now
	w.Shift.Shipped++
	w.Shift.Shipping = append(w.Shift.Shipping, it.Ref())
	w.say(it, tokens_ok, "ok", "merged · your sign-off")
	if !edited {
		w.Habits++
	} else {
		w.Habits = 0
	}
	return w.Habits >= 3
}

func (w *World) SendBack(it *Item, words string) {
	it.State = StRunning
	it.Changed = w.Now
	s := it.Stream
	s.Phases = append(s.Phases, Phase{Name: "prove", State: PhRunning, Dur: 6 * time.Minute, Left: 6 * time.Minute, Note: words}, Phase{Name: "proof", Dur: time.Minute, Left: time.Minute})
	s.Cur = len(s.Phases) - 2
	s.Ended = time.Time{}
	w.say(it, tokens_said, "said", "sent back: "+words)
}

func (w *World) Reverify(it *Item) {
	it.State = StRunning
	s := it.Stream
	s.Phases = append(s.Phases, Phase{Name: "re-verify", State: PhRunning, Dur: 4 * time.Minute, Left: 4 * time.Minute}, Phase{Name: "proof", Dur: time.Minute, Left: time.Minute})
	s.Cur = len(s.Phases) - 2
	s.Ended = time.Time{}
	w.say(it, tokens_test, "test", "re-running every check")
}

func (w *World) pulse(s *Stream, live bool) {
	v := 0
	if live {
		v = 2 + w.rng.Intn(6)
	} else {
		v = w.rng.Intn(2)
	}
	copy(s.Activity, s.Activity[1:])
	s.Activity[len(s.Activity)-1] = v
}

// Sleep jumps the clock by d, stepping the world a minute at a time, and marks
// the shift so the handover shows what happened while nobody looked.
func (w *World) Sleep(d time.Duration) {
	w.Shift = Shift{Since: w.Now}
	for i := 0; i < int(d/time.Minute); i++ {
		w.Step()
	}
}

// Counts for the floor's headers.
func (w *World) Count(st State) int {
	n := 0
	for _, it := range w.Items {
		if it.State == st {
			n++
		}
	}
	return n
}
