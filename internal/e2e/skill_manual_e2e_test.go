//go:build e2e

package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Read the actual tool result from a fresh terminal conversation for each
// question: an answer or a tool-call argument alone cannot prove retrieval.
func TestSkillManualOnTheTerminal(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	for _, memory := range []string{"on", "off"} {
		for index, question := range []struct{ query, heading string }{
			{"what is a skill", "What is a skill"},
			{"can you use skills", "Can you use skills"},
			{"do you have skills", "Can you use skills"},
			{"why can't you use my skills", "Why can't you use my skills"},
		} {
			t.Run(memory+"/"+question.query, func(t *testing.T) {
				home := newHome(t, map[string]any{"memory.enabled": memory, "models.fallbacks": "", "model_pool": "off"})
				workspace := newWorkspace(t, "skill-manual", false)
				stage := fmt.Sprintf("skill-manual-%s-%d", memory, index)
				r := startDefault(t, stage, home, workspace, 140, 42,
					"chat", "--model", model, "--one-model", "--debug")
				r.lit(fmt.Sprintf("Look up this question with the manual tool, setting query to exactly %q, then answer it briefly in your own words. Explain whether automatic memory saving being off disables skills. Finish with SKILL_MANUAL_CHECK_DONE. Do not change files.", question.query))
				r.keys("Enter")
				cleanChatWaitAnswer(t, r, "SKILL_MANUAL_CHECK_DONE", 2*time.Minute)
				journals, err := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*", "transcript.jsonl"))
				if err != nil || len(journals) != 1 {
					t.Fatalf("want one conversation journal, got %v: %v", journals, err)
				}
				matched := false
				for _, call := range manualCalls(t, session.Place{Dir: filepath.Dir(journals[0])}) {
					if call.Query != question.query {
						continue
					}
					for at, section := range renderedSections(call.Output) {
						if strings.HasPrefix(section.Title, question.heading) && strings.Contains(section.Body, "memory.enabled") {
							matched = true
							if index == 0 && at != 0 {
								t.Error("the definition was not the first returned section")
							}
						}
					}
				}
				if !matched {
					t.Fatal("the exact manual query did not deliver its answer and memory independence to the model")
				}
				cleanChatCapture(t, r, stage)
				cleanChatAuditModels(t, home, model, stage)
			})
		}
	}
}
