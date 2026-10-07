package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

type screen uint8

const (
	scFloor screen = iota
	scCard
	scStream
	scSignoff
	scHelp
)

type rowKind uint8

const (
	rowHeader rowKind = iota
	rowItem
	rowBlank
	rowText
)

type floorRow struct {
	kind    rowKind
	section string
	text    string
	item    *Item
}

type app struct {
	w       *World
	width   int
	height  int
	st      *tokens.Styler
	scr     screen
	cursor  int
	rows    []floorRow
	scroll  int
	repo    int // -1 = all
	filter  string
	typing  string // "" filter compose steer answer sendback words
	backlog bool   // show every open item, not just the delta
	older   int
	input   string
	cur     *Item
	logTop  int
	habit   *Item
	toast   string
	toastAt time.Time
	paused  bool
	acc     time.Duration
	tick    int
	edited  bool
	prev    screen
	helpFor screen
}

type tickMsg time.Time

func newApp(w *World) *app {
	p := tokens.DetectProfile(func(n string) string { return envGet(n) })
	// The geometric floor everywhere: a mock must look the same on every
	// terminal it is tried on, nerd font or not.
	a := &app{w: w, st: tokens.NewStylerIn(p, tokens.FocusNormal, tokens.Plain), repo: -1, width: 100, height: 40}
	return a
}

func (a *app) Init() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
	case tickMsg:
		a.tick++
		if !a.paused {
			a.acc += a.w.Speed
			for a.acc >= time.Minute {
				a.acc -= time.Minute
				a.w.Step()
			}
		}
		return a, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	case tea.KeyPressMsg:
		return a, a.key(m)
	}
	return a, nil
}

func (a *app) say(s string) { a.toast = s; a.toastAt = time.Now() }

func (a *app) key(m tea.KeyPressMsg) tea.Cmd {
	k := m.String()
	if k == "ctrl+c" {
		return tea.Quit
	}
	if a.typing != "" {
		return a.typingKey(m)
	}
	if a.habit != nil {
		switch k {
		case "y":
			a.habit.Repo.Habits = append(a.habit.Repo.Habits, "factory PRs from my own issues self-ship when the proof is green")
			a.say("banked · " + a.habit.Repo.Name + " · the next one needs nobody")
			a.w.Habits = 0
		default:
			a.say("not banked · ask again after three more")
			a.w.Habits = 0
		}
		a.habit = nil
		return nil
	}
	switch a.scr {
	case scHelp:
		a.scr = a.helpFor
		return nil
	case scCard:
		return a.cardKey(k)
	case scStream:
		return a.streamKey(k)
	case scSignoff:
		return a.signoffKey(k)
	}
	return a.floorKey(k)
}

func (a *app) typingKey(m tea.KeyPressMsg) tea.Cmd {
	k := m.String()
	switch k {
	case "esc":
		if a.typing == "filter" {
			a.filter = ""
		}
		a.typing, a.input = "", ""
		return nil
	case "enter":
		a.submit()
		return nil
	case "backspace":
		if len(a.input) > 0 {
			r := []rune(a.input)
			a.input = string(r[:len(r)-1])
		}
		if a.typing == "filter" {
			a.filter = a.input
		}
		return nil
	case "space":
		a.input += " "
	default:
		if m.Text != "" {
			a.input += m.Text
		}
	}
	if a.typing == "filter" {
		a.filter = a.input
		a.cursor = 0
	}
	return nil
}

