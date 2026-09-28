//go:build !windows

package steploop

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

func finishedAssistant(id string, summary bool, failed bool) msgmodel.WithParts {
	finish := "stop"
	info := msgmodel.Assistant{
		MessageBase: msgmodel.MessageBase{ID: id, SessionID: "ses"},
		Finish:      &finish,
	}
	if summary {
		flag := true
		info.Summary = &flag
	}
	if failed {
		converted := msgmodel.NewUnknownError("boom")
		info.Error = &converted
	}
	return msgmodel.WithParts{Info: info}
}

// The verbatim tail keeps the assistant whose token count triggered the
// compaction, and the projection places it after the summary. Its count must
// not trigger a second boundary.
func TestCompactedAfterSeesANewerCompletedSummary(t *testing.T) {
	tail := finishedAssistant("msg_0001", false, false)
	summary := finishedAssistant("msg_0002", true, false)
	failedSummary := finishedAssistant("msg_0003", true, true)
	projection := []msgmodel.WithParts{summary, tail}
	if !CompactedAfter(projection, tail.Info.(msgmodel.Assistant)) {
		t.Fatal("a newer completed summary was not detected")
	}
	if CompactedAfter([]msgmodel.WithParts{tail}, tail.Info.(msgmodel.Assistant)) {
		t.Fatal("no summary at all was reported as compacted-after")
	}
	if CompactedAfter([]msgmodel.WithParts{failedSummary, tail}, tail.Info.(msgmodel.Assistant)) {
		t.Fatal("an errored summary attempt counted as a boundary")
	}
	older := finishedAssistant("msg_0000", true, false)
	if CompactedAfter([]msgmodel.WithParts{older, tail}, tail.Info.(msgmodel.Assistant)) {
		t.Fatal("an older summary counted as newer")
	}
}
