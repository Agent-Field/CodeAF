package crewroute

import (
	"regexp"
	"strings"
)

// WHAT KIND OF WORK A TASK IS, READ OFF ITS OWN WORDS.
//
// The router routes on a task's CLASS and on nothing finer, and that is a
// measured choice rather than a shortcut: across the 22 real issues the crew
// was scored on, and the 46k DeepSWE trials behind them, the text of a task
// did not predict how hard that one task would be beyond the kind of work it
// was. What the kind of work DID predict was large and one-sided — a narrow
// fix is done as well by the cheapest crew as by the dearest, and open-ended
// work (a feature, a refactor, docs, a design) is only mergeable with a strong
// checker behind it. So the question this file answers is the only one worth
// asking of the text: which of the two is this?
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
// A harness's own wrapper around an issue — "Implement issue #412: …" and the
// standing instruction to keep the test suite green that follows every task
// it hands out — is read past, because it is the same sentence on every task
// and a signal that fires on everything tells the router nothing.
//
// ── UNCERTAIN IS OPEN-ENDED ──
//
// When the signals do not clearly favour one class, the answer is
// [OpenEnded], and that is the safe side on purpose: calling a narrow fix
// open-ended costs a strong checker it did not need — cents — while calling
// open-ended work a narrow fix sends it to a crew that was measured merging
// none of eight such tasks. The two mistakes are not the same size, so the
// tie does not go to the cheaper one.

// Class is the kind of work a task is. It is a string because it is written
// into the router's event log and onto a task's card, and a script reading
// either reads the word.
type Class string

const (
	// Bugfix is a NARROW, VERIFIABLE change: a defect, a regression, a missing
	// check, a test that should exist. The evidence says the cheapest crew does
	// it as well as any.
	Bugfix Class = "bugfix"
	// OpenEnded is work whose shape the task does not fix: a feature, a
	// refactor, documentation, a design. The evidence says the checker is the
	// lever here, and a strong one is the difference between mergeable and not.
	OpenEnded Class = "openended"
	// Other is work that changes nothing in particular — a question, an
	// investigation, a review. It is read on the average of the two measured
	// classes, because nothing measured it on its own.
	Other Class = "other"
)

// Classes lists the three, in the order a report prints them.
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
	{regexp.MustCompile(`(?i)^\s*(feat|feature|refactor|perf|docs?)(\([^)]*\))?!?:`), OpenEnded, 2, "a feature or refactor title"},
	{regexp.MustCompile(`(?i)^\s*(add|support|implement|introduce|enable|allow|create|design|define|document|redesign|rework|restructure|migrate|extend|expose|make)\b`), OpenEnded, 2, "a title that asks for something new"},
	{regexp.MustCompile(`(?i)\b(consider|proposal|rfc|epic|feature|enhancement|refactor|docs|documentation|more pythonic|should (probably )?(default|be|support|allow)|fractional|metadata)\b`), OpenEnded, 1, "an open-ended word in the title"},
}

// bodySignals are read off everything after the title.
var bodySignals = []signal{
	{regexp.MustCompile(`Traceback \(most recent call last\)|(?m)^\s+at [\w.$]+\(|panic: |goroutine \d+ \[`), Bugfix, 2, "a stack trace"},
	{regexp.MustCompile(`\b[A-Z][A-Za-z]*(Error|Exception)\b`), Bugfix, 1, "an exception in the text"},
	{regexp.MustCompile(`(?i)\b(steps to reproduce|to reproduce|describe the bug|expected behaviou?r|actual behaviou?r|minimal (reproducible )?example)\b`), Bugfix, 2, "a bug report's own headings"},
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

// wrapperLine is the harness's own sentence around an issue's title:
// "Implement issue #412: [bug] merge_list drops vlan interfaces". The verb is
// the harness talking and the tail is the issue's own title.
var wrapperLine = regexp.MustCompile(`(?i)^\s*(implement|fix|resolve|address|solve|close|work on)\s+(github\s+)?issue\s+#?\d+\s*[:\-—]\s*(.*)$`)

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
	for _, label := range task.Labels {
		if class, ok := labelWords[strings.ToLower(strings.TrimSpace(label))]; ok {
			note(signal{class: class, name: "the `" + strings.TrimSpace(label) + "` label"}, weightLabel)
		}
	}
	if otherTitle.MatchString(title) {
		note(signal{class: Other, name: "a title asking a question rather than for a change"}, weightTitle+1)
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
	bug, open, other := score[0], score[1], score[2]
	switch {
	case other > bug && other > open:
		return Reading{Class: Other, Why: best[Other].name, Sure: other-max(bug, open) >= clearMargin}
	case bug >= open+clearMargin:
		return Reading{Class: Bugfix, Why: best[Bugfix].name, Sure: true}
	case open >= bug+clearMargin:
		return Reading{Class: OpenEnded, Why: best[OpenEnded].name, Sure: true}
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
