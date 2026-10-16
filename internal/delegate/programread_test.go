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
