package session

// THE PICTURE HAND REFUSES A PROMPT THAT IS NOT THERE, AND PAYS NOTHING TO DO IT.
//
// This is the check tools_video.go and tools_music.go both make on their own
// first line, and the picture door is the one that lost it: the block around it
// moved when the web pair and the picture hand became plain functions a command
// line could share, and the guard did not move with it. Nothing went red,
// because nothing was asserting it.
//
// WHY IT MATTERS MORE HERE THAN IN THE ARGUMENT CHECKS AROUND IT. A provider
// asked to draw nothing still bills the call, and what comes back reads as the
// provider having a bad minute rather than as a caller having sent an empty
// string. So the cost is real money and a diagnosis pointing the wrong way.

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestThePictureHandRefusesAnEmptyPromptAndSendsNothing(t *testing.T) {
	painter := &scriptedMedia{base64: "", mediaType: "image/png"}
	agent, _ := newPainterAgent(t, painter, "paint/model")

	for _, testCase := range []struct {
		name string
		args string
	}{
		{"no prompt at all", `{}`},
		{"a prompt of spaces", `{"prompt":"   "}`},
		{"a prompt of one newline", `{"prompt":"\n"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, isError := runTool(t, agent, "generate_image", testCase.args)
			if !isError {
				t.Fatalf("a call with no prompt reported success: %s", result)
			}
			if !strings.Contains(result, "prompt is required") {
				t.Fatalf("the refusal reads %q, which does not say what is missing", result)
			}
		})
	}

	// AND THE PROVIDER WAS NEVER ASKED, which is the whole of why the guard is
	// before the request rather than in the reading of its answer. A test that
	// only checked the sentence would pass on a build that paid for every one
	// of these and then complained about what came back.
	painter.mu.Lock()
	sent := len(painter.seen)
	painter.mu.Unlock()
	if sent != 0 {
		t.Fatalf("the picture hand sent %d request(s) for a prompt that was not there", sent)
	}
}

// AND A PROMPT THAT IS REALLY THERE STILL DRAWS, which keeps the test above
// from passing on a door that refuses everything.
func TestAPromptWithWordsInItStillReachesThePainter(t *testing.T) {
	picture := pngOfSize(t, 8, 6)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	agent, _ := newPainterAgent(t, painter, "paint/model")

	result, isError := runTool(t, agent, "generate_image", `{"prompt":"  a harbour at dusk  "}`)
	if isError {
		t.Fatalf("a real prompt was refused: %s", result)
	}
	painter.mu.Lock()
	defer painter.mu.Unlock()
	if len(painter.seen) != 1 {
		t.Fatalf("the painter saw %d requests, want one", len(painter.seen))
	}
	// AND IT ARRIVES TRIMMED, which is what the guard reads and therefore what
	// the request must carry: two readings of one prompt is how a door starts
	// refusing calls it then sends anyway.
	if got := painter.seen[0].Prompt; got != "a harbour at dusk" {
		t.Fatalf("the painter was sent %q, want the trimmed prompt", got)
	}
}
