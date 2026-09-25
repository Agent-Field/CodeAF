package crewroute

import (
	"regexp"
	"strings"
)

// WHAT KIND OF WORK A TASK IS, READ OFF ITS OWN WORDS.
//
// The router routes on a task's CLASS and on nothing finer, by design
// (docs/design/model-pool/pareto-crewing.pdf): the class decides the crew. A
// narrow fix goes to the cheapest qualified crew, and open-ended work (a
// feature, a refactor, docs, a design) gets a strong checker behind it. So the
// question this file answers is the only one the router asks of the text:
// which of the two is this?
//
// ── THE SIGNALS ──
//
// It is rules, not a model, and every rule is written down here so a person
// asking "why did it call my task a bugfix" is answered by reading them:
//
//   - LABELS weigh most. An issue labelled `bug` or `enhancement` is a person
//     telling us the answer.
//   - THE TITLE weighs more than the body. A conventional-commit prefix
//     (`fix:`, `feat:`, `refactor:`), a `[bug]` tag or a leading verb
//     (`add`, `support`, `document`) is the author classifying their own work.
//   - THE BODY adds the rest: a stack trace, an exception's name, "crashes",
//     "regression", "steps to reproduce" lean narrow; "feature request",
//     "would be great", "consider adding", "refactor" lean open-ended.
//
// A harness's own wrapper around an issue — "Implement issue #12: …" and the
// standing instruction to keep the test suite green that follows every task
// it hands out — is read past, because it is the same sentence on every task
// and a signal that fires on everything tells the router nothing.
//
// ── UNCERTAIN IS OPEN-ENDED ──
//
// When the signals do not clearly favour one class, the answer is
// [OpenEnded], and that is the safe side on purpose: calling a narrow fix
// open-ended costs a strong checker it did not need — cents — while calling
// open-ended work a narrow fix sends it to a crew without the checker that
// work needs. The two mistakes are not the same size, so the tie does not go
// to the cheaper one.

// Class is the kind of work a task is. It is a string because it is written
// into the router's event log and onto a task's card, and a script reading
// either reads the word.
type Class string

const (
	// Bugfix is a NARROW, VERIFIABLE change: a defect, a regression, a missing
	// check, a test that should exist. It goes to the cheapest qualified crew.
	Bugfix Class = "bugfix"
	// OpenEnded is work whose shape the task does not fix: a feature, a
	// refactor, documentation, a design. It gets a strong checker: the checker
	// is the seat worth paying for here.
	OpenEnded Class = "openended"
	// Other is work that changes nothing in particular — a question, an
	// investigation, a review. It is read on the average of the other two
	// classes' links, because the weights carry none of its own.
	Other Class = "other"
)

// Classes lists the three, in the order a report prints them.
// Word is the class as a person reads it: `open-ended` rather than the
// logged `openended`, which stays the key the router's log and the evidence
// table are written in.
func (c Class) Word() string {
	if c == OpenEnded {
		return "open-ended"
	}
	return string(c)
}

var Classes = []Class{Bugfix, OpenEnded, Other}

// Task is what the classifier reads: the words a person or an issue gave the
// work, and the labels the issue carried when it came from a tracker.
type Task struct {
	Text   string
	Labels []string
}

// Reading is the classifier's answer: the class, the one line saying which
// signal decided it, and whether the signals were clear. Sure false is the
// uncertain case, which always reads [OpenEnded].
type Reading struct {
	Class Class
	Why   string
	Sure  bool
	// Complex is, for a bugfix, the signals that make it a fix with reach
	// ([complexFix]), in words; empty is a simple fix. It names no class of
	// its own — the task is still a bugfix on every line a person reads — and
	// it moves only the worker ([Decide]).
	Complex string
}

// signal is one rule: a pattern, the class it leans toward, how hard, and
// the words the reading names it by when it decides.
type signal struct {
	pattern *regexp.Regexp
	class   Class
	weight  int
	name    string
}

// The weights, spelled once. A label is a person's own answer and outweighs
// anything read off prose; a title is the author's own summary and outweighs
// a body, where the same word may be describing somebody else's code.
const (
	weightLabel = 4
	weightTitle = 2
	weightBody  = 1
	// clearMargin is how far one class must lead the other before the reading
	// is SURE. One point is a single body word, which is not a decision.
	clearMargin = 2
)

