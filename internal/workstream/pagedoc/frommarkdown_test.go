package pagedoc

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
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
