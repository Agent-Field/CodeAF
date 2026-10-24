package chatfiles

import (
	"bytes"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

func chat(t *testing.T) cell.Cell {
	t.Helper()
	root := filepath.Join(t.TempDir(), "chat")
	if err := os.MkdirAll(filepath.Join(root, cell.StateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	return cell.Cell{ID: "c", Root: root}
}

func put(t *testing.T, c cell.Cell, rel string, body []byte, age time.Duration) {
	t.Helper()
	path := filepath.Join(c.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func inventoryOf(t *testing.T, c cell.Cell) inventory.Inventory {
	t.Helper()
	s, err := inventory.Open(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	return s.Snapshot()
}

// move is the chat arriving on another machine: only its .cell/ folder comes.
func move(t *testing.T, from cell.Cell) cell.Cell {
	t.Helper()
	to := chat(t)
	if out, err := osexec.Command("cp", "-a", from.Root+"/"+cell.StateDir+"/.", to.Root+"/"+cell.StateDir).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return to
}

func TestJobLogsAndStubsComeBackOnTheOtherMachine(t *testing.T) {
	a := chat(t)
	files := map[string]string{
		"logs/jobs/2.log":         "server listening on 8794\n",
		"logs/stubs/0123abcd.txt": "the whole tool output\n",
	}
	for rel, body := range files {
		put(t, a, rel, []byte(body), time.Minute)
	}
	put(t, a, "logs/jobs/2.log.retention", []byte("x"), time.Minute)
	put(t, a, "logs/jobs/.retention.lock", nil, time.Minute)
	if err := (Carry{}).Compose(a); err != nil {
		t.Fatal(err)
	}
	b := move(t, a)
	if n, err := (Carry{}).Restore(b); err != nil || n != len(files) {
		t.Fatalf("restored %d files (%v), want %d", n, err, len(files))
	}
	for rel, body := range files {
		got, err := os.ReadFile(filepath.Join(b.Root, filepath.FromSlash(rel)))
		if err != nil || string(got) != body {
			t.Errorf("%s on the other machine = %q, %v; want %q", rel, got, err, body)
		}
	}
	if _, err := os.Stat(filepath.Join(b.Root, "logs/jobs/.retention.lock")); err == nil {
		t.Error("the job registry's own bookkeeping travelled")
	}
	if got := inventoryOf(t, a).ChatDir; got != a.Root {
		t.Errorf("ChatDir = %q, want the folder the files were in, %q", got, a.Root)
	}
}

func TestAFileOverTheLimitStaysAndIsNamed(t *testing.T) {
	a := chat(t)
	put(t, a, "logs/jobs/1.log", bytes.Repeat([]byte("x"), MaxFileBytes+1), time.Minute)
	put(t, a, "logs/jobs/2.log", []byte("small\n"), time.Minute)
	if err := (Carry{}).Compose(a); err != nil {
		t.Fatal(err)
	}
	b := move(t, a)
	if _, err := (Carry{}).Restore(b); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.Root, "logs/jobs/1.log")); err == nil {
		t.Error("a file over the limit was carried")
	}
	left := inventoryOf(t, b).Withheld
	if len(left) != 1 || left[0].Path != "logs/jobs/1.log" || !strings.Contains(left[0].Reason, "stayed on the machine that made it") {
		t.Errorf("withheld = %+v, want logs/jobs/1.log named with why it stayed", left)
	}
}

func TestTheNewestFilesFillTheTotalAndTheOldestAreNamed(t *testing.T) {
	a := chat(t)
	each := bytes.Repeat([]byte("y"), MaxFileBytes)
	for i := 0; i < MaxTotalBytes/MaxFileBytes+2; i++ {
		put(t, a, "logs/stubs/"+string(rune('a'+i))+".txt", each, time.Duration(i+1)*time.Hour)
	}
	if err := (Carry{}).Compose(a); err != nil {
		t.Fatal(err)
	}
	left := inventoryOf(t, a).Withheld
	if len(left) != 2 || left[0].Path != "logs/stubs/"+string(rune('a'+MaxTotalBytes/MaxFileBytes))+".txt" {
		t.Fatalf("withheld = %+v, want the two oldest", left)
	}
}

func TestAFileThatLooksLikeASecretStaysAndIsNamed(t *testing.T) {
	a := chat(t)
	put(t, a, "logs/jobs/3.log", []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----\n"), time.Minute)
	put(t, a, "logs/jobs/4.log", []byte("fine\n"), time.Minute)
	if err := (Carry{}).Compose(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.Root, CarriedPath, "logs/jobs/3.log")); err == nil {
		t.Error("a file that holds a key was put in the cell")
	}
	if _, err := os.Stat(filepath.Join(a.Root, CarriedPath, "logs/jobs/4.log")); err != nil {
		t.Errorf("a clean log was not carried: %v", err)
	}
	if left := inventoryOf(t, a).Withheld; len(left) != 1 || left[0].Path != "logs/jobs/3.log" {
		t.Errorf("withheld = %+v, want logs/jobs/3.log", left)
	}
}

func TestAChatWithNothingToCarryNamesNoFolder(t *testing.T) {
	a := chat(t)
	if err := (Carry{}).Compose(a); err != nil {
		t.Fatal(err)
	}
	if got := inventoryOf(t, a).ChatDir; got != "" {
		t.Errorf("ChatDir = %q, want none", got)
	}
}

// A task's journals are truth in .cell/tasks and travel with the cell; a second
// copy from beside the chat would be a second answer to where a journal is.
func TestTaskJournalsAreNotCarriedASecondTime(t *testing.T) {
	a := chat(t)
	put(t, a, "tasks/4/trajectory.jsonl", []byte("{}\n"), time.Minute)
	put(t, a, "logs/jobs/2.log", []byte("x\n"), time.Minute)
	if err := (Carry{}).Compose(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.Root, CarriedPath, "tasks")); err == nil {
		t.Error("task journals were copied into the carried files")
	}
}
