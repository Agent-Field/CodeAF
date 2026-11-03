package pagedoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type testCase struct {
	name, input, want string
}

func doc(blocks ...string) string {
	return `{"type":"doc","content":[` + strings.Join(blocks, ",") + `]}`
}

func runCases(t *testing.T, cases []testCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Markdown(json.RawMessage(tc.input))
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			if got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Ported from the desktop app's markdown tests: "paragraphs, heading levels 1–3 and clamping, empty blocks and exactly one terminal newline".
// Covers validation contract (a).
func TestMarkdownParagraphsHeadingsEmptyBlocksAndTerminalNewline(t *testing.T) {
	runCases(t, []testCase{
		{"paragraphs and empty paragraph", doc(`{"type":"paragraph","content":[{"type":"text","text":"First"}]}`, `{"type":"paragraph","content":[]}`, `{"type":"paragraph","content":[{"type":"text","text":"Second  "}]}`), "First\n\nSecond\n"},
		{"heading level 1", doc(`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title"}]}`), "# Title\n"},
		{"heading level 2", doc(`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Title"}]}`), "## Title\n"},
		{"heading level 3", doc(`{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Title"}]}`), "### Title\n"},
		{"heading level 0 clamped to 1", doc(`{"type":"heading","attrs":{"level":0},"content":[{"type":"text","text":"Title"}]}`), "# Title\n"},
		{"heading level -2 clamped to 1", doc(`{"type":"heading","attrs":{"level":-2},"content":[{"type":"text","text":"Title"}]}`), "# Title\n"},
		{"heading level 8 clamped to 3", doc(`{"type":"heading","attrs":{"level":8},"content":[{"type":"text","text":"Title"}]}`), "### Title\n"},
		{"heading default level", doc(`{"type":"heading","content":[{"type":"text","text":"Default"}]}`), "# Default\n"},
		{"empty heading and paragraph", doc(`{"type":"heading","content":[]}`, `{"type":"paragraph","content":[]}`), ""},
		{"null document", `null`, ""},
		{"doc without content", `{"type":"doc"}`, ""},
		{"doc with empty content", doc(), ""},
	})
}

// Ported from the desktop app's markdown tests: "paragraph and heading newlines become spaces, including inline code, and outer spaces and tabs are trimmed".
// Covers validation contract (a).
func TestMarkdownInlineNewlinesAndOuterWhitespace(t *testing.T) {
	runCases(t, []testCase{
		{"paragraph crlf in text", doc(`{"type":"paragraph","content":[{"type":"text","text":"Left\r\nright"}]}`), "Left right\n"},
		{"paragraph crlf in code", doc(`{"type":"paragraph","content":[{"type":"text","text":"Left\r\nright","marks":[{"type":"code"}]}]}`), "`Left right`\n"},
		{"paragraph crlf alone", doc(`{"type":"paragraph","content":[{"type":"text","text":"\r\n"}]}`), ""},
		{"paragraph crlf and whitespace", doc(`{"type":"paragraph","content":[{"type":"text","text":" \t\r\n    "}]}`), ""},
		{"paragraph cr in text", doc(`{"type":"paragraph","content":[{"type":"text","text":"Left\rright"}]}`), "Left right\n"},
		{"paragraph cr in code", doc(`{"type":"paragraph","content":[{"type":"text","text":"Left\rright","marks":[{"type":"code"}]}]}`), "`Left right`\n"},
		{"paragraph cr alone", doc(`{"type":"paragraph","content":[{"type":"text","text":"\r"}]}`), ""},
		{"paragraph cr and whitespace", doc(`{"type":"paragraph","content":[{"type":"text","text":" \t\r    "}]}`), ""},
		{"paragraph lf in text", doc(`{"type":"paragraph","content":[{"type":"text","text":"Left\nright"}]}`), "Left right\n"},
		{"paragraph lf in code", doc(`{"type":"paragraph","content":[{"type":"text","text":"Left\nright","marks":[{"type":"code"}]}]}`), "`Left right`\n"},
		{"paragraph lf alone", doc(`{"type":"paragraph","content":[{"type":"text","text":"\n"}]}`), ""},
		{"paragraph lf and whitespace", doc(`{"type":"paragraph","content":[{"type":"text","text":" \t\n    "}]}`), ""},
		{"paragraph outer spaces and tabs trimmed", doc(`{"type":"paragraph","content":[{"type":"text","text":"    Indented \t"}]}`), "Indented\n"},
		{"paragraph spaces and tabs only", doc(`{"type":"paragraph","content":[{"type":"text","text":"\t  "}]}`), ""},
		{"heading crlf in text", doc(`{"type":"heading","content":[{"type":"text","text":"Left\r\nright"}]}`), "# Left right\n"},
		{"heading crlf in code", doc(`{"type":"heading","content":[{"type":"text","text":"Left\r\nright","marks":[{"type":"code"}]}]}`), "# `Left right`\n"},
		{"heading crlf alone", doc(`{"type":"heading","content":[{"type":"text","text":"\r\n"}]}`), ""},
		{"heading crlf and whitespace", doc(`{"type":"heading","content":[{"type":"text","text":" \t\r\n    "}]}`), ""},
		{"heading cr in text", doc(`{"type":"heading","content":[{"type":"text","text":"Left\rright"}]}`), "# Left right\n"},
		{"heading cr in code", doc(`{"type":"heading","content":[{"type":"text","text":"Left\rright","marks":[{"type":"code"}]}]}`), "# `Left right`\n"},
		{"heading cr alone", doc(`{"type":"heading","content":[{"type":"text","text":"\r"}]}`), ""},
		{"heading cr and whitespace", doc(`{"type":"heading","content":[{"type":"text","text":" \t\r    "}]}`), ""},
		{"heading lf in text", doc(`{"type":"heading","content":[{"type":"text","text":"Left\nright"}]}`), "# Left right\n"},
		{"heading lf in code", doc(`{"type":"heading","content":[{"type":"text","text":"Left\nright","marks":[{"type":"code"}]}]}`), "# `Left right`\n"},
		{"heading lf alone", doc(`{"type":"heading","content":[{"type":"text","text":"\n"}]}`), ""},
		{"heading lf and whitespace", doc(`{"type":"heading","content":[{"type":"text","text":" \t\n    "}]}`), ""},
		{"heading outer spaces and tabs trimmed", doc(`{"type":"heading","content":[{"type":"text","text":"    Indented \t"}]}`), "# Indented\n"},
		{"heading spaces and tabs only", doc(`{"type":"heading","content":[{"type":"text","text":"\t  "}]}`), ""},
		{"whitespace-only blocks omitted between paragraphs", doc(`{"type":"paragraph","content":[{"type":"text","text":"A"}]}`, `{"type":"paragraph","content":[{"type":"text","text":"\n"}]}`, `{"type":"heading","content":[{"type":"text","text":"\n"}]}`, `{"type":"paragraph","content":[{"type":"text","text":"B"}]}`), "A\n\nB\n"},
		{"empty paragraph inside list item omitted", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"\n"}]},{"type":"paragraph","content":[{"type":"text","text":"    Kept    "}]}]}]}`), "- Kept\n"},
		{"code block newlines and spaces verbatim", doc(`{"type":"codeBlock","content":[{"type":"text","text":"  Left\r\nright\rnext\nlast  "}]}`), "```\n  Left\r\nright\rnext\nlast  \n```\n"},
	})
}

// Ported from the desktop app's markdown tests: "bullet and ordered lists, custom/default starts, nested and multi-block items, bare empty items".
// Covers validation contract (a).
func TestMarkdownBulletAndOrderedLists(t *testing.T) {
	runCases(t, []testCase{
		{"bullet list", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"One"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Two"}]}]}]}`), "- One\n- Two\n"},
		{"ordered list with custom start and multi-paragraph items", doc(`{"type":"orderedList","attrs":{"start":9,"ignored":true},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Nine"}]},{"type":"paragraph","content":[{"type":"text","text":"Again"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Ten"}]},{"type":"paragraph","content":[{"type":"text","text":"Last"}]}]}]}`), "9. Nine\n\n   Again\n10. Ten\n\n    Last\n"},
		{"ordered list with default start and empty item", doc(`{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Default"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[]}]}]}`), "1. Default\n2.\n"},
		{"nested lists with empty paragraphs and bare empty item", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[]},{"type":"paragraph","content":[{"type":"text","text":"Parent"}]},{"type":"orderedList","attrs":{"start":3},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Child"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Leaf"}]}]}]}]}]},{"type":"paragraph","content":[]},{"type":"paragraph","content":[{"type":"text","text":"After"}]}]},{"type":"listItem","content":[]}]}`), "- Parent\n  3. Child\n     - Leaf\n\n  After\n-\n"},
	})
}

// Ported from the desktop app's markdown tests: "code blocks preserve text, language, empty content and safe backtick fences".
// Covers validation contract (a).
func TestMarkdownCodeBlocks(t *testing.T) {
	runCases(t, []testCase{
		{"code block with language", doc(`{"type":"codeBlock","attrs":{"language":"ts"},"content":[{"type":"text","text":"const x = 1"}]}`), "```ts\nconst x = 1\n```\n"},
		{"code block with triple backticks in text and null language", doc(`{"type":"codeBlock","attrs":{"language":null},"content":[{"type":"text","text":"before ` + "```" + ` after\n"}]}`), "````\nbefore ``` after\n\n````\n"},
		{"empty code block", doc(`{"type":"codeBlock"}`), "```\n```\n"},
		{"code block preserves outer spaces", doc(`{"type":"codeBlock","content":[{"type":"text","text":"  x  "}]}`), "```\n  x  \n```\n"},
	})
}

// Ported from the desktop app's markdown tests: "inline code delimiters and padding handle embedded and edge backticks without escaping".
// Covers validation contract (a).
func TestMarkdownInlineCode(t *testing.T) {
	runCases(t, []testCase{
		{"inline code without escaping", doc(`{"type":"paragraph","content":[{"type":"text","text":"x * y","marks":[{"type":"code"}]}]}`), "`x * y`\n"},
		{"inline code with embedded backtick", doc(`{"type":"paragraph","content":[{"type":"text","text":"a` + "`" + `b","marks":[{"type":"code"}]}]}`), "``a`b``\n"},
		{"inline code with edge backticks", doc(`{"type":"paragraph","content":[{"type":"text","text":"` + "`x`" + `","marks":[{"type":"code"}]}]}`), "`` `x` ``\n"},
		{"inline code with leading double backticks", doc(`{"type":"paragraph","content":[{"type":"text","text":"` + "``x" + `","marks":[{"type":"code"}]}]}`), "``` ``x ```\n"},
	})
}

// Ported from the desktop app's markdown tests: "adjacent same-href text forms one link, code stays inside, destinations encode delimiters, absent href is plain text".
// Covers validation contract (a).
func TestMarkdownLinksAndDestinations(t *testing.T) {
	runCases(t, []testCase{
		{"adjacent same-href nodes with inline code and encoded destination", doc(`{"type":"paragraph","content":[{"type":"text","text":"Read ","marks":[{"type":"link","attrs":{"href":"https://example.com/a b(c)<d>"}}]},{"type":"text","text":"x","marks":[{"type":"code"},{"type":"link","attrs":{"href":"https://example.com/a b(c)<d>"}}]},{"type":"text","text":" now","marks":[{"type":"link","attrs":{"href":"https://example.com/a b(c)<d>"}}]}]}`), "[Read `x` now](https://example.com/a%20b%28c%29%3Cd%3E)\n"},
		{"adjacent different-href links stay separate", doc(`{"type":"paragraph","content":[{"type":"text","text":"one","marks":[{"type":"link","attrs":{"href":"https://a.test"}}]},{"type":"text","text":"two","marks":[{"type":"link","attrs":{"href":"https://b.test"}}]}]}`), "[one](https://a.test)[two](https://b.test)\n"},
		{"empty and missing href render plain escaped text", doc(`{"type":"paragraph","content":[{"type":"text","text":"empty*","marks":[{"type":"link","attrs":{"href":""}}]},{"type":"text","text":"missing","marks":[{"type":"link","attrs":{}}]}]}`), "empty\\*missing\n"},
	})
}

// Ported from the desktop app's markdown tests: "empty text and empty link runs render nothing while non-empty same-href runs remain linked".
// Covers validation contract (a).
func TestMarkdownEmptyTextAndLinkRuns(t *testing.T) {
	runCases(t, []testCase{
		{"paragraph empty text no marks", doc(`{"type":"paragraph","content":[{"type":"text","text":"","marks":[]}]}`), ""},
		{"paragraph empty text code mark", doc(`{"type":"paragraph","content":[{"type":"text","text":"","marks":[{"type":"code"}]}]}`), ""},
		{"paragraph empty text link mark", doc(`{"type":"paragraph","content":[{"type":"text","text":"","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]}`), ""},
		{"paragraph empty text code and link marks", doc(`{"type":"paragraph","content":[{"type":"text","text":"","marks":[{"type":"code"},{"type":"link","attrs":{"href":"https://example.com"}}]}]}`), ""},
		{"paragraph multiple empty link nodes", doc(`{"type":"paragraph","content":[{"type":"text","text":"","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},{"type":"text","text":"","marks":[{"type":"code"},{"type":"link","attrs":{"href":"https://example.com"}}]}]}`), ""},
		{"heading empty text no marks", doc(`{"type":"heading","content":[{"type":"text","text":"","marks":[]}]}`), ""},
		{"heading empty text code mark", doc(`{"type":"heading","content":[{"type":"text","text":"","marks":[{"type":"code"}]}]}`), ""},
		{"heading empty text link mark", doc(`{"type":"heading","content":[{"type":"text","text":"","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]}`), ""},
		{"heading empty text code and link marks", doc(`{"type":"heading","content":[{"type":"text","text":"","marks":[{"type":"code"},{"type":"link","attrs":{"href":"https://example.com"}}]}]}`), ""},
		{"heading multiple empty link nodes", doc(`{"type":"heading","content":[{"type":"text","text":"","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},{"type":"text","text":"","marks":[{"type":"code"},{"type":"link","attrs":{"href":"https://example.com"}}]}]}`), ""},
		{"empty link between plain text nodes", doc(`{"type":"paragraph","content":[{"type":"text","text":"Before"},{"type":"text","text":"","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},{"type":"text","text":" after"}]}`), "Before after\n"},
		{"non-empty link surrounded by empty same-href nodes", doc(`{"type":"paragraph","content":[{"type":"text","text":"","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},{"type":"text","text":"Guide","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},{"type":"text","text":"","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]}`), "[Guide](https://example.com)\n"},
	})
}

// Ported from the desktop app's markdown tests: "every live block kind with and without attributes, job zero and an empty factory, unknown kinds".
// Covers validation contract (a).
func TestMarkdownLiveBlocks(t *testing.T) {
	runCases(t, []testCase{
		{"all four kinds without attributes", doc(`{"type":"liveBlock","attrs":{"kind":"jobs","milestone_id":null,"factory":null,"job":null,"record_id":null}}`, `{"type":"liveBlock","attrs":{"kind":"waves","milestone_id":null,"factory":null,"job":null,"record_id":null}}`, `{"type":"liveBlock","attrs":{"kind":"job","milestone_id":null,"factory":null,"job":null,"record_id":null}}`, `{"type":"liveBlock","attrs":{"kind":"decision","milestone_id":null,"factory":null,"job":null,"record_id":null}}`), "[live: jobs]\n\n[live: waves]\n\n[live: job]\n\n[live: decision]\n"},
		{"all four kinds with attributes", doc(`{"type":"liveBlock","attrs":{"kind":"jobs","milestone_id":"m1","factory":null,"job":null,"record_id":null}}`, `{"type":"liveBlock","attrs":{"kind":"waves","milestone_id":"m2","factory":null,"job":null,"record_id":null}}`, `{"type":"liveBlock","attrs":{"kind":"job","milestone_id":null,"factory":"api","job":1107,"record_id":null}}`, `{"type":"liveBlock","attrs":{"kind":"decision","milestone_id":null,"factory":null,"job":null,"record_id":"r1"}}`), "[live: jobs milestone=m1]\n\n[live: waves milestone=m2]\n\n[live: job api#1107]\n\n[live: decision r1]\n"},
		{"job zero without factory", doc(`{"type":"liveBlock","attrs":{"kind":"job","milestone_id":null,"factory":null,"job":0,"record_id":null}}`), "[live: job #0]\n"},
		{"factory without job number", doc(`{"type":"liveBlock","attrs":{"kind":"job","milestone_id":null,"factory":"api","job":null,"record_id":null}}`), "[live: job]\n"},
		{"unknown kind and missing attrs", doc(`{"type":"liveBlock","attrs":{"kind":"canvas"}}`, `{"type":"liveBlock"}`), "[live: canvas]\n\n[live: unknown]\n"},
	})
}

// Ported from the desktop app's markdown tests: "live references require non-empty strings and job numbers require non-negative integers".
// Covers validation contract (a).
func TestMarkdownLiveBlockAttributeValidation(t *testing.T) {
	// JSON cannot carry NaN, so the NaN row from the TypeScript loop is omitted;
	// 1e400 and -1e400 cover positive and negative infinity after JSON parsing.
	runCases(t, []testCase{
		{"jobs milestone empty string", doc(`{"type":"liveBlock","attrs":{"kind":"jobs","milestone_id":""}}`), "[live: jobs]\n"},
		{"waves milestone empty string", doc(`{"type":"liveBlock","attrs":{"kind":"waves","milestone_id":""}}`), "[live: waves]\n"},
		{"decision record empty string", doc(`{"type":"liveBlock","attrs":{"kind":"decision","record_id":""}}`), "[live: decision]\n"},
		{"job factory empty string", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"","job":7}}`), "[live: job #7]\n"},
		{"jobs milestone number", doc(`{"type":"liveBlock","attrs":{"kind":"jobs","milestone_id":42}}`), "[live: jobs]\n"},
		{"waves milestone number", doc(`{"type":"liveBlock","attrs":{"kind":"waves","milestone_id":42}}`), "[live: waves]\n"},
		{"decision record number", doc(`{"type":"liveBlock","attrs":{"kind":"decision","record_id":42}}`), "[live: decision]\n"},
		{"job factory number", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":42,"job":7}}`), "[live: job #7]\n"},
		{"jobs milestone false", doc(`{"type":"liveBlock","attrs":{"kind":"jobs","milestone_id":false}}`), "[live: jobs]\n"},
		{"waves milestone false", doc(`{"type":"liveBlock","attrs":{"kind":"waves","milestone_id":false}}`), "[live: waves]\n"},
		{"decision record false", doc(`{"type":"liveBlock","attrs":{"kind":"decision","record_id":false}}`), "[live: decision]\n"},
		{"job factory false", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":false,"job":7}}`), "[live: job #7]\n"},
		{"jobs milestone object", doc(`{"type":"liveBlock","attrs":{"kind":"jobs","milestone_id":{}}}`), "[live: jobs]\n"},
		{"waves milestone object", doc(`{"type":"liveBlock","attrs":{"kind":"waves","milestone_id":{}}}`), "[live: waves]\n"},
		{"decision record object", doc(`{"type":"liveBlock","attrs":{"kind":"decision","record_id":{}}}`), "[live: decision]\n"},
		{"job factory object", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":{},"job":7}}`), "[live: job #7]\n"},
		{"jobs milestone null", doc(`{"type":"liveBlock","attrs":{"kind":"jobs","milestone_id":null}}`), "[live: jobs]\n"},
		{"waves milestone null", doc(`{"type":"liveBlock","attrs":{"kind":"waves","milestone_id":null}}`), "[live: waves]\n"},
		{"decision record null", doc(`{"type":"liveBlock","attrs":{"kind":"decision","record_id":null}}`), "[live: decision]\n"},
		{"job factory null", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":null,"job":7}}`), "[live: job #7]\n"},
		{"job string", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":"7"}}`), "[live: job]\n"},
		{"job fractional", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":1.5}}`), "[live: job]\n"},
		{"job negative", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":-1}}`), "[live: job]\n"},
		{"job positive infinity", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":1e400}}`), "[live: job]\n"},
		{"job negative infinity", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":-1e400}}`), "[live: job]\n"},
		{"job boolean", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":false}}`), "[live: job]\n"},
		{"job null", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":null}}`), "[live: job]\n"},
		{"job without factory", doc(`{"type":"liveBlock","attrs":{"kind":"job","job":7}}`), "[live: job #7]\n"},
		{"job zero with factory", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":0}}`), "[live: job api#0]\n"},
	})
}

// Ported from the desktop app's markdown tests: "every escaped character and paragraph line-start case, also inside lists".
// Covers validation contract (a).
func TestMarkdownEscapingAndParagraphLineStarts(t *testing.T) {
	runCases(t, []testCase{
		{"escape backslash", doc(`{"type":"paragraph","content":[{"type":"text","text":"\\"}]}`), "\\\\\n"},
		{"escape backtick", doc(`{"type":"paragraph","content":[{"type":"text","text":"` + "`" + `"}]}`), "\\`\n"},
		{"escape asterisk", doc(`{"type":"paragraph","content":[{"type":"text","text":"*"}]}`), "\\*\n"},
		{"escape underscore", doc(`{"type":"paragraph","content":[{"type":"text","text":"_"}]}`), "\\_\n"},
		{"escape open bracket", doc(`{"type":"paragraph","content":[{"type":"text","text":"["}]}`), "\\[\n"},
		{"escape close bracket", doc(`{"type":"paragraph","content":[{"type":"text","text":"]"}]}`), "\\]\n"},
		{"escape less than", doc(`{"type":"paragraph","content":[{"type":"text","text":"<"}]}`), "\\<\n"},
		{"escape greater than", doc(`{"type":"paragraph","content":[{"type":"text","text":">"}]}`), "\\>\n"},
		{"line start hash", doc(`{"type":"paragraph","content":[{"type":"text","text":"# title"}]}`), "\\# title\n"},
		{"line start hash after trim", doc(`{"type":"paragraph","content":[{"type":"text","text":"    # title\t "}]}`), "\\# title\n"},
		{"line start hash in list", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"# title"}]}]}]}`), "- \\# title\n"},
		{"line start minus", doc(`{"type":"paragraph","content":[{"type":"text","text":"- bullet"}]}`), "\\- bullet\n"},
		{"line start minus after trim", doc(`{"type":"paragraph","content":[{"type":"text","text":"    - bullet\t "}]}`), "\\- bullet\n"},
		{"line start minus in list", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"- bullet"}]}]}]}`), "- \\- bullet\n"},
		{"line start plus", doc(`{"type":"paragraph","content":[{"type":"text","text":"+ bullet"}]}`), "\\+ bullet\n"},
		{"line start plus after trim", doc(`{"type":"paragraph","content":[{"type":"text","text":"    + bullet\t "}]}`), "\\+ bullet\n"},
		{"line start plus in list", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"+ bullet"}]}]}]}`), "- \\+ bullet\n"},
		{"line start quote", doc(`{"type":"paragraph","content":[{"type":"text","text":"> quote"}]}`), "\\> quote\n"},
		{"line start quote after trim", doc(`{"type":"paragraph","content":[{"type":"text","text":"    > quote\t "}]}`), "\\> quote\n"},
		{"line start quote in list", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"> quote"}]}]}]}`), "- \\> quote\n"},
		{"line start dot ordered", doc(`{"type":"paragraph","content":[{"type":"text","text":"12. list"}]}`), "12\\. list\n"},
		{"line start dot ordered after trim", doc(`{"type":"paragraph","content":[{"type":"text","text":"    12. list\t "}]}`), "12\\. list\n"},
		{"line start dot ordered in list", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"12. list"}]}]}]}`), "- 12\\. list\n"},
		{"line start paren ordered", doc(`{"type":"paragraph","content":[{"type":"text","text":"42) list"}]}`), "42\\) list\n"},
		{"line start paren ordered after trim", doc(`{"type":"paragraph","content":[{"type":"text","text":"    42) list\t "}]}`), "42\\) list\n"},
		{"line start paren ordered in list", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"42) list"}]}]}]}`), "- 42\\) list\n"},
		{"mid-paragraph markers after normalized newlines", doc(`{"type":"paragraph","content":[{"type":"text","text":"one  \n# two\n3. three  "}]}`), "one   # two 3. three\n"},
	})
}

