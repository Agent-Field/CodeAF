package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type cutLines []string

func (c cutLines) Lines() []string { return c }
func (cutLines) Note() string      { return "" }
func (cutLines) Close() error      { return nil }

func TestV3InterruptedPutsOneLinePerCallUnderTheNotice(t *testing.T) {
	cfg := session.Config{Interrupted: cutLines{"bash: a — cut", "bash: b — cut"}}
	if got, want := v3Interrupted(cfg, "moved"), "moved\nbash: a — cut\nbash: b — cut"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := v3Interrupted(cfg, ""); strings.HasPrefix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatalf("with no notice got %q", got)
	}
	if got := v3Interrupted(session.Config{}, "moved"); got != "moved" {
		t.Fatalf("with nothing cut off got %q", got)
	}
}
