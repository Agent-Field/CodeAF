package session

// THE SCHEMA LAW: A PARAMETER DESCRIPTION IS A CONTRACT, IN ONE CLAUSE.
//
// The tool-schema block rides in front of every request this session makes —
// dozens per task, again on the next task, forever (prefixbudget_test.go weighs
// the whole of it). A paragraph written into a field description is therefore
// the most expensive prose in this repository, and it is also the prose nobody
// re-reads: the lane that adds it is measuring whether the model got the field
// right, not what the addition cost.
//
// So this file is the shape internal/iconlaw is for icons — a law that walks the
// tree itself and fails the build rather than waiting for review to notice. It
// asks three things of every string inside every schema this package can build:
//
//   - NO ALL-CAPS RUN OF THREE OR MORE WORDS. Caps are how this codebase spells
//     a stated law in a Go comment, and a description that borrows the habit
//     spends its emphasis where nothing is being emphasised: when four fields
//     all shout, none of them does. One capitalised word for the load-bearing
//     term is still allowed, and is usually the right amount.
//   - NO EN OR EM DASH. A dash is what a writer reaches for when a sentence has
//     grown a second clause it did not plan for, which is exactly the sentence
//     this law is trying to prevent. It is also the one character in this text
//     that costs three bytes instead of one on the wire.
//   - AT MOST [schemaFieldCeiling] BYTES. A field description says what the
//     field is FOR and what it takes. The rule the field GOVERNS goes with it;
//     the rule about which tool to reach for belongs to the page's routing
//     table, stated once for the whole belt rather than once per tool.
//
// WHAT TO DO WHEN IT FAILS. Cut the sentence that says which tool to use, and
// the sentence that says the same thing the field above it said. If what is left
// genuinely will not fit — a judge paragraph the model is being asked to write
// TO, rather than a field it is being asked to fill IN — register it in
// [schemaLawAllowed] with a reason and the bytes it is allowed, and the reason
// has to say why the words could not be fewer.
//
// The failure names the tool, the field and the offending text, because a lane
// reading this has just written one of them and has no other way to see which.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// schemaFieldCeiling is the byte budget one field description gets. It is a
// BYTE count for prefixbudget_test.go's reason: bytes are what this process can
// measure exactly, and every tokenizer this build talks to is within a small
// factor of four bytes to the token.
const schemaFieldCeiling = 200

// schemaCapsRun matches three or more shouted words in a row. Two is a term of
// art with a qualifier ("ONE simple command"); three is a lane writing prose in
// capitals, which is the habit this law exists to stop.
var schemaCapsRun = regexp.MustCompile(`\b[A-Z]{2,}\b(?:[^\w\n]{1,3}\b[A-Z]{2,}\b){2,}`)

// schemaLawException is one registered field: why its prose could not be fewer
// words, and the bytes it is allowed. The cap is part of the entry rather than
// waived entirely, so a registered field cannot go on growing unwatched.
type schemaLawException struct {
	bytes  int
	reason string
}

// schemaLawAllowed is the whole of the exception ledger, keyed by "tool.field".
//
// IT ONLY SHRINKS. An entry leaves when its owning lane cuts the field to fit;
// nothing is added to buy room for a sentence a routing table could carry. Two
// kinds of entry live here and the reason says which:
//
//   - A JUDGE PARAGRAPH. The model is writing prose INTO the field rather than
//     picking a value for it, and the description is the brief it writes to.
//     propose_task's `brief` is the archetype — it is a specification of what a
//     self-contained brief must contain, and every clause in it is a way briefs
//     have actually come back wrong.
//   - PI'S OWN WORDS. The seven verbs this belt takes verbatim from the lean
//     baseline (internal/exec/bare) are byte-identical on purpose: they are what
//     the diet is measured AGAINST, and editing them would move the ruler.
const schemaFieldOwnedElsewhere = "owned by another lane; registered so this law lands green and reported to it"

var schemaLawAllowed = map[string]schemaLawException{
	"propose_task.brief": {
		bytes: 1000,
		reason: "a judge paragraph: the model writes a self-contained brief TO this " +
			"specification, and each clause names a way briefs have come back wrong",
	},
	"edit.edits": {bytes: 240, reason: "pi's own words, byte-identical (internal/exec/bare); the diet is measured against them"},
	// Registered rather than edited, because the lane that owns the file is
	// cutting it in this same wave and two lanes editing one string is a merge
	// conflict for no gain. The quick-task wave's fields came into line on
	// its own branch and their entries left with it; `fork` left with the verb.
	"bash.background": {bytes: 260, reason: schemaFieldOwnedElsewhere + " (lane B, internal/exec/bare)"},
}

