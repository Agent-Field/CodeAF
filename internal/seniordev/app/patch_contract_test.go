//go:build !windows

package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSafeSeniorDevEnvironmentRedactsSecretLikeValues(t *testing.T) {
	t.Setenv("SENIOR_DEV_NET", "off")
	t.Setenv("SENIOR_DEV_EXAMPLE_TOKEN", "do-not-record-me")
	got := safeSeniorDevEnvironment()
	if got["SENIOR_DEV_NET"] != "off" {
		t.Fatalf("ordinary variable missing: %#v", got)
	}
	if got["SENIOR_DEV_EXAMPLE_TOKEN"] != "<redacted>" {
		t.Fatalf("secret-like value was not redacted: %#v", got)
	}
}

func TestPatchSummaryEmitsBoundedMachineReadableMetrics(t *testing.T) {
	runner := gitTestRepo(t)
	var output bytes.Buffer
	runner.events = newEventWriter(&output)
	writeWorkspace(t, runner, "main.go", "candidate\n")
	runner.emitPatchSummary(gitOutput(context.Background(), runner.workspace, "rev-parse", "HEAD"))
	for _, fragment := range []string{
		`"stage":"patch-summary"`, `"status":"completed"`,
		`"files":1`, `"additions":1`, `"deletions":1`, `"patch_bytes":`,
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("patch summary missing %s: %s", fragment, output.String())
		}
	}
}
