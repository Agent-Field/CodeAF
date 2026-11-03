package pagedoc

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

func TestWorkstreamFromMarkdownContractRoundTrip(t *testing.T) {
	md, err := os.ReadFile("testdata/example.md")
	if err != nil {
		t.Fatal(err)
	}
	workstreamMarkdownRoundTrip(t, string(md), string(md))
	for _, md := range []string{
		"# One\n\n## Two\n\n### Three\n",
		"- Parent\n  - Child\n- Next\n",
		"3. Parent\n   4. Child\n4. Next\n",
		"Keep \\*literal\\* and `a_b` and [a `code`](https://example.com).\n",
		"````go\na := \"```\"\n````\n",
		"[live: jobs]\n\n[live: waves milestone=m2]\n\n[live: job api#0]\n\n[live: decision r2]\n",
	} {
		workstreamMarkdownRoundTrip(t, md, md)
	}
}

func TestWorkstreamFromMarkdownDegradesUnsupportedMarkup(t *testing.T) {
	workstreamMarkdownRoundTrip(t, "See [guide](https://example.com/a(b)) and [more](https://example.com/a\\(b\\)).\n", "See [guide](https://example.com/a%28b%29) and [more](https://example.com/a%28b%29).\n")
	workstreamMarkdownRoundTrip(t, "#### Deep\n\n> **Plain** __words__\ncontinued\n\n---\n\n+ First\n* Second\n\n7) Ordered\n\n~~~js\ncall()\n~~~\n", "### Deep\n\nPlain words continued\n\n- First\n- Second\n\n7. Ordered\n\n```js\ncall()\n```\n")
	for _, md := range []string{"", "\n", "```\n", "[unfinished", "`unfinished", "text\\", string([]byte{255})} {
		if !json.Valid(FromMarkdown(md)) {
			t.Fatalf("invalid doc for %q", md)
		}
	}
}

func TestWorkstreamFromMarkdownLiveAttributes(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(FromMarkdown("[live: jobs milestone=m1]"), &doc); err != nil {
		t.Fatal(err)
	}
	node := doc["content"].([]any)[0].(map[string]any)
	want := map[string]any{"kind": "jobs", "milestone_id": "m1", "factory": nil, "job": nil, "record_id": nil}
	if node["type"] != "liveBlock" || !reflect.DeepEqual(node["attrs"], want) {
		t.Fatalf("live block: %+v", node)
	}
}

func workstreamMarkdownRoundTrip(t *testing.T, input, want string) {
	t.Helper()
	got, err := Markdown(FromMarkdown(input))
	if err != nil || got != want {
		t.Fatalf("round trip %q:\ngot  %q\nwant %q\nerror %v", input, got, want, err)
	}
}

func TestWorkstreamFromMarkdownListItemsStartWithParagraphs(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"- \n", "-\n"},
		{"- # Heading\n", "- # Heading\n"},
		{"- [live: jobs]\n", "- [live: jobs]\n"},
		{"- ```go\n  call()\n  ```\n", "- ```go\n  call()\n  ```\n"},
		{"- - Nested\n", "- - Nested\n"},
		{"- Parent\n  - # Nested heading\n  - \n", "- Parent\n  - # Nested heading\n  -\n"},
	}
	for _, tc := range cases {
		var doc map[string]any
		if err := json.Unmarshal(FromMarkdown(tc.input), &doc); err != nil {
			t.Fatal(err)
		}
		if count := workstreamCheckListParagraphs(t, doc); count == 0 {
			t.Fatalf("no list items in %q", tc.input)
		}
		workstreamMarkdownRoundTrip(t, tc.input, tc.want)
	}
}

func workstreamCheckListParagraphs(t *testing.T, node map[string]any) int {
	t.Helper()
	count := 0
	kids := children(node)
	if node["type"] == "listItem" {
		count++
		if len(kids) == 0 || kids[0]["type"] != "paragraph" {
			t.Fatalf("list item must begin with a paragraph: %+v", node)
		}
	}
	for _, child := range kids {
		count += workstreamCheckListParagraphs(t, child)
	}
	return count
}

func TestWorkstreamFromMarkdownUnmatchedDelimitersAreBounded(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"backticks", "Notes " + strings.Repeat("`", 200000), "Notes " + strings.Repeat("\\`", 200000) + "\n"},
		{"brackets", strings.Repeat("[", 200000), strings.Repeat("\\[", 200000) + "\n"},
		{"unfinished link", strings.Repeat("[", 200000) + "](unfinished", strings.Repeat("\\[", 200000) + "\\](unfinished\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			finished := make(chan json.RawMessage, 1)
			guard.Go("workstream/markdown-delimiters-test", func() { finished <- FromMarkdown(tc.input) })
			select {
			case doc := <-finished:
				if !json.Valid(doc) {
					t.Fatal("conversion returned invalid editor JSON")
				}
				got, err := Markdown(doc)
				if err != nil || got != tc.want {
					t.Fatalf("conversion lost delimiter text: got %d bytes, want %d; %v", len(got), len(tc.want), err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("malformed inline input exceeded the conversion bound")
			}
		})
	}
}

func TestWorkstreamFromMarkdownCodeRequiresMatchingRunLengths(t *testing.T) {
	workstreamMarkdownRoundTrip(t, "Notes ``a ` b`` and `c`.\n", "Notes ``a ` b`` and `c`.\n")
	workstreamMarkdownRoundTrip(t, "Notes ``unclosed ` tail.\n", "Notes \\`\\`unclosed \\` tail.\n")
	workstreamMarkdownRoundTrip(t, "Notes \\``code`.\n", "Notes \\``code`.\n")
}
