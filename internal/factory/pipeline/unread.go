package pipeline

import (
	"regexp"
	"strconv"
)

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
