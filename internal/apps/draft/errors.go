package draft

import (
	"errors"
	"fmt"
	"strings"
)

// Exit codes of Main.
const (
	ExitOK         = 0
	ExitRefused    = 1
	ExitUsage      = 2
	ExitNoBuilder  = 3
	maxIssueLines  = 5
	fieldHintLimit = 8
)

// Error is a refusal or a usage mistake. Its text is the exact line the CLI
// prints, "error: " included, so a caller can show it unchanged.
type Error struct {
	Code  int
	Lines []string
}

func (e *Error) Error() string {
	return "error: " + strings.Join(e.Lines, "\nerror: ")
}

// refuse is a verb the draft will not take: nothing was changed.
func refuse(format string, args ...any) error {
	return &Error{Code: ExitRefused, Lines: []string{fmt.Sprintf(format, args...)}}
}

// refuseAll refuses with one line per finding, capped so a broken document
// does not flood the worker's context.
func refuseAll(lines []string) error {
	if len(lines) > maxIssueLines {
		more := len(lines) - maxIssueLines
		lines = append(lines[:maxIssueLines:maxIssueLines], fmt.Sprintf("%d more problems; fix these first", more))
	}
	return &Error{Code: ExitRefused, Lines: lines}
}

// usage is a malformed command line: the verb was not understood.
func usage(format string, args ...any) error {
	return &Error{Code: ExitUsage, Lines: []string{fmt.Sprintf(format, args...)}}
}

// exitCode maps any error to the exit code Main returns.
func exitCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ExitRefused
}
