package crewroute

import "testing"

// A TICK IS ONE MODEL ON ONE PROVIDER. Unticking GLM 5.3 on OpenRouter leaves
// the same model allowed on Z.ai, and the rule says so in words `/crew models`
// reads back.
func TestARowTickSpeaksForOneProviderOnly(t *testing.T) {
	glm := Model{ID: "z-ai/glm-5.3", PromptPrice: 4e-7, CompletionPrice: 1.6e-6}
	for _, base := range []string{"all", "open", "≤1/5", "z-ai/glm-5.3, deepseek"} {
		rule, err := ParseAllowed(base)
		if err != nil {
			t.Fatal(err)
		}
		if base == "open" {
			glm.Open = true
		}
		off := rule.RowToggled(glm, "openrouter", false)
		if off.AdmitsOn(glm, "openrouter") {
			t.Errorf("%s: unticked on openrouter, still admitted there: %s", base, off)
		}
		if !off.AdmitsOn(glm, "z-ai") {
			t.Errorf("%s: unticking openrouter's row took z-ai's too: %s", base, off)
		}
		reread, err := ParseAllowed(off.String())
		if err != nil || reread.String() != off.String() {
			t.Fatalf("%s: rule %q does not read back: %v", base, off, err)
		}
		on := reread.RowToggled(glm, "openrouter", true)
		if on.String() != rule.String() {
			t.Errorf("%s: ticking back wrote %q, want the rule as it was, %q", base, on, rule)
		}
	}
}

// A LATER WORD ABOUT THE WHOLE MODEL OR PROVIDER OUTRANKS AN EARLIER ROW WORD,
// so the last word written is still the verdict.
func TestTheLastWordWinsBetweenRowAndModelWords(t *testing.T) {
	glm := Model{ID: "z-ai/glm-5.3"}
	rule, _ := ParseAllowed("all -glm-5.3@openrouter -glm-5.3")
	if rule.AdmitsOn(glm, "z-ai") || rule.AdmitsOn(glm, "openrouter") {
		t.Errorf("a later -glm-5.3 should leave it out everywhere")
	}
	rule, _ = ParseAllowed("all -glm-5.3 +glm-5.3@z-ai")
	if !rule.AdmitsOn(glm, "z-ai") || rule.AdmitsOn(glm, "openrouter") {
		t.Errorf("+glm-5.3@z-ai after -glm-5.3 should let it in on z-ai alone")
	}
	rule, _ = ParseAllowed("all +glm-5.3@openrouter -openrouter")
	if rule.AdmitsOn(glm, "openrouter") {
		t.Errorf("a later -openrouter should take openrouter's route away")
	}
	// A row word says nothing about the model as a whole.
	if !rule.AdmitsModel(glm) || rule.NamesModel("z-ai/glm-5.3") {
		t.Errorf("a row word must not be read as a model word")
	}
}

// A MODEL-WIDE TICK SPEAKS FOR EVERY ROUTE, so it clears the row words for
// that model rather than leaving one provider silently out.
func TestAModelTickClearsItsRowWords(t *testing.T) {
	glm := Model{ID: "z-ai/glm-5.3"}
	rule, _ := ParseAllowed("all -glm-5.3@openrouter")
	out, ok := rule.Toggled(glm, true)
	if !ok || out.String() != "all" || !out.AdmitsOn(glm, "openrouter") {
		t.Errorf("model tick: %q, want all", out)
	}
}
