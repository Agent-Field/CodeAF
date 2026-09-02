// A task spec is an object, authored once and carried forward.
//
// It used to be prose authored per-scale and re-authored on every retry, which
// is how a replacement node lost the module name, the filename and the
// acceptance check its predecessor had been given: the retry path did not
// re-target a spec, it wrote a new one from failure context, and prose has no
// field a rewrite can be forbidden from touching. An object does. Instruction
// and Method may be re-aimed; Done travels verbatim, because the criterion the
// work is judged against did not change when the worker did.
//
// Nothing here is specific to a kind of work. Kind is "run" or "read" and that
// is the only structural distinction the spec makes: a condition is either
// settled by executing something and reading the outcome, or by reading the
// artifact and finding something present. A prose deliverable's conditions are
// read conditions, a buildable one's are run conditions, and the orchestrator
// never inspects which.
package plan

import (
	"strings"
	"unicode/utf8"
)

// Check is one condition of a done-criterion.
//
// Every field is required for the check to be settleable by someone who has the
// result in front of them and did not do the work — which is the whole test of a
// criterion. Check says what to run or what to look for; Expect says what its
// outcome must be, or what would make it absent.
type Check struct {
	// Kind is "run" or "read". Universal across harnesses: a condition is
	// either settled by executing something and reading an outcome, or by
	// reading the artifact and finding something present.
	Kind   string `json:"kind"`
	Check  string `json:"check"`
	Expect string `json:"expect"`
}

// Check kinds. Two, because there are two ways a person holding a result can
// settle a question about it, and no third that is not one of these wearing a
// domain's clothes.
const (
	CheckRun  = "run"
	CheckRead = "read"
)

// MaxConditions caps a criterion's condition count.
//
// The cap is not tidiness. A weaker model handed "state the criterion" will
// happily produce twelve conditions, and every condition it invents becomes a
// requirement nobody made — so the ceiling is the structural half of the prompt
// clause that forbids inventing them.
const MaxConditions = 6

// Done is the positive stopping condition: what must be true once the work has
// landed, as distinct from the steps that get there.
type Done struct {
	// Produces names the identifiable outputs by the names they will carry, so
	// a reader holding only the criterion can tell whether they exist.
	Produces   []string `json:"produces,omitempty"`
	Conditions []Check  `json:"conditions,omitempty"`
}

// Empty reports whether this criterion says anything at all. An absent
// criterion is legal everywhere and is what every path did before criteria
// existed.
func (d Done) Empty() bool {
	return len(d.Produces) == 0 && len(d.Conditions) == 0
}

// Sentence renders the criterion as the one sufficiency statement a reader
// holding only the result can test: the produces line and every condition,
// in one line. It is the form that is journaled as a first-class event per
// node (see internal/store/planjournal.go), so a run's stopping condition is
// falsifiable from its own artifacts rather than only as a field inside the
// plan blob. Empty when the criterion says nothing, which is legal everywhere.
func (d Done) Sentence() string {
	if d.Empty() {
		return ""
	}
	var out strings.Builder
	if len(d.Produces) > 0 {
		out.WriteString("Produces: ")
		out.WriteString(strings.Join(d.Produces, "; "))
	}
	for _, condition := range d.Conditions {
		check := strings.TrimSpace(condition.Check)
		if check == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString(" | ")
		}
		kind := strings.TrimSpace(condition.Kind)
		if kind != CheckRun && kind != CheckRead {
			kind = CheckRead
		}
		out.WriteString("(")
		out.WriteString(kind)
		out.WriteString(") ")
		out.WriteString(check)
		if expect := strings.TrimSpace(condition.Expect); expect != "" {
			out.WriteString(" — ")
			out.WriteString(expect)
		}
	}
	return out.String()
}

// Spec is what a worker is handed: one object, authored once, carried forward.
//
// Instruction and Method are the two prose halves that already existed as
// Node.Brief and Node.Contract, named here for what they are. Done is new and
// is the reason the object exists at all — without a positive criterion, growth
// can only ever be bounded negatively, by counting rounds.
type Spec struct {
	Instruction string   `json:"instruction,omitempty"`
	Method      string   `json:"method,omitempty"`
	Done        Done     `json:"done,omitzero"`
	Sources     []string `json:"sources,omitempty"`
	// Accept is the acceptance checklist: the behaviours the REQUEST states,
	// read from the request before any work existed. Done is the criterion the
	// planner wrote about what the work will produce; this is what the person
	// asked for, and the two are different objects because they answer to
	// different authors. See accept.go.
	//
	// It travels here rather than beside the gate for the reason Done does: this
	// is the one object in the system carried forward verbatim through a retry
	// (RetargetSpec), so a repair round is judged against the same checklist its
	// predecessor was. Render deliberately omits it — the worker is never shown
	// the list it will be checked on.
	Accept []Point `json:"accept,omitempty"`
	// Constraints are the rules the person stated about what the run may or may
	// not DO. Unlike Accept, which belongs to the node that delivers, these are
	// stamped on every node of the job: the person said it about the run, so
	// every worker the run starts is under it. See constraint.go, and Render
	// below, which puts them FIRST.
	Constraints []Constraint `json:"constraints,omitempty"`
}

