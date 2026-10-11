package factory

import (
	"os"
	"strings"
	"testing"
)

func TestFactoryNameLimitsAndDefaults(t *testing.T) {
	for _, text := range []string{"  Our team  ", strings.Repeat("界", MaxNameChars), ""} {
		got, err := NormalizeName(text)
		if err != nil || got != strings.TrimSpace(text) {
			t.Fatalf("name %q = %q, %v", text, got, err)
		}
	}
	for _, text := range []string{strings.Repeat("界", MaxNameChars+1), "team\n", "\tteam", "te\x00am", "team\u0085", "\xff"} {
		if _, err := NormalizeName(text); err == nil {
			t.Errorf("accepted invalid name %q", text)
		}
	}
	if got := DisplayName("", []string{"Agent-Field/codeaf", "other/repo"}); got != "Agent-Field" {
		t.Fatalf("owner = %q", got)
	}
	if got := DisplayName("Our team", []string{"owner/repo"}); got != "Our team" {
		t.Fatalf("explicit name = %q", got)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	host, _, _ = strings.Cut(host, ".")
	if got := DisplayName("", nil); got != host {
		t.Fatalf("hostname = %q, want %q", got, host)
	}
}
