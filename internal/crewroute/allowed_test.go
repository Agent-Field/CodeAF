package crewroute

import "testing"

func TestAllowedRules(t *testing.T) {
	flash := Model{ID: "z-ai/glm-5.3-flash", Open: true, PromptPrice: 0.15e-6, CompletionPrice: 0.5e-6}
	kimi := Model{ID: "moonshotai/kimi-k3", Open: true, PromptPrice: 3e-6, CompletionPrice: 15e-6}
	opus := Model{ID: "anthropic/claude-opus-5", PromptPrice: 5e-6, CompletionPrice: 25e-6}
	v4 := Model{ID: "deepseek/deepseek-v4-flash-0731", Open: true, PromptPrice: 0.04e-6, CompletionPrice: 0.64e-6}
	cases := []struct {
		rule  string
		admit map[string]bool
	}{
		{"", map[string]bool{flash.ID: true, kimi.ID: true, opus.ID: true, v4.ID: true}},
		{"all", map[string]bool{flash.ID: true, kimi.ID: true, opus.ID: true, v4.ID: true}},
		{"open", map[string]bool{flash.ID: true, kimi.ID: true, opus.ID: false, v4.ID: true}},
		{"open -deepseek", map[string]bool{flash.ID: true, kimi.ID: true, opus.ID: false, v4.ID: false}},
		{"≤1/5", map[string]bool{flash.ID: true, kimi.ID: false, opus.ID: false, v4.ID: true}},
		{"<=1/5 +moonshotai/kimi-k3", map[string]bool{flash.ID: true, kimi.ID: true, opus.ID: false, v4.ID: true}},
		{"glm-5.3-flash, kimi-k3", map[string]bool{flash.ID: true, kimi.ID: true, opus.ID: false, v4.ID: false}},
		{"deepseek/deepseek-v4-flash", map[string]bool{v4.ID: true, flash.ID: false}},
		{"anthropic", map[string]bool{opus.ID: true, flash.ID: false}},
		{"-anthropic", map[string]bool{opus.ID: false, flash.ID: true}},
	}
	for _, tc := range cases {
		rule, err := ParseAllowed(tc.rule)
		if err != nil {
			t.Fatalf("%q: %v", tc.rule, err)
		}
		for _, m := range []Model{flash, kimi, opus, v4} {
			want, asked := tc.admit[m.ID]
			if !asked {
				continue
			}
			if got := rule.AdmitsModel(m); got != want {
				t.Errorf("%q admits %s = %v, want %v", tc.rule, m.ID, got, want)
			}
		}
	}
}

func TestAllowedRefusesWhatItCannotRead(t *testing.T) {
	for _, bad := range []string{"≤1", "<=a/b", "open kimi-k3", "≤-1/5"} {
		if _, err := ParseAllowed(bad); err == nil {
			t.Errorf("%q parsed; want a refusal", bad)
		}
	}
}

func TestAllowedRoundTripsAndModifies(t *testing.T) {
	for _, rule := range []string{"all", "open", "≤1/5", "open -deepseek +deepseek/deepseek-v4-flash", "glm-5.3-flash, kimi-k3"} {
		parsed, err := ParseAllowed(rule)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.String() != rule {
			t.Errorf("%q round-trips as %q", rule, parsed.String())
		}
	}
	open, _ := ParseAllowed("open -deepseek")
	if got := open.With(true, "deepseek").String(); got != "open +deepseek" {
		t.Errorf("With flips a sign: %q", got)
	}
	if got := open.With(false, "openrouter"); got.AdmitsRoute("openrouter") || !got.AdmitsRoute("z-ai") {
		t.Errorf("-openrouter: %q", got.String())
	}
}

// THE PANEL'S EDITS ARE THE SHORTEST RULE THAT SAYS THEM: a tick undone is the
// rule it was, and a list keeps at least one member.
func TestAllowedTogglesInTheShortestSpelling(t *testing.T) {
	kimi := Model{ID: "moonshotai/kimi-k3", Open: true}
	opus := Model{ID: "anthropic/claude-opus-5"}
	all, _ := ParseAllowed("all")

	off, ok := all.Toggled(kimi, false)
	if !ok || off.String() != "all -moonshotai/kimi-k3" {
		t.Fatalf("unticking kimi from all wrote %q", off.String())
	}
	back, _ := off.Toggled(kimi, true)
	if back.String() != "all" || back.Custom() {
		t.Fatalf("ticking it again wrote %q, want the plain base back", back.String())
	}

	open, _ := ParseAllowed("open")
	in, _ := open.Toggled(opus, true)
	if in.String() != "open +anthropic/claude-opus-5" || !in.AdmitsModel(opus) {
		t.Fatalf("ticking a closed model on open wrote %q", in.String())
	}

	list, _ := ParseAllowed("kimi-k3, glm-5.3-flash")
	grown, _ := list.Toggled(opus, true)
	if !grown.AdmitsModel(opus) || grown.Base != BaseList {
		t.Fatalf("ticking onto a list wrote %q", grown.String())
	}
	shrunk, _ := grown.Toggled(kimi, false)
	if shrunk.AdmitsModel(kimi) || len(shrunk.Mods) != 0 {
		t.Fatalf("unticking a member wrote %q, want it off the list and no exception", shrunk.String())
	}
	one, _ := ParseAllowed("kimi-k3")
	if kept, ok := one.Toggled(kimi, false); ok || kept.String() != one.String() {
		t.Fatalf("emptying a list was accepted as %q", kept.String())
	}

	away := all.RouteToggled("openrouter", false)
	if away.String() != "all -openrouter" || away.AdmitsRoute("openrouter") {
		t.Fatalf("taking openrouter away wrote %q", away.String())
	}
	if again := away.RouteToggled("openrouter", true); again.String() != "all" {
		t.Fatalf("giving it back wrote %q", again.String())
	}

	price, _ := ParseAllowed("open -deepseek")
	if re := price.Rebased(BasePrice, 1, 5); re.String() != "≤1/5" {
		t.Fatalf("rebasing onto a price wrote %q", re.String())
	}
	if !price.Custom() {
		t.Fatal("a base with an exception is not custom")
	}
}
