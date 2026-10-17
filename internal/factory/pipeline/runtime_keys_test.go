package pipeline

import "testing"

func TestRuntimeKeysInIsTheKeyNotTheWord(t *testing.T) {
	for name, c := range map[string]struct {
		text string
		want bool
	}{
		"legacy":                {"steps:\n  plan:\n    prompt: plan it\n    max: 2\n", false},
		"empty":                 {"", false},
		"kind check":            {"steps:\n  test: {kind: check, prompt: go test}\n", true},
		"kind chat":             {"steps:\n  plan:\n    kind: chat\n    prompt: p\n", true},
		"when":                  {"steps:\n  plan:\n    when: thin\n    prompt: p\n", true},
		"effort":                {"steps:\n  plan:\n    effort: cheap\n    prompt: p\n", true},
		"fanout":                {"steps:\n  plan:\n    fanout: per-file\n    prompt: p\n", true},
		"invalid word":          {"steps:\n  plan:\n    when: sometimes\n", true},
		"empty value":           {"steps:\n  plan:\n    effort:\n    prompt: p\n", true},
		"flow mapping":          {"steps: {plan: {when: thin}}\n", true},
		"padded key":            {"steps:\n  plan:\n    \" kind \": check\n", true},
		"alias step":            {"x: &s {kind: check}\nsteps:\n  plan: *s\n", true},
		"comment only":          {"steps:\n  plan:\n    prompt: p # kind: check, when: thin\n", false},
		"head comment":          {"# fanout: per-file\nsteps:\n  plan:\n    prompt: p\n", false},
		"quoted prompt":         {"steps:\n  plan:\n    prompt: \"kind: check\\nwhen: thin\"\n", false},
		"block scalar prompt":   {"steps:\n  plan:\n    prompt: |\n      effort: cheap\n      fanout: per-file\n", false},
		"scalar step":           {"steps:\n  plan: kind check when thin\n", false},
		"key outside steps":     {"kind: check\nwhen: thin\nsteps:\n  plan: {prompt: p}\n", false},
		"key in other mapping":  {"pipeline:\n  issue: [plan]\nfixed:\n  - kind\nsteps:\n  plan: {prompt: p}\n", false},
		"malformed":             {"steps: [\n  kind: check", false},
		"two documents":         {"steps:\n  a: {kind: check}\n---\nsteps: {}\n", false},
		"steps is a list":       {"steps:\n  - kind: check\n", false},
		"unrelated case change": {"steps:\n  plan:\n    Kind: check\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := RuntimeKeysIn(c.text); got != c.want {
				t.Fatalf("RuntimeKeysIn = %v, want %v", got, c.want)
			}
		})
	}
}
