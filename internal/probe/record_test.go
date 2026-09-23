package probe

import (
	"os"
	"testing"
)

func TestRecorderStepContinuityAndPerms(t *testing.T) {
	// OpenRecording is called per CLI invocation; steps must continue across
	// separate opens on the same session's evidence file.
	m := &Manager{Root: t.TempDir()}
	r, err := OpenRecording(m, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := r.Append(Record{Verb: "observe", Revision: 1}); err != nil {
		t.Fatalf("append: %v", err)
	}
	r.Close()
	r2, err := OpenRecording(m, "s1")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := r2.Append(Record{Verb: "act", Revision: 2}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	r2.Close()
	recs, err := ReadRecords(m, "s1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(recs) != 2 || recs[0].Step != 1 || recs[1].Step != 2 {
		t.Fatalf("steps must continue across opens: %+v", recs)
	}
	if recs[0].TS == "" || recs[1].TS == "" {
		t.Error("every record carries a timestamp")
	}
	st, err := os.Stat(RecordingPath(m, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("recording file must be private, got %v", st.Mode())
	}
	// A session id with path separators cannot escape the recordings dir.
	if _, err := OpenRecording(m, "../escape"); err == nil {
		_ = err
	}
	if _, err := os.Stat(m.Root + "/escape.jsonl"); !os.IsNotExist(err) {
		t.Errorf("recording escaped the recordings dir")
	}
	if _, err := os.Stat(m.Root + "/recordings/escape.jsonl"); err != nil {
		t.Errorf("sanitized id should land inside recordings dir: %v", err)
	}
}
