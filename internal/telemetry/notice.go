package telemetry

import (
	"fmt"
	"os"
	"sync"
)

// Notice is the exact text the contract fixes for the one line codeaf prints
// to stderr before the first session's events are ever sent, and that the
// installer prints. It is a constant, byte for byte, and a test holds the
// bytes.
const Notice = `codeaf sends anonymous usage counts to AgentField.
  Sent:  version, OS, mode (chat or task), how many sessions, how many errors.
  Never: anything about you or your work. No prompts, code, file names,
         paths, repo names, keys, email, IP, or machine name.
  See exactly what leaves:  codeaf telemetry show
  Turn off:                 CODEAF_TELEMETRY=off`

// noticeOnce keeps the notice to one line per process even when several
// sessions open under one run. It is separate from the failure channel's once:
// a failure that kept quiet must not swallow the notice, and a notice shown
// must not spend the failure's single line.
var noticeOnce sync.Once

// PrintNotice writes the notice to stderr once per process, the shape the
// surface job calls before the first session's events are sent. It never
// touches the on-disk marker — that is MarkNoticeShown's job, and the two are
// separate so a notice printed by the installer does not stand in for the
// surface having shown it.
func PrintNotice() {
	noticeOnce.Do(func() {
		fmt.Fprintln(os.Stderr, Notice)
	})
}