func TestEverySchemaStringIsAContractInOneClause(t *testing.T) {
	// THE WHOLE BELT AND THE WHOLE SHELF, which is the walk the manual gate
	// takes (manual_test.go states why at length): a verb held back for the tool
	// block's sake is still a verb this build can hand a model, so its prose is
	// still prose this law is about. The profile directory is handed over for
	// the same reason it is there — the settings pair is conditional on one, and
	// a law that built the belt without one would go green on text it never saw.
	agent := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	agent.tools = agent.belt()
	tools := agent.offeredTools()
	if len(tools) == 0 {
		t.Fatal("the belt is empty, so this law would pass on nothing")
	}

	seen, built := map[string]bool{}, map[string]bool{}
	for _, tool := range tools {
		built[tool.Name] = true
		if len(tool.Schema) == 0 {
			continue
		}
		var schema any
		if err := json.Unmarshal(tool.Schema, &schema); err != nil {
			t.Errorf("%s: its schema does not parse, so nothing can be read out of it: %v", tool.Name, err)
			continue
		}
		walkSchemaStrings(schema, "", func(field, text string) {
			key := tool.Name + "." + field
			seen[key] = true
			exception, registered := schemaLawAllowed[key]

			if run := schemaCapsRun.FindString(text); run != "" && !registered {
				t.Errorf("%s: the %s description shouts %q.\n"+
					"  Capitals are how a Go comment spells a law; a description spends its emphasis on one word at most.\n"+
					"  in: %s", tool.Name, field, run, text)
			}
			if index := strings.IndexAny(text, "–—"); index >= 0 && !registered {
				t.Errorf("%s: the %s description has an en or em dash at byte %d.\n"+
					"  A dash is a second clause the sentence did not plan for; make it a full stop or cut it.\n"+
					"  in: %s", tool.Name, field, index, text)
			}

			ceiling, why := schemaFieldCeiling, ""
			if registered {
				ceiling, why = exception.bytes, exception.reason
			}
			if len(text) <= ceiling {
				return
			}
			if registered {
				t.Errorf("%s: the %s description is %d bytes, past the %d its entry in schemaLawAllowed permits (%s).\n"+
					"  A registered field is bounded, not waived: cut it back, or say in the reason what grew.\n"+
					"  in: %s", tool.Name, field, len(text), ceiling, why, text)
				return
			}
			t.Errorf("%s: the %s description is %d bytes, past the %d a field description gets.\n"+
				"  Cut the sentence saying which tool to reach for (the page's routing table states it once) and the\n"+
				"  sentence the field above it already said. If it truly cannot fit, register it in schemaLawAllowed\n"+
				"  with a reason and its bytes.\n"+
				"  in: %s", tool.Name, field, len(text), schemaFieldCeiling, text)
		})
	}

	// AND THE LEDGER ONLY SHRINKS. An entry whose field this build no longer has
	// is an exemption still standing for text that is gone, and the next long
	// description to land under that name would inherit it silently.
	//
	// IT IS ASKED OF TOOLS THIS BELT ACTUALLY BUILT, and not of every key. A verb
	// that is not on this belt is a verb whose text this walk never read — a
	// conditional tool, or one a lane beside this is still landing — and calling
	// its entry stale would be this law reporting on prose it cannot see.
	for key, exception := range schemaLawAllowed {
		tool, _, _ := strings.Cut(key, ".")
		if built[tool] && !seen[key] {
			t.Errorf("schemaLawAllowed still exempts %s (%s), and %s no longer has that field: delete the entry",
				key, exception.reason, tool)
		}
	}
}

// walkSchemaStrings visits every description a model reads in one schema, under
// the field it belongs to.
//
// THE NAME IT REPORTS IS THE FIELD'S OWN, not the JSON pointer. A lane reading
// the failure is looking for `expects` or `parts.role` in a Go string; a path
// like `.properties.parts.items.properties.role.description` is the same fact
// spelled so that it cannot be grepped for. Only `description` values are read:
// an enum value or a type word is a wire token rather than prose, and holding
// those to a prose law would fail on `additionalProperties`.
func walkSchemaStrings(value any, field string, visit func(field, text string)) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	if text, present := object["description"].(string); present && field != "" {
		visit(field, text)
	}
	// An object's own fields, each named for what the model types.
	if properties, present := object["properties"].(map[string]any); present {
		for name, property := range properties {
			inner := name
			if field != "" {
				inner = field + "." + name
			}
			walkSchemaStrings(property, inner, visit)
		}
	}
	// An array's element carries its parent's name: `parts` and `parts.items`
	// are one field to everybody except the encoder.
	if items, present := object["items"]; present {
		walkSchemaStrings(items, field, visit)
	}
}
