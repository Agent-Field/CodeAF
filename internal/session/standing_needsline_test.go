package session

// THE LINE A PERSON READS WHEN A FIRING STOPPED ON THEM.
//
// A watch that runs a task carries the conversation's own approval rules and
// has nobody to ask, so a call the rules would have put to somebody is refused
// where it stands. The worker is told that in the engine's own words, which is
// right: it has to act on them. Those words then travelled, unchanged, onto the
// person's home screen under a mark saying they were needed — `refused in a
// task: default — nobody to ask`, four pieces of machinery in a row that is
// supposed to tell somebody what to do.
//
// These tests pin the sentence that goes to the person, and they pin that the
// engine's own line is not the one that goes.

import (
	"strings"
	"testing"
)

// The command below is invented for this test. It is not anybody's.
const needsLineCommand = `go test ./internal/widgets -run TestTheGauge`

func TestAFiringStoppedOnThePersonNamesWhatItWantedToRun(t *testing.T) {
	line := standingRefusal(Event{
		Kind: EventToolFailed,
		Tool: "bash",
		Args: `{"command":"` + needsLineCommand + `"}`,
		Hint: "refused in a task: default — nobody to ask",
	})
	if line == "" {
		t.Fatal("a refused call left the person no line at all")
	}
	if !strings.Contains(line, needsLineCommand) {
		t.Fatalf("the line does not name the command that was wanted: %q", line)
	}
	for _, machinery := range []string{"refused in a task", "default", "nobody to ask", "resolver"} {
		if strings.Contains(strings.ToLower(line), machinery) {
			t.Fatalf("the line a person reads carries machinery %q: %q", machinery, line)
		}
	}
	// AND IT DOES NOT CLAIM MORE THAN IT KNOWS. This reader sees one failed
	// call and nothing about the calls that came back fine before it.
	for _, overclaim := range []string{"nothing", "every", "all "} {
		if strings.Contains(strings.ToLower(line), overclaim) {
			t.Fatalf("the line claims more than one refused call can support (%q): %q", overclaim, line)
		}
	}
}

func TestAFiringStoppedOnANonShellHandNamesTheHand(t *testing.T) {
	line := standingRefusal(Event{
		Kind: EventToolFailed,
		Tool: "web_fetch",
		Args: `{"url":"https://example.invalid/a-page"}`,
		Hint: "refused in a task: default — nobody to ask",
	})
	if !strings.Contains(line, "web_fetch") {
		t.Fatalf("the line does not name the hand that was refused: %q", line)
	}
}

func TestAFiringWhoseArgumentsWillNotReadSaysSomething(t *testing.T) {
	line := standingRefusal(Event{
		Kind: EventToolFailed,
		Tool: "bash",
		Args: `{"command":`, // cut off mid-object, as a capped or malformed call is
		Hint: "refused in a task: default — nobody to ask",
	})
	if !strings.HasSuffix(line, standingRefusalSomething) {
		t.Fatalf("an unreadable call should leave the line saying %q: %q", standingRefusalSomething, line)
	}
}

func TestAnOrdinaryToolFailureIsNotThePersonsBusiness(t *testing.T) {
	line := standingRefusal(Event{
		Kind: EventToolFailed,
		Tool: "bash",
		Args: `{"command":"` + needsLineCommand + `"}`,
		Hint: "exit status 1",
	})
	if line != "" {
		t.Fatalf("an ordinary failure raised a line for the person: %q", line)
	}
}

// TestTheEnginesOwnRefusalIsUnchanged is the other half of the law: the worker
// still reads what it always read. This reader takes the engine's sentence as
// INPUT and never rewrites it, so a change here cannot reach the model.
func TestTheEnginesOwnRefusalIsUnchanged(t *testing.T) {
	const enginesOwn = "refused in a task: default — nobody to ask"
	event := Event{Kind: EventToolFailed, Tool: "bash",
		Args: `{"command":"` + needsLineCommand + `"}`, Hint: enginesOwn}
	_ = standingRefusal(event)
	if event.Hint != enginesOwn {
		t.Fatalf("the engine's own refusal was rewritten: %q", event.Hint)
	}
}
