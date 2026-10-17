package factory

import (
	"strings"
	"time"
	"unicode"
)

// Match is the floor's filter: the words a person types into the rail's `/`
// box, read as a test an item passes or fails. Every term must hold. A word
// that is one of the floor's own words, or a typed token, is a question about
// the item rather than a substring; anything else is a plain substring of the
// title, the repo, the area, the triage type and the author, without regard to
// case.
//
// The words are:
//
//	risky      the read named a risk, or the shown size is L ([ShownSize])
//	cheap      the estimate is at most 2.5
//	strangers  the author is a stranger
//	spend      the area or the title mentions spend, billing, ledger or cap
//	ui         the area is tui, pages or render
//	prs        the item is a pull request
//	bugs       the triage type is bug
//	thin       the readiness is under 55
//	mine       the person or author is the viewer's GitHub handle
//
// The typed tokens are all exact, never substrings, and case-insensitive:
//
//	label:x        the item carries label x; repeat to AND; x may hold colons
//	-label:x       the item does not carry label x
//	assignee:@h    Item.Person() is h; `@me` or `me` is the viewer; a bare `none` is
//	               nobody, while `@none` is the person whose handle is none
//	area:x         the triage area is x
//	step:x         the CURRENT phase is named x: the job is running or needs you
//	               and that phase is running, waiting or failed. A finished job
//	               has no current step, so its parked last phase never matches
//	age:>1d        older than the span since Created; also >1w, <72h (h, d, w)
//	repo:x         the repo is x, full (`acme/api`) or short (`api`); repeat to OR
//	author:@h      the author is h
//	type:x         the triage type is x; size:x the shown size ([ShownSize]) is x;
//	               an item with no label, diff or completed read matches none
//	#12 api#12     the item's number, optionally on that repo
//
// A value may be quoted after its colon (`label:"good first issue"`). A
// recognised token that is malformed (empty value, bad age, unclosed quote)
// matches NOTHING; it never falls back to the title. An unset field never
// matches a typed token that names it, and -label matches an item with no
// labels. `would:` is not a token yet: it needs the injected pipeline's dry
// run, so it stays an ordinary word.
//
// THE MATCHER LIVES HERE, beside the vocabulary, because a filter is a fact
// about the floor's vocabulary, not about one seam behind it. The empty query
// matches everything. The optional viewer is the linked GitHub handle; an
// absent or unknown viewer matches no item under mine or assignee:me.
// Match reads the clock once, when it is built.
func Match(q string, viewers ...string) func(Item) bool {
	return MatchAt(q, time.Now(), viewers...)
}

// MatchAt is Match with the clock handed in, so age: is deterministic. The
// returned func never reads the clock again.
func MatchAt(q string, now time.Time, viewers ...string) func(Item) bool {
	viewer := ""
	if len(viewers) > 0 {
		viewer = NormalizeHandle(viewers[0])
	}
	raw := lexQuery(strings.ToLower(q))
	if len(raw) == 0 {
		return func(Item) bool { return true }
	}
	terms := make([]term, len(raw))
	var repos []term
	rest := terms[:0:0]
	for i, r := range raw {
		terms[i] = parseTerm(r)
		switch terms[i].kind {
		case tmRepo:
			repos = append(repos, terms[i])
		default:
			rest = append(rest, terms[i])
		}
	}
	return func(it Item) bool {
		if len(repos) > 0 {
			hit := false
			for _, t := range repos {
				if t.bad {
					return false
				}
				if repoIs(it, t.val) {
					hit = true
				}
			}
			if !hit {
				return false
			}
		}
		for _, t := range rest {
			if !matchOne(it, t, viewer, now) {
				return false
			}
		}
		return true
	}
}

type termKind int

const (
	tmWord termKind = iota
	tmLabel
	tmAssignee
	tmArea
	tmStep
	tmAge
	tmRepo
	tmAuthor
	tmType
	tmSize
	tmRef
)

// term is one parsed query word. bad is a recognised token with a value that
// cannot be read: it matches nothing.
type term struct {
	kind  termKind
	text  string
	val   string
	neg   bool
	bad   bool
	who   whoKind
	older bool // age:>
	span  time.Duration
	num   int
}

