package e2e

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
	// Register the session's auxiliary roles exactly as the live harness does.
	_ "github.com/Agent-Field/codeaf/internal/session"
)

// liveVerificationModel chooses one model for the entire live verification run.
func liveVerificationModel() string {
	if model := strings.TrimSpace(os.Getenv("CODEAF_E2E_MODEL")); model != "" {
		return model
	}
	return "deepseek/deepseek-v4.1-flash"
}

// Media roles require their own capabilities; every role that answers with
// text belongs to the model whose behavior this run measures.
var textRoles = func() []roles.Role {
	var text []roles.Role
	for _, role := range roles.Vocabulary() {
		switch role {
		case roles.RoleVision, roles.RoleImageGen, roles.RoleSpeech, roles.RoleVideo:
			continue
		}
		text = append(text, role)
	}
	return text
}()

// pinnedTextModels is shared by the direct-session and real-binary harnesses
// so a new auxiliary role cannot inherit a model from the copied profile.
func pinnedTextModels(model string) map[string]any {
	rows := map[string]any{}
	for _, key := range []string{config.KeyChatModel, config.KeyTaskModel,
		config.KeyTierWorkerModel, config.KeyTierLowModel, config.KeyTierHighModel,
		config.KeyTierReflexModel, config.KeyTierMastermindModel, config.KeyModelFallbacks} {
		rows[key] = model
	}
	var pins []string
	for _, role := range textRoles {
		pins = append(pins, string(role)+":"+model)
	}
	rows[config.KeyModelRoles] = strings.Join(pins, ",")
	return rows
}

func TestEveryE2ETextRoleResolvesToThePinnedModel(t *testing.T) {
	for _, override := range []string{"", " vendor/override "} {
		t.Run(override, func(t *testing.T) {
			t.Setenv("CODEAF_E2E_MODEL", override)
			model := liveVerificationModel()
			want := "deepseek/deepseek-v4.1-flash"
			if override != "" {
				want = strings.TrimSpace(override)
			}
			if model != want {
				t.Fatalf("verification model = %q, want %q", model, want)
			}
			rows := pinnedTextModels(model)
			pins, err := config.ParseModelRoles(rows[config.KeyModelRoles].(string))
			if err != nil {
				t.Fatal(err)
			}
			source := roles.Source(func(key string) (string, bool) {
				for role, value := range pins {
					if key == roles.PinKey(roles.Role(role)) {
						return value, true
					}
				}
				return "inherited/other-model", true
			})
			for _, role := range textRoles {
				got, err := roles.Resolve(source, role, "inherited/conversation")
				if err != nil || got != model {
					t.Errorf("role %s = %q, %v; want %q", role, got, err, model)
				}
			}
		})
	}
}
