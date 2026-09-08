package session

// A NODE'S REPORT CARRIES THE FENCED TAIL IT PROMISES, PROVED (issue #577):
// bounded ordinary prose, a well-formed fenced exception, an honest cut mark,
// and the same report on the checkpoint row the person reads later.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const fencedTaskReply = "The command finished, exit 0.\n" +
	"Its last line, verbatim:\n" +
	"```\n" +
	"all 41 checks passed\n" +
	"```\n" +
	"files: none"

// C1 and C2: the issue's own reply keeps the promised line and its closing
// fence, rather than leaving an opening delimiter as the report's last word.
func TestAReportKeepsTheFenceItOpens(t *testing.T) {
	report := composeTaskReport(fencedTaskReply)
	if !strings.Contains(report, "all 41 checks passed") {
		t.Fatalf("the fenced tail is missing from the report:\n%s", report)
	}
	if strings.Count(report, "```")%2 != 0 {
		t.Fatalf("the report has an unmatched fence:\n%s", report)
	}
	if strings.HasSuffix(strings.TrimSpace(report), "```") {
		t.Fatalf("the report ends on a bare fence:\n%s", report)
	}
}

// C3: a reader can tell a complete short report from one whose later lines are
// still in the task's journal.
func TestAReportThatDroppedLinesSaysSo(t *testing.T) {
	cut := composeTaskReport("one\ntwo\nthree\nfour\nfive\nsix")
	if cut != "one\ntwo\nthree\n"+taskReportCut {
		t.Fatalf("cut report = %q", cut)
	}

	whole := composeTaskReport("one\ntwo")
	if whole != "one\ntwo" {
		t.Fatalf("whole report = %q", whole)
	}
	if strings.Contains(whole, taskReportCut) {
		t.Fatalf("a whole report says it was cut: %q", whole)
	}
}

// C4: the fence exception buys no room for ordinary prose, including a prose
// line that needs the existing per-line clip.
func TestAReportStillCutsOrdinaryProseToThreeLines(t *testing.T) {
	long := strings.Repeat("x", 400)
	report := composeTaskReport(strings.Join([]string{long, "two", "three", "four", "five", "six"}, "\n"))
	lines := strings.Split(report, "\n")
	if len(lines) != taskReportLines+1 {
		t.Fatalf("report has %d lines, want three plus the cut mark:\n%s", len(lines), report)
	}
	if len(lines[0]) != taskReportLineLimit || !strings.HasSuffix(lines[0], "…") {
		t.Fatalf("long line has %d bytes: %q", len(lines[0]), lines[0])
	}
	if strings.Join(lines[1:], "\n") != "two\nthree\n"+taskReportCut {
		t.Fatalf("ordinary tail moved:\n%s", report)
	}
}

// C2 and C5: an overlong block is closed by the report, while an opener that
// could carry no content is removed rather than left to promise an empty block.
func TestAReportWillNotHangAnOpeningFence(t *testing.T) {
	block := []string{"before", "```go"}
	for line := 1; line <= taskReportLines+taskReportFenceLines; line++ {
		block = append(block, "content line")
	}
	report := composeTaskReport(strings.Join(block, "\n"))
	if !strings.HasSuffix(report, "```\n"+taskReportCut) {
		t.Fatalf("the overlong block is not closed and marked:\n%s", report)
	}
	if strings.Count(report, "```")%2 != 0 {
		t.Fatalf("the overlong report has an unmatched fence:\n%s", report)
	}
	short := composeTaskReport("before\n```go\ninside")
	if !strings.HasSuffix(short, "```\n"+taskReportCut) {
		t.Fatalf("the unclosed block is not closed and marked:\n%s", short)
	}

	bare := composeTaskReport("one\ntwo\n```")
	if bare != "one\ntwo\n"+taskReportCut {
		t.Fatalf("bare opener report = %q", bare)
	}
	if strings.Count(bare, "```")%2 != 0 || strings.HasSuffix(strings.TrimSpace(bare), "```") {
		t.Fatalf("the bare opener still hangs:\n%s", bare)
	}
}

// C6: closing a cut block and marking the cut are the only words in a report
// that did not come from the node, and every carried source line keeps order.
func TestAReportSaysOnlyWhatTheNodeSaid(t *testing.T) {
	source := []string{"before", "~~~~sh"}
	for line := 1; line <= taskReportLines+taskReportFenceLines; line++ {
		source = append(source, fmt.Sprintf("source line %d", line))
	}
	report := strings.Split(composeTaskReport(strings.Join(source, "\n")), "\n")

	next := 0
	for _, line := range report {
		if line == "~~~~" || line == taskReportCut {
			continue
		}
		for next < len(source) && source[next] != line {
			next++
		}
		if next == len(source) {
			t.Fatalf("report invented %q:\n%s", line, strings.Join(report, "\n"))
		}
		next++
	}
}

// C7: the durable checkpoint row carries the same fenced tail after a real
// node runs, lands, and writes its report through the store's production door.
func TestACheckpointRowKeepsTheReportsFencedTail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &routedCompleter{
		parent: []step{proposeCall("Record the result", "write result.go"), finalText("handed off")},
		child: []step{
			writeCall("call-result", "result.go", "package taskaudit\n"),
			finalText(fencedTaskReply),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = newGoModuleRepo(t)
		config.SessionFile = journal
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
	})
	graph := agent.graph()

	events, err := agent.Submit(context.Background(), "record the result")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	record := recordOf(t, readCheckpoint(t, taskCheckpointPath(journal)), 1)
	if !strings.Contains(record.Report, "all 41 checks passed") {
		t.Fatalf("checkpoint report lost the fenced tail:\n%s", record.Report)
	}
	if strings.Count(record.Report, "```")%2 != 0 {
		t.Fatalf("checkpoint report has an unmatched fence:\n%s", record.Report)
	}
	if strings.HasSuffix(strings.TrimSpace(record.Report), "```") {
		t.Fatalf("checkpoint report ends on a bare fence:\n%s", record.Report)
	}
}
