package tokens

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOperatorLinkLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	first, err := RotateOperator(path, "owner", now, false)
	if err != nil || !first.Operator || first.Expires.Sub(first.Created) != 24*time.Hour {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := RotateOperator(path, "owner", now, false)
	if err != nil || second.Token == first.Token {
		t.Fatalf("restart: %+v %v", second, err)
	}
	if err := Bind(path, first.ID, Profile{Handle: "owner"}, now); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("revoked claim: %v", err)
	}
	if err := Bind(path, second.ID, Profile{Handle: "other"}, now); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("wrong login: %v", err)
	}
	if err := Bind(path, second.ID, Profile{Handle: "owner"}, second.Expires); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expiry: %v", err)
	}
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); out <- Bind(path, second.ID, Profile{Handle: "owner"}, now) }()
	}
	wg.Wait()
	close(out)
	claimed := 0
	for err := range out {
		if err == nil {
			claimed++
		} else if !errors.Is(err, os.ErrPermission) {
			t.Fatal(err)
		}
	}
	if claimed != 1 {
		t.Fatalf("successful claims: %d", claimed)
	}
	claim, err := LoadOperator(path)
	if err != nil || claim.Login != "owner" || !claim.ClaimedAt.Equal(now) {
		t.Fatalf("persisted claim: %+v %v", claim, err)
	}
	if row, err := RotateOperator(path, "other", now, false); err != nil || row.Token != "" {
		t.Fatalf("claimed startup minted a link: %+v %v", row, err)
	}
	recovery, err := RotateOperator(path, "other", now, true)
	if err != nil || recovery.InvitedLogin != "owner" {
		t.Fatalf("recovery: %+v %v", recovery, err)
	}
	for _, file := range []string{path, filepath.Join(filepath.Dir(path), "operator.json")} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private state: %s %v %v", file, info, err)
		}
	}
}

// A minted operator link is named once: a later invite never renames it.
func TestInviteOperatorNamesAnUnnamedLinkOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	row, err := RotateOperator(path, "", time.Now(), false)
	if err != nil || row.InvitedLogin != "" {
		t.Fatalf("mint = %+v, %v", row, err)
	}
	if err := InviteOperator(path, row.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := InviteOperator(path, row.ID, "intruder"); err != nil {
		t.Fatal(err)
	}
	rows, err := Load(path)
	if err != nil || len(rows) != 1 || rows[0].InvitedLogin != "owner" {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}
