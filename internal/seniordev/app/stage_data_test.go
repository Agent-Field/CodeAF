//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
)

// A STAGE RECORD CARRIES A CURATED COPY OF ITS DATA: the plain facts a page can
// say, a list counted rather than carried, a sentence cut to a row, and none of
// the machinery — tree ids, paths, model pools — that stays on stderr.
func TestAStageRecordCarriesACuratedCopyOfItsData(t *testing.T) {
	raw := stageRecordData(map[string]any{
		"reason": "tests pass " + strings.Repeat("and more ", 40), "evidence": "go test ./... ok",
		"checklist_satisfied": true, "checklist_items": 5, "checklist_ticked": 4,
		"patch_bytes": 812, "patch_files": 4, "tree_sha": "t1", "commit_sha": "c1",
		"commands":  []any{map[string]any{"cmd": "go test ./..."}, map[string]any{"cmd": "go build ./..."}},
		"workspace": "/tmp/copy",
	})
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("data %s: %v", raw, err)
	}
	for _, key := range []string{"tree_sha", "commit_sha", "workspace", "evidence"} {
		if _, ok := got[key]; ok {
			t.Fatalf("the record carries %q, which is machinery: %s", key, raw)
		}
	}
	if got["checklist_items"] != float64(5) || got["checklist_ticked"] != float64(4) || got["patch_files"] != float64(4) || got["checklist_satisfied"] != true {
		t.Fatalf("the record lost a fact: %s", raw)
	}
	if got["commands"] != float64(2) {
		t.Fatalf("commands = %v, want the list counted", got["commands"])
	}
	if reason := got["reason"].(string); len(reason) > stageDataTextMost {
		t.Fatalf("the reason is %d bytes, want it cut to %d", len(reason), stageDataTextMost)
	}
	if stageRecordData(map[string]any{"tree_sha": "t1"}) != nil || stageRecordData(nil) != nil {
		t.Fatal("a stage with nothing to say carries data")
	}
}

// THE COPY FITS THE PROTOCOL'S CAP, whatever the stage held: the longest
// sentences go first, and the numbers stay.
func TestAStageRecordsDataFitsTheCap(t *testing.T) {
	data := map[string]any{"attempt": 2, "retry": 1, "max_retries": 3}
	for _, key := range []string{"reason", "detail", "error", "class", "reason_class", "phase", "source", "recorder", "summary_status", "from", "to"} {
		data[key] = strings.Repeat("é", 400)
	}
	raw := stageRecordData(data)
	if len(raw) > delegate.StageDataCap || len(raw) == 0 {
		t.Fatalf("data is %d bytes, want some, and at most %d", len(raw), delegate.StageDataCap)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["attempt"] != float64(2) || got["retry"] != float64(1) || got["max_retries"] != float64(3) {
		t.Fatalf("the numbers were dropped before the sentences: %s", raw)
	}
}

// A COMPACTION IS A STAGE, reported after the history was rewritten: summarized
// when the model's summary stands, fallback when the deterministic record
// stood in, with the tokens before and after.
func TestACompactionIsReportedAsAStage(t *testing.T) {
	var output bytes.Buffer
	sink := newSeniorDevCompactionDecisionSink(nil, newEventWriter(&output))
	sink.CompactionDecision(compaction.CompactionDecision{SummaryStatus: "valid", Before: 120000, After: 9000})
	sink.CompactionDecision(compaction.CompactionDecision{SummaryStatus: "summary-error"})
	var statuses []string
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var value event
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		if value.Stage != "compaction" {
			t.Fatalf("event = %+v, want the compaction stage", value)
		}
		statuses = append(statuses, value.Status)
	}
	if strings.Join(statuses, ",") != "summarized,fallback" {
		t.Fatalf("statuses = %v, want summarized then fallback", statuses)
	}
}
