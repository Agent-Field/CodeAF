package main

import (
	"strings"
	"testing"
)

func TestVersionStringAlwaysAnswers(t *testing.T) {
	version := versionString()
	if strings.TrimSpace(version) == "" {
		t.Fatal("versionString() is empty")
	}
	if !strings.HasPrefix(version, "aforge") {
		t.Fatalf("versionString() = %q, want an aforge-prefixed string", version)
	}
}
