package builder

import (
	"context"
	"fmt"
	"slices"
	"strings"

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
		return verb(s, ctx, b, t, req.Args)
	}
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

// runCheck is one numbered check round on the rendered files, with the events
// the Building screen shows.
func (s *Service) runCheck(ctx context.Context, b *build, files spec.Files) (bridge.CheckReport, error) {
	return s.checkRound(ctx, b, files, b.nextRound())
}

func (s *Service) verbCheck(ctx context.Context, b *build, t *seat, _ []string) draft.Reply {
	files, _, err := s.drafted(b, t)
	if err != nil {
		return refused("error: the draft could not be read — try again")
	}
	report, err := s.runCheck(ctx, b, files)
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
	return refused(fmt.Sprintf("error: the check found %d things to fix:\n%s", len(report.Issues), formatReport(report)))
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
	report, err := s.runCheck(ctx, b, files)
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
	files, st, err := s.drafted(b, t)
	if err != nil {
		return refused("error: the draft could not be read — try again")
	}
	if !phaseAtLeast(st.Phase, draft.PhasePlanned) {
		return refused("error: the plan is not ready — run codeaf app plan ready first")
	}
	report, err := s.runCheck(ctx, b, files)
	if err != nil {
		return refused("error: the check could not run — try again")
	}
	if !report.Pass {
		return reportReply(report)
	}
	if _, err := t.drafts.Append(draft.AuthorCodeaf, "phase", []string{"done"}, ""); err != nil {
		return refused(err.Error())
	}
	return accepted("ok the app is done — now run plandb done")
}
