package head

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The live failure, seeded exactly: a job that wrote its whole judgement into a
// file and settled with a sentence about having written it. Everything the head
// could see about the verdict was the path.
const (
	assessmentVerdict = "The proposed architecture is valid: the plugin boundary it assumes already exists and the migration it describes is reversible."
	assessmentMeta    = "The deliverable is written and verified against the actual repo source. The assessment is complete and consistent."
)

// seedAssessmentJob settles one job whose summary is the meta sentence and a
// path, and returns the directory the artifact lives in so a test can put a
// second, unrecorded file beside it.
func seedAssessmentJob(t *testing.T, graph *store.Store, body string) (store.Node, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "architecture_plan_assessment.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	spliceSurgeryJob(t, graph, "task-6979", "Architecture plan review", "check whether the plan is valid")
	completeNodeWith(t, graph, "task-6979", assessmentMeta+"\nFiles:\n"+path)
	node, found, err := graph.Node("task-6979")
	if err != nil || !found {
		t.Fatalf("seeded job missing: found=%t err=%v", found, err)
	}
	return node, dir
}

// The whole point of the reader: the answer was one file open away and the head
// had no way to open it. It reaches the file by the path the job recorded, by
// the file's own name, and by nothing at all when the job wrote only one.
func TestReadReachesTheAnswerTheResultOnlyNames(t *testing.T) {
	graph := openHeadStore(t)
	node, _ := seedAssessmentJob(t, graph, assessmentVerdict)
	head := New(nil, graph)
	recorded := resultFiles(node)
	if len(recorded) != 1 {
		t.Fatalf("seeded job recorded %v, want exactly one path", recorded)
	}
	for name, argument := range map[string]string{
		"full path": recorded[0],
		"file name": filepath.Base(recorded[0]),
		"omitted":   "",
	} {
		t.Run(name, func(t *testing.T) {
			rendered, err := head.readArtifact(node, argument)
			if err != nil {
				t.Fatalf("read %q: %v", argument, err)
			}
			if !strings.Contains(rendered, assessmentVerdict) {
				t.Fatalf("read %q did not return the verdict:\n%s", argument, rendered)
			}
			if !strings.Contains(rendered, recorded[0]) {
				t.Fatalf("read %q did not say which file it opened:\n%s", argument, rendered)
			}
		})
	}
}

