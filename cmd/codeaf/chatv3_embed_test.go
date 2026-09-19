package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestTheEmbedAdapterIsBoundOrHonestlyAbsent(t *testing.T) {
	source := roles.Source(func(key string) (string, bool) {
		if key == roles.PinKey(roles.RoleEmbed) {
			return "openai/text-embedding-3-small", true
		}
		return "", false
	})
	keyed := config.Config{APIKey: "sk-test", BaseURL: config.DefaultBaseURL}
	if got := v3Embedder(keyed, source, nil); got == nil {
		t.Fatal("a keyed profile with an embed pin got no adapter")
	}
	if got := v3Embedder(config.Config{BaseURL: config.DefaultBaseURL}, source, nil); got != nil {
		t.Fatal("a keyless profile must not bind a stub embedder")
	}

	model, ok, err := v3Embedder(keyed, source, nil).Available(nil)
	if err != nil || !ok || model != "openai/text-embedding-3-small" {
		t.Fatalf("Available = %q %v %v", model, ok, err)
	}
	if strings.Contains(model, "sk-") || strings.Contains(model, "key") {
		t.Fatalf("availability printed a key: %q", model)
	}
	if embed.DegradedLexical("receipts").Mode != embed.LabelDegraded {
		t.Fatal("the fallback must stay labelled degraded")
	}
}
