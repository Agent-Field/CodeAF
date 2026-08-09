package afield

import (
	"regexp"
	"testing"
)

// labelShape is the one format every trace label must render in: lowercase
// kebab-case with path glyphs, never leading/trailing separators.
var labelShape = regexp.MustCompile(`^[a-z0-9*][a-z0-9*./-]*$`)

func TestToolLabel(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
		want string
	}{
		{"read basename", "read", `{"filePath":"/home/x/repo/internal/util/process.go"}`, "read-process.go"},
		{"write basename", "write", `{"filePath":"cmd/swedog/cp.go","content":"x"}`, "write-cp.go"},
		{"edit basename", "edit", `{"filePath":"main.go","oldString":"a","newString":"b"}`, "edit-main.go"},
		{"bash go test", "bash", `{"command":"go test ./... -count=1"}`, "go-test"},
		{"bash cd chain", "bash", `{"command":"cd /tmp/x && git diff --stat"}`, "git-diff"},
		{"bash env assignment", "bash", `{"command":"FOO=1 BAR=2 make lint"}`, "make-lint"},
		{"bash plain", "bash", `{"command":"./scripts/run.sh --fast"}`, "run.sh"},
		{"bash flag subcommand skipped", "bash", `{"command":"go -v"}`, "go"},
		{"bash empty", "bash", `{"command":""}`, "bash"},
		{"grep pattern", "grep", `{"pattern":"TODO"}`, "grep-todo"},
		{"glob pattern", "glob", `{"pattern":"**/*.go"}`, "glob-**/*.go"},
		{"task intent", "task", `{"description":"Fix the parser precedence bug","prompt":"..."}`, "task-fix-the-parser"},
		{"patch file", "apply_patch", `{"patch":"*** Begin Patch\n*** Update File: internal/x/y.go\n"}`, "patch-y.go"},
		{"patch fallback", "apply_patch", `{"patch":"garbage"}`, "apply-patch"},
		{"unknown tool", "repo_overview", `{}`, "repo-overview"},
		{"bad json falls back", "read", `{not json`, "read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToolLabel(tc.tool, tc.args)
			if got != tc.want {
				t.Fatalf("ToolLabel(%q, %q) = %q, want %q", tc.tool, tc.args, got, tc.want)
			}
			if !labelShape.MatchString(got) {
				t.Fatalf("label %q is not normalized", got)
			}
		})
	}
}

func TestIntentLabel(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"lead-in stripped", "I'll fix the parser precedence bug first.", "fix-the-parser"},
		{"stacked lead-ins", "Okay, let me run the tests now.", "run-the-tests"},
		{"markdown stripped", "## Now I will update the README", "update-the-readme"},
		{"sentence cut", "Reading the config. Then editing.", "reading-the-config"},
		{"colon cut", "First step: inspect the scheduler", "step"},
		{"empty", "", ""},
		{"only lead-in", "Okay.", ""},
		{"short kept", "Done", "done"},
		{"plain verb phrase", "Running go vet across packages", "running-go-vet"},
		{"truncation article dropped", "Build calcsrv, a CLI calculator service", "build-calcsrv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IntentLabel(tc.text); got != tc.want {
				t.Fatalf("IntentLabel(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestStageLabel(t *testing.T) {
	if got := StageLabel("classifier", nil); got != "classify-goal" {
		t.Fatalf("classifier = %q", got)
	}
	if got := StageLabel("audit", map[string]any{"cycle": 2}); got != "audit-2" {
		t.Fatalf("audit cycle = %q", got)
	}
	if got := StageLabel("scheduler", map[string]any{"cycle": float64(3)}); got != "dispatch-leaves-3" {
		t.Fatalf("scheduler cycle = %q", got)
	}
	if got := StageLabel("mystery-stage", nil); got != "mystery-stage" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Implement expr/ast parser", "implement-expr/ast-parser"},
		{"Build calcsrv, a", "build-calcsrv"},
		{"classify goal", "classify-goal"},
		{"leaf 07 → main", "leaf-07-main"},
		{"already-normal-1.go", "already-normal-1.go"},
		{"  spaced   out  ", "spaced-out"},
		{"expr/ ast", "expr/ast"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClipPrefersBoundary(t *testing.T) {
	long := "abcdefghij klmnopqrst uvwxyzabcd efgh"
	got := clip(long, 28)
	if len(got) > 28 {
		t.Fatalf("clip returned %d chars: %q", len(got), got)
	}
	if got != "abcdefghij klmnopqrst" {
		t.Fatalf("clip = %q", got)
	}
	kebab := "implement-expr/parser-and-evaluator"
	got = clip(kebab, 28)
	if got != "implement-expr/parser" {
		t.Fatalf("kebab clip = %q", got)
	}
}
