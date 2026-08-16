package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
)

// harnessMachineryError is the refusal a coder leaf receives when it tries to
// write, edit, or patch a path inside the workspace's .codeaf/ tree.
const harnessMachineryError = "harness machinery; not part of the task"

// executeAs runs a tool call as the named agent. The D9 .codeaf/ write guard
// is agent-scoped (it refuses only the coder), so the refusal tests must carry
// Agent == "coder"; the default execute helper sets no agent.
func executeAs(t *testing.T, registry *Registry, agent, name string, input any) (steploop.ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return registry.Execute(context.Background(), steploop.ToolCall{
		Name: name, Input: raw, Agent: agent,
	})
}

// TestApplyPatchOfferedToDeepseek (D9 acceptance a) asserts that the
// multi-file apply_patch tool is present in the coder's filtered toolset for
// the benchmark's deepseek model. Upstream gated it to gpt-family models only;
// the gate is removed, so deepseek coder leaves are no longer limited to
// single-file edits.
func TestApplyPatchOfferedToDeepseek(t *testing.T) {
	registry := New(t.TempDir())
	names := definitionNames(registry.DefinitionsFor(FilterInput{
		ProviderID: "openrouter",
		ModelID:    "deepseek/deepseek-v4-flash",
		AgentName:  "coder",
	}))
	if !containsName(names, "apply_patch") {
		t.Fatalf("deepseek coder toolset missing apply_patch: %v", names)
	}
	// edit and write remain available too — the ungate keeps all three editors.
	if !containsName(names, "edit") || !containsName(names, "write") {
		t.Fatalf("deepseek coder toolset missing edit/write: %v", names)
	}
}

// TestHarnessMachineryWriteGuard (D9 acceptance b) asserts that a coder leaf's
// write, edit, and apply_patch all refuse to target .codeaf/contract.json —
// the harness's own registered contract. A coder leaf editing it (the FEATURE
// cell regressed this way) corrupts the run. The guard is agent-scoped: the
// harness's own agents (auditor, architect, …) still write their legitimate
// .codeaf/ files through these same tools (see TestNonCoderCanWriteCodeaf).
func TestHarnessMachineryWriteGuard(t *testing.T) {
	workDir := t.TempDir()
	// The engine writes the contract itself, directly via os.WriteFile in
	// internal/session/contract — never through this tool path. Stage it as
	// the engine would so the edit "file exists" branch could otherwise proceed.
	if err := os.MkdirAll(filepath.Join(workDir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workDir, ".codeaf", "contract.json"),
		[]byte(`{"command":"./check.sh"}`), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	registry := New(workDir)

	t.Run("write", func(t *testing.T) {
		_, err := executeAs(t, registry, "coder", "write", map[string]any{
			"filePath": ".codeaf/contract.json",
			"content":  "hijacked",
		})
		if err == nil || !strings.Contains(err.Error(), harnessMachineryError) {
			t.Fatalf("write .codeaf/contract.json error = %v", err)
		}
		// The file is untouched.
		got, readErr := os.ReadFile(filepath.Join(workDir, ".codeaf", "contract.json"))
		if readErr != nil || strings.Contains(string(got), "hijacked") {
			t.Fatalf("harness file was modified: %q (err=%v)", got, readErr)
		}
	})

	t.Run("edit", func(t *testing.T) {
		_, err := executeAs(t, registry, "coder", "edit", map[string]any{
			"filePath":  ".codeaf/contract.json",
			"oldString": `{"command":"./check.sh"}`,
			"newString": `{"command":"echo pwned"}`,
		})
		if err == nil || !strings.Contains(err.Error(), harnessMachineryError) {
			t.Fatalf("edit .codeaf/contract.json error = %v", err)
		}
	})

	t.Run("apply_patch", func(t *testing.T) {
		patch := "*** Begin Patch\n" +
			"*** Update File: .codeaf/contract.json\n" +
			"@@\n" +
			`-{"command":"./check.sh"}` + "\n" +
			`+{"command":"echo pwned"}` + "\n" +
			"*** End Patch"
		_, err := executeAs(t, registry, "coder", "apply_patch", map[string]any{"patchText": patch})
		if err == nil || !strings.Contains(err.Error(), harnessMachineryError) {
			t.Fatalf("apply_patch .codeaf/contract.json error = %v", err)
		}
	})

	t.Run("apply_patch move into .codeaf", func(t *testing.T) {
		// A move whose destination lands under .codeaf/ is refused too.
		if err := os.WriteFile(filepath.Join(workDir, "src.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		patch := "*** Begin Patch\n" +
			"*** Update File: src.txt\n" +
			"*** Move to: .codeaf/contract.json\n" +
			"@@\n" +
			"-x\n" +
			"+y\n" +
			"*** End Patch"
		_, err := executeAs(t, registry, "coder", "apply_patch", map[string]any{"patchText": patch})
		if err == nil || !strings.Contains(err.Error(), harnessMachineryError) {
			t.Fatalf("apply_patch move into .codeaf error = %v", err)
		}
	})
}

// TestNonCoderCanWriteCodeaf (D9) locks the guard's agent scope: the auditor
// legitimately writes its verdict to .codeaf/auditor-verdict.json through the
// write tool, and must keep doing so. A blanket .codeaf/ block would break the
// audit pipeline; only the coder worker is refused.
func TestNonCoderCanWriteCodeaf(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := New(workDir)
	verdict := `{"verdict":"pass"}`
	_, err := executeAs(t, registry, "auditor", "write", map[string]any{
		"filePath": ".codeaf/auditor-verdict.json",
		"content":  verdict,
	})
	if err != nil {
		t.Fatalf("auditor write .codeaf/auditor-verdict.json error = %v", err)
	}
	assertTestFile(t, workDir, ".codeaf/auditor-verdict.json", verdict)
}

// TestHarnessMachineryReadStillAllowed (D9) asserts the guard is write-only:
// the root-cut flow instructs the coder to read .codeaf/contract.json, and
// that read must keep working. Reads use resolvePath, not the mutation choke
// point, so they are unaffected by the guard (and unscoped by agent).
func TestHarnessMachineryReadStillAllowed(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := `{"command":"./check.sh"}`
	if err := os.WriteFile(
		filepath.Join(workDir, ".codeaf", "contract.json"), []byte(want), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	result, err := executeAs(t, New(workDir), "coder", "read", map[string]any{
		"filePath": ".codeaf/contract.json",
	})
	if err != nil {
		t.Fatalf("read .codeaf/contract.json error = %v", err)
	}
	if !strings.Contains(result.Output, want) {
		t.Fatalf("read output = %q, want it to contain %q", result.Output, want)
	}
}

// TestNormalPathWriteStillWorks (D9 acceptance c) asserts the guard does not
// block legitimate writes to ordinary task files.
func TestNormalPathWriteStillWorks(t *testing.T) {
	workDir := t.TempDir()
	registry := New(workDir)
	_, err := executeAs(t, registry, "coder", "write", map[string]any{
		"filePath": "normal.txt",
		"content":  "hello",
	})
	if err != nil {
		t.Fatalf("write normal.txt error = %v", err)
	}
	assertTestFile(t, workDir, "normal.txt", "hello")
}
