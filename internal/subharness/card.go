package subharness

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// THE PREVIEW CARD: the one rendering of a harness, and the thing a person
// actually approves.
//
// A sub-harness is built in conversation, which means the shape arrives as JSON
// a model wrote. Nobody approves JSON. What somebody can approve is a plain
// account of what will happen, in the order it happens, with the choices and
// the rounds and the checks written where they occur — because the question
// being asked is not "is this valid", it is "is this what I meant", and that
// question is answered by reading the order of events.
//
//	triage-flake · v2 · chase a flaky test to a fix
//
//	1  start    starts when you run it  · takes since, label
//
//	2  name-it  name the test that failed and why  · reads files, searches inside files · up to 8 turns
//
//	3  pick
//	   if contains flaky → rerun
//	     ·  rerun   runs a command — go test -run TestFoo -count 20
//	     ·  tries   repeat until ok (up to 3 times)
//	     ·  check   check — go test ./...
//	     ·  land    asks you — land the fix?
//	   otherwise → explain
//	     ·  explain  say why it is not flaky
//
//	can use · reads files · searches inside files · runs commands
//	may pick its own way as it runs, up to 4 times
//	checks its own work and fixes what it finds
//
// ── NOT ONE WORD OF MACHINERY ──
//
// This card once printed the node KIND beside every step — `agent.loop`,
// `parallel.split`, `verify` — plus `width 3`, `join all`, `invariants`, and
// the raw tool ids off each node's whitelist slice. Every one of those is a name
// for something inside this binary, and the person being asked to keep the
// recipe has never seen inside this binary. They read a column of vocabulary
// that could only be understood by somebody who did not need the card.
//
// So STRUCTURE IS SHOWN AND NEVER NAMED. A fan-out is a heading and an indent;
// a choice is `if …` and `otherwise`; a bounded loop is `repeat until … (up to
// 3 times)`; a gather prints NOTHING AT ALL, because the outdent back to the
// main flow is already what it means and a row saying "join" would be a row
// about the drawing rather than about the work. The one presentation table on
// this page ([cardToolWords]) turns a belt tool's registered name into what it does,
// and falls through to the name itself for anything it has never heard of.
//
// ── THE STEPS ARE THE RUN'S OWN ORDER ──
//
// A program is a DAG with its edges written down (registry.go), and the walk
// that runs it is a topological one (run.go) — so the card is numbered in that
// same order and a person reading it top to bottom has read the run. NUMBERS
// BELONG TO THE MAIN FLOW ONLY. Anything reachable exclusively through one lane
// of a fan-out or one arm of a choice is drawn once, nested under the thing that
// opens it, with a dim bullet instead of a number: it is not a step of the run,
// it is a step of that lane, and a flat numbered list said otherwise.
//
// ONE RENDERER, EVERY SURFACE. The chat tool prints these lines into a tool
// result, the TUI panel draws the same lines into a block, and a test asserts
// against them. A second rendering would be a second thing that could disagree
// with the program it claims to describe — and the approval is given against
// what was drawn, not against what was stored.

const (
	// cardDetailCap bounds one step's detail. A brief is paragraphs; a card row
	// is a row, and the whole brief is one `show` away. It is smaller than it
	// was because the row now starts at an indent, and a detail sized for
	// column zero would run off the edge of every nested lane.
	cardDetailCap = 56

	// cardStep is one nesting level, in spaces. Indentation is the ONLY thing
	// this card has for depth — no box drawing, no rules, no borders — so it is
	// generous enough to be read at a glance and identical at every level.
	cardStep = 5

	// cardNameCap bounds the column a step's name is padded to. One column for
	// the whole card rather than one per level, so the details line up down the
	// page; a name longer than this simply pushes its own detail along rather
	// than dragging every other row right with it.
	cardNameCap = 20
)

// cardLanesWord heads a fan-out's lanes. It says what happens without naming
// what does it, which is this card's whole law about structure.
const cardLanesWord = "side by side, one lane each:"

// cardOtherwiseWord is a choice's second arm. exec.go's branch takes the FIRST
// successor when the condition holds and the SECOND when it does not, so these
// two words are the run's real behaviour and not a convention of the drawing.
const cardOtherwiseWord = "otherwise"