// titleSignals are read off the first line of the task, after any harness
// wrapper is removed ([taskTitle]).
var titleSignals = []signal{
	{regexp.MustCompile(`(?i)^\s*(fix|bugfix|hotfix)(\([^)]*\))?!?:`), Bugfix, 2, "a fix: title"},
	{regexp.MustCompile(`(?i)^\s*(test|tests|ci|chore|build)(\([^)]*\))?!?:`), Bugfix, 1, "a narrow chore title"},
	{regexp.MustCompile(`(?i)\[\s*bug\s*\]|^\s*bug\s*[:\-]|^\s*BUG\b`), Bugfix, 2, "a bug tag in the title"},
	{regexp.MustCompile(`(?i)\b(crash(es|ed|ing)?|error|exception|fails?|failing|broken|regression|drops?|rejects?|ignored|incorrect(ly)?|wrong|overflow\w*|hangs?|leaks?)\b`), Bugfix, 1, "a failure word in the title"},
	{regexp.MustCompile(`\b[A-Z][A-Za-z]+(Error|Exception)\b`), Bugfix, 1, "an exception named in the title"},
	// A DEFECT SAID IN PLAIN WORDS is a fix whether or not it wears a `fix:`
	// prefix: a leading verb that repairs something, the word bug or broken,
	// and a sentence that sets what happens against what should.
	{regexp.MustCompile(`(?i)^\s*(please\s+)?(fix|repair|correct|resolve|patch)\b`), Bugfix, 2, "a title that asks for a repair"},
	{regexp.MustCompile(`(?i)\b(bug|bugs|buggy|broken)\b`), Bugfix, 1, "the word bug or broken"},
	{defectSentence, Bugfix, 2, "what happens set against what should"},
	// A TITLE THAT SAYS WHAT THE CODE DOES NOT DO is a defect report: "never
	// keeps a square image", "does not close the file", "fails to parse",
	// "should reject". "should support" and its kin ask for something new and
	// are weighed back on the open-ended side below.
	{regexp.MustCompile(`(?i)\b(never|doesn't|doesn’t|does not|don't|don’t|do not|fails to|failed to|cannot|can't|can’t|won't|won’t|no longer|should(n't|n’t| not)?)\b`), Bugfix, 1, "a title that says what the code does not do"},
	// A CALL WRITTEN OUT IN THE TITLE — `RandomResizedCrop(scale=(1, 1))`,
	// `parse("")` — is somebody quoting the input that misbehaves.
	{regexp.MustCompile(`\b[A-Za-z_][\w.]*\((?:[^()]|\([^()]*\))+\)`), Bugfix, 1, "a call written out in the title"},
	{regexp.MustCompile(`(?i)\bshould (also )?(support|allow|accept|provide|offer|expose|have|be able)\b`), OpenEnded, 1, "a title asking for something the code should also do"},
	{regexp.MustCompile(`(?i)^\s*(feat|feature|refactor|perf|docs?)(\([^)]*\))?!?:`), OpenEnded, 2, "a feature or refactor title"},
	{regexp.MustCompile(`(?i)^\s*(add|support|implement|introduce|enable|allow|create|design|define|document|redesign|rework|restructure|migrate|extend|expose|make)\b`), OpenEnded, 2, "a title that asks for something new"},
	{regexp.MustCompile(`(?i)\b(consider|proposal|rfc|epic|feature|enhancement|refactor|docs|documentation|more pythonic|should (probably )?(default|be|support|allow)|fractional|metadata)\b`), OpenEnded, 1, "an open-ended word in the title"},
}

