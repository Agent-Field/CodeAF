package agentjson

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadFileStateRecoversDroppedLeadingDot is the go-humanize regression: the
// auditor wrote a complete, valid verdict to the private artifact path minus
// its leading dot, and the harness reported "did not produce a parseable
// verdict after 3 attempts" while the work sat on disk beside it.
func TestReadFileStateRecoversDroppedLeadingDot(t *testing.T) {
	dir := t.TempDir()
	expected := filepath.Join(dir, ".auditor-verdict.json.agentjson-4284993955")
	written := filepath.Join(dir, "auditor-verdict.json.agentjson-4284993955")
	body := `{"verdict":"pass","blockers":[]}`
	if err := os.WriteFile(written, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	state := readFileState(expected)
	if state.Kind != FileValidJSON {
		t.Fatalf("state = %#v, want valid-json recovered from the dropped-dot sibling", state)
	}
	if string(state.Data) != body {
		t.Fatalf("data = %s, want %s", state.Data, body)
	}
}

// TestReadFileStateRecoveryIsNarrow: the recovery only applies to this
// harness's own random-suffixed private artifacts, and only when the expected
// path is genuinely absent.
func TestReadFileStateRecoveryIsNarrow(t *testing.T) {
	t.Run("non-agentjson dotfile is not recovered", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"a":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if state := readFileState(filepath.Join(dir, ".config.json")); state.Kind != FileMissing {
			t.Fatalf("state = %#v, want missing: only .agentjson- artifacts are eligible", state)
		}
	})

	t.Run("no sibling means missing", func(t *testing.T) {
		dir := t.TempDir()
		if state := readFileState(
			filepath.Join(dir, ".auditor-verdict.json.agentjson-1"),
		); state.Kind != FileMissing {
			t.Fatalf("state = %#v, want missing", state)
		}
	})

	t.Run("expected path wins when both exist", func(t *testing.T) {
		dir := t.TempDir()
		expected := filepath.Join(dir, ".v.json.agentjson-7")
		if err := os.WriteFile(expected, []byte(`{"from":"expected"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "v.json.agentjson-7"), []byte(`{"from":"sibling"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		state := readFileState(expected)
		if string(state.Data) != `{"from":"expected"}` {
			t.Fatalf("data = %s, want the expected path to win", state.Data)
		}
	})

	t.Run("recovered garbage is still a parse error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "v.json.agentjson-9"), []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		state := readFileState(filepath.Join(dir, ".v.json.agentjson-9"))
		if state.Kind != FileParseError {
			t.Fatalf("state = %#v, want parse-error: recovery must not relax validation", state)
		}
	})
}
