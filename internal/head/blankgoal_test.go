package head

import (
	"context"
	"os"
	"strings"
	"testing"
)

// THE PERSON'S OWN WORDS ARE ALWAYS A VALID GOAL. A compile whose reply decodes
// but carries a blank goal and no question used to end the whole run with
// "empty goal" while the instruction sat in the request the whole time; now
// the words stand as the goal, and the receipt says the compiler supplied no
// reading of its own (#335).
func TestABlankGoalBecomesTheVerbatimRequest(t *testing.T) {
	client := &formatClient{replies: []string{`{"goal":"","scale":"task"}`}}
	brief, err := NewCompiler(client).Compile(context.Background(), "rename the file", "")
	if err != nil {
		t.Fatalf("a blank goal ended the compile: %v", err)
	}
	if brief.Goal != "Verbatim request:\nrename the file" {
		t.Fatalf("goal = %q, want the request alone under its anchor", brief.Goal)
	}
	if brief.Note != NoGlossNote {
		t.Fatalf("note = %q, want the receipt to say the compiler supplied no reading", brief.Note)
	}
	if brief.Scale != ScaleTask {
		t.Fatalf("scale = %q, want the rest of the brief kept", brief.Scale)
	}
}

// A goal the model did write earns no note: the receipt line is for the one
// case where the person would otherwise not know what happened.
func TestAWrittenGoalCarriesNoNote(t *testing.T) {
	client := &formatClient{replies: []string{`{"goal":"Rename the file","scale":"task"}`}}
	brief, err := NewCompiler(client).Compile(context.Background(), "rename the file", "")
	if err != nil {
		t.Fatal(err)
	}
	if brief.Note != "" {
		t.Fatalf("note = %q, want none", brief.Note)
	}
}

// The shape that ended a real run, replayed through the compiler's own parsing
// path: a several-hundred-word brief, a reply cut inside the goal — by far the
// longest field — and a continuation that restarts a whole object from the
// field after the cut. The continuation goes out without a shape hint now, and
// even when a model restarts anyway the run keeps the person's words.
func TestALongBriefCutInsideTheGoalStillCompiles(t *testing.T) {
	brief, err := os.ReadFile("testdata/brief-335.txt")
	if err != nil {
		t.Fatal(err)
	}
	instruction := strings.TrimSpace(string(brief))
	if words := len(strings.Fields(instruction)); words < 400 {
		t.Fatalf("the fixture has %d words, want the several-hundred-word brief", words)
	}
	client := &formatClient{replies: []string{
		`{"structure":"stratifies","goal":"Land lanes L2, L3 and L4 of the lens issue so that the repository builds and every named test`,
		`{"title":"Lens lanes L2 to L4","scale":"project","contract":"","parts":[],"builds_on":[],"assumptions":["The branch is the one already carrying L1."],"question":"","question_options":[],"trial_of":0}`,
	}}
	compiled, err := NewCompiler(client).WithOneShotErrands().Compile(context.Background(), instruction, "")
	if err != nil {
		t.Fatalf("the brief did not compile: %v", err)
	}
	if !strings.HasSuffix(compiled.Goal, "Verbatim request:\n"+instruction) {
		t.Fatalf("the goal lost the person's words:\n%s", compiled.Goal)
	}
	if compiled.Note != NoGlossNote || compiled.Title != "Lens lanes L2 to L4" || compiled.Scale != ScaleProject {
		t.Fatalf("brief = %+v, want the restarted fields kept and the note set", compiled)
	}
	// The attempt asks for JSON on the wire; the continuation of a fragment
	// asks for the room and nothing else, because a fragment has no shape.
	if len(client.formats) != 2 || client.formats[0] != "json_object" || client.formats[1] != "" {
		t.Fatalf("response formats = %v, want json_object on the attempt and none on the continuation", client.formats)
	}
}
