//go:build unix

package delegate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLaunchReadsRefuseSpecialFilesInsteadOfReportingAnEmptyLaunch(t *testing.T) {
	const launch = "0123456789abcdef0123456789abcdef"
	dir := t.TempDir()
	for _, name := range []string{ActionsFile, ConversationFile, ProgramFile} {
		path := filepath.Join(dir, name)
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		// Keeps a regression's ordinary open from blocking: reading the zero-size
		// action/call section would incorrectly report a known empty launch.
		fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(fd)
	}
	page := ReadLaunchActions(context.Background(), dir, launch, func(a Action) (Shown, bool) { return Shown{}, false }, -1, 60, ReadBudget{Bytes: 1024, Lines: 100})
	if page.Complete || page.Cut != ReadCutUnreadable {
		t.Fatalf("special action log: %+v", page)
	}
	if calls, known := CountLaunchCalls(context.Background(), dir, launch, ReadBudget{Bytes: 1024, Lines: 100}); known {
		t.Fatalf("special call log claimed %d calls", calls)
	}
	if _, known := ReadProgramWithin(dir, 1024); known {
		t.Fatal("special program record became authoritative")
	}
	// EVERY reader of the record is the bounded one: the page's, the row's, the
	// clock's.
	if _, known := ReadProgram(dir); known {
		t.Fatal("ReadProgram read a special program record")
	}
	// A symlink to a special file is equally unsafe.
	if err := os.Remove(filepath.Join(dir, ProgramFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, ActionsFile), filepath.Join(dir, ProgramFile)); err != nil {
		t.Fatal(err)
	}
	if _, known := ReadProgramWithin(dir, 1024); known {
		t.Fatal("linked special program record became authoritative")
	}
}

// A RECORD LARGER THAN ANY codeaf WRITES IS NO RECORD, for every reader.
func TestAnOversizedProgramRecordIsNoRecord(t *testing.T) {
	dir := t.TempDir()
	big := []byte(`{"name":"senior-dev","effort":"` + strings.Repeat("x", ProgramRecordCap) + `"}`)
	if err := os.WriteFile(filepath.Join(dir, ProgramFile), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, known := ReadProgram(dir); known {
		t.Fatal("an oversized record was read")
	}
	if err := WriteProgram(dir, ProgramRecord{Name: "senior-dev"}); err != nil {
		t.Fatal(err)
	}
	if record, known := ReadProgram(dir); !known || record.Name != "senior-dev" {
		t.Fatalf("an ordinary record = %+v %v", record, known)
	}
}
