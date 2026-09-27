package modelapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestAuthFailureSentenceNamesKeySourceWithoutTheKey(t *testing.T) {
	for _, row := range []struct {
		name   string
		status int
		source string
		key    string
	}{
		{"shell OpenRouter", http.StatusUnauthorized, "the shell's OPENROUTER_API_KEY", fakeKey("sk-or-v1-", "0123456789abcdef")},
		{"shell OpenAI", http.StatusForbidden, "the shell's OPENAI_API_KEY", fakeKey("sk-proj-", "0123456789abcdef")},
		{"profile", http.StatusUnauthorized, "the key saved in your profile", fakeKey("sk-or-v1-", "fedcba9876543210")},
	} {
		t.Run(row.name, func(t *testing.T) {
			server := &Server{ctx: context.Background(), config: Config{AuthKeySource: row.source}}
			status, said := server.failure(&provider.APIError{Status: row.status, Message: row.key}, context.Background(), "test/model")
			if status != row.status {
				t.Fatalf("auth status = %d, want %d", status, row.status)
			}
			if !strings.Contains(said, row.source) {
				t.Fatalf("auth sentence = %q, want source %q", said, row.source)
			}
			if strings.Contains(said, row.key) {
				t.Fatalf("auth sentence exposes the key: %q", said)
			}
		})
	}
}

// fakeKey builds a key-shaped string at run time, so no key-shaped literal
// sits in the source for a secret scanner to mistake for a real one.
func fakeKey(prefix, block string) string {
	return prefix + strings.Repeat(block, 4)
}
