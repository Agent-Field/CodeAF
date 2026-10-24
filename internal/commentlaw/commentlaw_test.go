// Package commentlaw keeps source comments focused on product behavior.
package commentlaw

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var rules = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:the\s+owner|owner's)\s+(?:has\s+(?:already\s+)?)?(?:asked|ruled|decided|ruling|feedback|report|review|wants|picks?)\b|ruled by the owner`),
	regexp.MustCompile(`(?i)\bthis lane\s+(?:added|fixes|proves|ends|may not edit|measured|replaces)|(?-i:\blane [A-Z][0-9]*\b)|\b(?:ai|coding|development) lane\b`),
	regexp.MustCompile(`(?i)\b(?:spark|blackmac|dgx|dumb|studio|santosh|abir|zeropoint)\b`),
	regexp.MustCompile(`(?i)(?:^|[\s"'(\x60])/(?:home|Users)/[^/\s]+|(?-i:STAGE-[0-9][A-Z0-9-]*(?:\.md)?)|\b(?:LEDGER|DECISION)\.md`),
	regexp.MustCompile(`(?i)\b(?:opencode|claude code|codex|pi)\b`),
	regexp.MustCompile(`(?i)\b(?:faster|slower|better|worse|versus|compared|unlike)\b[^.\n]{0,80}\b(?:opencode|claude code|codex|pi)\b|\b(?:opencode|claude code|codex|pi)\b[^.\n]{0,80}\b(?:faster|slower|better|worse|versus)\b`),
}

// These exceptions describe formats, services and source identifiers. Each
// exemption applies to one rule and one comment; other rules still apply.
var exceptions = []struct {
	path, text *regexp.Regexp
	rule       int
	term       string
}{
	{regexp.MustCompile(`\.go$`), regexp.MustCompile(`(?i)codex/gpt|CLI|provider|transport|codexauth|sign.in|model|catalog|account|credential|service|backend|profile|272k|window|context|skills?|plugin|agentskills\.io`), 4, "codex"}, // Codex is an authenticated model service.
	{regexp.MustCompile(`^(internal/(skills|home|store|resident|session)/|cmd/codeaf/)`), regexp.MustCompile(`(?i)skills?|plugins?|agentskills\.io|registry|settings\.json`), 4, "skills"},                                              // Skill discovery reads other tools' file formats.
	{regexp.MustCompile(`^(internal/exec/bare/|internal/session/|internal/tui3/toolstat\.go|cmd/harness-design/)`), regexp.MustCompile(`(?i)\bpi\b`), 4, "pi"},                                                                          // The tool library preserves compatible wire schemas and results.
	{regexp.MustCompile(`\.go$`), regexp.MustCompile(`(?i)TERM.?=.?dumb|dumb terminal|TERM unset or.*dumb|found a dumb|LM Studio`), 2, "terminal"},                                                                                      // Terminal capabilities and provider names are protocols.
	{regexp.MustCompile(`^internal/tui3/(spark|wallcontract|wallview|render|spendplace|homegrid)\.go$`), regexp.MustCompile(`(?i)\bspark\b`), 2, "spark"},                                                                               // Spark names a chart sample or field in these renderers.
}

func TestSourceComments(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range violations(root) {
		t.Error(problem)
	}
}

