package pipeline

import "regexp"

var yamlReadLine = regexp.MustCompile(`(?:^|: )yaml: line ([0-9]{1,9}): ([^\r\n]*)`)

// UnreadWhy names the file a person should fix, with the parser's line when
// available. Only known parser sentences are safe to repeat: other errors can
// contain credentials, local paths or repository text. The fixed sentences and
// bounded line number keep the answer on one line, under 160 ASCII characters.
func UnreadWhy(err error) string {
	if err == nil {
		return ""
	}
	why := "could not read repository rules; check the file's syntax and access"
	where := RepoFile
	if m := yamlReadLine.FindStringSubmatch(err.Error()); m != nil {
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
	return where + ": " + why
}