// cardNeverWord labels an arm past the second. exec.go picks between exactly
// two, so a third edge out of a choice is a shape nothing will ever walk —
// and a card that quietly drew it as another `otherwise` would be hiding the
// one fact a person needs in order to notice the mistake.
const cardNeverWord = "never taken"

// cardToolWords is the ONE presentation table on this card: a tool's registered
// name beside what it DOES, in the words of somebody who has never read this
// repository. The names are the harness belt's own (session's harness_belt.go —
// the seven wire tools plus the media family), because that is the entire set a
// whitelist may legally hold.
//
// It is presentation and nothing else. No behaviour reads it, no whitelist is
// checked against it, and a name it has never heard of falls through to itself
// rather than to a shrug — which is the whole reason a table is acceptable here:
// the card has to survive a whitelist naming a verb this build does not have,
// and that is precisely the page a person most needs to be able to read.
//
// TWO FORMS, because one call and one permission are different sentences. A
// fixed tool call does a thing once ("runs a command"); a loop step is ALLOWED a
// thing for as long as it runs ("runs commands").
var cardToolWords = map[string]struct{ once, can string }{
	"read":           {"reads a file", "reads files"},
	"write":          {"writes a file", "writes files"},
	"edit":           {"edits a file", "edits files"},
	"bash":           {"runs a command", "runs commands"},
	"grep":           {"searches inside files", "searches inside files"},
	"find":           {"finds a file by name", "finds files by name"},
	"ls":             {"lists a folder", "lists folders"},
	"generate_image": {"makes an image", "makes images"},
	"view_image":     {"looks at an image", "looks at images"},
	"generate_video": {"makes a video", "makes videos"},
	"generate_music": {"makes music", "makes music"},
	"speak":          {"says something aloud", "speaks aloud"},
}

// toolOnce is what calling this tool once does.
func toolOnce(name string) string {
	if words, known := cardToolWords[name]; known {
		return words.once
	}
	return name
}

// toolCan is what being allowed this tool means.
func toolCan(name string) string {
	if words, known := cardToolWords[name]; known {
		return words.can
	}
	return name
}

// toolCanList translates a list of tool names and drops the repeats. Two names
// can mean one sentence — a step allowed both `grep` and `find` is a step that
// looks through files — and a person reading "searches inside files · searches
// inside files" would be reading about the table rather than about the work.
func toolCanList(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		word := toolCan(strings.TrimSpace(name))
		if word == "" || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	return out
}

// dynSentence is the dynamism rung as a sentence about the run, with the budget
// folded into it. The rungs and what each one unlocks are registry.go's, and the
// wording here is one sentence per rung of that same ladder.
//
// `fixed` decides nothing and therefore says nothing: a line reading "decides
// nothing" is a line about the absence of a feature, which is what the emptiness
// law is for.
func dynSentence(d Dyn) string {
	within := ""
	if d.Cap > 0 {
		within = fmt.Sprintf(", up to %d times", d.Cap)
	}
	switch d.Ladder {
	case DynBranch:
		return "may pick its own way as it runs" + within
	case DynWidth:
		if d.Cap > 0 {
			return fmt.Sprintf("may open up to %d extra lanes while it runs", d.Cap)
		}
		return "may decide how many lanes to open while it runs"
	case DynMeta:
		return "may rewrite its own instructions as it runs" + within
	case DynRecursive:
		return "may hand work to other saved recipes as it runs" + within
	case DynSelfmod:
		return "may save a new version of itself" + within
	}
	return ""
}

// verifySentence is the verification rung as a sentence about the work, never as
// the rung's own word. `accept` believes whatever it is handed and so says
// nothing at all, and an unrecognised rung says nothing rather than guessing.
func verifySentence(v Verify) string {
	switch v.Ladder {
	case VerifySchema:
		return "checks the shape of what it made"
	case VerifyInvariants:
		return "checks its own work against the rules it was given"
	case VerifyLoop:
		return "checks its own work and fixes what it finds"
	case VerifyReport:
		return "checks its own work and writes up what it found"
	case VerifyRederive:
		return "checks its own work by doing it a second way"
	case VerifyAdversarial:
		return "checks its own work by trying to break it"
	case VerifyHuman:
		return "asks you to look before it is done"
	}
	return ""
}

// Card is the harness as a person reads it, as one block of text.
func Card(h Harness) string { return strings.Join(CardLines(h), "\n") }

