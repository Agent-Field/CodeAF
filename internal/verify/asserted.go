package verify

// What a check ASSERTS, as opposed to what it merely mentions.
//
// The surface reader beside this one answers "what names does this file spell".
// That question is not enough to settle whether a check exercises a behaviour: a
// check that CALLS `write("short", expand=True)` and then asserts only that the
// widget has more than zero lines spells `expand` and weighs nothing about it.
// textual s13 wrote three such checks, every one of them mapped to a stated
// behaviour, and shipped at exit 0 with the hidden check for that exact
// behaviour red (docs/design/gate/ACCEPTANCE.md).
//
// So this reads the other half: for each check a file declares, the TEXT OF ITS
// ASSERTION STATEMENTS and nothing else — the lines that would fail if what they
// weigh were wrong. Everything outside an assertion is setup, and setup is what
// a check mentions rather than what it checks.
//
// It is a reader of SHAPE, one per language, in the register the rest of this
// package reads in: declaration-level, conservative, and silent where it is not
// sure. A file in a language with no reader here, or a check whose declaration
// this cannot find, comes back UNKNOWN rather than empty — the difference
// between "nothing is asserted" and "nobody looked" is the whole of what the
// door downstream is allowed to act on.

import (
	"regexp"
	"strings"
)

// assertionTextBytes bounds what one check's assertions contribute.
//
// A check that asserts more than sixteen kilobytes has said everything it is
// going to say about any one name long before the cap; past it the reader stops
// appending and the check keeps the text it has, which degrades in the safe
// direction — a truncated assertion text can only fail to name an observable,
// and failing to name one leaves the point OPEN rather than closing it.
const assertionTextBytes = 16 << 10

// Assertions is what each check in one file asserts: the declaration's name,
// folded to lower case, against the text of its assertion statements.
//
// A key that is PRESENT with empty text is a check this reader found and whose
// assertions weigh nothing it could see. A key that is ABSENT is a check this
// reader could not find at all. Those are two different answers and the caller
// spends them differently, which is why this is a map and never a list.
type Assertions map[string]string

// AssertionsIn reads one file and returns what each check in it asserts.
//
// A nil answer means this program has no reader for the file's language, which
// is the same silence PublicSurface keeps for the same reason: a language read
// by guesswork is a language read wrongly, and every wrong reading here becomes
// a repair round bought against a check that was fine.
//
// The body may be given lower-cased — the callers that already hold a folded
// copy of a file pass it — so every shape this matches is spelled in a way that
// case cannot change, and every key is folded on the way in.
func AssertionsIn(file, body string) Assertions {
	switch surfaceLanguage(lastSegment(file)) {
	case "python":
		return pythonAssertions(body)
	case "go":
		return goAssertions(body)
	case "script":
		return scriptAssertions(body)
	case "rust":
		return rustAssertions(body)
	}
	return nil
}

// Text is what this check asserts, and whether the check was found at all.
func (a Assertions) Text(check string) (string, bool) {
	if a == nil || check == "" {
		return "", false
	}
	text, found := a[strings.ToLower(strings.TrimSpace(check))]
	return text, found
}

// Named is Text for a runner identity that a runner printed with its groups
// JOINED BY SPACES rather than by a separator this program can see.
//
// vitest prints `circuit breaker origin keying keys by origin, not path` for a
// case declared `it("keys by origin, not path")` inside two nested `describe`
// blocks, and nothing in that string says where the groups end and the case
// begins. So the identity's own trailing words are tried, longest first, and the
// first that is a case this file declares is the case. A suffix of one word is
// not tried: one word matching one case name is a coincidence, and the cost of
// the wrong case here is a finding about a check nobody wrote.
//
// ofetch s16 is what this is for: forty-seven mapped pairings, every identity in
// that shape, and the assertion door found not one of them.
func (a Assertions) Named(identity string) (string, bool) {
	if text, found := a.Text(identity); found {
		return text, true
	}
	words := strings.Fields(strings.TrimSpace(identity))
	for start := 1; start+1 < len(words); start++ {
		if text, found := a.Text(strings.Join(words[start:], " ")); found {
			return text, true
		}
	}
	return "", false
}

// gather accumulates one declaration's assertion text inside the cap.
type gather struct {
	held map[string]*strings.Builder
	name string
}

func newGather() *gather { return &gather{held: map[string]*strings.Builder{}} }

