package subharness

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeSalvaged is what every case here really asserts: not that some bytes came
// back, but that the strict decoder reads them into the fields the model meant.
func decodeSalvaged(t *testing.T, raw string) (map[string]any, Salvaged) {
	t.Helper()
	out, err := SalvageDetail(raw)
	if err != nil {
		t.Fatalf("salvage refused %q: %v", clipText(raw, 200), err)
	}
	var into map[string]any
	if err := json.Unmarshal(out.JSON, &into); err != nil {
		t.Fatalf("salvage returned bytes the decoder will not read: %v\n%s", err, out.JSON)
	}
	return into, out
}

func clipText(text string, at int) string {
	if len(text) <= at {
		return text
	}
	return text[:at] + "…"
}

// A clean reply must climb nothing. The rung name is the signal a run prints, so
// a pipeline that quietly "salvaged" every well-formed page would be reporting a
// model problem that is not there.
func TestSalvageLeavesACleanReplyAlone(t *testing.T) {
	raw := `{"cues": ["research"], "n": 3}`
	got, out := decodeSalvaged(t, raw)
	if !out.Clean() || out.Rung != SalvageStrict {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageStrict)
	}
	if string(out.JSON) != raw {
		t.Errorf("the bytes were rewritten: %s", out.JSON)
	}
	if got["n"] != float64(3) {
		t.Errorf("decoded %v", got)
	}
}

func TestSalvageStripsAFence(t *testing.T) {
	raw := "```json\n{\"id\": {\"name\": \"journal\"}}\n```"
	got, out := decodeSalvaged(t, raw)
	if out.Rung != SalvageExtract {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageExtract)
	}
	if id := got["id"].(map[string]any); id["name"] != "journal" {
		t.Errorf("decoded %v", got)
	}
}

// The apology in front and the summary behind are the two halves of the same
// habit, and both have to go.
func TestSalvageStripsProseOnBothSides(t *testing.T) {
	raw := `Sure! Here's the harness you asked for, "as designed":

{"harness": {"id": {"name": "journal"}}}

Let me know if you'd like me to adjust the verify rung.`
	got, out := decodeSalvaged(t, raw)
	if out.Rung != SalvageExtract {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageExtract)
	}
	if _, ok := got["harness"]; !ok {
		t.Errorf("decoded %v", got)
	}
}

// The expensive failure this whole file was written for.
func TestSalvageNormalisesTypographicDelimiters(t *testing.T) {
	raw := "{“cues”: [“event log”], “justification”: “the topology is four nodes”}"
	got, out := decodeSalvaged(t, raw)
	if out.Rung != SalvageSanitize {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageSanitize)
	}
	if got["justification"] != "the topology is four nodes" {
		t.Errorf("decoded %v", got)
	}
	cues := got["cues"].([]any)
	if len(cues) != 1 || cues[0] != "event log" {
		t.Errorf("cues = %v", cues)
	}
}

// A STRING VALUE IS THE MODEL'S. An em-dash in a brief is what the designer wrote
// and it survives every rung; a salvage pass that flattened it would be editing
// the design in order to parse it.
func TestSalvagePreservesPunctuationInsideStrings(t *testing.T) {
	raw := "{“brief”: “write the memo — ranked, with each cause’s observable … and stop”}"
	got, _ := decodeSalvaged(t, raw)
	brief := got["brief"].(string)
	for _, must := range []string{"—", "’", "…"} {
		if !strings.Contains(brief, must) {
			t.Errorf("%q was rewritten out of the brief: %q", must, brief)
		}
	}
}

// The mirror of the case above: a legal page whose ASCII string HAPPENS to quote
// something typographically must not have that inner quote read as a delimiter.
func TestSalvageDoesNotTreatAQuotedPhraseAsADelimiter(t *testing.T) {
	raw := "{\"desc\": \"adopt the “event log” design\", \"n\": 1}"
	got, out := decodeSalvaged(t, raw)
	if !out.Clean() {
		t.Errorf("a legal page climbed to %q", out.Rung)
	}
	if got["desc"] != "adopt the “event log” design" {
		t.Errorf("decoded %q", got["desc"])
	}
}

func TestSalvageRemovesTrailingCommasAndComments(t *testing.T) {
	raw := `{
  // the cues a person would actually type
  "cues": ["event log", "journal",],
  "n": 2, /* counted in the COST pass */
}`
	got, out := decodeSalvaged(t, raw)
	if out.Rung != SalvageLenient {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageLenient)
	}
	if len(got["cues"].([]any)) != 2 || got["n"] != float64(2) {
		t.Errorf("decoded %v", got)
	}
}

