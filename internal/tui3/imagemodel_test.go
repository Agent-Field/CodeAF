package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// WHICH IMAGE MODEL DREW, and what it was asked for.
//
// `generate_image` is the one hand where the model behind the call is a choice
// a person makes per call, so the row says which one ran and the expansion says
// what it was given. Both facts come off roads the row already draws from — the
// arguments session sends, and the tool's own result line — which is what these
// tests pin: the words, and the nothing that is drawn when nobody knows.

// The finished row names the file and then the image model, and the model is a
// qualifier behind it rather than the substance.
func TestTheImageRowNamesTheModelThatDrew(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, call("generate_image",
		`{"prompt":"a harbour at dawn","path":"harbour.png"}`,
		"harbour.png — 64×32 png, 1.2KB, generated on vendor/paint-5"))

	line := toolLineOf(t, a)
	if !strings.Contains(line, "harbour.png · paint-5") {
		t.Fatalf("the row did not name the image model:\n%s", line)
	}
}

// And the vendor prefix comes off, because that is the spelling this surface
// already gives a model everywhere else ([modelui.ModelWord]).
func TestTheImageRowSpellsTheModelTheShortWay(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, call("generate_image",
		`{"prompt":"a harbour","path":"harbour.png"}`,
		"harbour.png — 64×32 png, 1.2KB, generated on vendor/paint-5"))

	if line := toolLineOf(t, a); strings.Contains(line, "vendor/paint-5") {
		t.Fatalf("the row carried the provider prefix:\n%s", line)
	}
}

// A result that names no model adds NOTHING to the row — not a placeholder and
// not a dangling separator. This is the running call, and the transcript
// replayed from an engine that never wrote the marker.
func TestAnUnknownImageModelAddsNothingToTheRow(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, call("generate_image",
		`{"prompt":"a harbour","path":"harbour.png"}`, "harbour.png — 64×32 png, 1.2KB"))

	line := toolLineOf(t, a)
	if !strings.Contains(line, "harbour.png") {
		t.Fatalf("the file went missing with the model:\n%s", line)
	}
	if strings.Contains(line, "harbour.png ·") {
		t.Fatalf("a qualifier was drawn for a model nobody knows:\n%s", line)
	}
}

// Opening the step shows what went IN: the prompt in the person's own words,
// and the model that drew under it.
func TestOpeningAnImageStepShowsThePromptAndTheModel(t *testing.T) {
	a, _ := pictureApp(t, call("generate_image",
		`{"prompt":"a harbour at dawn","path":".aforge-v3/images/harbour.png"}`,
		".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on vendor/paint-5"))

	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, `"a harbour at dawn"`) {
		t.Fatalf("the prompt was not shown:\n%s", body)
	}
	if !strings.Contains(body, "drawn with paint-5") {
		t.Fatalf("the model line was not shown:\n%s", body)
	}
	// The picture is still the answer under it all.
	if !strings.Contains(body, halfBlock) {
		t.Fatalf("the picture went away:\n%s", body)
	}
}

// A call that asked in its own words keeps BOTH halves: what somebody would
// say again, and what actually ran.
func TestAnImageStepThatAskedForAModelSaysWhatItGot(t *testing.T) {
	a, _ := pictureApp(t, call("generate_image",
		`{"prompt":"a harbour","path":".aforge-v3/images/harbour.png","model":"best"}`,
		".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on vendor/paint-5"))

	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, "asked for best · drawn with paint-5") {
		t.Fatalf("the request and the answer were not both kept:\n%s", body)
	}
}

// And an expansion with no model anywhere says nothing about one.
func TestAnImageStepWithNoModelAnywhereSaysNothingAboutOne(t *testing.T) {
	a, _ := pictureApp(t, call("generate_image",
		`{"prompt":"a harbour","path":".aforge-v3/images/harbour.png"}`,
		".aforge-v3/images/harbour.png — 64×32 png, 1.2KB"))

	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, `"a harbour"`) {
		t.Fatalf("the prompt was not shown:\n%s", body)
	}
	if strings.Contains(body, "drawn with") || strings.Contains(body, "asked for") {
		t.Fatalf("a model was named where none is known:\n%s", body)
	}
}

// The other inputs are one dim line each, and an input nobody gave is not a
// line at all.
func TestTheImageStepShowsTheInputsItWasGiven(t *testing.T) {
	a, _ := pictureApp(t, call("generate_image",
		`{"prompt":"a harbour","path":".aforge-v3/images/harbour.png","size":"1024x1024",`+
			`"reference_paths":["art/sketch.png"]}`,
		".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on vendor/paint-5"))

	body := strings.Join(openFirst(t, a), "\n")
	for _, want := range []string{"1024x1024", "from art/sketch.png"} {
		if !strings.Contains(body, want) {
			t.Fatalf("%q was not shown:\n%s", want, body)
		}
	}
	// Nothing was given for the frame's shape, so there is no line for it.
	if strings.Contains(body, "16:9") {
		t.Fatalf("an input nobody gave was drawn:\n%s", body)
	}
}

// The three shapes of the model line, stated once, away from the frame.
func TestTheModelLineSaysOnlyWhatIsKnown(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		args   string
		output string
		want   string
	}{
		{"nothing known", `{"prompt":"x"}`, "", ""},
		{"only the answer", `{"prompt":"x"}`,
			"a.png — 1KB, generated on vendor/paint-5", "drawn with paint-5"},
		{"only the request", `{"prompt":"x","model":"seedream"}`, "", "asked for seedream"},
		{"both, and they differ", `{"prompt":"x","model":"best"}`,
			"a.png — 1KB, generated on vendor/paint-5", "asked for best · drawn with paint-5"},
		{"both, and they agree", `{"prompt":"x","model":"paint-5"}`,
			"a.png — 1KB, generated on vendor/paint-5", "drawn with paint-5"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := imageModelLine(argsOf(testCase.args), testCase.output); got != testCase.want {
				t.Fatalf("line = %q, want %q", got, testCase.want)
			}
		})
	}
}
