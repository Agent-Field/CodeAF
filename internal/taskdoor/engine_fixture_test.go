//go:build linux || darwin

package taskdoor_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// taskEngineBinary keeps a source-only CI checkout useful without replacing the real engine.
func taskEngineBinary(t *testing.T) string {
	t.Helper()
	binary, configured, err := furrowbin.ConfiguredBinary()
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		return binary
	}
	binary, err = furrowbin.Ensure()
	if errors.Is(err, furrowbin.ErrNotEmbedded) {
		t.Skip("task-copy proof needs a real engine: run make build first, or set " + furrowbin.BinaryEnvVar)
	}
	if err != nil {
		t.Fatal(err)
	}
	return binary
}
