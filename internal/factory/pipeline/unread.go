package pipeline

import (
	"errors"
	"regexp"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ErrNotHereYet marks a read that could ask nobody: GitHub could not be reached
// and the repository has no checkout on this computer yet. It says nothing about
// the file. The runner reads again once the clone is done.
var ErrNotHereYet = errors.New("the repository is not on this computer yet")

// ReadRefusal is a forge's answer to a read, with the words that are safe to
// show a person. The wrapped error keeps the detail for problems and logs.
type ReadRefusal struct {
	Why string
	Err error
}

func (r *ReadRefusal) Error() string { return r.Err.Error() }
func (r *ReadRefusal) Unwrap() error { return r.Err }

// UnreadNote is what a job's activity says when it starts with its repository's
// rules unread.
const UnreadNote = "Started with the repository's rules unread; its plan will wait for you"

// MarkUnread records a failed read of the repository's rules on rules.
func MarkUnread(rules *factory.StartRules, err error) {
	rules.NoFile = false
	rules.Unread = true
	rules.Pending = errors.Is(err, ErrNotHereYet)
	rules.UnreadWhy, rules.UnreadFile, rules.UnreadLine = UnreadDiagnostic(err)
}

var yamlReadLine = regexp.MustCompile(`(?:^|: )yaml: line ([0-9]{1,9}): ([^\r\n]*)`)

// UnreadWhy names the file a person should fix, with the parser's line when
// available. Only known parser sentences are safe to repeat: other errors can
// contain credentials, local paths or repository text. The fixed sentences and
// bounded line number keep the answer on one line, under 160 ASCII characters.
func UnreadWhy(err error) string {
	why, _, _ := UnreadDiagnostic(err)
	return why
}

// UnreadDiagnostic returns a safe explanation and repository-relative location.
// The YAML decoder returns syntax errors as text, without a typed line field.
func UnreadDiagnostic(err error) (string, string, int) {
	if err == nil {
		return "", "", 0
	}
	why := "could not read repository rules; check the file's syntax and access"
	where := RepoFile
	line := 0
	var refused *ReadRefusal
	switch {
	case errors.Is(err, ErrNotHereYet):
		return where + ": read once the repository is on this computer", RepoFile, 0
	case errors.As(err, &refused):
		return where + ": " + refused.Why, RepoFile, 0
	}
	if m := yamlReadLine.FindStringSubmatch(err.Error()); m != nil {
		line, _ = strconv.Atoi(m[1])
		where += " line " + m[1]
		switch m[2] {
		case "mapping values are not allowed in this context",
			"did not find expected key",
			"could not find expected ':'",
			"did not find expected node content",
			"did not find expected ',' or ']'",
			"did not find expected ',' or '}'",
			"found unexpected end of stream",
			"found character that cannot start any token",
			"found unknown escape character",
			"found unexpected document indicator":
			why = m[2]
		}
	}
	return where + ": " + why, RepoFile, line
}