// Ported from the desktop app's markdown tests: "unknown blocks flatten descendant text, unknown inline nodes render their own text, unknown marks and malformed JSON never throw".
// Covers validation contract (a) and (d).
func TestMarkdownUnknownNodesMarksAndMalformedShapes(t *testing.T) {
	runCases(t, []testCase{
		{"unknown block flattens descendant text", doc(`{"type":"future","content":[{"type":"paragraph","content":[{"type":"text","text":"a*"}]},{"type":"other","content":[{"type":"text","text":"b_"}]}]}`), "a\\*b\\_\n"},
		{"unknown inline node renders own text and unknown mark is ignored", doc(`{"type":"paragraph","content":[{"type":"futureInline","text":"<x>","content":[{"type":"text","text":"ignored"}]},{"type":"text","text":" ok","marks":[{"type":"futureMark"}]}]}`), "\\<x\\> ok\n"},
		{"empty unknown block omitted", doc(`{"type":"future"}`), ""},
		{"top-level null", `null`, ""},
		{"top-level false", `false`, ""},
		{"top-level number", `7`, ""},
		{"top-level string", `"wrong"`, ""},
		{"top-level array", `[]`, ""},
		{"top-level empty object", `{}`, ""},
		{"malformed content elements and non-string text", `{"type":"doc","content":[null,3,{},{"type":"paragraph","content":[null,{"type":"text","text":7,"marks":[null]}]}]}`, ""},
		{"codeBlock with null attrs and non-array content", `{"type":"codeBlock","attrs":null,"content":"bad"}`, "```\n```\n"},
	})
}

