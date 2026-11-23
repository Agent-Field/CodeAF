package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A step sent back can finish between two reports, so the worker's last report
// and its current copy both show the same question. The person already answered
// the first one, so the second must still reach the factory.
func TestDiffReportsTheSameQuestionAskedAgainBetweenReports(t *testing.T) {
	const q = "review is next · continue, or send back the step before?"
	item := func(log ...factory.LogLine) factory.Item {
		return factory.Item{State: factory.StateNeedsYou, Question: q, QKind: factory.QKindApprove, Stream: &factory.Stream{Cur: 2, Log: log}}
	}
	at := func(n int) time.Time { return time.Unix(int64(n), 0) }
	first := factory.LogLine{At: at(1), Tone: "ask", Text: q}
	sent := item(first)
	again := item(first, factory.LogLine{At: at(2), Tone: "said", Text: "noted: check it"}, factory.LogLine{At: at(3), Tone: "thought", Text: "exit 0"}, factory.LogLine{At: at(4), Tone: "ask", Text: q})
	report, ok := Diff(sent, again)
	if !ok || report.Question == nil || report.Question.Question != q || report.QuestionKind != factory.QKindApprove {
		t.Fatalf("the second ask did not travel: ok=%v %+v", ok, report.Question)
	}
	still, _ := Diff(sent, item(first, factory.LogLine{At: at(2), Tone: "thought", Text: "still waiting"}))
	if still.Question != nil {
		t.Fatalf("a question still standing was asked again: %+v", still.Question)
	}
}
