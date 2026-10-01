package modelapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// The actual served model decides whose credential explanation is applicable.
func TestGatewayAuthSourceBelongsToTheServedModel(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "default-test-key")
	profile := t.TempDir()
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(config.DefaultBaseURL)}, modelsource.Connected{Source: modelsource.Source{ID: "direct", Written: "direct"}})
	for _, status := range []int{401, 403} {
		for _, model := range []string{"test/model", "direct/model"} {
			t.Run(fmt.Sprintf("%d/%s", status, model), func(t *testing.T) {
				calls := &script{reply: func(context.Context, string, []ai.Message, ai.Request) (*ai.Response, error) {
					return nil, &provider.APIError{Status: status, Message: fakeAuthKey()}
				}}
				_, api := open(t, modelapi.Config{CompleterFor: calls.completerFor, AuthKeySource: func(model string) string { return config.APIKeySourceForModel(profile, sources, model) }})
				outer, payload := post(t, api, api.Token, `{"model":"`+model+`","messages":[{"role":"user","content":"hello"}]}`)
				if outer != http.StatusBadGateway {
					t.Fatalf("status=%d, want 502", outer)
				}
				got := strings.Contains(string(payload), "the shell's OPENROUTER_API_KEY")
				if got != (model == "test/model") {
					t.Fatalf("wrong key source for %s: %s", model, payload)
				}
				if strings.Contains(string(payload), fakeAuthKey()) {
					t.Fatal("gateway exposed a key")
				}
			})
		}
	}
}

func fakeAuthKey() string { return "sk-or-v1-" + strings.Repeat("0123456789abcdef", 4) }