// Ported from the desktop app's markdown tests: "whole-document fixture is the wire contract's exact JSON and markdown example".
// Covers validation contract (a) and (b).
func TestMarkdownRendersTheContractExample(t *testing.T) {
	rawJSON, err := os.ReadFile(filepath.Join("testdata", "example.json"))
	if err != nil {
		t.Fatalf("read example.json: %v", err)
	}
	rawMD, err := os.ReadFile(filepath.Join("testdata", "example.md"))
	if err != nil {
		t.Fatalf("read example.md: %v", err)
	}
	example := map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": float64(1)}, "content": []any{map[string]any{"type": "text", "text": "Spec"}}},
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "Keep "},
			map[string]any{"type": "text", "text": "event_id", "marks": []any{map[string]any{"type": "code"}}},
			map[string]any{"type": "text", "text": " and "},
			map[string]any{"type": "text", "text": "guide", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "https://example.com/spec"}}}},
			map[string]any{"type": "text", "text": "."},
		}},
		map[string]any{"type": "bulletList", "content": []any{
			map[string]any{"type": "listItem", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Retry safely"}}}}},
			map[string]any{"type": "listItem", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Record outcomes"}}}}},
		}},
		map[string]any{"type": "orderedList", "attrs": map[string]any{"start": float64(3)}, "content": []any{
			map[string]any{"type": "listItem", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Ship"}}}}},
		}},
		map[string]any{"type": "codeBlock", "attrs": map[string]any{"language": "ts"}, "content": []any{map[string]any{"type": "text", "text": "send(event)"}}},
		map[string]any{"type": "liveBlock", "attrs": map[string]any{"kind": "jobs", "milestone_id": "m1", "factory": nil, "job": nil, "record_id": nil}},
		map[string]any{"type": "liveBlock", "attrs": map[string]any{"kind": "waves", "milestone_id": nil, "factory": nil, "job": nil, "record_id": nil}},
		map[string]any{"type": "liveBlock", "attrs": map[string]any{"kind": "job", "milestone_id": nil, "factory": "api", "job": float64(1107), "record_id": nil}},
		map[string]any{"type": "liveBlock", "attrs": map[string]any{"kind": "decision", "milestone_id": nil, "factory": nil, "job": nil, "record_id": "r1"}},
	}}
	var decoded any
	if err := json.Unmarshal(rawJSON, &decoded); err != nil {
		t.Fatalf("unmarshal example.json: %v", err)
	}
	if !reflect.DeepEqual(decoded, example) {
		t.Errorf("example.json differs from the reference example")
	}
	const expected = "# Spec\n\nKeep `event_id` and [guide](https://example.com/spec).\n\n- Retry safely\n- Record outcomes\n\n3. Ship\n\n```ts\nsend(event)\n```\n\n[live: jobs milestone=m1]\n\n[live: waves]\n\n[live: job api#1107]\n\n[live: decision r1]\n"
	if string(rawMD) != expected {
		t.Errorf("example.md: got %q, want %q", string(rawMD), expected)
	}
	got, err := Markdown(json.RawMessage(rawJSON))
	if err != nil {
		t.Fatalf("Markdown(example.json): unexpected error: %v", err)
	}
	if got != string(rawMD) {
		t.Errorf("Markdown(example.json): got %q, want %q", got, string(rawMD))
	}
}

