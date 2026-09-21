package orchestrate

import (
	"reflect"
	"strings"
	"testing"
)

// untaught is the one field the contract has and the law deliberately does not
// teach: Kind selects a subharness node kind, empty is the agent loop, and a
// planner shown the field would pick one. The run decides that, not the model.
var untaught = map[string]bool{"kind": true}

// The law quotes the Amendment schema verbatim, which means the schema is in two
// places: here as struct tags, there as a JSON block a model is held to. Drift
// either way is a run that refuses a well-formed amendment or a planner taught a
// key the decoder will reject.
func TestTheLawTeachesTheContractsOwnKeys(t *testing.T) {
	for _, shape := range []any{Amendment{}, Node{}, Cancel{}, DonePlan{}} {
		typ := reflect.TypeOf(shape)
		for at := 0; at < typ.NumField(); at++ {
			tag, _, _ := strings.Cut(typ.Field(at).Tag.Get("json"), ",")
			if tag == "" || tag == "-" || untaught[tag] {
				continue
			}
			if !strings.Contains(PlannerPrompt, `"`+tag+`"`) {
				t.Errorf("%s.%s is on the wire as %q and the law never names it",
					typ.Name(), typ.Field(at).Name, tag)
			}
		}
	}
}

// The reverse direction, and the more expensive one: a key the law teaches and
// the contract does not have is a planner writing amendments that will not parse.
func TestTheLawTeachesNothingTheContractRefuses(t *testing.T) {
	legal := map[string]bool{}
	for _, shape := range []any{Amendment{}, Node{}, Cancel{}, DonePlan{}} {
		typ := reflect.TypeOf(shape)
		for at := 0; at < typ.NumField(); at++ {
			if tag, _, _ := strings.Cut(typ.Field(at).Tag.Get("json"), ","); tag != "" {
				legal[tag] = true
			}
		}
	}
	// PART THREE's schema block is the contract as the model reads it; the worked
	// example is prose and quotes ids, so only the schema is held to this.
	schema, ok := between(PlannerPrompt, "# PART THREE · THE AMENDMENT", "# PART FOUR")
	if !ok {
		t.Fatal("PART THREE is not in the law, so the schema is not quoted anywhere")
	}
	for _, key := range keysIn(schema) {
		if !legal[key] {
			t.Errorf("the law teaches the key %q and no shape in the contract has it", key)
		}
	}
}

func TestTheLawIsPresentAndWhole(t *testing.T) {
	if len(PlannerPrompt) < 4000 {
		t.Fatalf("the law is %d bytes, which is not the ten laws", len(PlannerPrompt))
	}
	for _, law := range []string{
		"1 · YOU PLAN, NEVER WORK", "2 · FAN-OUT", "3 · SMALL-NODE",
		"4 · CONTEXT CONTRACT", "5 · MARGINAL VALUE", "6 · NOOP IS CHEAP",
		"7 · FUEL", "8 · STEERING OUTRANKS THE PLAN", "9 · DONE", "10 · WRITE-SCOPE",
	} {
		if !strings.Contains(PlannerPrompt, law) {
			t.Errorf("law %q is missing", law)
		}
	}
	// The five things the view is rendered from (planner.go's renderView), named
	// so a rendering that grew a sixth section has to come here and say so.
	for _, part := range []string{"GOAL", "FUEL", "RESULTS", "FRONTIER", "STEERING"} {
		if !strings.Contains(PlannerPrompt, part) {
			t.Errorf("the law never describes the view's %s section", part)
		}
	}
}

// keysIn pulls every "key": out of a JSON-ish block.
func keysIn(text string) []string {
	var out []string
	for rest := text; ; {
		open := strings.IndexByte(rest, '"')
		if open < 0 {
			return out
		}
		rest = rest[open+1:]
		close := strings.IndexByte(rest, '"')
		if close < 0 {
			return out
		}
		word, after := rest[:close], strings.TrimLeft(rest[close+1:], " ")
		if strings.HasPrefix(after, ":") {
			out = append(out, word)
		}
		rest = rest[close+1:]
	}
}

func between(text, from, to string) (string, bool) {
	start := strings.Index(text, from)
	if start < 0 {
		return "", false
	}
	rest := text[start:]
	end := strings.Index(rest, to)
	if end < 0 {
		return rest, true
	}
	return rest[:end], true
}