func (a *app) submit() {
	words := strings.TrimSpace(a.input)
	mode := a.typing
	a.typing, a.input = "", ""
	switch mode {
	case "filter":
		a.filter = words
	case "compose":
		if words == "" {
			return
		}
		r := a.w.Repos[0]
		if a.repo >= 0 {
			r = a.w.Repos[a.repo]
		}
		it := a.w.NewFromWords(r, words)
		a.cur = it
		a.scr = scCard
		a.say("written in the terminal · [g] also opens it on github")
	case "steer":
		if a.cur != nil {
			a.w.Steer(a.cur, words)
		}
	case "answer":
		if a.cur != nil {
			a.w.Answer(a.cur, true, words)
		}
	case "sendback":
		if a.cur != nil {
			if words == "" {
				words = "prove " + firstFail(a.cur)
			}
			a.w.SendBack(a.cur, words)
			a.scr = scStream
			a.say("sent back · it loops into the stream, no new brief")
		}
	case "words":
		if a.cur != nil {
			title, o := parseOrder(words, a.cur.Order)
			if title != "" {
				o.Constraints = append(o.Constraints, title)
			}
			a.cur.Order = o
			a.say("words became chips")
		}
	case "ask":
		if a.cur != nil {
			a.say("asked the floor: “" + words + "” · it answers in the stream")
		}
	case "step":
		if a.cur != nil && words != "" {
			a.cur.Order.Steps = append(a.cur.Order.Steps, parseStep(words))
			a.say("a step, in words · [b] would bank it for every item on " + short(a.cur.Repo))
		}
	}
}

func firstFail(it *Item) string {
	for _, c := range it.Proof {
		if !c.OK {
			return c.Text
		}
	}
	for _, c := range it.Policy {
		if !c.OK {
			return c.Text
		}
	}
	return "it again"
}

// ---- floor

func (a *app) visible() func(*Item) bool {
	sem := semantic(a.filter)
	return func(it *Item) bool {
		if a.repo >= 0 && it.Repo != a.w.Repos[a.repo] {
			return false
		}
		return sem(it)
	}
}

func (a *app) buildRows() {
	vis := a.visible()
	w := a.w
	var needs, streams, queued, fresh, landed, merged []*Item
	a.older = 0
	for _, it := range w.Items {
		if !vis(it) {
			continue
		}
		switch it.State {
		case StNeedsYou:
			needs = append(needs, it)
		case StRunning:
			streams = append(streams, it)
		case StQueued:
			queued = append(queued, it)
		case StNew:
			if !a.backlog && a.filter == "" && w.Now.Sub(it.Created) > 3*24*time.Hour {
				a.older++
				continue
			}
			fresh = append(fresh, it)
		case StLanded:
			landed = append(landed, it)
		case StMerged:
			merged = append(merged, it)
		}
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i].Changed.Before(needs[j].Changed) })
	sort.Slice(streams, func(i, j int) bool { return streams[i].Stream.Bench < streams[j].Stream.Bench })
	sort.Slice(queued, func(i, j int) bool { return queued[i].Changed.Before(queued[j].Changed) })
	sort.Slice(fresh, func(i, j int) bool {
		si, sj := score(fresh[i]), score(fresh[j])
		if si != sj {
			return si > sj
		}
		return fresh[i].Created.After(fresh[j].Created)
	})
	sort.Slice(landed, func(i, j int) bool { return landed[i].Changed.Before(landed[j].Changed) })
	sort.Slice(merged, func(i, j int) bool { return merged[i].Changed.After(merged[j].Changed) })
	if len(merged) > 8 {
		merged = merged[:8]
	}
	var rows []floorRow
	add := func(section string, items []*Item, empty string) {
		if len(items) == 0 && empty == "" {
			return
		}
		rows = append(rows, floorRow{kind: rowHeader, section: section, text: fmt.Sprint(len(items))})
		if len(items) == 0 {
			rows = append(rows, floorRow{kind: rowText, text: empty})
		}
		for _, it := range items {
			rows = append(rows, floorRow{kind: rowItem, section: section, item: it})
		}
		rows = append(rows, floorRow{kind: rowBlank})
	}
	add("needs you", needs, "")
	all := append(append([]*Item{}, streams...), queued...)
	add("streams", all, "nothing on the benches · [L] launches what you mark")
	newNote := "quiet · nothing arrived that you have not seen"
	if a.older > 0 {
		newNote = fmt.Sprintf("%d older open items behind [A] · or / to search them", a.older)
	}
	add("new", fresh, newNote)
	if a.older > 0 && len(fresh) > 0 {
		rows[len(rows)-1] = floorRow{kind: rowText, text: fmt.Sprintf("%d older open items behind [A] · / searches all of them", a.older)}
		rows = append(rows, floorRow{kind: rowBlank})
	}
	add("landed", landed, "")
	add("shipped", merged, "")
	a.rows = rows
	if a.cursor >= len(rows) {
		a.cursor = len(rows) - 1
	}
	if a.cursor < 0 {
		a.cursor = 0
	}
	if len(rows) > 0 && rows[a.cursor].kind != rowItem {
		a.move(1)
	}
}

