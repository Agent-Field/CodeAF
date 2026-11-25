package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// P1-4 (persistence half): what is saved of a chat's model access comes back
// as it was written, a file never written is an empty table, a damaged file is
// an error that does not quote it, and the file is private to the server's user.
func TestHostedBearersRoundTripAndNeverHoldsABearer(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := st.HostedBearers(); err != nil || len(rows) != 0 {
		t.Fatalf("a table never written = %v, %v; want empty", rows, err)
	}
	bearer := "cmt_secret-text-that-must-never-reach-the-file"
	hash := strings.Repeat("ab", 32)
	end := time.Date(2026, 12, 1, 9, 30, 0, 0, time.UTC)
	want := map[string]HostedBearer{hash: {Session: "hs_" + strings.Repeat("1", 24), Person: "santosh", Expires: end}}
	if err := st.SaveHostedBearers(want); err != nil {
		t.Fatal(err)
	}
	got, err := st.HostedBearers()
	if err != nil || len(got) != 1 || got[hash].Session != want[hash].Session || got[hash].Person != "santosh" || !got[hash].Expires.Equal(end) {
		t.Fatalf("read back %+v, %v", got, err)
	}
	path := filepath.Join(st.Root(), hostedBearersFile)
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), bearer) || !strings.Contains(string(raw), hash) {
		t.Fatalf("the file does not hold exactly the hash: %q, %v", raw, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the file's mode = %v, %v; want 0600", info, err)
	}
	// Replacing the table drops a row that is no longer in it.
	if err := st.SaveHostedBearers(nil); err != nil {
		t.Fatal(err)
	}
	if rows, err := st.HostedBearers(); err != nil || len(rows) != 0 {
		t.Fatalf("after an empty save = %v, %v", rows, err)
	}
}

func TestHostedBearersDamagedFileIsAnErrorThatQuotesNothing(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret := "cmt_this-text-is-in-the-damaged-file"
	if err := os.WriteFile(filepath.Join(st.Root(), hostedBearersFile), []byte(`{"rows": [`+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := st.HostedBearers()
	if err == nil || rows != nil {
		t.Fatalf("a damaged file read as %v, %v", rows, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error quotes the file: %q", err)
	}
}
