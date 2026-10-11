package tokens

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileIsNoRows(t *testing.T) {
	rows, err := Load(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil || rows != nil {
		t.Fatalf("missing file = %v, %v", rows, err)
	}
}

func TestAddRemoveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "factory", "tokens.json")
	row, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	row.InvitedBy = "sam"
	row.Created = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	row.Expires = row.Created.Add(7 * 24 * time.Hour)
	if err := Add(path, row); err != nil {
		t.Fatal(err)
	}
	if err := Add(path, row); err != ErrExists {
		t.Fatalf("duplicate id = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, %v", info, err)
	}
	got, err := Load(path)
	if err != nil || len(got) != 1 || got[0].ID != row.ID || got[0].Token != row.Token || got[0].InvitedBy != "sam" || !got[0].Expires.Equal(row.Expires) {
		t.Fatalf("loaded = %+v, %v", got, err)
	}
	if err := Remove(path, row.ID); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path, row.ID); err != ErrNotFound {
		t.Fatalf("second remove = %v", err)
	}
	got, err = Load(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("after remove = %+v, %v", got, err)
	}
}

func TestWhitespaceTokenIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := Add(path, Token{ID: "tok_ab", Token: "two words"}); err == nil {
		t.Fatal("a token with whitespace was stored")
	}
}