// A comma before a closing brace is a trailing comma; a comma inside a brief is
// a comma, and the same goes for a slash. This is why the rung is a scanner and
// not a regular expression.
func TestSalvageLeavesCommasAndSlashesInsideStrings(t *testing.T) {
	raw := `{"brief": "rank them, // then stop, ]", "n": 1,}`
	got, _ := decodeSalvaged(t, raw)
	if got["brief"] != "rank them, // then stop, ]" {
		t.Errorf("the brief was edited: %q", got["brief"])
	}
}

func TestSalvageDropsBytesThatAreNotUTF8(t *testing.T) {
	raw := "{\"brief\": \"the whisper\xff\xfe.cpp lane\", \"n\": 1}"
	got, out := decodeSalvaged(t, raw)
	if out.Rung != SalvageSanitize {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageSanitize)
	}
	if brief := got["brief"].(string); strings.ContainsRune(brief, '�') {
		t.Errorf("a replacement character survived: %q", brief)
	} else if brief != "the whisper.cpp lane" {
		t.Errorf("brief = %q", brief)
	}
}

// A brace inside a string is not a brace. Extract has to know that or it cuts the
// object off in the middle of a sentence.
func TestSalvageCountsBracesOutsideStringsOnly(t *testing.T) {
	raw := "Here it is:\n{\"brief\": \"the args are {\\\"path\\\": {{input}}} — call it once\", \"tail\": {\"n\": 1}}\nDone."
	got, _ := decodeSalvaged(t, raw)
	if !strings.Contains(got["brief"].(string), "{{input}}") {
		t.Errorf("the brief was cut: %q", got["brief"])
	}
	if got["tail"].(map[string]any)["n"] != float64(1) {
		t.Errorf("the object was cut short: %v", got)
	}
}

// A long brief laid out over lines is illegal JSON and is nobody's design error.
func TestSalvageEscapesRawNewlinesInsideStrings(t *testing.T) {
	raw := "{\"brief\": \"first line\nsecond line\", \"n\": 1}"
	got, out := decodeSalvaged(t, raw)
	if out.Rung != SalvageLenient {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageLenient)
	}
	if got["brief"] != "first line\nsecond line" {
		t.Errorf("brief = %q", got["brief"])
	}
}

// The rungs compose: one reply, fenced AND smart-quoted AND trailing-comma'd.
func TestSalvageClimbsSeveralRungsAtOnce(t *testing.T) {
	raw := "Here you go:\n```json\n{“cues”: [“journal”,], “n”: 1,}\n```"
	got, out := decodeSalvaged(t, raw)
	if out.Rung != SalvageLenient {
		t.Errorf("rung = %q, want %q", out.Rung, SalvageLenient)
	}
	if len(out.Climbed) != 4 {
		t.Errorf("climbed = %v", out.Climbed)
	}
	if got["n"] != float64(1) {
		t.Errorf("decoded %v", got)
	}
}

// The error is written to be handed back to a model, so it has to say where the
// parser stopped rather than that something, somewhere, was wrong.
func TestSalvageReportsWhereItFailed(t *testing.T) {
	_, err := Salvage(`{"cues": ["a" "b"], "justification": "x"}`)
	if err == nil {
		t.Fatal("a broken object salvaged anyway")
	}
	for _, must := range []string{SalvageLenient, "byte", "around here"} {
		if !strings.Contains(err.Error(), must) {
			t.Errorf("the error does not mention %q: %v", must, err)
		}
	}
}

func TestSalvageRefusesAReplyWithNoObjectInIt(t *testing.T) {
	if _, err := Salvage("I'm sorry, I can't design that harness."); err == nil {
		t.Error("prose salvaged into JSON")
	}
}

// Salvage is handed whatever a model produced, including the reply that stopped
// mid-escape because the completion budget ran out. Every rung has to survive
// that without panicking, because a panic here takes the run with it — and the
// honest answer to a truncated reply is an error the repair turn can read.
func TestSalvageSurvivesTheWorstItWillBeHanded(t *testing.T) {
	for _, raw := range []string{
		"",
		"{",
		"}",
		`{"a": "b`,
		`{"a": "b\`,
		"{“a”: “b",
		"```json",
		"{{{{{{",
		"[[[]]",
		"{\"a\": \"\xff",
		strings.Repeat("{\"a\":", 200),
		`{"a": /* unterminated`,
		`{"a": 1,,}`,
	} {
		out, err := SalvageDetail(raw)
		if err != nil {
			continue
		}
		if !json.Valid(out.JSON) {
			t.Errorf("salvage accepted %q and returned bytes that are not JSON: %s", raw, out.JSON)
		}
	}
}
