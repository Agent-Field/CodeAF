package session

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func writeIndexRows(t *testing.T, path string, from, to int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for i := from; i < to; i++ {
		row := fmt.Sprintf(`{"id":"%d","sessionId":"s%d","name":"task-%d","label":"task %d","title":"task number %d, groomed","status":"done","endedAt":%q}`+"\n",
			i, i%40, i, i, i, time.Unix(1_700_000_000+int64(i), 0).UTC().Format(time.RFC3339))
		if _, err := file.WriteString(row); err != nil {
			t.Fatal(err)
		}
	}
}

// THE PROJECT'S INDEX IS SHARED BY EVERY CONVERSATION IN IT, so listing the
// tasks of a project's hundred conversations read and decoded the same file a
// hundred times. An index nobody has appended to since it was read is answered
// from that reading; a row appended after it is read.
func TestAnUnchangedTaskIndexIsDecodedOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), taskIndexName)
	writeIndexRows(t, path, 0, 1500)
	want := ReadTaskIndex(path)
	if len(want) != 1500 {
		t.Fatalf("read %d rows, want 1500", len(want))
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 40 {
		// Whoever gets a reading may reorder it; the next must not see that.
		got := ReadTaskIndex(path)
		got[0], got[len(got)-1] = got[len(got)-1], got[0]
	}
	runtime.ReadMemStats(&after)
	const budget = 64 << 20
	if spent := after.TotalAlloc - before.TotalAlloc; spent > budget {
		t.Fatalf("40 readings of an unchanged index allocated %d bytes, want under %d", spent, budget)
	}
	if got := ReadTaskIndex(path); !reflect.DeepEqual(got, want) {
		t.Fatal("a reading changed what the next one saw")
	}

	writeIndexRows(t, path, 1500, 1501)
	if got := ReadTaskIndex(path); len(got) != 1501 {
		t.Fatalf("after an append the index has %d rows, want 1501", len(got))
	}
}
