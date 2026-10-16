package secaf

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// quietSink is a reader's sink that keeps only the terminal.
type quietSink struct{}

func (quietSink) Hello(delegate.Hello)       {}
func (quietSink) Stage(delegate.StageRecord) {}
func (quietSink) Step(delegate.StepRecord)   {}
func (quietSink) Terminal(delegate.Terminal) {}

// endLineData is the data of the `end` line sec's real ending becomes in an
// action log, through the emitter, the reader and the log's own writer.
func endLineData(t *testing.T, ending delegate.Ending) (json.RawMessage, *delegate.EndFacts) {
	t.Helper()
	var wire bytes.Buffer
	if err := delegate.NewEmitter(&wire).Terminal(ending); err != nil {
		t.Fatal(err)
	}
	reading, err := delegate.Read(&wire, quietSink{})
	if err != nil || reading.Terminal == nil {
		t.Fatalf("the terminal did not read back: %+v, %v", reading, err)
	}
	dir := t.TempDir()
	if err := delegate.AppendAction(dir, delegate.EndAction(time.Unix(0, 0).UTC(), *reading.Terminal)); err != nil {
		t.Fatal(err)
	}
	logged, err := delegate.ReadActions(dir, 0)
	if err != nil || len(logged) != 1 {
		t.Fatalf("read back %d lines, %v", len(logged), err)
	}
	return logged[0].Data, logged[0].Ending()
}

// THE REAL "NOTHING TO AUDIT" ENDING SAYS SO AS A FACT. An audit of the
// changes on a tree with none ends pass with nothing_to_audit:true and no
// count, and its `end` line holds exactly the data the delegate golden pins.
func TestTheRealNothingToAuditEndingCarriesTheFact(t *testing.T) {
	repo := vulnerableRepo(t)
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "base")
	models := &stubModels{}
	host := &stubHost{workspace: repo, records: t.TempDir(), api: models.serve(t)}
	run(context.Background(), host, options{brief: "changes", severity: "low", sessions: 4, maxTurns: 5}, io.Discard)
	if host.ending == nil || host.ending.Status != delegate.StatusPass || models.calls != 0 {
		t.Fatalf("an audit with nothing changed ended %+v after %d calls", host.ending, models.calls)
	}
	data, facts := endLineData(t, *host.ending)
	if facts == nil || facts.NothingToAudit == nil || !*facts.NothingToAudit ||
		facts.Confirmed != nil || facts.Likely != nil || facts.Inconclusive != nil || facts.RuledOut != nil || facts.HuntCoverage != nil {
		t.Fatalf("the ending's facts are %+v", facts)
	}
	golden, err := os.Open(filepath.Join("..", "delegate", "testdata", "end-sec-nothing.golden"))
	if err != nil {
		t.Fatal(err)
	}
	defer golden.Close()
	lines := bufio.NewScanner(golden)
	if !lines.Scan() {
		t.Fatal("the golden is empty")
	}
	var pinned struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(lines.Bytes(), &pinned); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, pinned.Data) {
		t.Fatalf("the real ending wrote %s, the golden pins %s", data, pinned.Data)
	}
}