func score(it *Item) float64 {
	s := 0.0
	switch it.Tier {
	case TierOwner:
		s += 30
	case TierCollab:
		s += 15
	}
	if it.Kind == KindCI {
		s += 60
	}
	if it.Kind == KindPR {
		s += 10
	}
	s += float64(it.Triage.Readiness) / 4
	s -= it.Triage.Est
	if hasLabel(it, "factory") {
		s += 20
	}
	return s
}

func (a *app) move(d int) {
	n := len(a.rows)
	if n == 0 {
		return
	}
	i := a.cursor
	for step := 0; step < n; step++ {
		i += d
		if i < 0 {
			i = 0
			break
		}
		if i >= n {
			i = n - 1
			break
		}
		if a.rows[i].kind == rowItem {
			break
		}
	}
	if a.rows[i].kind == rowItem {
		a.cursor = i
	}
}

func (a *app) current() *Item {
	if a.cursor < len(a.rows) && a.rows[a.cursor].kind == rowItem {
		return a.rows[a.cursor].item
	}
	return nil
}

func (a *app) marked() []*Item {
	var out []*Item
	for _, it := range a.w.Items {
		if it.Marked && it.State == StNew {
			out = append(out, it)
		}
	}
	return out
}

// open PEEKS: a sheet over the floor, whatever the row is. Enter again walks
// in. Esc is always the floor. That is the whole map.
func (a *app) open(it *Item) {
	a.cur = it
	a.edited = false
	a.logTop = -1
	a.scr = scCard
}

func (a *app) walkIn(it *Item) {
	switch it.State {
	case StLanded:
		a.scr = scSignoff
	default:
		a.scr = scStream
		a.logTop = -1
	}
}

