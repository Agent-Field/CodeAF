package artifactregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutDeduplicatesWithoutRewriting(t *testing.T) {
	workspace := t.TempDir()
	ref := PutArtifact(workspace, "diff", "original")
	path := filepath.Join(workspace, ".codeaf", "artifacts", ref.ID+".txt")
	if err := os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := PutArtifact(workspace, "other-kind", "original")
	if again.ID != ref.ID {
		t.Fatalf("same content got ids %q and %q", ref.ID, again.ID)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "corrupt" {
		t.Fatalf("existing file was rewritten: %q", got)
	}
}

func TestRenderRefFailsOpenForMissingAndCorruptCopies(t *testing.T) {
	workspace := t.TempDir()
	ref := PutArtifact(workspace, "spec", strings.Repeat("body", 20))
	path := filepath.Join(workspace, ".codeaf", "artifacts", ref.ID+".txt")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got := RenderRef(ref, &RenderRefOptions{})
	if !strings.Contains(got, "durable copy unavailable; full body inlined below") ||
		!strings.HasSuffix(got, "\n"+ref.Body) {
		t.Fatalf("missing copy did not fail open:\n%s", got)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = RenderRef(ref, nil)
	if !strings.HasSuffix(got, "\n"+ref.Body) {
		t.Fatalf("corrupt copy did not fail open:\n%s", got)
	}
}

func TestGetArtifactIntegrityAndLossyUTF8(t *testing.T) {
	workspace := t.TempDir()
	id := "manual"
	path := filepath.Join(workspace, ".codeaf", "artifacts", id+".txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{'a', 0xe2, 0x82}, 0o644); err != nil {
		t.Fatal(err)
	}
	body, ok := GetArtifact(workspace, id)
	if !ok || body != "a�" {
		t.Fatalf("lossy read = %q, %v", body, ok)
	}
	sum := sha256.Sum256([]byte(body))
	if _, ok := GetArtifact(workspace, id, hex.EncodeToString(sum[:])); !ok {
		t.Fatal("matching expected sha rejected")
	}
	if _, ok := GetArtifact(workspace, id, strings.Repeat("0", 64)); ok {
		t.Fatal("mismatched expected sha accepted")
	}
}

func TestPutArtifactSwallowsFilesystemFailure(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(workspace, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := PutArtifact(workspace, "diff", "body")
	if ref.Body != "body" || ref.ID == "" {
		t.Fatalf("best-effort write did not return ref: %+v", ref)
	}
	if _, ok := GetArtifact(workspace, ref.ID); ok {
		t.Fatal("artifact unexpectedly readable through a file workspace")
	}
}

func TestArtifactAbsPathUsesNodeResolveSemantics(t *testing.T) {
	if got, want := artifactAbsPath("/workspace", "/tmp/absolute-id"), "/tmp/absolute-id.txt"; got != want {
		t.Fatalf("absolute id path = %q, want %q", got, want)
	}
}
