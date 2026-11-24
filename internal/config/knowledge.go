package config

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── WHICH MODEL BUILDS THE LIBRARY ──────────────────────────────────────────
//
// The Library's compiler is a BACKGROUND SEAT (internal/roles' background.go):
// nobody typed its calls, and the owner's rule is that it answers on the model
// the person chose. Until 2026-11-16 a profile that never wrote the knowledge
// row compiled on `deepseek/deepseek-v4.1-flash` whatever the person had
// picked; that default is gone, and the row is now an override a person may
// write, never a model this build chooses for them.

// KnowledgeModelRowAt is the knowledge row as the person wrote it, and empty
// when they wrote nothing — which a settings surface draws as "follows your
// model" rather than as a model id nobody chose.
func KnowledgeModelRowAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyKnowledgeModel); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// KnowledgeModelAt is the model one compilation runs on: the row when the
// person wrote one, else chosen — the model the caller knows the person (or
// the organization) is on — else [ChosenModelAt]. It is read for each
// compilation, so a saved choice reaches the next batch without a restart.
func KnowledgeModelAt(profileDir, chosen string) string {
	if row := KnowledgeModelRowAt(profileDir); row != "" {
		return row
	}
	if model, _ := roles.SplitEffort(chosen); model != "" {
		return model
	}
	model, _ := roles.SplitEffort(ChosenModelAt(profileDir))
	return model
}

// ChosenModelAt is THE MODEL THIS PROFILE CHOSE TO TALK TO, for a background
// seat that runs with no conversation in front of it to borrow a model from:
// the saved choice ([ChatModelAt]), then CODEAF_MODEL, then the bottom rung a
// new conversation would open on ([ChatDefaultAt]). It is the same ladder the
// conversation's own launch walks (cmd/codeaf's v3TalkModel), so a background
// call and the conversation it serves land on one model.
func ChosenModelAt(profileDir string) string {
	if saved := ChatModelAt(profileDir); saved != "" {
		return saved
	}
	if named := strings.TrimSpace(env.Get(ModelEnv)); named != "" {
		return named
	}
	return ChatDefaultAt(profileDir)
}