// Covers validation contract (c).
func TestMarkdownRefusesInvalidJSON(t *testing.T) {
	cases := []struct {
		name, input string
	}{
		{"unclosed brace", `{`},
		{"trailing comma", `{"type":"doc",}`},
		{"bare word", `nope`},
		{"trailing token", `{"type":"doc"} x`},
		{"unclosed array", `[1,2`},
		{"unclosed content array", `{"type":"doc","content":[`},
		{"nesting past encoding/json limit", strings.Repeat("[", 10001) + strings.Repeat("]", 10001)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Markdown(json.RawMessage(tc.input))
			if err == nil {
				t.Fatalf("%s: expected error, got nil (output %q)", tc.name, got)
			}
			if !strings.HasPrefix(err.Error(), "pagedoc: ") {
				t.Errorf("%s: error %q does not start with %q", tc.name, err.Error(), "pagedoc: ")
			}
			if got != "" {
				t.Errorf("%s: got %q, want %q", tc.name, got, "")
			}
		})
	}
}

// Covers validation contract (c).
func TestMarkdownTreatsAnAbsentDocumentAsEmpty(t *testing.T) {
	cases := []struct {
		name  string
		input json.RawMessage
	}{
		{"nil", nil},
		{"empty", json.RawMessage("")},
		{"whitespace only", json.RawMessage("  \n")},
		{"json null", json.RawMessage("null")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Markdown(tc.input)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			if got != "" {
				t.Errorf("%s: got %q, want %q", tc.name, got, "")
			}
		})
	}
}

