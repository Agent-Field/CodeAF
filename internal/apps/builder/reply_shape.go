package builder

import (
	"errors"
	"log/slog"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// The lines the owner reads when a model reply cannot be used. The parser's
// own text is written for the model and goes to the debug log only.
const (
	logWrongShape      = "The model's reply was in the wrong shape"
	logWrongShapeRetry = logWrongShape + "; repairing"
	logUnreadable      = "The model's reply could not be read"
)

// ruleReplyShape marks a file a WRITE or FIX reply never delivered, so the
// next FIX round writes it.
const ruleReplyShape = "builder/reply-shape"

// replyError is a reply that still did not parse after its one correction.
type replyError struct{ err error }

func (e *replyError) Error() string { return e.err.Error() }
func (e *replyError) Unwrap() error { return e.err }

// ownerLog is the build-log line for err: a short sentence for a reply the
// parser rejected, the error itself for anything else.
func ownerLog(err error) string {
	var shape *prompt.ShapeError
	var reply *replyError
	switch {
	case errors.As(err, &shape):
		return logWrongShape
	case errors.As(err, &reply):
		return logUnreadable
	}
	return err.Error()
}

// debugLog keeps the exact error, such as the parser's complaint, for whoever
// debugs the build; the owner reads ownerLog's line instead.
func debugLog(app, what string, err error) {
	slog.Warn("apps builder: "+what, "app", app, "error", err.Error())
}

// unwritten is what a round still owes after replies in the wrong shape: the
// issues the owner sees on the Check report, and the parser's hint that only
// the FIX prompt reads (the shape expected, the keys sent, the exact paths).
type unwritten struct {
	issues []spec.Issue
	hints  []string
}

func (u *unwritten) add(other unwritten) {
	u.issues = append(u.issues, other.issues...)
	u.hints = append(u.hints, other.hints...)
}

// report appends the hints to the Check report FIX is given.
func (u unwritten) report(checks string) string {
	sort.Strings(u.hints)
	return strings.Join(append([]string{checks}, u.hints...), "\n")
}

// shapeIssues turns a reply that never took the right shape into one Check
// issue per file it should have written, so FIX repairs it like any finding.
func shapeIssues(err error, paths []string) (unwritten, bool) {
	var shape *prompt.ShapeError
	if !errors.As(err, &shape) {
		return unwritten{}, false
	}
	out := unwritten{hints: []string{ruleReplyShape + ": " + shape.Error()}}
	for _, p := range paths {
		out.issues = append(out.issues, spec.Issue{File: p, Rule: ruleReplyShape,
			Message: logWrongShape + "; fix: write this file again"})
	}
	return out, true
}
