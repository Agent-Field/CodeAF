package exec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The test docs/SUBHARNESSES.md ends its checklist with, executed.
//
// "The test of the law: grep the reconciler, narrator, TUI, head, and store for
// the string 'swe' — none of them may contain it." It is not a style rule. The
// engine of record is singular, and it stays singular exactly as long as no
// part of it can tell which worker ran a leaf: the moment the reconciler has an
// `if subharness == "swe"` in it, the seam has stopped being a seam and the
// next specialist arrives as a rewrite of the dispatch paths instead of a
// registration.
//
// The word-boundary match is what makes this runnable rather than aspirational:
// "answer" contains those three letters and is not a mention of anything.
//
// Test files are exempt and only test files. A test may name a worker because
// naming one is how you check that nothing else does — internal/store's own
// subharness test splices a node whose worker is swe, which is the durability
// of the field being proven, not the store branching on it.
func TestNoEngineOfRecordPackageNamesTheSWEWorker(t *testing.T) {
	// The named packages, plus the reason each one is on the list.
	forbidden := map[string]string{
		"../resident": "the reconciler dispatches by registration, never by name",
		"../tui":      "a job card renders a node, and every node renders the same way",
		"../head":     "the head speaks about work, not about which worker did it",
		"../store":    "the store carries the choice as data and never reads it",
	}
	mention := regexp.MustCompile(`(?i)\bswe\b`)
	for directory, why := range forbidden {
		err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for index, line := range strings.Split(string(raw), "\n") {
				if mention.MatchString(line) {
					t.Errorf("%s:%d names the swe worker — %s\n  %s",
						path, index+1, why, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The other side of the same law, stated so it cannot rot into "swe is
// mentioned nowhere": there are exactly four places it may be named, and the
// executor is one of them. A test that only forbids would pass just as well
// against a build where the worker had been deleted.
func TestTheSWEWorkerIsNamedWhereItIsAllowedToBe(t *testing.T) {
	if SWESubharness != "swe" {
		t.Fatalf("the worker's name is %q; docs/SUBHARNESSES.md and every profile file say swe", SWESubharness)
	}
	worker := &SWE{}
	if worker.Subharness() != SWESubharness {
		t.Fatalf("the executor answers to %q", worker.Subharness())
	}
}

// THE MACHINE STREAM NEVER BECOMES PROSE.
//
// The reported defect, in one sentence: a task room's record showed hundreds of
// raw `{"id":"evt_…","type":"message.part.delta",…}` lines rendered as content,
// because every NDJSON line the coding engine wrote was `note`d into the
// recorder and the room draws one row per line it cannot parse. This walks the
// engine's real event shapes past the consumer and asserts the fork: the raw
// feed is whole in the sidecar, the recorder holds only sentences, and what it
// holds is in the recorder's OWN grammar so the room's existing lens draws it as
// tool rows with bounded output boxes.
func TestTheRawEngineStreamNeverReachesTheRecorderAsProse(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, "41")
	run := &sweRun{trace: trace, outcome: &Outcome{}}

	// The shapes are copied from a real recorder written by the reporter's own
	// profile (task-9196), trimmed but not reshaped.
	const part = `"id":"prt_1","sessionID":"ses_1","messageID":"msg_1","type":"tool",` +
		`"callID":"call_1","tool":"bash"`
	lines := []string{
		`{"type":"stage","stage":"bootstrap","status":"ready","data":{}}`,
		`{"id":"evt_1","type":"session.created","properties":{"sessionID":"ses_1"}}`,
		`{"id":"evt_2","type":"message.part.updated","properties":{"sessionID":"ses_1","part":{` + part +
			`,"state":{"status":"pending","input":{},"raw":""}}}}`,
		`{"id":"evt_3","type":"message.part.updated","properties":{"sessionID":"ses_1","part":{` + part +
			`,"state":{"status":"running","input":{"command":"go test ./..."},"raw":""}}}}`,
		`{"id":"evt_4","type":"message.part.delta","properties":{"sessionID":"ses_1","delta":"ok"}}`,
		`{"id":"evt_5","type":"message.part.delta","properties":{"sessionID":"ses_1","delta":"ay"}}`,
		`{"id":"evt_6","type":"message.part.updated","properties":{"sessionID":"ses_1","part":{` + part +
			`,"state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok  aforge 1.2s"}}}}`,
		`{"id":"evt_7","type":"message.part.updated","properties":{"sessionID":"ses_1","part":{` + part +
			`,"state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok  aforge 1.2s"}}}}`,
		`{"id":"evt_8","type":"message.part.updated","properties":{"sessionID":"ses_1","part":` +
			`{"id":"prt_2","type":"text","text":"the tests pass","time":{"start":1,"end":2}}}}`,
		`{"id":"evt_9","type":"message.part.updated","properties":{"sessionID":"ses_1","part":` +
			`{"id":"prt_2","type":"text","text":"the tests pass","time":{"start":1,"end":2}}}}`,
		`a plain line the runtime printed`,
	}
	for _, line := range lines {
		run.consume(line + "\n")
	}
	trace.close()

	recorded, err := os.ReadFile(tracePath(t, space, "41"))
	if err != nil {
		t.Fatal(err)
	}
	recorder := string(recorded)
	for _, never := range []string{`{"id":"evt_`, "message.part.delta", "sessionID", `"properties"`} {
		if strings.Contains(recorder, never) {
			t.Fatalf("the recorder carries the machine stream as prose (%q):\n%s", never, recorder)
		}
	}
	// The recorder's own five shapes, which is what makes the room's lens draw
	// this as a tool row with a bounded box rather than as an unparsed note.
	for _, want := range []string{
		"stage: bootstrap ready",
		`call bash {"command":"go test ./..."}`,
		"  → 15B: ok  aforge 1.2s",
		"text: the tests pass",
		"a plain line the runtime printed",
		streamNote,
	} {
		if !strings.Contains(recorder, want) {
			t.Fatalf("the recorder is missing %q:\n%s", want, recorder)
		}
	}
	// ONE ROW PER THING THAT HAPPENED. The bus republishes a part on every
	// update; a recorder that wrote a row per republication would be the same
	// defect one shape further in.
	if n := strings.Count(recorder, "call bash"); n != 1 {
		t.Fatalf("one call was recorded %d times:\n%s", n, recorder)
	}
	if n := strings.Count(recorder, "text: the tests pass"); n != 1 {
		t.Fatalf("one sentence was recorded %d times:\n%s", n, recorder)
	}
	if lines := strings.Count(strings.TrimSpace(recorder), "\n") + 1; lines > 8 {
		t.Fatalf("the recorder is %d lines for eleven events — it is still a stream:\n%s",
			lines, recorder)
	}

	// Nothing was lost: the raw feed is whole, in the file no surface renders.
	full, _, err := space.ScratchPath(streamName("41"))
	if err != nil {
		t.Fatal(err)
	}
	spilled, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("the raw stream was dropped rather than moved: %v", err)
	}
	sidecar := string(spilled)
	for _, want := range []string{"message.part.delta", `"id":"evt_1"`, `"status":"pending"`} {
		if !strings.Contains(sidecar, want) {
			t.Fatalf("the sidecar is not the whole stream — missing %q", want)
		}
	}
	if strings.Contains(sidecar, "a plain line the runtime printed") {
		t.Fatal("a line that is not machine-shaped was spilled to the machine sidecar")
	}
}
