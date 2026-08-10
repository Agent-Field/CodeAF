package permission

import (
	"errors"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
)

func TestWildcardUTF16AndTrailingOptional(t *testing.T) {
	cases := []struct {
		value   string
		pattern string
		want    bool
	}{
		{"git", "git *", true},
		{"git status", "git *", true},
		{"gitstatus", "git *", false},
		{"a/b", `a\b`, true},
		{"💩", "?", false},
		{"💩", "??", true},
		{"a\nb", "a*b", true},
	}
	for _, test := range cases {
		if got := WildcardMatch(test.value, test.pattern); got != test.want {
			t.Errorf("WildcardMatch(%q, %q) = %v, want %v", test.value, test.pattern, got, test.want)
		}
	}
}

func TestAutonomousAskTreatsAskAsAllowAndDenyAsError(t *testing.T) {
	config, err := ParseConfigJSON([]byte(`{"read":{"*":"ask","secret":"deny"}}`))
	if err != nil {
		t.Fatal(err)
	}
	rules := FromConfig(config)
	service := &Service{}
	if err := service.Ask("read", []string{"public"}, rules); err != nil {
		t.Fatalf("ask should proceed: %v", err)
	}
	err = service.Ask("read", []string{"secret"}, rules)
	var denied DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v", err, err)
	}
	want := "The user has specified a rule which prevents you from using this specific tool call. " +
		"Here are some of the relevant rules " +
		`[{"permission":"read","pattern":"*","action":"ask"},{"permission":"read","pattern":"secret","action":"deny"}]`
	if err.Error() != want {
		t.Fatalf("error = %q", err)
	}
	if len(service.Pending()) != 0 {
		t.Fatalf("autonomous service has pending requests")
	}
}

func TestEveryBakedAgentPermissionFrontmatterParses(t *testing.T) {
	for _, name := range baked.ListBakedAgents() {
		t.Run(name, func(t *testing.T) {
			markdown, ok := baked.GetBakedAgentMarkdown(name)
			if !ok {
				t.Fatal("missing markdown")
			}
			rules, err := RulesetFromFrontmatter(markdown)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(rules) == 0 {
				t.Fatal("permission rules unexpectedly empty")
			}
		})
	}
}
