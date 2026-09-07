package session

import (
	"strings"
	"testing"
)

func TestTaskReportAccountKeepsDiagnosticsAndRemovesOnlyExactAnswerSuffix(t *testing.T) {
	answer := "## Release guide\n\n" + strings.Repeat("Installation details. ", 30) + "\n\nRun the checks.\nRead the full guide."
	account := "its branch could not merge\nfatal: not a git repository"
	for _, test := range []struct{ name, report, result, want string }{
		{"full", withReport(account, answer), answer, account},
		{"preview", withReport(account, firstLines(answer, taskReportLines)), answer, account},
		{"only answer", firstLines(answer, taskReportLines), answer, ""},
		{"rewritten", account + "\nother findings", answer, account + "\nother findings"},
		{"not a suffix", firstLines(answer, taskReportLines) + "\n" + account, answer, firstLines(answer, taskReportLines) + "\n" + account},
		{"no answer", account, "", account},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := TaskReportAccount(test.report, test.result); got != test.want {
				t.Fatalf("account = %q; want %q", got, test.want)
			}
		})
	}
}