// whoKind separates assignee's two sentinels from a literal handle, so the
// handle "none" stays reachable as `@none` and is never read as nobody.
type whoKind int

const (
	whoHandle whoKind = iota
	whoNone
	whoMe
)

type rawTerm struct {
	text string
	bad  bool
}

var typedKeys = map[string]termKind{
	"label": tmLabel, "assignee": tmAssignee, "area": tmArea, "step": tmStep,
	"age": tmAge, "repo": tmRepo, "author": tmAuthor, "type": tmType, "size": tmSize,
}

// typedPrefix reports whether s is exactly `key:` or `-label:`, the only
// place a quote opens a quoted value; elsewhere a quote is an ordinary
// character, as it always was.
func typedPrefix(s string) bool {
	if s == "-label:" {
		return true
	}
	key, ok := strings.CutSuffix(s, ":")
	if !ok {
		return false
	}
	_, known := typedKeys[key]
	return known
}

// lexQuery splits on white space like strings.Fields, except that a quoted
// value after a typed prefix may hold spaces. One pass, no backtracking.
func lexQuery(q string) []rawTerm {
	var out []rawTerm
	var buf strings.Builder
	inQuote, started, bad := false, false, false
	flush := func() {
		if started {
			out = append(out, rawTerm{text: buf.String(), bad: bad || inQuote})
		}
		buf.Reset()
		started, bad = false, false
	}
	for _, r := range q {
		switch {
		case !inQuote && unicode.IsSpace(r):
			flush()
		case r == '"' && !inQuote && typedPrefix(buf.String()):
			inQuote, started = true, true
		case r == '"' && inQuote:
			inQuote = false
		default:
			buf.WriteRune(r)
			started = true
		}
	}
	flush()
	return out
}

func parseTerm(r rawTerm) term {
	t := term{kind: tmWord, text: r.text}
	if n, ok := parseRef(r.text); ok {
		t.kind, t.val, t.num = tmRef, n.repo, n.num
		return t
	}
	key, val, ok := strings.Cut(r.text, ":")
	if !ok {
		return t
	}
	if key == "-label" {
		key, t.neg = "label", true
	}
	kind, known := typedKeys[key]
	if !known {
		return t
	}
	t.kind, t.bad = kind, r.bad
	val = strings.TrimSpace(val)
	switch kind {
	case tmAssignee, tmAuthor:
		switch {
		case kind == tmAssignee && val == "none":
			t.who = whoNone
		case kind == tmAssignee && (val == "me" || val == "@me"):
			t.who = whoMe
		default:
			t.val = NormalizeHandle(val)
			t.bad = t.bad || t.val == ""
		}
	case tmAge:
		t.older, t.span, ok = parseAge(val)
		t.bad = t.bad || !ok
	default:
		t.val = val
		t.bad = t.bad || val == ""
	}
	return t
}

type ref struct {
	repo string
	num  int
}

// parseRef reads `#12` or `repo#12`. Anything else after the hash makes it an
// ordinary word.
func parseRef(s string) (ref, bool) {
	i := strings.LastIndexByte(s, '#')
	if i < 0 || i == len(s)-1 || len(s)-i > 10 {
		return ref{}, false
	}
	n := 0
	for _, c := range s[i+1:] {
		if c < '0' || c > '9' {
			return ref{}, false
		}
		n = n*10 + int(c-'0')
	}
	if strings.ContainsAny(s[:i], ":\"") {
		return ref{}, false
	}
	return ref{repo: s[:i], num: n}, true
}

// parseAge reads `>1d`, `>1w` or `<72h`: at most four digits, so the span
// cannot overflow.
func parseAge(v string) (older bool, span time.Duration, ok bool) {
	if len(v) < 3 || (v[0] != '>' && v[0] != '<') {
		return false, 0, false
	}
	unit := map[byte]time.Duration{'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[v[len(v)-1]]
	digits := v[1 : len(v)-1]
	if unit == 0 || len(digits) == 0 || len(digits) > 4 {
		return false, 0, false
	}
	n := 0
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false, 0, false
		}
		n = n*10 + int(c-'0')
	}
	return v[0] == '>', time.Duration(n) * unit, true
}

func repoIs(it Item, v string) bool {
	repo := strings.ToLower(it.Repo)
	if repo == "" {
		return false
	}
	if repo == v {
		return true
	}
	return !strings.Contains(v, "/") && repo[strings.LastIndexByte(repo, '/')+1:] == v
}

