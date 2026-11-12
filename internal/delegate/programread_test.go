package delegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedProgramRecordPreservesFactsAndRejectsOverBudgetMetadata(t *testing.T) {
	dir := t.TempDir()
	want := ProgramRecord{Name: "review", Launch: "0123456789abcdef0123456789abcdef"}
	if err := WriteProgram(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadProgramWithin(dir, 1024)
	if !ok || got.Name != want.Name || got.Launch != want.Launch {
		t.Fatalf("known record: %+v, %v", got, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, ProgramFile), []byte(`{"name":"`+strings.Repeat("x", 2048)+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadProgramWithin(dir, 1024); ok {
		t.Fatal("oversize metadata was treated as a program")
	}
}

// A valid producer record can exceed the read budget after JSON escapes labels.
// The limit stays a real bound: a known writer is not permission to read it all.
func TestEscapedProducedProgramCanBeUnavailableWithoutInventingFacts(t *testing.T) {
	dir := t.TempDir()
	want := ProgramRecord{Name: "review", Launch: "0123456789abcdef0123456789abcdef", Stages: []string{strings.Repeat("<", maxLineBytes/4)}}
	if err := WriteProgram(dir, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, ProgramFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= ProgramRecordCap {
		t.Fatalf("fixture did not exceed metadata read bound: %d", info.Size())
	}
	got, ok := ReadProgram(dir)
	if ok || got.Name != "" || got.Launch != "" {
		t.Fatalf("over-budget produced metadata became authoritative: %+v, %v", got, ok)
	}
}