func (a *app) floorKey(k string) tea.Cmd {
	a.buildRows()
	it := a.current()
	switch k {
	case "q":
		return tea.Quit
	case "?":
		a.helpFor, a.scr = scFloor, scHelp
	case "j", "down":
		a.move(1)
	case "k", "up":
		a.move(-1)
	case "g", "home":
		a.cursor = 0
		a.move(1)
		a.move(-1)
	case "G", "end":
		a.cursor = len(a.rows) - 1
		a.move(-1)
		a.move(1)
	case "enter":
		if it != nil {
			a.open(it)
		}
	case "space":
		if it != nil && it.State == StNew {
			it.Marked = !it.Marked
			a.move(1)
		}
	case "L":
		ms := a.marked()
		if len(ms) == 0 && it != nil && it.State == StNew {
			ms = []*Item{it}
		}
		for _, m := range ms {
			m.Marked = false
			a.w.Launch(m)
		}
		if len(ms) > 0 {
			a.say(fmt.Sprintf("launched %d · benches %d · the rest queue", len(ms), a.w.Benches))
		}
	case "p":
		if it != nil && it.State == StNew {
			it.Order.Gate = GatePlan
			a.w.Launch(it)
			a.say("plan first · it comes back with the plan before any code")
		}
	case "r":
		if it != nil && it.State == StNew {
			it.Order.Gate = GateShip
			a.w.Launch(it)
			a.say("running · you sign off at the end")
		}
	case "d":
		if it != nil && (it.State == StNew || it.State == StLanded) {
			it.State = StDismissed
			a.say("hidden until it changes")
		}
	case "y":
		if it != nil && it.State == StNeedsYou {
			a.w.Answer(it, true, "")
		}
	case "a":
		if it != nil && it.State == StNew && len(it.Triage.Questions) > 0 {
			it.Triage.Readiness += 30
			it.Triage.Questions = nil
			a.say("two questions drafted for " + it.Author + " · nothing posts without you · [enter] to read")
		}
	case "x":
		if it != nil && (it.State == StRunning || it.State == StNeedsYou || it.State == StQueued) {
			a.w.Stop(it)
		}
	case "n":
		if it != nil && it.State == StNeedsYou {
			a.w.Answer(it, false, "")
		} else {
			a.typing, a.input = "compose", ""
		}
	case "/":
		a.typing, a.input = "filter", a.filter
	case "esc":
		a.filter = ""
		for _, m := range a.w.Items {
			m.Marked = false
		}
	case "]":
		a.repo++
		if a.repo >= len(a.w.Repos) {
			a.repo = -1
		}
		a.cursor = 0
	case "[":
		a.repo--
		if a.repo < -1 {
			a.repo = len(a.w.Repos) - 1
		}
		a.cursor = 0
	case "S":
		a.w.Sleep(8 * time.Hour)
		a.say("you slept 8h · the shift report is what you came back to")
	case "T":
		a.w.Sleep(time.Hour)
	case ">":
		a.w.Speed *= 2
		if a.w.Speed > 20*time.Minute {
			a.w.Speed = 20 * time.Minute
		}
	case "<":
		a.w.Speed /= 2
		if a.w.Speed < 5*time.Second {
			a.w.Speed = 5 * time.Second
		}
	case "P":
		a.paused = !a.paused
	case "A":
		a.backlog = !a.backlog
		a.cursor = 0
	case "ctrl+l":
		a.w.Shift = Shift{Since: a.w.Now}
	}
	return nil
}

// ---- card

var modelNames = []string{"deepseek-v4.1-flash", "qwen3.5-coder", "kimi-k3", "glm-5", "minimax-m3"}

func next(cur string) string {
	for i, m := range modelNames {
		if m == cur {
			return modelNames[(i+1)%len(modelNames)]
		}
	}
	return modelNames[0]
}

func (a *app) cardKey(k string) tea.Cmd {
	it := a.cur
	o := &it.Order
	if it.State != StNew && it.State != StDismissed {
		// A peek at something already on the floor: the stream's own keys, and
		// enter walks in.
		switch k {
		case "esc", "q":
			a.scr = scFloor
			return nil
		case "enter", "o":
			a.walkIn(it)
			return nil
		case "?":
			a.helpFor, a.scr = scCard, scHelp
			return nil
		}
		return a.streamKey(k)
	}
	if k >= "1" && k <= "9" {
		i := int(k[0] - '1')
		if i < len(o.Steps) {
			o.Steps[i].On = !o.Steps[i].On
		}
		return nil
	}
	switch k {
	case "esc", "q":
		a.scr = scFloor
	case "?":
		a.helpFor, a.scr = scCard, scHelp
	case "s":
		a.typing, a.input = "step", ""
	case "enter":
		a.w.Launch(it)
		a.scr = scStream
		a.logTop = -1
		switch o.Gate {
		case GatePlan:
			a.say("it will come back with the plan · nothing is written until you say go")
		case GateShip:
			a.say("running to a PR · you sign off on the proof sheet")
		default:
			a.say("self-shipping · green proof merges without you")
		}
	case "t":
		o.Gate = (o.Gate + 1) % 3
	case "m":
		o.WriteModel = next(o.WriteModel)
	case "M":
		o.ReviewModel = next(o.ReviewModel)
	case "N":
		o.PlanModel = next(o.PlanModel)
	case "+", "=":
		o.Rounds++
	case "-":
		if o.Rounds > 1 {
			o.Rounds--
		}
	case "c":
		switch {
		case o.Cap < 5:
			o.Cap = 5
		case o.Cap < 8:
			o.Cap = 8
		case o.Cap < 15:
			o.Cap = 15
		case o.Cap < 30:
			o.Cap = 30
		default:
			o.Cap = 2
		}
	case "x":
		o.Security = !o.Security
	case "w":
		a.typing, a.input = "words", ""
	case "g":
		if it.Origin == OriginTerminal {
			it.Synced = !it.Synced
			if it.Synced {
				a.say(fmt.Sprintf("opened on github as %s%s · comments flow both ways", it.Repo.Name, it.Ref()))
			}
		}
	case "a":
		if len(it.Triage.Questions) > 0 {
			it.Triage.Questions = nil
			it.Triage.Readiness += 30
			a.say("questions posted to " + it.Author + " · the item waits for the answer")
		}
	case "d":
		it.State = StDismissed
		a.scr = scFloor
	case "b":
		if len(o.Steps) > 0 {
			it.Repo.Steps = append([]Step{}, o.Steps...)
			a.say("banked · every item on " + short(it.Repo) + " carries these steps now")
		}
	}
	return nil
}

