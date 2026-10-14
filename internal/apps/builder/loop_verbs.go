package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// verbFn answers one server verb for the worker. The reply is text for the
// worker, so it may name files and paths exactly; it never reaches the owner.
type verbFn func(s *Service, ctx context.Context, b *build, t *seat, args []string) draft.Reply

// serverVerbs are the `codeaf app` verbs that need services the worker's shell
// does not have (the checker and its sample data). The CLI reaches them over
// the build's socket; there is no switch, only this table.
var serverVerbs = map[string]verbFn{
	"check": (*Service).verbCheck,
	"try":   (*Service).verbTry,
	"done":  (*Service).verbDone,
}

// verbHandler is the socket's handler while a worker is live. One verb runs at
// a time, because a check numbers a round and writes the same events.
func (s *Service) verbHandler(b *build, t *seat) draft.Handler {
	return func(ctx context.Context, req draft.Request) draft.Reply {
		verb, ok := serverVerbs[req.Verb]
		if !ok {
			return refused("error: " + req.Verb + " is not a Builder verb — use check, try or done")
		}
		t.verbMu.Lock()
		defer t.verbMu.Unlock()
		if !t.planIsReady() {
			return refused(planFirst)
		}
		return verb(s, ctx, b, t, req.Args)
	}
}

// planFirst is the answer to a check, try or done while the plan is still
// being made: they judge a built app, and there is none yet.
const planFirst = "error: check runs after plan ready — finish the plan first"

func (t *seat) planIsReady() bool {
	st, err := t.drafts.State()
	return err == nil && phaseAtLeast(st.Phase, draft.PhasePlanned)
}

func refused(text string) draft.Reply  { return draft.Reply{Text: text} }
func accepted(text string) draft.Reply { return draft.Reply{OK: true, Text: text} }

// drafted renders what the journal holds now. The journal is read here, not
// taken from the feed, so a verb checks exactly what the worker has committed.
func (s *Service) drafted(b *build, t *seat) (spec.Files, draft.State, error) {
	st, err := t.drafts.State()
	if err != nil {
		return nil, st, err
	}
	return s.render(b, st), st, nil
}

// runCheck is the build's one check on the rendered files, made quietly. The report is kept
// by the sha256 of the files, so check, try and done reuse it for as long as
// the render does not change; a check is minutes of the worker's wall and the
// same files give the same report. Calls arrive one at a time (verbHandler
// holds verbMu), so a second call waits for the running check and then reads
// its report, and never starts another.
func (s *Service) runCheck(ctx context.Context, b *build, t *seat, files spec.Files) (bridge.CheckReport, error) {
	sum := digest(files)
	if report, ok := t.checked.get(sum); ok {
		return report, nil
	}
	// Rendered files are checked as they are: the preview gate compares this
	// report's digest with the files previewed, so no repair runs here.
	if err := s.shortenPlanLines(ctx, b, files); err != nil {
		return bridge.CheckReport{}, err
	}
	report, err := s.checkQuietly(ctx, b, files, b.nextRound(), nil)
	if err == nil {
		t.checked.put(sum, report)
	}
	return report, err
}

// checkMemo holds the report of the last check and the files it was made on.
type checkMemo struct {
	mu     sync.Mutex
	sum    string
	report bridge.CheckReport
	have   bool
}

func (m *checkMemo) get(sum string) (bridge.CheckReport, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.report, m.have && m.sum == sum
}

func (m *checkMemo) put(sum string, report bridge.CheckReport) {
	m.mu.Lock()
	m.sum, m.report, m.have = sum, report, true
	m.mu.Unlock()
}

// digest is the sha256 of the files by path, in path order.
func digest(files spec.Files) string {
	h := sha256.New()
	for _, path := range files.Paths() {
		fmt.Fprintf(h, "%s\x00%d\x00", path, len(files[path]))
		h.Write(files[path])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Service) verbCheck(ctx context.Context, b *build, t *seat, _ []string) draft.Reply {
	files, _, err := s.drafted(b, t)
	if err != nil {
		return refused("error: the draft could not be read — try again")
	}
	report, err := s.runCheck(ctx, b, t, files)
	if err != nil {
		return refused("error: the check could not run — try again")
	}
	return reportReply(report)
}

// reportReply is the report as the worker reads it: one issue a line, each with
// the file and the path inside it.
func reportReply(report bridge.CheckReport) draft.Reply {
	if report.Pass {
		return accepted(fmt.Sprintf("ok every screen opens (%d screens, %d runs tried)", len(report.Rendered), len(report.Runs)))
	}
	return refused("error: the check found " + things(len(report.Issues)) + " to fix:\n" + formatReport(report))
}

// verbTry answers for one run: the issues the check found in its file, or ok
// when the check ran it on sample data without any.
func (s *Service) verbTry(ctx context.Context, b *build, t *seat, args []string) draft.Reply {
	if len(args) != 1 {
		return refused("error: name one run — codeaf app try <run>")
	}
	file := "runs/" + args[0] + ".yaml"
	files, _, err := s.drafted(b, t)
	if err != nil {
		return refused("error: the draft could not be read — try again")
	}
	if files[file] == nil {
		return refused("error: there is no run " + args[0] + " in the draft — add it with codeaf app run first")
	}
	report, err := s.runCheck(ctx, b, t, files)
	if err != nil {
		return refused("error: the run could not be tried — try again")
	}
	return tryReply(report, args[0], file)
}

func tryReply(report bridge.CheckReport, id, file string) draft.Reply {
	var found []string
	for _, issue := range report.Issues {
		if issue.File == file {
			found = append(found, issue.String())
		}
	}
	switch {
	case len(found) > 0:
		return refused("error: run " + id + " failed on sample data:\n" + strings.Join(found, "\n"))
	case slices.Contains(report.Runs, id):
		return accepted("ok run " + id + " ran on sample data")
	}
	return accepted("ok run " + id + " has no findings (the checker did not run it here)")
}

// verbDone is the finish: the full check, and only when it passes the draft
// moves to done. The worker's CLI never writes that phase itself.
func (s *Service) verbDone(ctx context.Context, b *build, t *seat, _ []string) draft.Reply {
	files, _, err := s.drafted(b, t)
	if err != nil {
		return refused("error: the draft could not be read — try again")
	}
	report, err := s.runCheck(ctx, b, t, files)
	if err != nil {
		return refused("error: the check could not run — try again")
	}
	if !report.Pass {
		return reportReply(report)
	}
	// The accepted check is the one the owner reads: the interim ones were quiet.
	s.announceCheck(b, report)
	if _, err := t.drafts.Append(draft.AuthorCodeaf, "phase", []string{"done"}, ""); err != nil {
		return refused(err.Error())
	}
	return accepted("ok the app is done — now run plandb done")
}