// bodySignals are read off everything after the title.
var bodySignals = []signal{
	{regexp.MustCompile(`Traceback \(most recent call last\)|(?m)^\s+at [\w.$]+\(|panic: |goroutine \d+ \[`), Bugfix, 2, "a stack trace"},
	{regexp.MustCompile(`\b[A-Z][A-Za-z]*(Error|Exception)\b`), Bugfix, 1, "an exception in the text"},
	{regexp.MustCompile(`(?i)\b(steps to reproduce|to reproduce|describe the bug|expected behaviou?r|actual behaviou?r|minimal (reproducible )?example)\b`), Bugfix, 2, "a bug report's own headings"},
	{defectSentence, Bugfix, 2, "what happens set against what should"},
	{regexp.MustCompile(`(?i)\b(failing|failed|fails) (unit |integration )?tests?\b|\btests? (is |are )?(failing|fail|broken)\b`), Bugfix, 1, "a failing test named"},
	{regexp.MustCompile(`(?i)\b(crash(es|ed)?|regression|exit code \d+|stack ?trace|segfault|fails with|raises|silently (drops?|discards?|ignores?)|returns? (the )?wrong)\b`), Bugfix, 1, "a failure described in the text"},
	{regexp.MustCompile(`(?i)\b(is your feature request|feature request|would be (really )?(great|nice|useful|helpful)|it'?d be great|consider adding|nice to have|enhancement|design (doc|proposal)|user stor(y|ies))\b`), OpenEnded, 2, "a feature request's own words"},
	{regexp.MustCompile(`(?i)\b(refactor(ing)?|restructur(e|ing)|redesign|extensib(le|ility)|new (api|option|parameter|feature|command|endpoint|page|metric)s?|not well documented|documentation)\b`), OpenEnded, 1, "open-ended work described in the text"},
}

// otherSignals mark work that changes nothing in particular. They are read
// off the title only, because a question's body is full of the words a bug
// report uses.
var otherTitle = regexp.MustCompile(`(?i)^\s*(explain|investigate|why\b|how (do|does|can|should)|what (is|does|are)|review|audit|summari[sz]e|analy[sz]e|research|benchmark|compare)\b`)

// labelWords maps an issue label, folded, to the class it names.
var labelWords = map[string]Class{
	"bug": Bugfix, "type: bug": Bugfix, "kind/bug": Bugfix, "defect": Bugfix, "regression": Bugfix,
	"crash": Bugfix, "type:bug": Bugfix, "bugfix": Bugfix, "fix": Bugfix,
	"enhancement": OpenEnded, "feature": OpenEnded, "feature request": OpenEnded, "type: feature": OpenEnded,
	"kind/feature": OpenEnded, "refactor": OpenEnded, "documentation": OpenEnded, "docs": OpenEnded,
	"design": OpenEnded, "proposal": OpenEnded, "rfc": OpenEnded, "epic": OpenEnded,
	"question": Other, "investigation": Other, "discussion": Other,
}

// labelKeys are the words a tracker's own label spellings are built on —
// `bug :bug:`, `type/bug`, `Kind: Enhancement ✨` — read when the label is
// not one of [labelWords] as written. Labels with none of them (`help
// wanted`, `good first issue`) say nothing about the kind of work.
var labelKeys = map[string]Class{
	"bug": Bugfix, "bugs": Bugfix, "defect": Bugfix, "regression": Bugfix, "crash": Bugfix, "bugfix": Bugfix,
	"enhancement": OpenEnded, "feature": OpenEnded, "refactor": OpenEnded, "documentation": OpenEnded,
	"docs": OpenEnded, "proposal": OpenEnded, "rfc": OpenEnded, "epic": OpenEnded,
	"question": Other, "investigation": Other, "discussion": Other,
}

// labelWordRun is one run of letters in a folded label.
var labelWordRun = regexp.MustCompile(`[a-z]+`)

// labelClass is the class a label names: as written first, then word by
// word, with an emoji shortcode (`:bug:`) read as its word.
func labelClass(label string) (Class, bool) {
	folded := strings.ToLower(strings.TrimSpace(label))
	if class, ok := labelWords[folded]; ok {
		return class, true
	}
	if strings.Contains(folded, "🐛") {
		return Bugfix, true
	}
	for _, word := range labelWordRun.FindAllString(folded, -1) {
		if class, ok := labelKeys[word]; ok {
			return class, true
		}
	}
	return "", false
}

// wrapperLine is the harness's own sentence around an issue's title:
// "Implement issue #12: [bug] export drops the last row". The verb is
// the harness talking and the tail is the issue's own title.
var wrapperLine = regexp.MustCompile(`(?i)^\s*(implement|fix|resolve|address|solve|close|work on)\s+(github\s+)?issue\s+#?\d+\s*[:\-—]\s*(.*)$`)