// ---- stream

func (a *app) streamKey(k string) tea.Cmd {
	it := a.cur
	switch k {
	case "esc", "q":
		a.scr = scFloor
	case "?":
		a.helpFor, a.scr = scStream, scHelp
	case "j", "down":
		if a.logTop >= 0 {
			a.logTop++
		}
	case "k", "up":
		if a.logTop < 0 {
			a.logTop = max(0, len(it.Stream.Log)-10)
		} else if a.logTop > 0 {
			a.logTop--
		}
	case "G":
		a.logTop = -1
	case "s":
		a.typing, a.input = "steer", ""
	case "y", "n":
		if it.State == StNeedsYou {
			a.w.Answer(it, k == "y", "")
		}
	case "a":
		if it.State == StNeedsYou {
			a.typing, a.input = "answer", ""
		}
	case "x":
		a.w.Stop(it)
	case "p":
		if it.Stream != nil {
			it.Stream.Paused = !it.Stream.Paused
		}
	case "m":
		it.Order.ReviewModel = next(it.Order.ReviewModel)
		a.w.say(it, tokens_said, "said", "reviewer is now "+it.Order.ReviewModel+" · from the next round")
	case "enter":
		if it.State == StLanded {
			a.scr = scSignoff
		}
	}
	return nil
}

// ---- sign-off

func (a *app) signoffKey(k string) tea.Cmd {
	it := a.cur
	allOK := true
	for _, c := range it.Proof {
		allOK = allOK && c.OK
	}
	for _, c := range it.Policy {
		allOK = allOK && c.OK
	}
	switch k {
	case "esc", "q":
		a.scr = scFloor
	case "?":
		a.helpFor, a.scr = scSignoff, scHelp
	case "enter":
		if allOK {
			a.merge(it)
		} else {
			a.typing, a.input = "sendback", ""
		}
	case "a":
		a.edited = true
		a.merge(it)
	case "c":
		a.typing, a.input = "sendback", ""
	case "o":
		a.w.Reverify(it)
		a.scr = scStream
	case "d":
		a.say("the diff is the appendix · " + it.Diff + " · opens in your editor")
	case "x":
		a.w.Stop(it)
		a.scr = scFloor
	case "s":
		a.scr = scStream
	}
	return nil
}

func (a *app) merge(it *Item) {
	if a.w.SignOff(it, a.edited) {
		a.habit = it
	}
	a.scr = scFloor
	a.say("merged · " + it.Repo.Name + " " + it.Ref())
}

func envGet(n string) string { return getenv(n) }
