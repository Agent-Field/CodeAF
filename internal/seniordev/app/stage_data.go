//go:build !windows

package app

import (
	"encoding/json"
	"sort"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// A stage's data goes two places. The whole of it goes to stderr, one line per
// stage, for a person reading why the run did what it did (eventWriter's
// noteStage). A small copy of it goes on the protocol's `stage` record, for
// codeaf's page to say in words: which attempt, how many requirements were
// ticked, how many files the hand-in held, what the project's own check
// found.
//
// THE COPY IS CURATED, NEVER COMPUTED. Every value on the record is a value the
// run already put in the stage's data; this file only chooses which, shortens a
// sentence and counts a list. Nothing here is new knowledge, and nothing here
// reaches the model.

// stageDataKeys are the keys a stage record may carry, each a plain fact a
// page can say. Tree and commit ids, paths, pools of models and the run's
// environment are left to stderr: they are machinery, and a page has nothing
// to say with them.
var stageDataKeys = map[string]bool{
	// implement: the attempt, the retries and corrections.
	"attempt": true, "retry": true, "max_retries": true, "delay_ms": true,
	"class": true, "http_status": true, "correction": true,
	"budget_exhausted": true, "transport_retries": true,
	// submit, and the reasons given anywhere.
	"reason": true, "reason_class": true, "detail": true, "error": true,
	"checklist_satisfied": true, "checklist_items": true, "checklist_ticked": true,
	"patch_bytes": true, "patch_files": true,
	// patch-summary.
	"files": true, "additions": true, "deletions": true, "binary_files": true,
	// verification and the checks of the tree.
	"commands": true, "vacuous": true, "phase": true, "failing": true,
	"timed_out": true, "suite_dead": true, "safety_regression": true,
	// landing and ship.
	"source": true, "timeout_ms": true,
	// bootstrap and intake.
	"recorder": true, "spec_bytes": true,
	// compaction-capacity and compaction.
	"limit_tokens": true, "pinned_capacity_tokens": true,
	"before_tokens": true, "after_tokens": true, "summary_status": true,
	// model-switch.
	"from": true, "to": true,
}

// stageDataTextMost is the most bytes one sentence on the record keeps: a
// reason or an error is read by a person in one row, and the whole of it is on
// stderr.
const stageDataTextMost = 160

// stageRecordData is a stage's data as the protocol record carries it: the
// allowed keys, each a number, a yes or no, or a sentence cut to
// [stageDataTextMost] bytes; a list counted rather than carried (a
// verification's commands are each a step record of their own); and the whole
// held to [delegate.StageDataCap] by dropping the longest sentences first. Nil
// when nothing is left, so a stage with nothing to say carries no data.
func stageRecordData(data map[string]any) json.RawMessage {
	kept := map[string]any{}
	for key, value := range data {
		if !stageDataKeys[key] {
			continue
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				kept[key] = clipBytes(oneLine(v), stageDataTextMost)
			}
		case bool, int, int64, uint64, float64:
			kept[key] = v
		case []any:
			kept[key] = len(v)
		}
	}
	for len(kept) > 0 {
		raw, err := json.Marshal(kept)
		if err != nil {
			return nil
		}
		if len(raw) <= delegate.StageDataCap {
			return raw
		}
		delete(kept, longestText(kept))
	}
	return nil
}

// longestText is the key whose value takes the most bytes, a sentence before a
// number, and the first in key order among equals, so the cut is the same on
// every run.
func longestText(kept map[string]any) string {
	keys := make([]string, 0, len(kept))
	for key := range kept {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	longest, most := keys[0], -1
	for _, key := range keys {
		size := 0
		if text, ok := kept[key].(string); ok {
			size = len(text) + 1
		}
		if size > most {
			longest, most = key, size
		}
	}
	return longest
}