// declare starts a new declaration. An empty name closes the one that was open
// without opening another, which is what a class header or a package boundary
// does.
func (g *gather) declare(name string) {
	g.name = strings.ToLower(strings.TrimSpace(name))
	if g.name == "" {
		return
	}
	if _, held := g.held[g.name]; !held {
		g.held[g.name] = &strings.Builder{}
	}
}

// add appends one assertion statement's text to whichever declaration is open.
func (g *gather) add(text string) {
	held := g.held[g.name]
	if g.name == "" || held == nil || held.Len() >= assertionTextBytes {
		return
	}
	held.WriteString(text)
	held.WriteByte('\n')
}

func (g *gather) assertions() Assertions {
	if len(g.held) == 0 {
		return nil
	}
	read := make(Assertions, len(g.held))
	for name, text := range g.held {
		read[name] = text.String()
	}
	return read
}

// indentOf is the width of the whitespace a line opens with, which is how every
// reader in this package tells a nested thing from a sibling.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// openBrackets is how far a statement is from finished. A statement that ends
// with brackets open continues on the next line, which is how a person writes a
// long assertion and how this reader keeps the rest of it.
//
// It counts brackets in the raw text, quotes and all. A bracket inside a string
// literal miscounts it — and miscounting can only make the reader keep READING,
// never stop early, so an assertion is captured whole or captured with a line of
// its neighbour attached. Both answers name every observable the assertion names.
func openBrackets(line string, depth int) int {
	for _, char := range line {
		switch char {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

// ── Python ───────────────────────────────────────────────────────────────────

var (
	pythonAssert = regexp.MustCompile(`^\s*assert(\s|\()`)
	// The shapes a python suite spells an assertion in that are not the
	// statement: pytest's context manager, and unittest's own methods. Both are
	// matched where they are WRITTEN rather than by a list of method names, so a
	// project's own assertFoo helper counts as one.
	pythonRaises = regexp.MustCompile(`\bpytest\.raises\s*\(|\bself\.assert[A-Za-z_]*\s*\(|\bassert_[A-Za-z_]*\s*\(`)
	pythonClassM = regexp.MustCompile(`^(\s*)class\s+[A-Za-z_][A-Za-z0-9_]*\s*[(:]`)
)

// pythonAssertions reads a python file's checks and the statements that weigh
// something in each of them.
//
// THE OWNER OF AN ASSERTION IS THE OUTERMOST DEFINITION IT SITS IN, and that is
// the rule this reader turns on. A textual test declares an App subclass with a
// `compose` method inside the test function's own body; a reader that took the
// nearest enclosing `def` would file every assertion after it under `compose`
// and find the test itself asserting nothing at all. So a definition nested
// inside an open one is passed over, and its lines stay the outer check's.
func pythonAssertions(body string) Assertions {
	read := newGather()
	ownerIndent, classIndent := -1, -1
	depth := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if depth > 0 {
			read.add(line)
			depth = openBrackets(line, depth)
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := indentOf(line)
		if match := pythonClassM.FindStringSubmatch(line); match != nil {
			// A class at or outside the open definition ends it. A class INSIDE
			// one is a fixture the check declares for itself and changes nothing.
			if ownerIndent < 0 || indent <= ownerIndent {
				read.declare("")
				ownerIndent, classIndent = -1, indent
			}
			continue
		}
		if match := pythonDef.FindStringSubmatch(line); match != nil {
			// A definition at or outside the class header closes the class: the
			// body ended where the indentation came back.
			if classIndent >= 0 && indent <= classIndent {
				classIndent = -1
			}
			if ownerIndent < 0 || indent <= ownerIndent {
				if classIndent < 0 || indent > classIndent {
					read.declare(match[2])
					ownerIndent = indent
				}
			}
			continue
		}
		if ownerIndent < 0 || indent <= ownerIndent {
			continue
		}
		if pythonAssert.MatchString(line) || pythonRaises.MatchString(line) {
			read.add(line)
			depth = openBrackets(line, 0)
		}
	}
	return read.assertions()
}

// ── Go ───────────────────────────────────────────────────────────────────────

var (
	goFunc = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	// The two ways a go check says something failed: the testing type's own
	// reporting methods, and the assertion libraries every go project reaches
	// for. Both are matched on the CALL and never on a list of check names.
	goReport = regexp.MustCompile(`(?i)\b[a-z_][a-z0-9_]*\.(?:errorf?|fatalf?|fail|failnow)\s*\(`)
	goAssert = regexp.MustCompile(`(?i)\b(?:assert|require|is)\.[a-z_][a-z0-9_]*\s*\(`)
	goIf     = regexp.MustCompile(`^\s*if\s`)
)

// goAssertions reads a go file's functions and what each of them asserts.
//
// A GO CHECK'S ASSERTION IS TWO LINES AND ONLY ONE OF THEM NAMES ANYTHING. The
// idiom is `if got != want {` on one line and `t.Fatalf(...)` on the next, and
// the comparison — the half that names the observable — is on the `if`. So the
// last condition seen is carried and joins the report it guards. A report with
// no condition before it stands on its own, which is what `t.Fatal(err)` after a
// call is.
func goAssertions(body string) Assertions {
	read := newGather()
	lastIf, depth := "", 0
	for _, line := range strings.Split(body, "\n") {
		if depth > 0 {
			read.add(line)
			depth = openBrackets(line, depth)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if match := goFunc.FindStringSubmatch(line); match != nil {
			read.declare(match[1])
			lastIf = ""
			continue
		}
		if goIf.MatchString(line) {
			lastIf = line
			// An `if` is not itself an assertion — it is a condition, and it
			// becomes one only where the branch under it reports a failure.
			continue
		}
		if goAssert.MatchString(line) {
			read.add(line)
			depth = openBrackets(line, 0)
			continue
		}
		if goReport.MatchString(line) {
			if lastIf != "" {
				read.add(lastIf)
				lastIf = ""
			}
			read.add(line)
			depth = openBrackets(line, 0)
		}
	}
	return read.assertions()
}

// ── Javascript and typescript ────────────────────────────────────────────────

var (
	// The shapes a javascript runner spells one check in: a name in quotes as
	// the first argument. `describe` is deliberately absent — it groups checks
	// and asserts nothing itself, and a group's name owning its children's
	// assertions would let one assertion anywhere in a file answer for the group.
	scriptCase = regexp.MustCompile(
		"^\\s*(?:it|test)(?:\\.\\w+)*\\s*\\(\\s*[`'\"]([^`'\"]+)[`'\"]")
	// expect(...) is vitest, jest and chai; assert.* is node's own and chai's
	// second face; t.something(...) is ava and node:test.
	scriptAssert = regexp.MustCompile(
		`(?i)\bexpect\s*\(|\bassert\s*[.(]|\bt\.(?:is|not|deepequal|notdeepequal|true|false|throws|regex|like)\s*\(`)
)

// scriptAssertions reads a javascript or typescript file's checks and what each
// of them asserts.
//
// A check ends where the next one begins, which is all the structure a runner's
// own identity gives this reader to work with: the identities it is asked about
// are `file > group > name`, and the name is the leaf.
func scriptAssertions(body string) Assertions {
	read := newGather()
	depth := 0
	for _, line := range strings.Split(body, "\n") {
		if depth > 0 {
			read.add(line)
			depth = openBrackets(line, depth)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if match := scriptCase.FindStringSubmatch(line); match != nil {
			read.declare(match[1])
			continue
		}
		if scriptAssert.MatchString(line) {
			read.add(line)
			depth = openBrackets(line, 0)
		}
	}
	return read.assertions()
}

// ── Rust ─────────────────────────────────────────────────────────────────────

var (
	rustFn        = regexp.MustCompile(`^\s*(?:pub(?:\s*\([^)]*\))?\s+)?(?:async\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustAssertMac = regexp.MustCompile(`\b(?:debug_)?assert(?:_eq|_ne|_matches)?!\s*[(\[]|\.unwrap_err\s*\(|\bpanic!\s*\(`)
)

// rustAssertions reads a rust file's functions and the macros in each that weigh
// something. The macro is the whole of rust's assertion vocabulary, so there is
// nothing else to look for.
func rustAssertions(body string) Assertions {
	read := newGather()
	depth := 0
	for _, line := range strings.Split(body, "\n") {
		if depth > 0 {
			read.add(line)
			depth = openBrackets(line, depth)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if match := rustFn.FindStringSubmatch(line); match != nil {
			read.declare(match[1])
			continue
		}
		if rustAssertMac.MatchString(line) {
			read.add(line)
			depth = openBrackets(line, 0)
		}
	}
	return read.assertions()
}
