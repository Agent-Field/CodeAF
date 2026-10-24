package cellstore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

type xosManifest struct{ ID, Head string }

var xosFiles = map[string][]byte{
	"nested/binary.dat": {0, 1, 2, 255, 13, 10, 0},
	"run.sh":            []byte("#!/bin/bash\nprintf 'same bytes\\n'\n"),
	"lines.txt":         []byte("LF\nCRLF\r\nno final newline"),
	cell.TranscriptPath: []byte("{\"role\":\"user\",\"content\":\"continue here\"}\n"),
}

func xosDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("CODEAF_XOS_DIR")
	if dir == "" {
		t.Skip("K4 needs CODEAF_XOS_DIR shared with the native Windows runner")
	}
	return dir
}

func xosEngine(t *testing.T, c cell.Cell) (Engine, SyncEngine) {
	t.Helper()
	bin, configured, err := furrowbin.ConfiguredBinary()
	if err != nil || !configured {
		t.Fatalf("K4 needs an explicit real engine: %v", err)
	}
	e := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: Spawn{Binary: bin}}
	return e, e.Sync(SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)}, LedgerName("http://local.invalid", "k4"))
}

func xosExport(t *testing.T, e SyncEngine, c cell.Cell, head, dir string) {
	t.Helper()
	out, err := e.Export(context.Background(), c, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Frames) == 0 {
		t.Fatal("K4 exported no frames")
	}
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, frame := range out.Frames {
		data, err := os.ReadFile(frame.Path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "frames", filepath.Base(frame.Path)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(xosManifest{c.ID, head})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("K4 exported %d frames, %d bytes, head %s", len(out.Frames), out.Bytes, head)
}

func xosImport(t *testing.T, dir string) (Engine, SyncEngine, cell.Cell) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m xosManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	c := cell.Cell{ID: m.ID, Root: t.TempDir()}
	e, s := xosEngine(t, c)
	// Import consumes its inbox; the shared fixture remains available for reruns.
	inbox := t.TempDir()
	// A take-back exports only changes: its ledger already holds the original
	// objects. Reconstruct that Linux base before importing the Windows delta.
	sources := []string{dir}
	if filepath.Base(dir) == "return" {
		sources = append([]string{filepath.Dir(dir)}, sources...)
	}
	for _, source := range sources {
		entries, err := os.ReadDir(filepath.Join(source, "frames"))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(source, "frames", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			_, objects, err := blobstore.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, object := range objects {
				if err := os.WriteFile(filepath.Join(inbox, object.RID), object.Bytes, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if _, err := s.ImportPrimed(context.Background(), c, m.Head, inbox); err != nil {
		t.Fatal(err)
	}
	if err := s.Materialize(context.Background(), c, m.Head); err != nil {
		t.Fatal(err)
	}
	opened, err := cell.OpenAt(c.Root, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Meta().Class != cell.HostBound {
		t.Fatal("K4 changed the chat's command access")
	}
	return e, s, opened
}

func xosCheck(t *testing.T, c cell.Cell, changed bool) {
	t.Helper()
	for name, want := range xosFiles {
		if changed && name == "lines.txt" {
			want = append(append([]byte(nil), want...), []byte("\r\nWindows edit\n")...)
		}
		got, err := os.ReadFile(filepath.Join(c.Root, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("K4 %s changed bytes: got %q want %q (%v)", name, got, want, err)
		}
	}
}

// TestK4LinuxExport proves K4's Linux half through the real Go seal/export
// doors, including executable modes, binary bytes, transcript and refused names.
func TestK4LinuxExport(t *testing.T) {
	dir := xosDir(t)
	if runtime.GOOS != "linux" {
		t.Skip("K4 source half runs on Linux")
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range xosFiles {
		path := filepath.Join(c.Root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0644)
		if name == "run.sh" {
			mode = 0755
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"aux.c", "con", "a:b", "trailing.", "trailing ", "README", "readme"} {
		if err := os.WriteFile(filepath.Join(c.Root, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	e, s := xosEngine(t, c)
	seal, err := e.Seal(context.Background(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	xosExport(t, s, c, seal.Turn.ID, dir)
	_, _, copy := xosImport(t, dir)
	xosCheck(t, copy, false)
}

// TestK4WindowsContinue proves K4 natively: import/materialize preserves every
// portable byte, names that NTFS refuses are explained, and an edit exports back.
func TestK4WindowsContinue(t *testing.T) {
	dir := xosDir(t)
	if runtime.GOOS != "windows" {
		t.Skip("K4 middle half runs natively on Windows")
	}
	e, s, c := xosImport(t, dir)
	xosCheck(t, c, false)
	record, err := inventory.Open(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]string{}
	for _, item := range record.Snapshot().Withheld {
		if strings.Contains(item.Path, "\\") {
			t.Fatalf("K3 persisted a native path: %s", item.Path)
		}
		held[item.Path] = item.Reason
	}
	for _, name := range []string{"aux.c", "con", "a:b", "trailing.", "trailing "} {
		if held[name] == "" {
			t.Errorf("K4 missing not brought along reason for %q: %+v", name, held)
		}
	}
	if held["README"] == "" && held["readme"] == "" {
		t.Error("K4 missing case-collision reason")
	}
	if err := os.WriteFile(filepath.Join(c.Root, "lines.txt"), append(append([]byte(nil), xosFiles["lines.txt"]...), []byte("\r\nWindows edit\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	seal, err := e.Seal(context.Background(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	xosExport(t, s, c, seal.Turn.ID, filepath.Join(dir, "return"))
}

// TestK4LinuxReturn proves K4's return half: Windows edits preserve the other
// bytes and the executable mode recorded before the first move.
func TestK4LinuxReturn(t *testing.T) {
	dir := xosDir(t)
	if runtime.GOOS != "linux" {
		t.Skip("K4 final half runs on Linux")
	}
	_, _, c := xosImport(t, filepath.Join(dir, "return"))
	xosCheck(t, c, true)
	info, err := os.Stat(filepath.Join(c.Root, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("K4 recorded executable mode became %o", info.Mode().Perm())
	}
}
