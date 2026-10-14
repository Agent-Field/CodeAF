package draft

import (
	"io"
	"sort"
	"strings"
)

// Verb is one `codeaf app` command. Write verbs fill Parse, Check, Apply and
// Describe and end as one journal line. Read verbs fill Read and touch
// nothing. Remote verbs fill Parse only and are answered by the running
// Builder over its socket.
type Verb struct {
	Usage string
	// Body says the verb reads its document from stdin.
	Body bool
	// Internal verbs are written by codeaf itself, never typed by a worker.
	Internal bool
	Remote   bool

	Parse    func(args []string, stdin io.Reader) (Entry, error)
	Check    func(State, Entry) error
	Apply    func(*State, Entry)
	Describe func(State, Entry) string
	Read     func(State, Defaults, []string) (string, error)
}

// registry is filled by each verb file's init, one entry per verb. Verbs with
// a family name, such as "plan data", are looked up by their first two words.
var registry = map[string]Verb{}

func add(name string, v Verb) {
	if _, dup := registry[name]; dup {
		panic("draft: verb registered twice: " + name)
	}
	registry[name] = v
}

// lookup finds the verb a command line names and the arguments after its
// name: two words first ("plan data"), then one.
func lookup(args []string) (name string, v Verb, rest []string, ok bool) {
	if len(args) >= 2 {
		name = args[0] + " " + args[1]
		if v, ok = registry[name]; ok {
			return name, v, args[2:], true
		}
	}
	if len(args) >= 1 {
		name = args[0]
		v, ok = registry[name]
		return name, v, args[1:], ok
	}
	return "", Verb{}, nil, false
}

// Usages lists every verb the worker may type, sorted, one usage line each.
func Usages() []string {
	var lines []string
	for _, v := range registry {
		if !v.Internal {
			lines = append(lines, v.Usage)
		}
	}
	sort.Strings(lines)
	return lines
}

// def describes a write verb by its typed arguments T. register turns it into
// a Verb, so Parse, Check and Apply share one decode and cannot disagree.
type def[T any] struct {
	Usage    string
	Body     bool
	Internal bool
	// Values and Bools name the flags the verb takes; Values may repeat.
	Values []string
	Bools  []string

	Decode func(a argv, body string) (T, error)
	Piece  func(T) Piece
	Check  func(State, Entry, T) error
	Apply  func(*State, T)
	// Line is the owner-readable sentence, built from the state after Apply.
	Line func(State, T) string
}

func register[T any](name string, d def[T]) {
	decode := func(args []string, body string) (T, error) {
		var zero T
		a, err := scanArgs(args, d.Usage, d.Values, d.Bools)
		if err != nil {
			return zero, err
		}
		return d.Decode(a, body)
	}
	v := Verb{Usage: d.Usage, Body: d.Body, Internal: d.Internal}
	v.Parse = func(args []string, stdin io.Reader) (Entry, error) {
		body := ""
		if d.Body {
			raw, err := io.ReadAll(stdin)
			if err != nil {
				return Entry{}, usage("cannot read stdin: %v", err)
			}
			body = string(raw)
		}
		t, err := decode(args, body)
		if err != nil {
			return Entry{}, err
		}
		return Entry{Verb: name, Args: args, Body: body, Piece: d.Piece(t)}, nil
	}
	v.Check = func(st State, e Entry) error {
		t, err := decode(e.Args, e.Body)
		if err != nil {
			return err
		}
		return d.Check(st, e, t)
	}
	v.Apply = func(st *State, e Entry) {
		if t, err := decode(e.Args, e.Body); err == nil {
			d.Apply(st, t)
		}
	}
	v.Describe = func(st State, e Entry) string {
		t, _ := decode(e.Args, e.Body)
		return d.Line(st, t)
	}
	add(name, v)
}

// argv is one scanned command line: positionals and flags, in any order.
type argv struct {
	pos   []string
	vals  map[string][]string
	bools map[string]bool
	usage string
}

// scanArgs accepts --flag value, --flag=value and bare boolean flags
// anywhere among the positionals. A value that starts with "--" must use the
// --flag=value form.
func scanArgs(args []string, usageLine string, values, bools []string) (argv, error) {
	a := argv{vals: map[string][]string{}, bools: map[string]bool{}, usage: usageLine}
	isVal, isBool := setOf(values), setOf(bools)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			a.pos = append(a.pos, arg)
			continue
		}
		name, val, hasVal := strings.Cut(arg[2:], "=")
		switch {
		case isBool[name] && !hasVal:
			a.bools[name] = true
		case isVal[name] && hasVal:
			a.vals[name] = append(a.vals[name], val)
		case isVal[name] && i+1 < len(args):
			i++
			a.vals[name] = append(a.vals[name], args[i])
		case isVal[name]:
			return a, usage("--%s needs a value — usage: %s", name, usageLine)
		default:
			return a, usage("unknown flag --%s — usage: %s", name, usageLine)
		}
	}
	return a, nil
}

func setOf(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// str returns the last value of a flag, or "".
func (a argv) str(name string) string {
	if v := a.vals[name]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

// list returns a comma-separated flag as trimmed non-empty items.
func (a argv) list(name string) []string {
	var out []string
	for _, v := range a.vals[name] {
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	}
	return out
}

// exactly demands n positionals and says the usage line when it fails.
func (a argv) exactly(n int) error {
	if len(a.pos) != n {
		return usage("expected %d argument(s), got %d — usage: %s", n, len(a.pos), a.usage)
	}
	return nil
}
