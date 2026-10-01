package main

import (
	"strings"
	"testing"
)

func TestDocHelpNamesItsRequiredPathAndPageRange(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	out, errs := captureUsage(t)
	if err := runDoc([]string{"--help"}); err != nil && exitCodeOf(err) != 0 {
		t.Fatalf("doc --help failed: %v", err)
	}
	if errs.Len() != 0 {
		t.Fatalf("doc --help wrote to stderr: %s", errs.String())
	}
	if !strings.Contains(out.String(), "codeaf doc PATH [--pages A-B]") {
		t.Fatalf("doc help omits its required argument:\n%s", out.String())
	}
	if strings.Contains(out.String(), "codeaf patch") {
		t.Fatalf("doc help borrows patch's synopsis:\n%s", out.String())
	}
}

// A shared front-page row saves height without making one command's help
// borrow another command's arguments. Alternative forms retain their pipes.
func TestSharedHelpRowsKeepEachCommandsOwnSynopsis(t *testing.T) {
	for _, test := range []struct {
		name, want string
	}{
		{"patch", "  codeaf patch FILE --old TEXT --new TEXT"},
		{"doc", "  codeaf doc PATH [--pages A-B]"},
		{"image", "  codeaf image \"PROMPT\" --out PATH"},
		{"manual", "  codeaf manual <page> | \"<question>\""},
		{"web", "  codeaf web fetch URL | web search QUERY"},
	} {
		shape := usageForCommand(test.name)
		if !strings.Contains(shape, test.want) || strings.Contains(shape, " | codeaf ") {
			t.Errorf("%s borrows another command's synopsis or lost its arguments: %q, want %q", test.name, shape, test.want)
		}
	}
}
