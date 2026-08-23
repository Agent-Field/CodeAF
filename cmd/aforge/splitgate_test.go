package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGateDecisions(t *testing.T) {
	files, _ := filepath.Glob("../../bench/swarm/tasks/*.txt")
	for _, f := range files {
		b, _ := os.ReadFile(f)
		t.Logf("%-30s enumerated=%d divide=%v", filepath.Base(f),
			enumeratedItems(string(b)), divisionWorthIt(string(b)))
	}
}