// defectSentence is a sentence that sets what happens against what should:
// "returns a - b instead of a + b", "should keep the order but drops it",
// "expected 3, got 4".
var defectSentence = regexp.MustCompile(`(?i)\b(returns?|gives?|prints?|produces?|yields?|shows?)\b[^.]*\binstead of\b|\bshould\b[^.]*\bbut\b|\bexpected\b[^.]*\b(got|but|received|instead)\b`)

// tinyEdit is a change too small to be open-ended whatever verb it opens
// with: a docstring, a typo, a comment, one line. It reads as work of no
// particular class — the cheap crew — and it outweighs a leading "add".
var tinyEdit = regexp.MustCompile(`(?i)\b(docstring|docstrings|typo|typos|spelling|misspell\w*|one[- ]line|single[- ]line|whitespace|a comment|code comment|trailing comma)\b`)

// boilerplate is the standing instruction a harness appends to every task.
// It is cut before the body is read, because it is the same on every task.
var boilerplate = regexp.MustCompile(`(?is)work in this repository\..*$`)

// Classify reads a task's class. It is pure and allocation-light: the rules
// are compiled once at load, and a task is read in a single pass per rule.
func Classify(task Task) Reading {
	title, body := taskTitle(task.Text)
	var score [3]int
	best := map[Class]signal{}
	note := func(s signal, weight int) {
		score[classIndex(s.class)] += weight
		if held, ok := best[s.class]; !ok || weight > held.weight {
			best[s.class] = signal{class: s.class, weight: weight, name: s.name}
		}
	}
	labelled := map[Class]bool{}
	for _, label := range task.Labels {
		if class, ok := labelClass(label); ok {
			labelled[class] = true
			note(signal{class: class, name: "the `" + strings.TrimSpace(label) + "` label"}, weightLabel)
		}
	}
	if otherTitle.MatchString(title) {
		note(signal{class: Other, name: "a title asking a question rather than for a change"}, weightTitle+1)
	}
	if tinyEdit.MatchString(title) {
		note(signal{class: Other, name: "a small scoped edit"}, 2*weightTitle+1)
	}
	for _, s := range titleSignals {
		if s.pattern.MatchString(title) {
			note(s, s.weight*weightTitle)
		}
	}
	for _, s := range bodySignals {
		if s.pattern.MatchString(body) {
			note(s, s.weight*weightBody)
		}
	}
	reading := decide(score, best, labelled)
	if reading.Class == Bugfix {
		reading.Complex = complexFix(body)
	}
	return reading
}

// decide is the reading the scores make. A task a person labelled a bug and
// nobody labelled anything else is a bugfix whatever its prose leaves unsaid:
// the label is the answer, and the open-ended default is for tasks nobody
// answered.
func decide(score [3]int, best map[Class]signal, labelled map[Class]bool) Reading {
	bug, open, other := score[0], score[1], score[2]
	switch {
	case other > bug && other > open:
		return Reading{Class: Other, Why: best[Other].name, Sure: other-max(bug, open) >= clearMargin}
	case bug >= open+clearMargin:
		return Reading{Class: Bugfix, Why: best[Bugfix].name, Sure: true}
	case open >= bug+clearMargin:
		return Reading{Class: OpenEnded, Why: best[OpenEnded].name, Sure: true}
	case labelled[Bugfix] && !labelled[OpenEnded]:
		return Reading{Class: Bugfix, Why: best[Bugfix].name, Sure: true}
	case bug == 0 && open == 0:
		return Reading{Class: OpenEnded, Why: "nothing in the task says which kind of work it is", Sure: false}
	}
	return Reading{Class: OpenEnded, Why: "the task reads both ways, so it gets the stronger checker", Sure: false}
}

// classIndex is a class's slot in the score array.
func classIndex(class Class) int {
	switch class {
	case Bugfix:
		return 0
	case OpenEnded:
		return 1
	}
	return 2
}

