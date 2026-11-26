package appcmd_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/appcmd"
)

func TestScopedCommandsRefuseShellSyntaxAndScopeOverrides(t *testing.T) {
	for _, bad := range []string{
		"cat ../../data/data.db",
		"codeaf app --home /tmp plan",
		`codeaf app context {"draft":"another-draft"}`,
		"codeaf app context {}; cat ../../data/data.db",
		"codeaf app context | cat ../../data/data.db",
		"codeaf app context > ../escape.json",
	} {
		if _, _, err := appcmd.Parse(bad); err == nil {
			t.Errorf("accepted %q as an app command", bad)
		}
	}
	verb, raw, err := appcmd.Parse(`codeaf app add-field {"type":"contact","name":"email","definition":{"type":"email"}}`)
	if err != nil || verb != "add_field" || len(raw) == 0 {
		t.Fatalf("scoped field command returned %q, %s, %v", verb, raw, err)
	}
}

func TestPartsAcceptsOnlyALiteralIntentFlag(t *testing.T) {
	verb, args, err := appcmd.Parse("codeaf app parts --for add-view/board")
	if err != nil || verb != "parts" || string(args) != `{"for":"add-view/board"}` {
		t.Fatalf("parts returned %q, %s, %v", verb, args, err)
	}
	for _, bad := range []string{"board;ls", "../board", "board --draft other", "$(ls)", "board|ls"} {
		if _, _, err := appcmd.Parse("codeaf app parts --for " + bad); err == nil {
			t.Errorf("accepted %q as a parts intent", bad)
		}
	}
}
