package approval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDescribeNamesTheCommandAndWhatItTouches(t *testing.T) {
	bash := func(command string) json.RawMessage {
		raw, _ := json.Marshal(map[string]string{"command": command})
		return raw
	}
	cases := []struct {
		tool    string
		args    json.RawMessage
		command string
		touches []string
	}{
		{"bash", bash("gh issue view 12 -R o/r"), "gh issue view 12 -R o/r", []string{"github.com · as you"}},
		{"bash", bash("cd x && aws s3 ls"), "cd x && aws s3 ls", []string{"AWS · as you"}},
		{"bash", bash("git status"), "git status", nil},
		{"bash", bash("npm test"), "npm test", nil},
		{"edit", json.RawMessage(`{"path":"src/a.go"}`), `edit {"path":"src/a.go"}`, []string{"src/a.go (writes)"}},
		{"read", json.RawMessage(`{"path":"src/a.go"}`), `read {"path":"src/a.go"}`, []string{"src/a.go (reads)"}},
		{"fetch", json.RawMessage(`{"url":"https://example.com/x"}`), `fetch {"url":"https://example.com/x"}`, []string{"example.com · network"}},
		{"bash", json.RawMessage(`not json`), "bash", nil},
	}
	for _, c := range cases {
		d := Describe(c.tool, c.args)
		if d.Command != c.command || strings.Join(d.Touches, "|") != strings.Join(c.touches, "|") {
			t.Errorf("Describe(%s %s) = %+v", c.tool, c.args, d)
		}
	}
}

func TestDescribeClipsALongCommandWithoutRewritingIt(t *testing.T) {
	long := "echo " + strings.Repeat("a", commandLimit+50)
	raw, _ := json.Marshal(map[string]string{"command": long})
	d := Describe("bash", raw)
	if !strings.HasSuffix(d.Command, "…") || !strings.HasPrefix(long, strings.TrimSuffix(d.Command, "…")) {
		t.Fatalf("clip = %q", d.Command[:20])
	}
}

// A description never changes the answer: the policy is asked with and
// without one built, and says the same thing.
func TestDescribeIsDisplayOnly(t *testing.T) {
	policy := Policy{Default: ActionPrompt}
	for _, command := range []string{"gh issue view 1", "gh pr merge 1", "aws s3 ls", "git status", "rm -rf /"} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		before := policy.Check("bash", raw)
		_ = Describe("bash", raw)
		if after := policy.Check("bash", raw); after != before {
			t.Errorf("%q: %v became %v", command, before, after)
		}
	}
}
