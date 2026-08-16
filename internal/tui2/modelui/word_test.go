package modelui

import "testing"

func TestModelWord(t *testing.T) {
	t.Parallel()
	cases := []struct{ slug, want string }{
		{"", ""},
		{"   ", ""},
		{"anthropic/claude-sonnet-4-20250514", "claude-sonnet-4"},
		{"anthropic/claude-sonnet-4-2025-05-14", "claude-sonnet-4"},
		{"~anthropic/claude-opus-4.1", "claude-opus-4.1"},
		{"openai/gpt-oss-120b:high", "gpt-oss-120b"},
		{"meta-llama/llama-3.3-70b-instruct:free", "llama-3.3-70b-instruct"},
		{"qwen/qwen3.5-9b", "qwen3.5-9b"},
		// No vendor, no variant, no stamp: it comes back whole.
		{"local-model", "local-model"},
		// A trailing number that is not a stamp is part of the name.
		{"openai/o3-2026", "o3-2026"},
		{"x/model-12345678", "model"},
		// A trailing slash names no model and must not index past the end.
		{"anthropic/", "anthropic/"},
	}
	for _, c := range cases {
		if got := ModelWord(c.slug); got != c.want {
			t.Errorf("ModelWord(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

func TestEffortReadsOnlyTheClosedSet(t *testing.T) {
	t.Parallel()
	cases := []struct{ slug, want string }{
		{"openai/gpt-oss-120b:high", "high"},
		{"openai/gpt-oss-120b:HIGH", "high"},
		{"openai/gpt-oss-120b:medium", "medium"},
		{"openai/gpt-oss-120b:low", "low"},
		{"openai/gpt-oss-120b:off", "off"},
		// Everything else is a variant, not an effort. ":free" is the one that
		// would put a lie on the chip if the set were open.
		{"meta/llama:free", ""},
		{"meta/llama:online", ""},
		{"anthropic/claude-sonnet-4", ""},
		{"", ""},
		{"x:", ""},
		{":high", "high"},
	}
	for _, c := range cases {
		if got := Effort(c.slug); got != c.want {
			t.Errorf("Effort(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

// lower must never change a string's byte length: the match highlighter uses
// its offsets to index the ORIGINAL, and a lowering that shrank a rune would
// paint an escape sequence into the middle of one.
func TestLowerPreservesByteLength(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "ABC", "abc", "İstanbul", "МОДЕЛЬ", "Ünïcøde-MODEL", "ΣΣΣ"} {
		if got := lower(s); len(got) != len(s) {
			t.Errorf("lower(%q) is %d bytes, want %d", s, len(got), len(s))
		}
	}
}