// CardLines is the card, one string per line, unstyled. Nothing here is padded
// to a width: the surfaces that care about width own their own fitting, and a
// renderer that guessed at one would be guessing for the narrowest reader.
func CardLines(h Harness) []string {
	h = h.Normalize()
	lines := []string{cardHead(h), ""}
	lines = append(lines, stepLines(h.Program)...)
	if foot := cardFoot(h); len(foot) > 0 {
		lines = append(lines, "")
		lines = append(lines, foot...)
	}
	return lines
}

// cardHead is the identity line: what it is called, which version, and what it
// is for.
func cardHead(h Harness) string {
	parts := []string{"(unnamed)"}
	if name := strings.TrimSpace(h.Id.Name); name != "" {
		parts[0] = name
	}
	if h.Id.Version > 0 {
		parts = append(parts, fmt.Sprintf("v%d", h.Id.Version))
	} else {
		// A draft has no version because nothing has accepted it yet. Saying so
		// is the difference between a card somebody is being asked to approve
		// and a card of something already registered.
		parts = append(parts, "draft")
	}
	if note := strings.TrimSpace(h.Id.Desc); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, " · ")
}

// cardFoot is the bounds, one quiet sentence at a time: what it may reach, how
// much shape it may grow, how hard it checks itself, and what it has been tried
// on. These are what a person is really approving — the steps say what it does
// today, and the foot says what it is ALLOWED to do.
//
// Every line of it is conditional. A recipe that decides nothing, checks nothing
// and was tried on nothing prints one line, because a foot padded out with
// "dynamism  fixed" and "verify  accept" is four lines saying that four features
// are switched off.
func cardFoot(h Harness) []string {
	var lines []string
	if words := toolCanList(h.Whitelist); len(words) > 0 {
		lines = append(lines, "can use · "+strings.Join(words, " · "))
	} else {
		// An empty whitelist is not an unknown, it is a BOUND, and it is the
		// strongest one this card can report: a recipe that touches nothing.
		// The emptiness law is about zeroes standing in for facts nobody has,
		// and this is a fact somebody chose.
		lines = append(lines, "can use · nothing")
	}
	if sentence := dynSentence(h.Dyn); sentence != "" {
		lines = append(lines, sentence)
	}
	if sentence := verifySentence(h.Verify); sentence != "" {
		lines = append(lines, sentence)
	}
	if len(h.Tests) > 0 {
		names := make([]string, 0, len(h.Tests))
		for _, test := range h.Tests {
			names = append(names, test.Name)
		}
		lines = append(lines, "tried on · "+strings.Join(names, " · "))
	}
	return lines
}

// ── the walk that draws the shape ───────────────────────────────────────────

// cardWalk is one drawing of one program. It carries the order the run takes and
// a record of what has already been put on the page, because a node drawn inside
// a lane must not appear again on the main flow — the old card listed a split's
// lanes twice, once as a preview under the split and once more as numbered steps
// of their own, and there is no reading of that which is not "it happens twice".
type cardWalk struct {
	p     Program
	order []string
	pad   int
	drawn map[string]bool
}

// stepLines renders the program in the order it runs.
//
// A cycle is drawn in file order rather than refused: the card is what a person
// reads to find out a shape is wrong, so it has to survive the shapes Validate
// is about to reject.
func stepLines(p Program) []string {
	order, err := topo(p)
	if err != nil {
		order = make([]string, len(p.Nodes))
		for at, node := range p.Nodes {
			order[at] = node.Id
		}
	}
	w := &cardWalk{p: p, order: order, pad: cardPad(p), drawn: map[string]bool{}}

	// The main flow is everything that is not the exclusive property of some
	// lane or arm. Those are drawn where they are chosen, by [cardWalk.nest],
	// and claiming them up front is what keeps the main flow's numbers meaning
	// "steps of the run" rather than "nodes in the file".
	//
	// THE CLAIM AND THE DRAWING ASK ONE FUNCTION ([cardWalk.owned]), because a
	// claim the nesting then declines to draw is a step that vanishes off the
	// card altogether — and a card that silently omits a step is worse than the
	// one full of machinery it replaced.
	claimed := map[string]bool{}
	for _, id := range order {
		node, found := p.Node(id)
		if !found {
			continue
		}
		for _, inside := range w.owned(node) {
			claimed[inside] = true
		}
	}
	main := make([]string, 0, len(order))
	for _, id := range order {
		if !claimed[id] {
			main = append(main, id)
		}
	}
	return w.run(main, 0, true)
}