// Empty reports whether this spec carries nothing. An empty spec renders to the
// empty string, and every reader downstream falls back to the fields it read
// before the spec existed — which is what makes the whole wave a one-line read
// swap to roll back.
func (s Spec) Empty() bool {
	return strings.TrimSpace(s.Instruction) == "" &&
		strings.TrimSpace(s.Method) == "" &&
		len(s.Sources) == 0 &&
		len(s.Accept) == 0 &&
		len(s.Constraints) == 0 &&
		s.Done.Empty()
}

// Render writes the spec as the text a worker or a foreign engine reads.
//
// THE RULES THE PERSON SET COME FIRST, ABOVE EVERYTHING. A rule about what the
// run may or may not do is not one consideration among several: it is the
// boundary the rest of the spec is written inside, and a worker that reads its
// assignment before the boundary has already decided what to do by the time it
// meets the rule. It costs nothing at the cache seam either — constraints are
// stamped once per job and are the most stable bytes in the whole object.
//
// Field order past them is fixed and Method comes next: it is the half that is
// stable for the life of a job, so it belongs at the front of a string that will
// be a cache prefix once this render reaches an engine of its own. Done comes
// last because it is what a retry rewrites least and an instruction rewrites
// most — the order puts the churn where a prefix match has already been spent.
//
// limit bounds the whole render in bytes; zero or less is unbounded. An empty
// spec renders to "".
func (s Spec) Render(limit int) string {
	if s.Empty() {
		return ""
	}
	var out strings.Builder
	section := func(label, body string) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(label)
		out.WriteString(":\n")
		out.WriteString(body)
	}
	section(ConstraintsHeading, strings.Join(ConstraintLines(s.Constraints), "\n"))
	section("How this kind of work is done well", s.Method)
	section("The work", s.Instruction)
	if len(s.Sources) > 0 {
		section("What it touches", strings.Join(s.Sources, "; "))
	}
	if !s.Done.Empty() {
		var done strings.Builder
		if len(s.Done.Produces) > 0 {
			done.WriteString("It produces: ")
			done.WriteString(strings.Join(s.Done.Produces, "; "))
		}
		for _, condition := range s.Done.Conditions {
			check := strings.TrimSpace(condition.Check)
			if check == "" {
				continue
			}
			if done.Len() > 0 {
				done.WriteString("\n")
			}
			kind := strings.TrimSpace(condition.Kind)
			if kind != CheckRun && kind != CheckRead {
				kind = CheckRead
			}
			done.WriteString("- (")
			done.WriteString(kind)
			done.WriteString(") ")
			done.WriteString(check)
			if expect := strings.TrimSpace(condition.Expect); expect != "" {
				done.WriteString(" — ")
				done.WriteString(expect)
			}
		}
		section("Done when", done.String())
	}
	return clipSpec(out.String(), limit)
}

// Criterion is the wave's rollback switch. On, the brief call returns an
// instruction and a done-criterion together. Off, it returns today's prose
// brief and every spec's Done stays empty — which every reader downstream
// already handles, because an absent criterion has always been legal.
var Criterion = true

// NormalizeDone bounds and cleans what a model returned. A criterion is only
// worth carrying if a reader can settle it, so a condition with no check is
// dropped, an unrecognised kind falls back to read — the weaker claim — and the
// list is capped.
func NormalizeDone(done Done) Done {
	var clean Done
	for _, produces := range done.Produces {
		if name := strings.TrimSpace(produces); name != "" {
			clean.Produces = append(clean.Produces, name)
		}
	}
	for _, condition := range done.Conditions {
		if len(clean.Conditions) >= MaxConditions {
			break
		}
		check := strings.TrimSpace(condition.Check)
		if check == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(condition.Kind))
		if kind != CheckRun && kind != CheckRead {
			kind = CheckRead
		}
		clean.Conditions = append(clean.Conditions, Check{
			Kind:   kind,
			Check:  check,
			Expect: strings.TrimSpace(condition.Expect),
		})
	}
	return clean
}

// clipSpec bounds a render on a rune boundary, so a clipped spec is still text.
func clipSpec(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimRight(cut, " \n\t")
}