// Covers validation contract (c).
func TestMarkdownRendersADeepDocumentWithoutPanicking(t *testing.T) {
	deep := strings.Repeat(`{"type":"future","content":[`, 4000) + `{"type":"text","text":"x"}` + strings.Repeat(`]}`, 4000)
	got, err := Markdown(json.RawMessage(deep))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "x\n" {
		t.Errorf("deep document: got %q, want %q", got, "x\n")
	}
}

// Covers validation contract (d) and (e).
func TestMarkdownMatchesTheReferenceOnEdgeCases(t *testing.T) {
	runCases(t, []testCase{
		{"1 line separator u2028 triggers line-start escape", doc(`{"type":"paragraph","content":[{"type":"text","text":"a\u2028# b"}]}`), "a\u2028\\# b\n"},
		{"2 newline inside link destination triggers line-start escape", doc(`{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"a\n- b"}}]}]}`), "[x](a\n\\-%20b)\n"},
		{"3 bulletList with direct paragraph child", doc(`{"type":"bulletList","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]}`), "- a\n"},
		{"4 bulletList with null child", doc(`{"type":"bulletList","content":[null]}`), "-\n"},
		{"5 orderedList with null children", doc(`{"type":"orderedList","content":[null,null]}`), "1.\n2.\n"},
		{"6 unknown inline node with code mark and embedded backtick", doc(`{"type":"paragraph","content":[{"type":"mention","text":"a` + "`" + `b","marks":[{"type":"code"}]}]}`), "``a`b``\n"},
		{"7 unknown inline node with link mark breaks link run", doc(`{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"link","attrs":{"href":"https://e.x"}}]},{"type":"x","text":"b","marks":[{"type":"link","attrs":{"href":"https://e.x"}}]},{"type":"text","text":"c","marks":[{"type":"link","attrs":{"href":"https://e.x"}}]}]}`), "[a](https://e.x)b[c](https://e.x)\n"},
		{"8 first link mark with empty href hides later link mark", doc(`{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"link","attrs":{"href":""}},{"type":"link","attrs":{"href":"https://x"}}]}]}`), "a\n"},
		{"9 text node at block level", doc(`{"type":"text","text":"*a*"}`), "\\*a\\*\n"},
		{"10 nested doc inside doc", doc(doc(`{"type":"paragraph","content":[{"type":"text","text":"a"}]}`, `{"type":"paragraph","content":[{"type":"text","text":"b"}]}`)), "a\n\nb\n"},
		{"11 bare paragraph root without doc", `{"type":"paragraph","content":[{"type":"text","text":"x"}]}`, "x\n"},
		{"12 lf followed by cr becomes two spaces", doc(`{"type":"paragraph","content":[{"type":"text","text":"a\n\rb"}]}`), "a  b\n"},
		{"13 non-breaking spaces preserved at paragraph edges", doc(`{"type":"paragraph","content":[{"type":"text","text":"\u00a0x\u00a0"}]}`), "\u00a0x\u00a0\n"},
		{"14 heading text starting with hash is not line-start escaped", doc(`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"# x"}]}`), "# # x\n"},
		{"15 codeBlock collects nested descendant text", doc(`{"type":"codeBlock","content":[{"type":"text","text":"a"},{"type":"x","text":"b","content":[{"type":"text","text":"c"}]}]}`), "```\nabc\n```\n"},
		{"16 codeBlock ignores non-string language", doc(`{"type":"codeBlock","attrs":{"language":5},"content":[{"type":"text","text":"x"}]}`), "```\nx\n```\n"},
		{"17a liveBlock empty kind", doc(`{"type":"liveBlock","attrs":{"kind":""}}`), "[live: unknown]\n"},
		{"17b liveBlock null attrs", doc(`{"type":"liveBlock","attrs":null}`), "[live: unknown]\n"},
		{"17c liveBlock array attrs", doc(`{"type":"liveBlock","attrs":[]}`), "[live: unknown]\n"},
		{"18 bulletList codeBlock with crlf indents cr line", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"codeBlock","content":[{"type":"text","text":"a\r\nb"}]}]}]}`), "- ```\n  a\r\n  b\n  ```\n"},
		{"19 bulletList codeBlock with blank line keeps blank line empty", doc(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"codeBlock","content":[{"type":"text","text":"a\n\nb"}]}]}]}`), "- ```\n  a\n\n  b\n  ```\n"},
		{"20 orderedList start 1e21 uses exponential format", doc(`{"type":"orderedList","attrs":{"start":1e21},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]}`), "1e+21. x\n"},
		{"21 orderedList start 1e20 uses fixed format", doc(`{"type":"orderedList","attrs":{"start":1e20},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]}`), "100000000000000000000. x\n"},
		{"22 orderedList start -0 renders as 0", doc(`{"type":"orderedList","attrs":{"start":-0},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}`), "0. a\n1. b\n"},
		{"23 orderedList start -2 counts up through negative integers", doc(`{"type":"orderedList","attrs":{"start":-2},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}`), "-2. a\n-1. b\n"},
		{"24a orderedList fractional start falls back to 1", doc(`{"type":"orderedList","attrs":{"start":2.5},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]}`), "1. x\n"},
		{"24b orderedList string start falls back to 1", doc(`{"type":"orderedList","attrs":{"start":"3"},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]}`), "1. x\n"},
		{"24c orderedList infinite start falls back to 1", doc(`{"type":"orderedList","attrs":{"start":1e400},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]}`), "1. x\n"},
		{"25 orderedList start 3.0 is accepted as integer", doc(`{"type":"orderedList","attrs":{"start":3.0},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]}`), "3. x\n"},
		{"26 liveBlock job -0 renders as 0", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":-0}}`), "[live: job api#0]\n"},
		{"27 liveBlock job 1e21 uses exponential format", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":1e21}}`), "[live: job api#1e+21]\n"},
		{"28 liveBlock job 3.0 renders as 3", doc(`{"type":"liveBlock","attrs":{"kind":"job","factory":"api","job":3.0}}`), "[live: job api#3]\n"},
		{"29 heading level 2.9 truncates to 2", doc(`{"type":"heading","attrs":{"level":2.9},"content":[{"type":"text","text":"T"}]}`), "## T\n"},
		{"30a heading string level falls back to 1", doc(`{"type":"heading","attrs":{"level":"2"},"content":[{"type":"text","text":"T"}]}`), "# T\n"},
		{"30b heading infinite level falls back to 1", doc(`{"type":"heading","attrs":{"level":1e400},"content":[{"type":"text","text":"T"}]}`), "# T\n"},
		{"30c heading level -0.5 clamps to 1", doc(`{"type":"heading","attrs":{"level":-0.5},"content":[{"type":"text","text":"T"}]}`), "# T\n"},
	})
}
