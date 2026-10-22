package main

import (
	"bytes"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"strings"
	"testing"
)

func TestCellGCOnAHomeWithNoCellsReportsAnEmptyBudget(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_HOME", t.TempDir())
	var out bytes.Buffer
	if err := runCellIn([]string{"gc", "--dry-run"}, &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cells hold 0.0 MiB") {
		t.Fatalf("report: %q", out.String())
	}
}

func TestCellNamesItsVerbsWhenAskedForANonsense(t *testing.T) {
	celltest.Available(t)
	if err := runCell([]string{"nope"}); err == nil || !strings.Contains(err.Error(), "gc") {
		t.Fatalf("want a usage line naming gc, got %v", err)
	}
}
