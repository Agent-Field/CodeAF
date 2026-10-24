package session

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/providerhttp"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

func TestOriginRefusalEndingKeepsHostAndDoesNotBlameTheKey(t *testing.T) {
	origin, _ := url.Parse("https://api.mock.test/start")
	target, _ := url.Parse("https://mock-user:mock-password@redirect.mock.test/job?token=mock-query-secret")
	err := providerhttp.CheckRedirect(&http.Request{URL: target}, []*http.Request{{URL: origin}})
	if err == nil {
		t.Fatal("expected boundary refusal")
	}
	if provider.Evidence(err).TerminalTransport {
		t.Fatal("origin refusal misclassified as expired credentials")
	}
	ended := endingWords(err, taxonomy.Verdict{Class: taxonomy.Work, Action: taxonomy.ActionReport}, "mock provider", "shell key")
	if !strings.Contains(ended.Error(), "redirect.mock.test") || !strings.Contains(ended.Error(), "check the configured service address") {
		t.Fatalf("ending lost actionable host: %v", ended)
	}
	for _, secret := range []string{"mock-user", "mock-password", "mock-query-secret", "key was not accepted"} {
		if strings.Contains(ended.Error(), secret) {
			t.Fatalf("unsafe or incorrect ending: %v", ended)
		}
	}
}