// The security boundary. The argument only ever CHOOSES among paths the graph
// recorded, so nothing the caller can type builds a path — not an absolute one
// somewhere else, not a neighbour of a real artifact, not a traversal out of the
// directory the job wrote in.
func TestReadOpensNothingTheGraphDoesNotPointAt(t *testing.T) {
	graph := openHeadStore(t)
	node, dir := seedAssessmentJob(t, graph, assessmentVerdict)
	head := New(nil, graph)
	secret := filepath.Join(dir, "private_notes.md")
	if err := os.WriteFile(secret, []byte("not for the head"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, argument := range map[string]string{
		"absolute elsewhere":   "/etc/passwd",
		"unrecorded neighbour": secret,
		"traversal":            filepath.Join(dir, "..", filepath.Base(dir), "private_notes.md"),
		"bare name":            "private_notes.md",
	} {
		t.Run(name, func(t *testing.T) {
			rendered, err := head.readArtifact(node, argument)
			if err == nil {
				t.Fatalf("read %q was allowed and returned:\n%s", argument, rendered)
			}
			if strings.Contains(err.Error(), "not for the head") {
				t.Fatalf("the refusal leaked the file's contents: %v", err)
			}
			// A refusal the model cannot act on is a dead end, so it names the
			// set that would have worked.
			if !strings.Contains(err.Error(), "architecture_plan_assessment.md") {
				t.Fatalf("refusal did not say what the job did write: %v", err)
			}
		})
	}
}

// The boundary's second half. A recorded name is trusted for where it sits, not
// for where it leads: resolved, it has to land in the directory it was recorded
// in, so a worker that wrote a link out of its workspace hands back a refusal.
func TestReadRefusesARecordedPathThatLinksOutOfItsDirectory(t *testing.T) {
	graph := openHeadStore(t)
	root := t.TempDir()
	inside, outside := filepath.Join(root, "workspace"), filepath.Join(root, "elsewhere")
	for _, dir := range []string{inside, outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(outside, "secrets.md")
	if err := os.WriteFile(target, []byte("the private thing"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(inside, "assessment.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	spliceSurgeryJob(t, graph, "linked", "Linked job", "write the assessment")
	completeNodeWith(t, graph, "linked", "wrote it\n"+link)
	node, found, err := graph.Node("linked")
	if err != nil || !found {
		t.Fatalf("seeded job missing: found=%t err=%v", found, err)
	}

	rendered, err := New(nil, graph).readArtifact(node, link)
	if err == nil {
		t.Fatalf("a link out of the workspace was followed and returned:\n%s", rendered)
	}
	if !strings.Contains(err.Error(), "outside the directory it was recorded in") {
		t.Fatalf("refusal did not name the reason: %v", err)
	}
	if strings.Contains(err.Error(), "the private thing") {
		t.Fatalf("the refusal leaked what it refused to open: %v", err)
	}
}

// A document is allowed to be longer than the prompt can hold; it is not allowed
// to enter the prompt silently truncated. The window keeps the opening and the
// ending — a verdict is as often in the last paragraph as the first — and says
// between them exactly how much is missing.
func TestArtifactReadKeepsBothEndsAndMarksWhatItElided(t *testing.T) {
	graph := openHeadStore(t)
	const opening = "OPENING: the plan under review has three parts."
	const ending = "VERDICT: valid, with one reversible caveat."
	body := opening + "\n" + strings.Repeat("filler paragraph that nobody needs.\n", 900) + ending
	if len(body) <= beltArtifactBytes {
		t.Fatalf("the oversize fixture is only %d bytes", len(body))
	}
	node, _ := seedAssessmentJob(t, graph, body)

	rendered, err := New(nil, graph).readArtifact(node, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, opening) || !strings.Contains(rendered, ending) {
		t.Fatalf("the window dropped one end of the document:\n%s", rendered)
	}
	marker := fmt.Sprintf("…[%d bytes elided of %d]…", len(body)-beltArtifactBytes, len(body))
	if !strings.Contains(rendered, marker) {
		t.Fatalf("elision marker %q missing from:\n%s", marker, rendered)
	}
	// The cap bounds the file's bytes; the path line, the size and the marker
	// are the read's own frame and are counted separately by design.
	frame := len(rendered) - beltArtifactBytes
	if frame > 200 {
		t.Fatalf("read is %d bytes, %d over its %d-byte content cap",
			len(rendered), frame, beltArtifactBytes)
	}
}

// A job with no recorded artifact is not a hole in the boundary: there is
// simply nothing to open, and the reader says so instead of guessing a path.
func TestReadingAJobThatWroteNothingIsRefused(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "spoken", "Spoken answer", "say what the number is")
	completeNodeWith(t, graph, "spoken", "The number is 41.")
	node, found, err := graph.Node("spoken")
	if err != nil || !found {
		t.Fatalf("seeded job missing: found=%t err=%v", found, err)
	}
	if rendered, err := New(nil, graph).readArtifact(node, "anything.md"); err == nil {
		t.Fatalf("a job with no files opened one:\n%s", rendered)
	}
}

// A one-leaf job records its file under the leaf while every question about it
// is asked of the job. A boundary the caller cannot address is the same as no
// reader at all, so a job's parts are inside its set.
func TestAJobReachesTheFileItsOwnLeafWrote(t *testing.T) {
	graph := openHeadStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "leaf-output.md")
	if err := os.WriteFile(path, []byte(assessmentVerdict), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "parent", Title: "Review", Brief: "check the plan", Stage: 0},
		{ID: "parent-leaf", Parent: "parent", Brief: "write the assessment", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "leafread", Intent: "check the plan"}); err != nil {
		t.Fatal(err)
	}
	completeNodeWith(t, graph, "parent-leaf", "wrote it up\n"+path)
	node, found, err := graph.Node("parent")
	if err != nil || !found {
		t.Fatalf("seeded parent missing: found=%t err=%v", found, err)
	}
	rendered, err := New(nil, graph).readArtifact(node, "leaf-output.md")
	if err != nil {
		t.Fatalf("the job could not reach its own leaf's file: %v", err)
	}
	if !strings.Contains(rendered, assessmentVerdict) {
		t.Fatalf("read returned no substance:\n%s", rendered)
	}
}