// taskTitle splits a task into the line that names it and the rest. The
// first non-blank line is the title, a Markdown heading's hashes and a
// harness wrapper removed; when the wrapper carries the title, the heading
// line above it (usually the same words) is dropped as a duplicate.
func taskTitle(text string) (title, body string) {
	text = boilerplate.ReplaceAllString(text, "")
	lines := strings.Split(text, "\n")
	at := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			at = i
			break
		}
	}
	if at < 0 {
		return "", ""
	}
	title = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(lines[at]), "#"))
	rest := lines[at+1:]
	if m := wrapperLine.FindStringSubmatch(title); m != nil {
		title = strings.TrimSpace(m[3])
	}
	// A wrapper line further down names the same issue again; it is read as
	// the title's second spelling and not as body text.
	kept := rest[:0:0]
	for _, line := range rest {
		if m := wrapperLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if title == "" {
				title = strings.TrimSpace(m[3])
			}
			continue
		}
		kept = append(kept, line)
	}
	return title, strings.Join(kept, "\n")
}

// A FIX WITH REACH IS NOT A ONE-LINE FIX. The cheapest worker is the pick for
// fixes of the ordinary size, and a defect whose repair crosses files, a wire
// contract or a language's rules
// is a different job wearing the same word: the cheap worker's patch is the
// narrow one, and the checker then rejects it or — worse — passes it. So a
// bugfix is read once more for REACH, off its body, on signals that say what
// the repair must hold together rather than what broke:
//
//   - more than one source file named;
//   - an API, an endpoint, a status code or a protocol;
//   - language rules — i18n, locales, plurals, grammar, Unicode;
//   - a long report, or more than one reproduction;
//   - existing tests, guards or behaviour that must keep passing.
//
// Two of them make a complex fix; one alone is how an ordinary report reads.
// A complex fix moves its worker one rung up the seat's front ([Decide]); its
// planner and checker are the fix's own, and a pin is never overruled.

// reachSignal is one of the signals above, and the words it is named by.
type reachSignal struct {
	name string
	hit  func(body string) bool
}

// The patterns the reach signals read.
var (
	sourceFile  = regexp.MustCompile(`\b[\w./-]+\.(py|go|ts|tsx|js|jsx|mjs|rs|java|kt|rb|c|cc|cpp|h|hpp|cs|php|swift|scala|ex|exs|vue|svelte)\b`)
	wireWords   = regexp.MustCompile(`(?i)\b(api|apis|endpoints?|status codes?|http ?[1-5]\d\d|[1-5]\d\d (error|response)|protocols?|grpc|websockets?|rpc|wire format|openapi)\b`)
	languageRul = regexp.MustCompile(`(?i)\b(i18n|l10n|locales?|locali[sz]ation|internationali[sz]ation|translations?|plurali[sz]ation|plurals?|grammar|language rules?|unicode|utf-?8|diacritics?|right-to-left|rtl)\b`)
	keepPassing = regexp.MustCompile(`(?i)\b(existing|current|other|all) (unit |integration )?(tests?|checks?|guards?|behaviou?r)\b[^.]{0,60}\b(pass|passing|keep|kept|still|remain|break|breaking|unchanged)|\bwithout breaking\b|\bbackwards? compat`)
	reproMark   = regexp.MustCompile(`(?im)^\s*#+\s*(repro|reproduction|to reproduce|steps to reproduce)\b|^\s*(repro|reproduction) \d`)
)

// complexLongBody is how long a report is, after the title, before its length
// alone is a signal of reach: a few paragraphs, or a trace and a table.
const complexLongBody = 2000

var reachSignals = []reachSignal{
	{"more than one file", func(body string) bool {
		seen := map[string]bool{}
		for _, f := range sourceFile.FindAllString(body, -1) {
			seen[f] = true
		}
		return len(seen) > 1
	}},
	{"an API or protocol", wireWords.MatchString},
	{"language rules", languageRul.MatchString},
	{"a long report or several repros", func(body string) bool {
		return len(body) > complexLongBody || strings.Count(body, "```") >= 4 || len(reproMark.FindAllString(body, -1)) > 1
	}},
	{"existing tests that must keep passing", keepPassing.MatchString},
}

// complexFix is the reach a fix's body shows, in words; empty is a simple fix.
func complexFix(body string) string {
	var hits []string
	for _, s := range reachSignals {
		if s.hit(body) {
			hits = append(hits, s.name)
		}
	}
	if len(hits) < 2 {
		return ""
	}
	return strings.Join(hits, ", ")
}
