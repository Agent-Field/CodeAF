package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

// The object has to survive being written down and read back, because that is
// the whole of what it is for: a spec that cannot make the round trip through
// the journal is prose again by the second attempt.
func TestSpecRoundTrips(t *testing.T) {
	original := Spec{
		Instruction: "write the thing the request asked for",
		Method:      "read what exists before writing anything",
		Sources:     []string{"one place", "another place"},
		Done: Done{
			Produces: []string{"the named result"},
			Conditions: []Check{
				{Kind: CheckRun, Check: "the stated command", Expect: "it reports success"},
				{Kind: CheckRead, Check: "the named result", Expect: "it is present and not a placeholder"},
			},
		},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Spec
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Render(0) != original.Render(0) {
		t.Fatalf("round trip changed the spec:\n%s\n---\n%s", original.Render(0), restored.Render(0))
	}
	if len(restored.Done.Conditions) != 2 || restored.Done.Conditions[0].Kind != CheckRun {
		t.Fatalf("criterion did not survive: %+v", restored.Done)
	}
}

// The rollback proof. An empty spec renders to nothing at all, so every reader
// that falls through to Brief and Contract takes exactly the path it took
// before this wave existed — no marker, no placeholder, no empty heading.
func TestEmptySpecRendersToNothing(t *testing.T) {
	var empty Spec
	if !empty.Empty() {
		t.Fatal("the zero spec says it carries something")
	}
	if rendered := empty.Render(0); rendered != "" {
		t.Fatalf("the empty spec rendered %q, want the empty string", rendered)
	}
	// A spec whose fields are present but blank is the same fact.
	blank := Spec{Instruction: "   ", Method: "\n", Done: Done{}}
	if !blank.Empty() || blank.Render(0) != "" {
		t.Fatalf("a blank spec is not empty: %q", blank.Render(0))
	}
	// And it must survive the journal as nothing, not as an object.
	encoded, err := json.Marshal(struct {
		Spec Spec `json:"spec,omitzero"`
	}{})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "{}" {
		t.Fatalf("an empty spec was journaled as %s", encoded)
	}
}

// Field order is a contract, not a formatting preference: this string becomes a
// cache prefix, and the half that is stable for the life of a job has to sit in
// front of the half a retry rewrites.
func TestSpecRenderOrderIsStable(t *testing.T) {
	spec := Spec{
		Instruction: "INSTRUCTION",
		Method:      "METHOD",
		Sources:     []string{"SOURCE"},
		Done:        Done{Produces: []string{"PRODUCES"}},
	}
	rendered := spec.Render(0)
	positions := []int{
		strings.Index(rendered, "METHOD"),
		strings.Index(rendered, "INSTRUCTION"),
		strings.Index(rendered, "SOURCE"),
		strings.Index(rendered, "PRODUCES"),
	}
	for index := range positions {
		if positions[index] < 0 {
			t.Fatalf("field %d missing from render:\n%s", index, rendered)
		}
		if index > 0 && positions[index] < positions[index-1] {
			t.Fatalf("field order is not stable:\n%s", rendered)
		}
	}
	// Rendering twice is rendering the same bytes.
	if spec.Render(0) != rendered {
		t.Fatal("two renders of one spec disagreed")
	}
}

func TestSpecRenderIsBounded(t *testing.T) {
	spec := Spec{Instruction: strings.Repeat("é", 500)}
	rendered := spec.Render(64)
	if len(rendered) > 64 {
		t.Fatalf("render ran to %d bytes past a 64-byte limit", len(rendered))
	}
	if !isValidUTF8(rendered) {
		t.Fatal("the bounded render cut a rune in half")
	}
	if unbounded := spec.Render(0); len(unbounded) <= 64 {
		t.Fatalf("an unbounded render was clipped anyway: %d bytes", len(unbounded))
	}
}

func isValidUTF8(value string) bool {
	for _, r := range value {
		if r == '�' {
			return false
		}
	}
	return true
}

// A criterion is only worth carrying if a reader can settle it, and a weak
// model will happily return more conditions than anyone asked for.
func TestNormalizeDoneBoundsAndCleans(t *testing.T) {
	var raw Done
	raw.Produces = []string{"  a file  ", "   "}
	for index := 0; index < MaxConditions+4; index++ {
		raw.Conditions = append(raw.Conditions, Check{Kind: "RUN", Check: "check", Expect: "expect"})
	}
	raw.Conditions = append(raw.Conditions, Check{Kind: "run", Check: "   ", Expect: "nothing"})
	clean := NormalizeDone(raw)
	if len(clean.Produces) != 1 || clean.Produces[0] != "a file" {
		t.Fatalf("produces = %#v", clean.Produces)
	}
	if len(clean.Conditions) != MaxConditions {
		t.Fatalf("conditions = %d, want the cap %d", len(clean.Conditions), MaxConditions)
	}
	for _, condition := range clean.Conditions {
		if condition.Kind != CheckRun {
			t.Fatalf("kind was not normalized: %q", condition.Kind)
		}
	}
	// An unrecognised kind falls to the weaker claim rather than being invented.
	odd := NormalizeDone(Done{Conditions: []Check{{Kind: "compile", Check: "something"}}})
	if odd.Conditions[0].Kind != CheckRead {
		t.Fatalf("kind %q, want %q", odd.Conditions[0].Kind, CheckRead)
	}
}
