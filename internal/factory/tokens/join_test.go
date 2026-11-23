package tokens

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func humanLink(t *testing.T, path string, max int, edit func(*Token)) Token {
	t.Helper()
	row, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	row.AccessLink, row.MaxUses, row.Created, row.Expires = true, max, t0, t0.Add(HumanLinkTTL)
	if edit != nil {
		edit(&row)
	}
	if err := Add(path, row); err != nil {
		t.Fatal(err)
	}
	return row
}

func who(handle string) Profile { return Profile{Handle: handle} }

func TestLegacyRowsMeanOnePersonAndKeepTheirExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	old := `{"tokens":[{"access_link":true,"id":"tok_a","token":"s1","expires":"2026-10-17T00:00:00Z"},
	{"access_link":true,"id":"tok_b","token":"s2","joined":"2026-10-02T00:00:00Z","profile":{"handle":"raj"}}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Limit() != 1 || rows[0].Used() != 0 || rows[0].Reason(t0) != "" || rows[0].Counted() {
		t.Fatalf("legacy open = %+v", rows[0])
	}
	if rows[0].Reason(t0.Add(24*time.Hour)) != "expired" {
		t.Fatal("old explicit expiry not honoured")
	}
	if rows[1].Used() != 1 || rows[1].Remaining() != 0 || rows[1].Reason(t0) != "used_up" || !rows[1].Has("raj") {
		t.Fatalf("legacy joined = %+v", rows[1])
	}
	// The one person comes back: nothing is consumed and nothing breaks.
	got, err := Join(path, "tok_b", who("raj"), t0)
	if err != nil || got.Consumed {
		t.Fatalf("returning legacy person = %+v %v", got, err)
	}
	if _, err := Join(path, "tok_b", who("sam"), t0); !errors.Is(err, ErrUsedUp) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("second person on single-use = %v", err)
	}
}

func TestJoinSpendsOnceAndRefusesInPrecedenceOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	row := humanLink(t, path, 2, nil)
	for i, handle := range []string{"a", "a", "b"} {
		got, err := Join(path, row.ID, who(handle), t0.Add(time.Duration(i)*time.Minute))
		if err != nil || got.Consumed != (i != 1) {
			t.Fatalf("join %s = %+v %v", handle, got, err)
		}
	}
	rows, _ := Load(path)
	if rows[0].Used() != 2 || rows[0].Remaining() != 0 || rows[0].Profile.Handle != "a" {
		t.Fatalf("row = %+v", rows[0])
	}
	if _, err := Join(path, row.ID, who("c"), t0); !errors.Is(err, ErrUsedUp) {
		t.Fatalf("third = %v", err)
	}
	// Someone counted is turned away by revoke and expiry, not by the limit.
	if _, err := Join(path, row.ID, who("a"), t0.Add(8*24*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired member = %v", err)
	}
	if err := RemoveAt(path, row.ID, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := Join(path, row.ID, who("c"), t0); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked = %v", err)
	}
	rows, _ = Load(path)
	if rows[0].Token != "" || rows[0].RevokedAt.IsZero() || rows[0].Reason(t0) != "revoked" || rows[0].EndedAt(t0) != t0.Add(time.Hour) {
		t.Fatalf("tombstone = %+v", rows[0])
	}
}

func TestLinkForOneLoginRefusesAnyoneElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	row := humanLink(t, path, 1, func(r *Token) { r.InvitedLogin = "mona" })
	if _, err := Join(path, row.ID, who("raj"), t0); !errors.Is(err, ErrWrongPerson) {
		t.Fatalf("wrong person = %v", err)
	}
	if got, err := Join(path, row.ID, who("mona"), t0); err != nil || !got.Consumed {
		t.Fatalf("right person = %+v %v", got, err)
	}
}

func TestUnjoinGivesBackOnlyTheUseJustTaken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	row := humanLink(t, path, 2, nil)
	for _, h := range []string{"a", "b"} {
		if _, err := Join(path, row.ID, who(h), t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := Unjoin(path, row.ID, "a"); err != nil {
		t.Fatal(err)
	}
	rows, _ := Load(path)
	if rows[0].Used() != 1 || rows[0].Has("a") || !rows[0].Has("b") || rows[0].Profile.Handle != "b" {
		t.Fatalf("after unjoin = %+v", rows[0])
	}
	if got, err := Join(path, row.ID, who("c"), t0); err != nil || !got.Consumed {
		t.Fatalf("freed place = %+v %v", got, err)
	}
}

func TestConcurrentPeopleCannotExceedTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	row := humanLink(t, path, 5, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := Join(path, row.ID, who(fmt.Sprintf("p%d", i)), t0)
			if err != nil && !errors.Is(err, ErrUsedUp) {
				t.Error(err)
			}
			if got.Consumed {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	rows, _ := Load(path)
	if won != 5 || rows[0].Used() != 5 {
		t.Fatalf("won %d, stored %d", won, rows[0].Used())
	}
}

// The helper process joins as the handle in the environment and prints the outcome.
func TestJoinHelperProcess(t *testing.T) {
	path, id, handle := os.Getenv("TOKENS_JOIN_PATH"), os.Getenv("TOKENS_JOIN_ID"), os.Getenv("TOKENS_JOIN_AS")
	if path == "" {
		t.Skip("helper for TestConcurrentProcessesCannotExceedTheLimit")
	}
	got, err := Join(path, id, who(handle), t0)
	switch {
	case err == nil && got.Consumed:
		fmt.Println("RESULT consumed")
	case errors.Is(err, ErrUsedUp):
		fmt.Println("RESULT used_up")
	default:
		fmt.Println("RESULT error", err)
	}
}

func TestConcurrentProcessesCannotExceedTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	row := humanLink(t, path, 3, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestJoinHelperProcess$", "-test.count=1")
			cmd.Env = append(os.Environ(), "TOKENS_JOIN_PATH="+path, "TOKENS_JOIN_ID="+row.ID, fmt.Sprintf("TOKENS_JOIN_AS=p%d", i))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("helper: %v %s", err, out)
				return
			}
			for _, line := range strings.Split(string(out), "\n") {
				if rest, ok := strings.CutPrefix(line, "RESULT "); ok {
					mu.Lock()
					counts[rest]++
					mu.Unlock()
				}
			}
		}(i)
	}
	wg.Wait()
	rows, _ := Load(path)
	if counts["consumed"] != 3 || counts["used_up"] != 5 || rows[0].Used() != 3 {
		t.Fatalf("counts = %v, stored %d", counts, rows[0].Used())
	}
}

func TestPlainTokensAndOperatorClaimsAreNotHumanLinks(t *testing.T) {
	plain, _ := Mint()
	if plain.Human() || plain.Counted() {
		t.Fatal("plain token read as a human link")
	}
	op, _ := Mint()
	op.Operator, op.AccessLink = true, true
	if op.Human() {
		t.Fatal("operator claim read as a human link")
	}
	// A plain token keeps its first-handle-sticks binding.
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := Add(path, plain); err != nil {
		t.Fatal(err)
	}
	if err := Bind(path, plain.ID, who("a"), t0); err != nil {
		t.Fatal(err)
	}
	if err := Bind(path, plain.ID, who("b"), t0); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("second handle on a plain token = %v", err)
	}
}