// owned is every node this step will draw UNDER itself: the lanes of a fan-out
// and the arms of a choice whose target nothing else leads to, each one whole.
// Anything else — an ordinary step, a gather, an arm pointing back into work
// that happens either way — owns nothing and hands on to the next row.
//
// It is the single answer to "where does this node get drawn", asked once by the
// main flow deciding what to leave out and once by [cardWalk.nest] drawing it.
func (w *cardWalk) owned(node Node) []string {
	var out []string
	for _, next := range w.p.Successors(node.Id) {
		switch node.Kind {
		case KindParallelSplit:
			// A fan-out wired straight to its own gather is a lane with no work
			// in it, and the gather says nothing wherever it is reached from.
			if lane, found := w.p.Node(next); found && lane.Kind == KindParallelJoin {
				continue
			}
		case KindBranch:
			if !w.exclusive(next) {
				continue
			}
		default:
			continue
		}
		out = append(out, w.scope(next)...)
	}
	return out
}

// cardPad is the column every step's detail starts in, before indentation.
func cardPad(p Program) int {
	pad := 0
	for _, node := range p.Nodes {
		if width := len([]rune(node.Id)); width > pad {
			pad = width
		}
	}
	if pad > cardNameCap {
		pad = cardNameCap
	}
	return pad
}

