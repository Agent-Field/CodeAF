package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// These are refusals after the flags parsed, before an errand could start. A
// caller that asked for JSON still needs the same envelope as a failed run.
func TestDoParsedRefusalsKeepTheJSONContract(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"blank brief", []string{"--json", "--dir", t.TempDir(), ""}, "no goal given"},
		{"two crew choices", []string{"--json", "--best", "--cheap", "write a note"}, "--best and --cheap ask for two different crews · say one"},
		{"bad slots", []string{"--json", "--slots", "many", "write a note"}, "--slots wants a whole number of workers, 0 for no limit; got \"many\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := captureDoRefusal(t, tc.args)
			if got := exitCodeOf(err); got != 1 {
				t.Fatalf("exit = %d, want 1: %v", got, err)
			}
			fields := errandJSONFields(t, stdout)
			for key, want := range map[string]any{
				"ok": false, "stop": "error", "answer": "", "error": tc.want,
			} {
				if got := fields[key]; got != want {
					t.Errorf("%s = %v, want %v", key, got, want)
				}
			}
			if files, ok := fields["files"].([]any); !ok || len(files) != 0 {
				t.Errorf("files = %v, want []", fields["files"])
			}
			for _, key := range envelopeContract {
				if _, ok := fields[key]; !ok {
					t.Errorf("envelope lacks %q", key)
				}
			}
			if stderr != "error: "+tc.want+"\n" {
				t.Errorf("stderr = %q, want human refusal", stderr)
			}
		})
	}
}

func TestDoParsedRefusalsWithoutJSONKeepTheirOldReturn(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	for _, args := range [][]string{
		{"--dir", t.TempDir(), ""},
		{"--best", "--cheap", "write a note"},
		{"--slots", "many", "write a note"},
	} {
		stdout, stderr, err := captureDoRefusal(t, args)
		if exitCodeOf(err) != 1 || stdout != "" || stderr != "" {
			t.Errorf("args %q: exit %d, stdout %q, stderr %q", args, exitCodeOf(err), stdout, stderr)
		}
		var status exitStatus
		if errors.As(err, &status) {
			t.Errorf("args %q: old error was replaced by a status", args)
		}
	}
}

func TestDoFlagParseRefusalKeepsUsageWithoutAJSONEnvelope(t *testing.T) {
	stdout, stderr, err := captureDoRefusal(t, []string{"--json", "--no-such-flag", "write a note"})
	if exitCodeOf(err) != 1 || stdout != "" || !strings.Contains(stderr, "has no --no-such-flag flag") {
		t.Fatalf("exit %d, stdout %q, stderr %q", exitCodeOf(err), stdout, stderr)
	}
}

func captureDoRefusal(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	errs, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errs.Close()
	oldOut, oldErr, oldUsageOut, oldUsageErr := os.Stdout, os.Stderr, usageOut, usageErr
	os.Stdout, os.Stderr, usageOut, usageErr = out, errs, out, errs
	defer func() { os.Stdout, os.Stderr, usageOut, usageErr = oldOut, oldErr, oldUsageOut, oldUsageErr }()
	runErr := runDo(args)
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := errs.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(errs)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr), runErr
}
