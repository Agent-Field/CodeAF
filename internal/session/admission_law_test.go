package session

// THE LAW: EVERY DOOR THAT ADMITS WORK COMPILES ITS CONTEXT THE SAME WAY.
//
// The defect this guards against is not a wrong answer, it is a missing one: a
// door added later that builds a spec of its own and never calls the compiler
// hands its worker a brief with no conversation behind it, and nothing fails.
// It was exactly that shape elsewhere — two doors carried a projection, the
// division road and the automatic handover did not, and the tests passed.
//
// So the check is on the SOURCE. Every file that admits into the graph either
// reaches [Agent.admissionContext] or is named below with the reason it does
// not. A new door is a compile-time-visible decision rather than a silent
// omission.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// admissionExempt are the admissions that are not a worker being handed a brief,
// with the reason each is not.
//
// Neither of these composes a worker's opening document: a design node is handed
// to the designer's body (harness_task.go) and a run node is handed to a
// compiled program (subharness_run.go). Their specs carry a brief and an
// acceptance so the room and the index have a row to draw, and nothing in either
// reads a quoted conversation.
var admissionExempt = map[string]string{
	"harness_task.go":   "a sub-harness being designed, not a worker with a brief",
	"subharness_run.go": "a compiled program being run, not a worker with a brief",
}

func TestEveryAdmissionDoorCompilesItsContext(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package: %v", err)
	}
	doors := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		source := string(body)
		if !strings.Contains(source, "graph.admit(") && !strings.Contains(source, "graph().admit(") {
			continue
		}
		if name == "task_run.go" {
			// The admission itself lives here; it is the thing the doors call.
			continue
		}
		doors++
		if _, exempt := admissionExempt[name]; exempt {
			continue
		}
		if !strings.Contains(source, "admissionContext()") && !strings.Contains(source, "admission:") {
			t.Errorf("%s admits work without compiling an admission context. "+
				"Call a.admissionContext() at the door, or add the file to admissionExempt with the reason.", name)
		}
	}
	// A guard that stops finding the doors is a guard that passes forever.
	if doors < 5 {
		t.Fatalf("only %d admission doors found; the scan has stopped seeing them", doors)
	}
}