// currentStep is the lower-cased name of the phase a job is at right now, or
// "" when it is at none. A landed or shipped stream parks Cur on its last
// phase, and a queued one has not started, so the job must be running or need
// someone AND the phase itself must be live: running, waiting or failed. A
// done phase is behind the job, never its current step.
func currentStep(it Item) string {
	if it.State != StateRunning && it.State != StateNeedsYou {
		return ""
	}
	s := it.Stream
	if s == nil || s.Cur < 0 || s.Cur >= len(s.Phases) {
		return ""
	}
	p := s.Phases[s.Cur]
	switch p.State {
	case PhaseRunning, PhaseWaiting, PhaseFailed:
		return strings.ToLower(p.Name)
	}
	return ""
}

// HasWord reports whether q, read with the filter's own lexer, holds w as a
// whole word of its own. A word inside a quoted value (label:"x is:dismissed")
// belongs to that value and is not found, so a caller deciding on a word
// cannot be steered by another term's quotes.
func HasWord(q, w string) bool {
	w = strings.ToLower(w)
	for _, r := range lexQuery(strings.ToLower(q)) {
		if !r.bad && r.text == w {
			return true
		}
	}
	return false
}

func matchOne(it Item, t term, viewer string, now time.Time) bool {
	if t.bad {
		return false
	}
	switch t.kind {
	case tmLabel:
		has := false
		for _, l := range it.Labels {
			if strings.ToLower(strings.TrimSpace(l)) == t.val {
				has = true
				break
			}
		}
		return has != t.neg
	case tmAssignee:
		switch t.who {
		case whoNone:
			return it.Person() == ""
		case whoMe:
			return viewer != "" && it.Person() == viewer
		}
		return it.Person() == t.val
	case tmArea:
		return strings.ToLower(it.Triage.Area) == t.val
	case tmStep:
		return currentStep(it) == t.val
	case tmAge:
		if it.Created.IsZero() {
			return false
		}
		age := max(now.Sub(it.Created), 0)
		if t.older {
			return age > t.span
		}
		return age < t.span
	case tmAuthor:
		return NormalizeHandle(it.Author) == t.val
	case tmType:
		return strings.ToLower(it.Triage.Type) == t.val
	case tmSize:
		shown, _ := ShownSize(it)
		return strings.ToLower(shown) == t.val
	case tmRef:
		n := it.Num
		if n == 0 {
			n = it.ID
		}
		return n > 0 && n == t.num && (t.val == "" || repoIs(it, t.val))
	}
	return matchTerm(it, t.text, viewer)
}

// ThinReadiness is the readiness under which an item is thin and wants
// questions before it is worth running.
const ThinReadiness = 55

// CheapEst is the largest estimate the word cheap still covers.
const CheapEst = 2.5

func matchTerm(it Item, t, viewer string) bool {
	area := strings.ToLower(it.Triage.Area)
	title := strings.ToLower(it.Title)
	switch t {
	case "is:dismissed":
		return IsDismissed(it, viewer)
	case "risky":
		shown, _ := ShownSize(it)
		return len(it.Triage.Risk) > 0 || shown == "L"
	case "cheap":
		return it.Triage.Est > 0 && it.Triage.Est <= CheapEst
	case "strangers":
		return it.Tier == TierStranger
	case "spend":
		for _, w := range []string{"spend", "billing", "ledger", "cap"} {
			if strings.Contains(area, w) || strings.Contains(title, w) {
				return true
			}
		}
		return false
	case "ui":
		switch area {
		case "tui", "pages", "render":
			return true
		}
		return false
	case "prs":
		return it.Kind == KindPR
	case "bugs":
		return it.Triage.Type == "bug"
	case "thin":
		return it.Triage.Readiness < ThinReadiness
	case "mine":
		return viewer != "" && (it.Person() == viewer || NormalizeHandle(it.Author) == viewer)
	}
	hay := title + " " + strings.ToLower(JobRef(it)) + " " + strings.ToLower(it.Repo) + " " + area + " " +
		strings.ToLower(it.Triage.Type) + " " + strings.ToLower(it.Author)
	return strings.Contains(hay, t)
}
