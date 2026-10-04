package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/home"
)

// reportCell makes a cell on a fresh home and records the given flushes for it.
func reportCell(t *testing.T, flushes ...cellsync.Flush) cell.Cell {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.Create(home.Dir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	r := cellstats.NewRecorder(home.Dir(), c.ID, nil)
	for _, f := range flushes {
		r.OnFlush(f)
	}
	return c
}

func TestCellReport(t *testing.T) {
	c := reportCell(t,
		cellsync.Flush{Head: "aaaaaaaaaaaaaaaa", Turns: 2, Frames: 1, Objects: 14},
		cellsync.Flush{Head: "bbbbbbbbbbbbbbbb", Turns: 1, Frames: 1, Objects: 3})
	var out bytes.Buffer
	if err := runCellIn([]string{"report", c.ID}, &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"aaaaaaaaaa ", "bbbbbbbbbb ", "total", " 17 "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

func TestCellReportShowsTheVaultRow(t *testing.T) {
	c := reportCell(t, cellsync.Flush{Head: "aaaaaaaaaaaaaaaa", Turns: 1})
	cellstats.NewRecorder(home.Dir(), cellstats.VaultScope, cellstats.MeterFunc(func() cellstats.Counts {
		return cellstats.Counts{Puts: 3, BytesUp: 4242}
	})).Settle()
	var out bytes.Buffer
	if err := runCellIn([]string{"report", c.ID}, &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "vault") || !strings.Contains(out.String(), "4242") {
		t.Fatalf("report lacks the vault row:\n%s", out.String())
	}
}

func TestCellReportPrintsNothingWithoutRows(t *testing.T) {
	c := reportCell(t)
	file := filepath.Join(t.TempDir(), "out.jsonl")
	var out bytes.Buffer
	if err := runCellIn([]string{"report", "--export", file, c.ID}, &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("an empty report printed %q", out.String())
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("an export of nothing wrote a file")
	}
}

func TestCellReportExportOnlyOnRequest(t *testing.T) {
	c := reportCell(t, cellsync.Flush{Head: "aaaa", Turns: 1, Objects: 2})
	file := filepath.Join(t.TempDir(), "out.jsonl")

	var plain bytes.Buffer
	if err := runCellIn([]string{"report", c.ID}, &plain, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), cellstats.ExportNotice) {
		t.Fatal("a plain report talked about exporting")
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("a plain report wrote an export")
	}

	var out bytes.Buffer
	if err := runCellIn([]string{"report", c.ID, "--export", file}, &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	stats, _ := os.ReadFile(cellstats.Path(home.Dir(), c.ID))
	if !bytes.Equal(got, stats) {
		t.Fatalf("export = %q, want the stats lines %q", got, stats)
	}
	if !strings.Contains(out.String(), cellstats.ExportNotice) {
		t.Fatalf("export did not say what it holds:\n%s", out.String())
	}
}

func TestCellReportExportNeedsAFile(t *testing.T) {
	c := reportCell(t)
	if err := runCellIn([]string{"report", c.ID, "--export"}, &bytes.Buffer{}, t.TempDir()); err == nil {
		t.Fatal("--export with no file was accepted")
	}
}
