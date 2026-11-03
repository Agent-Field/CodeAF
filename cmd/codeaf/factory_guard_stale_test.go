package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory"
)

func staleGuardFixture(t *testing.T) (string, string, func(string)) {
 t.Helper()
 root, file, run := repairCheckFixture(t)
 bin := t.TempDir()
 if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\necho PASS\nexit \"${CHECK_EXIT:-0}\"\n"), 0700); err != nil { t.Fatal(err) }
 return root, file, func(command string) { run("export PATH="+repairShellQuote(bin)+":\"$PATH\"; "+command) }
}

func TestGuardStaleWiderPass(t *testing.T) {
 root, file, run := staleGuardFixture(t)
 run("CHECK_EXIT=7 go test -run TestA ./pkg")
 run("go test ./...")
 door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file:file, dir:root, requireFresh:true, requireAnyCheck:true}
 t.Cleanup(door.closeVerification)
 if err := door.Report(t.Context(), factory.StageResult{Done:true}); err != nil { t.Fatal(err) }
}