func violations(root string) []string {
	var found []string
	for _, scope := range []string{"cmd", "internal", "scripts", "relay/src", "bench/canary", "bench/run.sh"} {
		err := filepath.WalkDir(filepath.Join(root, scope), func(path string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if excluded(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			if excluded(rel) || !source(rel) {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			comments, err := sourceComments(rel, string(body))
			if err != nil {
				return err
			}
			for _, c := range comments {
				for i, rule := range rules {
					matches := rule.FindAllStringIndex(c.text, -1)
					if len(matches) == 0 {
						continue
					}
					for _, match := range matches {
						term := strings.ToLower(c.text[match[0]:match[1]])
						allowed := false
						for _, ex := range exceptions {
							if ex.rule == i && ex.path.MatchString(rel) && ex.text.MatchString(c.text) && (i != 2 || ex.term == "spark" && term == "spark" || ex.term == "terminal" && (term == "dumb" || term == "studio")) && (i != 4 || ex.term == term || ex.term == "skills" && (term == "claude code" || term == "opencode" || term == "codex")) {
								allowed = true
								break
							}
						}
						if !allowed {
							found = append(found, fmt.Sprintf("%s:%d: comment should describe product behavior: %s", rel, c.line+strings.Count(c.text[:match[0]], "\n"), term))
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			found = append(found, fmt.Sprintf("%s: %v", scope, err))
		}
	}
	sort.Strings(found)
	return found
}

func excluded(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == "testdata" || part == "fixtures" || part == "node_modules" {
			return true
		}
	}
	base := filepath.Base(path)
	return path == "internal/manual" || strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_test.sh") || strings.HasSuffix(base, "_test.py") || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, ".test.js")
}
func source(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".js", ".ts", ".py", ".sh":
		return true
	}
	return false
}

type comment struct {
	line int
	text string
}

func sourceComments(path, body string) ([]comment, error) {
	if filepath.Ext(path) == ".go" {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, body, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		var out []comment
		for _, group := range file.Comments {
			out = append(out, comment{set.Position(group.Pos()).Line, group.Text()})
		}
		return out, nil
	}
	return scriptComments(path, body), nil
}

// Quoted program text is skipped so a URL, shell expansion or fixture string
// cannot be mistaken for commentary. Block comments retain their starting line.
func scriptComments(path, body string) []comment {
	var out []comment
	hash := strings.HasSuffix(path, ".py") || strings.HasSuffix(path, ".sh")
	line := 1
	var heredocs []string
	for i := 0; i < len(body); {
		if body[i] == '\n' {
			line++
			i++
			for _, delimiter := range heredocs {
				for i < len(body) {
					end := strings.IndexByte(body[i:], '\n')
					if end < 0 {
						end = len(body) - i
					}
					text := strings.TrimSuffix(strings.TrimLeft(body[i:i+end], "\t"), "\r")
					i += end
					if i < len(body) {
						i++
						line++
					}
					if text == delimiter {
						break
					}
				}
			}
			heredocs = nil
			continue
		}
		if strings.HasSuffix(path, ".sh") && strings.HasPrefix(body[i:], "<<") && !strings.HasPrefix(body[i:], "<<<") {
			j := i + 2
			if j < len(body) && body[j] == '-' {
				j++
			}
			for j < len(body) && (body[j] == ' ' || body[j] == '\t') {
				j++
			}
			if j < len(body) && (body[j] == '"' || body[j] == '\'') {
				j++
			}
			start := j
			for j < len(body) && (body[j] == '_' || body[j] >= 'a' && body[j] <= 'z' || body[j] >= 'A' && body[j] <= 'Z' || body[j] >= '0' && body[j] <= '9') {
				j++
			}
			if j > start {
				heredocs = append(heredocs, body[start:j])
			}
		}
		if hash && body[i] == '#' && (i == 0 || strings.ContainsRune(" \t\n;", rune(body[i-1]))) || !hash && strings.HasPrefix(body[i:], "//") {
			start, l := i, line
			for i < len(body) && body[i] != '\n' {
				i++
			}
			out = append(out, comment{l, body[start:i]})
			continue
		}
		if !hash && strings.HasPrefix(body[i:], "/*") {
			start, l := i, line
			i += 2
			for i < len(body) && !strings.HasPrefix(body[i:], "*/") {
				if body[i] == '\n' {
					line++
				}
				i++
			}
			if i < len(body) {
				i += 2
			}
			out = append(out, comment{l, body[start:i]})
			continue
		}
		if strings.HasSuffix(path, ".py") && (strings.HasPrefix(body[i:], "\"\"\"") || strings.HasPrefix(body[i:], "'''")) {
			start, l, quote := i, line, body[i:i+3]
			doc := strings.TrimSpace(body[strings.LastIndex(body[:i], "\n")+1:i]) == ""
			i += 3
			for i < len(body) && !strings.HasPrefix(body[i:], quote) {
				if body[i] == '\n' {
					line++
				}
				if body[i] == '\\' && i+1 < len(body) {
					i += 2
				} else {
					i++
				}
			}
			if i < len(body) {
				i += 3
			}
			if doc {
				out = append(out, comment{l, body[start:i]})
			}
			continue
		}
		if strings.ContainsRune("\"'`", rune(body[i])) {
			quote := body[i]
			i++
			for i < len(body) {
				if body[i] == '\n' {
					line++
				}
				if body[i] == '\\' {
					i += 2
					continue
				}
				if body[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}
		i++
	}
	return out
}

func TestCommentPatternsAndBoundaries(t *testing.T) {
	cases := []struct {
		name, path, body string
		want             int
	}{
		{"Go attribution", "internal/example/source.go", "package example\n// The owner asked for this behavior.\nvar x=1\n", 1},
		{"block and inline comments", "cmd/example/source.go", "package main\n/* Abir requested this. */\nvar x=1 // lane A measured this.\n", 2},
		{"JavaScript", "relay/src/source.js", "const url='https://example.com'; // measured on Spark\n/* /Users/name/work */", 2},
		{"Python docstring", "scripts/source.py", "\"\"\"Measured on Spark.\"\"\"\n", 1},
		{"Python", "scripts/source.py", "text = '# Spark'\n# STAGE-1-CONTRACTS.md\n", 1},
		{"shell heredoc", "scripts/example.sh", "cat <<'EOF'\n# Spark\nEOF\n# Describe the product.\n", 0},
		{"Python string", "scripts/source.py", "data=\"\"\"# Spark\"\"\"\n", 0},
		{"template literal", "relay/src/source.js", "const text=`// Spark`; // Describe the product.\n", 0},
		{"shell", "bench/run.sh", "echo '# Spark' # blackmac\n", 1},
		{"quoted words", "internal/example/source.go", "package example\nvar x=\"the owner asked on Spark\"\n", 0},
		{"multiline attribution", "internal/example/source.go", "package example\n// The owner\n// ruled that this works.\n", 1},
		{"documentation URL", "internal/example/source.go", "package example\n// https://example.com/docs/home/getting-started\n", 0},
		{"public issue", "internal/example/source.go", "package example\n// Avoid duplicate delivery after reconnect (#123).\n", 0},
		{"routing ownership", "internal/example/source.go", "package example\n// The goal owner decided to continue on the lane a request uses.\n", 0},
		{"comparison", "internal/config/source.go", "package config\n// The Codex model service is faster than other tools.\n", 1},
		{"authentication", "internal/config/source.go", "package config\n// The Codex model service requires an account bearer.\n", 0},
		{"no cross-name exception", "internal/config/source.go", "package config\n// The Codex model service outperforms opencode.\n", 1},
		{"fixture", "internal/example/testdata/source.go", "// Spark\n", 0},
		{"Go test", "internal/example/source_test.go", "// Spark\n", 0},
		{"manual", "internal/manual/example.go", "// Spark\n", 0},
		{"hosted tree", "relay/hosted/src/source.js", "// Spark\n", 0},
		{"excluded scripts", "scripts/test_example.py", "# Spark\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tc.path)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0644); err != nil {
				t.Fatal(err)
			}
			got := violations(root)
			if len(got) != tc.want {
				t.Fatalf("got %v, want %d violations", got, tc.want)
			}
			if tc.want > 0 && !strings.Contains(got[0], tc.path+":") {
				t.Fatalf("missing source location: %v", got)
			}
		})
	}
}
