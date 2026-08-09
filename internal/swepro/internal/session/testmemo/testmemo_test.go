package testmemo

import (
	"math"
	"testing"
)

func TestLatestExitCodeNaNIsFailure(t *testing.T) {
	messages := []any{map[string]any{
		"parts": []any{map[string]any{
			"type": "tool",
			"state": map[string]any{
				"input":    "go test ./...",
				"exitCode": math.NaN(),
			},
		}},
	}}
	got := LatestTestCommandPassed(messages)
	if got == nil || *got {
		t.Fatalf("NaN exit code should be a numeric failure: %v", got)
	}
}

func TestMemoStoresObjectReferences(t *testing.T) {
	memo := NewTestCommandMemo()
	value := &CachedTestResult{Code: 0, Stdout: "before", Stderr: ""}
	memo.Set("key", value)
	value.Stdout = "after"
	if got := memo.Get("key"); got == nil || got.Stdout != "after" {
		t.Fatalf("memo did not preserve object identity: %#v", got)
	}
}
