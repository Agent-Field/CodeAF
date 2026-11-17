package factory_test

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestStageMethodShippedAndPersonalAsks(t *testing.T) {
	budgets := map[string]int{"plan": 470, "write": 510, "test": 365, "review": 580, "security": 400, "proof": 320, "read": 240, "checks": 190, "bisect": 480, "fix": 410}
	for _, st := range factory.ShippedStages() {
		if utf8.RuneCountInString(st.Ask) > factory.AskMost {
			t.Errorf("%s ask exceeds %d cells", st.Name, factory.AskMost)
		}
		method := factory.StageMethod(st)
		if st.Kind != factory.StageChat {
			if method != "" {
				t.Errorf("%s gate has a method", st.Name)
			}
			continue
		}
		if strings.TrimSpace(method) == "" {
			t.Errorf("%s has no method", st.Name)
		}
		// The literal budget limits the cost paid on every stage conversation.
		if utf8.RuneCountInString(method) > budgets[st.Name] {
			t.Errorf("%s method exceeds %d characters", st.Name, budgets[st.Name])
		}
		if regexp.MustCompile(`(?i)\b(go|golang|python|javascript|typescript|rust|npm|pytest|git|bash|plugin|skill)\b`).MatchString(method) {
			t.Errorf("%s method names a language or tool", st.Name)
		}
		st.Ask += " with my instructions"
		if factory.StageMethod(st) != "" {
			t.Errorf("%s edited ask inherited a method", st.Name)
		}
	}
	it := factory.Item{}
	factory.EnsureFollowPR(&it)
	for _, st := range it.Stages {
		if utf8.RuneCountInString(st.Ask) > factory.AskMost {
			t.Errorf("%s ask exceeds %d cells", st.Name, factory.AskMost)
		}
	}
	st, _ := factory.ShippedStage("write")
	st.Name = "custom"
	if factory.StageMethod(st) != "" {
		t.Error("custom step inherited a method")
	}
}