// body is every node reachable ONLY through head — the work that belongs to one
// lane or one arm and to nothing else.
//
// The rule is the one a person would use reading the arrows: a node is inside
// when every arrow into it comes from inside. Because [cardWalk.order] is
// topological, a node's predecessors have all been decided by the time it is
// reached, so one pass answers it. A gather has arrows from several lanes and is
// therefore inside none of them, which is exactly why the outdent back to the
// main flow lands where it does.
func (w *cardWalk) body(head string) []string {
	from := -1
	for at, id := range w.order {
		if id == head {
			from = at
			break
		}
	}
	if from < 0 {
		return nil
	}
	inside := map[string]bool{head: true}
	var out []string
	for _, id := range w.order[from+1:] {
		preds := w.p.Predecessors(id)
		if len(preds) == 0 {
			continue
		}
		all := true
		for _, pred := range preds {
			if !inside[pred] {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		inside[id] = true
		out = append(out, id)
	}
	return out
}

// scope is a lane or an arm whole: its head and everything only it can reach.
func (w *cardWalk) scope(head string) []string {
	return append([]string{head}, w.body(head)...)
}

// exclusive reports whether a node is reached one way only. An arm pointing at a
// node several things lead to is a POINTER and stays one — nesting the shape of
// the whole rest of the program under one arm of one choice would say that the
// rest only happens if that choice goes that way.
func (w *cardWalk) exclusive(id string) bool { return len(w.p.Predecessors(id)) == 1 }

// run draws a run of steps at one level. Numbers are the main flow's alone; every
// nested level gets a bullet, and the blank line between top-level steps is the
// separation that makes the numbers a rhythm instead of a wall.
func (w *cardWalk) run(scope []string, level int, number bool) []string {
	var lines []string
	count := 0
	for _, id := range scope {
		if w.drawn[id] {
			continue
		}
		node, found := w.p.Node(id)
		if !found {
			continue
		}
		w.drawn[id] = true
		// A GATHER DRAWS NOTHING. Coming back from side by side to one thread is
		// already said by the indent ending, and a row for it would be the card
		// describing its own layout.
		if node.Kind == KindParallelJoin {
			continue
		}
		count++
		marker := "·"
		if number {
			marker = strconv.Itoa(count)
			if count > 1 {
				lines = append(lines, "")
			}
		}
		lines = append(lines, w.row(node, marker, level))
		lines = append(lines, w.nest(node, level)...)
	}
	return lines
}

// nest draws what a step CHOOSES BETWEEN, under the step that chooses. An
// ordinary step's successor is simply the next row; a fan-out's lanes and a
// choice's arms are facts about this step rather than about what comes after it,
// and they are the one thing in a program somebody has to be able to point at.
func (w *cardWalk) nest(node Node, level int) []string {
	switch node.Kind {
	case KindParallelSplit:
		var lanes []string
		for _, next := range w.p.Successors(node.Id) {
			// A split wired straight to its own gather is a lane with no work
			// in it, and the gather says nothing wherever it is reached from.
			if lane, found := w.p.Node(next); found && lane.Kind == KindParallelJoin {
				continue
			}
			lanes = append(lanes, w.run(w.scope(next), level+1, false)...)
		}
		if len(lanes) == 0 {
			return nil
		}
		return append([]string{cardHeading(level+1, cardLanesWord)}, lanes...)

	case KindBranch:
		var lines []string
		for at, next := range w.p.Successors(node.Id) {
			word := cardOtherwiseWord
			switch {
			case at == 0:
				word = "if " + clipDetail(firstLine(node.Fields.Get("when")))
			case at > 1:
				word = cardNeverWord
			}
			lines = append(lines, cardHeading(level+1, word+" → "+next))
			if w.exclusive(next) {
				lines = append(lines, w.run(w.scope(next), level+1, false)...)
			}
		}
		return lines
	}
	return nil
}

// row is one step: its marker, its name, and what it does. The marker sits at the
// level's own column and the name always three cells past it, so that a bullet
// and a number occupy the same shape and the eye counts depth by indent alone.
func (w *cardWalk) row(node Node, marker string, level int) string {
	lead := strings.Repeat(" ", cardStep*level) + marker
	if gap := 3 - len([]rune(marker)); gap > 0 {
		lead += strings.Repeat(" ", gap)
	} else {
		lead += " "
	}
	name := node.Id
	detail := stepDetail(node)
	if pad := w.pad - len([]rune(name)); pad > 0 && detail != "" {
		name += strings.Repeat(" ", pad)
	}
	return strings.TrimRight(lead+name+"  "+detail, " ")
}

// cardHeading is a structure line — the lanes' heading, a choice's arm — set one
// step in from the step it belongs to, which puts it directly under that step's
// name rather than under its number.
func cardHeading(level int, text string) string {
	indent := cardStep*level - 2
	if indent < 0 {
		indent = 0
	}
	return strings.Repeat(" ", indent) + text
}

// stepDetail is the right-hand side of one row: what this particular step does,
// said the way somebody would say it out loud, out of the fields its kind
// actually uses. The kind itself is never printed and neither is any field name.
func stepDetail(node Node) string {
	f := node.Fields
	switch node.Kind {
	case KindAgentLoop:
		var meta []string
		if words := toolCanList(splitList(f.Get("tools"))); len(words) > 0 {
			meta = append(meta, strings.Join(words, ", "))
		}
		if model := f.Get("model"); model != "" {
			meta = append(meta, model)
		}
		if turns := f.Int("max_turns", 0); turns > 0 {
			meta = append(meta, fmt.Sprintf("up to %d turns", turns))
		}
		return cardDetail(clipDetail(firstLine(f.Get("brief"))), meta)

	case KindToolCall:
		return cardPhrase(toolOnce(f.Get("tool")), clipDetail(firstLine(f.Get("args"))))

	case KindBranch:
		// The arms carry the whole of it: the condition is written on the arm it
		// holds for, where a person is looking when they ask which way it went.
		return ""

	case KindLoopUntil:
		return fmt.Sprintf("repeat until %s (up to %d times)",
			clipDetail(firstLine(f.Get("until"))), f.Int("max_rounds", DefaultRounds))

	case KindParallelSplit:
		detail := ""
		if over := f.Get("over"); over != "" {
			detail = "across " + clipDetail(firstLine(over))
		}
		var meta []string
		if width := f.Int("width", 1); width > 1 {
			meta = append(meta, fmt.Sprintf("up to %d at once", width))
		}
		return cardDetail(detail, meta)

	case KindParallelJoin:
		return ""

	case KindHumanGate:
		return cardPhrase("asks you", clipDetail(firstLine(f.Get("ask"))))

	case KindVerify:
		// The node's own rung is deliberately not here. The foot already says in
		// a sentence how hard this recipe checks itself, and a rung's word
		// repeated down the middle of the page would be the ladder's vocabulary
		// beside every check that uses it.
		return cardPhrase("check", clipDetail(firstLine(f.Get("check"))))

	case KindSubharnessCall:
		var meta []string
		if version := f.Int("version", 0); version > 0 {
			meta = append(meta, fmt.Sprintf("version %d", version))
		}
		return cardDetail(cardPhrase("runs the saved recipe", f.Get("name")), meta)

	case KindTrigger:
		detail := ""
		switch source := f.Get("source"); source {
		case TriggerHosted:
			detail = "starts when you run it"
		case TriggerIdle:
			detail = "starts when nothing else is happening"
			if spec := f.Get("spec"); spec != "" {
				detail += " for " + clipDetail(firstLine(spec))
			}
		case TriggerWatch:
			detail = "starts when something it watches changes"
			if spec := f.Get("spec"); spec != "" {
				detail = "starts when " + clipDetail(firstLine(spec)) + " changes"
			}
		case TriggerCommand:
			detail = cardPhrase("starts by running", clipDetail(firstLine(f.Get("command"))))
		default:
			detail = cardPhrase("starts on", clipDetail(source))
		}
		var meta []string
		if args := splitList(f.Get("args")); len(args) > 0 {
			meta = append(meta, "takes "+strings.Join(args, ", "))
		}
		return cardDetail(detail, meta)
	}
	return ""
}

// cardPhrase joins what a step does to the thing it does it to. The dash is used
// rather than a colon because these read as sentences — "asks you — land the
// fix?" — and a step whose subject is missing is left as the verb alone rather
// than as a verb with a shrug after it.
func cardPhrase(word, rest string) string {
	if rest = strings.TrimSpace(rest); rest == "" {
		return strings.TrimSpace(word)
	}
	if word = strings.TrimSpace(word); word == "" {
		return rest
	}
	return word + " — " + rest
}

// cardDetail sets a step's meta — what it may reach, how many turns it has — off
// behind the thing it is meta ABOUT, with a wider gap than the meta's own
// separator so that the tail reads as one quiet aside rather than as more of the
// sentence.
func cardDetail(text string, meta []string) string {
	tail := strings.Join(meta, " · ")
	switch {
	case tail == "":
		return text
	case strings.TrimSpace(text) == "":
		return tail
	}
	return text + "  · " + tail
}

// ── the run, read back ──────────────────────────────────────────────────────

// RunLines is one trace as a person reads it: the path the run actually took,
// one row per step, with the rounds and the failure where they landed.
//
// It is the card's twin and it is deliberately the same shape — id on the left,
// kind beside it, detail on the right — because the two are read against each
// other. The question a person opens a trace with is "where did this differ from
// the card", and two renderings with different columns would make them find that
// out by eye.
func RunLines(t Trace) []string {
	head := t.Id.Name
	if head == "" {
		head = "(unnamed)"
	}
	head += fmt.Sprintf(" · v%d · %s", t.Id.Version, t.status())
	if t.Elapsed > 0 {
		head += " · " + t.Elapsed.Round(time.Millisecond).String()
	}
	lines := []string{head, ""}
	for _, step := range t.Trail {
		lines = append(lines, StepLine(step))
	}
	if t.Spent > 0 {
		lines = append(lines, "", fmt.Sprintf("spent  %d of the dynamism budget", t.Spent))
	}
	return lines
}

// RunCard is [RunLines] as one block of text.
func RunCard(t Trace) string { return strings.Join(RunLines(t), "\n") }

// StepLine is one executed step as a person reads it: the mark, the number, the
// id, the kind, and then what it left behind or what went wrong with it.
//
// It is exported because a surface watching a run LIVE ([RunWatched]) draws the
// same step the card draws when the trace is read back afterwards, and two
// renderings of one step would let the live row and the report disagree about
// what happened — which is the drift [RunLines] itself is written against, one
// column set over.
func StepLine(step Trail) string {
	mark := "✓"
	if step.Err != "" {
		mark = "✗"
	}
	row := fmt.Sprintf("%s %-3d %-12s %-14s", mark, step.Step, step.Id, step.Kind)
	detail := []string{}
	if step.Err != "" {
		detail = append(detail, step.Err)
	} else if step.Out != "" {
		detail = append(detail, clipDetail(firstLine(step.Out)))
	}
	if step.Elapsed > 0 {
		detail = append(detail, step.Elapsed.Round(time.Millisecond).String())
	}
	return strings.TrimRight(row+" "+strings.Join(detail, " · "), " ")
}

// join renders a trace row's parts and drops the empties: a row that says
// "hosted · " is a row with a shrug on the end of it.
func join(parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return text
}

// clipDetail bounds one detail on a rune boundary.
func clipDetail(text string) string {
	if len(text) <= cardDetailCap {
		return text
	}
	cut := cardDetailCap - len("…")
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut] + "…"
}
