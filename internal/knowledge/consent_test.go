package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

func TestCaptureConsentSharesConcurrentReadsAndOwnWrites(t *testing.T) {
	profile := t.TempDir()
	v := Open(profile)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	previous := consentNow
	consentNow = func() time.Time { return now }
	t.Cleanup(func() { consentNow = previous })
	var opens atomic.Int64
	path := filepath.Join(v.root, "knowledge.sqlite")
	t.Cleanup(SetDatabaseHookForTest(func(opened string) {
		if opened == path {
			opens.Add(1)
		}
	}))

	var callers sync.WaitGroup
	for range 20 {
		callers.Go(func() {
			// Each chat has its own Vault for the same profile database.
			chat := Open(profile)
			for range 50 {
				if enabled, since := chat.CaptureConsent(); enabled || !since.IsZero() {
					t.Errorf("fresh consent = (%v, %v); want (false, zero)", enabled, since)
				}
			}
		})
	}
	callers.Wait()
	if got := opens.Load(); got != 1 {
		t.Fatalf("1000 concurrent consent calls opened database %d times; want 1", got)
	}
	if err := Open(profile).StartBuild(); err != nil {
		t.Fatal(err)
	}
	enabled, since := v.CaptureConsent()
	if !enabled || since.IsZero() || !since.Equal(v.EnabledSince()) {
		t.Fatalf("own opt-in did not immediately refresh frozen-clock consent: (%v, %v)", enabled, since)
	}
	before := opens.Load()
	for range 50 {
		if enabled, cachedSince := Open(profile).CaptureConsent(); !enabled || !cachedSince.Equal(since) {
			t.Fatalf("shared enabled consent = (%v, %v); want (true, %v)", enabled, cachedSince, since)
		}
	}
	if got := opens.Load(); got != before {
		t.Fatalf("enabled consent reopened database %d times", got-before)
	}
}

func TestCaptureConsentRechecksExternalWritesAndKeepsFreshReaders(t *testing.T) {
	v := Open(t.TempDir())
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	previous := consentNow
	consentNow = func() time.Time { return now }
	t.Cleanup(func() { consentNow = previous })
	var opens atomic.Int64
	path := filepath.Join(v.root, "knowledge.sqlite")
	t.Cleanup(SetDatabaseHookForTest(func(opened string) {
		if opened == path {
			opens.Add(1)
		}
	}))
	if enabled, since := v.CaptureConsent(); enabled || !since.IsZero() {
		t.Fatalf("fresh consent = (%v, %v)", enabled, since)
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("initial consent opened database %d times; want 1", got)
	}
	written := now.Add(time.Second)
	uri := sqliteuri.File(path)
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO state(key,payload) VALUES('config',?)", fmt.Sprintf(`{"enabled":true,"schema":1,"created_at":%q}`, written.Format(time.RFC3339Nano)))
	closeErr := db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if !v.Enabled() || !v.EnabledSince().Equal(written) {
		t.Fatal("Enabled and EnabledSince did not read the external opt-in immediately")
	}
	before := opens.Load()
	for _, advance := range []time.Duration{0, consentRecheck - time.Nanosecond} {
		now = now.Add(advance)
		if enabled, since := v.CaptureConsent(); enabled || !since.IsZero() {
			t.Fatalf("external opt-in visible before recheck: (%v, %v)", enabled, since)
		}
	}
	if got := opens.Load(); got != before {
		t.Fatalf("unexpired consent reopened database %d times", got-before)
	}
	now = now.Add(2 * time.Nanosecond)
	enabled, since := v.CaptureConsent()
	if !enabled || !since.Equal(written) {
		t.Fatalf("external opt-in missing after recheck: (%v, %v); want (true, %v)", enabled, since, written)
	}
	if got := opens.Load(); got != before+1 {
		t.Fatalf("expired consent opened database %d times; want 1", got-before)
	}
}

func TestCaptureConsentCachesReadErrors(t *testing.T) {
	v := Open(t.TempDir())
	// A file where the directory belongs makes every database open fail.
	if err := os.WriteFile(v.root, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	previous := consentNow
	consentNow = func() time.Time { return now }
	t.Cleanup(func() { consentNow = previous })
	var opens atomic.Int64
	path := filepath.Join(v.root, "knowledge.sqlite")
	t.Cleanup(SetDatabaseHookForTest(func(opened string) {
		if opened == path {
			opens.Add(1)
		}
	}))
	for range 50 {
		if enabled, since := v.CaptureConsent(); enabled || !since.IsZero() {
			t.Fatalf("failed read returned consent: (%v, %v)", enabled, since)
		}
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("failed reads opened database %d times; want 1", got)
	}
	now = now.Add(consentRecheck + time.Nanosecond)
	if enabled, since := v.CaptureConsent(); enabled || !since.IsZero() {
		t.Fatalf("failed retry returned consent: (%v, %v)", enabled, since)
	}
	if got := opens.Load(); got != 2 {
		t.Fatalf("expired failed read opened database %d times; want 2 total", got)
	}
}

type consentEmptyHistory struct{}

func (consentEmptyHistory) Inventory(context.Context) ([]Journal, error) { return nil, nil }
func (consentEmptyHistory) Read(context.Context, Journal, JournalCursor) (JournalBatch, error) {
	return JournalBatch{}, nil
}

func TestCaptureConsentRefreshesWhenEmptyHistoryArmsCapture(t *testing.T) {
	v := Open(t.TempDir())
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	previous := consentNow
	consentNow = func() time.Time { return now }
	t.Cleanup(func() { consentNow = previous })
	if enabled, _ := v.CaptureConsent(); enabled {
		t.Fatal("fresh profile already enabled")
	}
	lastInventory := ""
	if err := v.runPass(context.Background(), consentEmptyHistory{}, func() string { return "" }, &lastInventory); err != nil {
		t.Fatal(err)
	}
	enabled, since := v.CaptureConsent()
	if !enabled || since.IsZero() || !since.Equal(v.EnabledSince()) {
		t.Fatalf("empty inventory did not immediately refresh frozen-clock consent: (%v, %v)", enabled, since)
	}
}

func TestConfirmConsentReadsNowAndSharesTheAnswer(t *testing.T) {
	v := Open(t.TempDir())
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	previous := consentNow
	consentNow = func() time.Time { return now }
	t.Cleanup(func() { consentNow = previous })
	var opens atomic.Int64
	path := filepath.Join(v.root, "knowledge.sqlite")
	t.Cleanup(SetDatabaseHookForTest(func(opened string) {
		if opened == path {
			opens.Add(1)
		}
	}))
	if enabled, _ := v.CaptureConsent(); enabled {
		t.Fatal("fresh profile already enabled")
	}
	written := now.Add(time.Second)
	uri := sqliteuri.File(path)
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO state(key,payload) VALUES('config',?)", fmt.Sprintf(`{"enabled":true,"schema":1,"created_at":%q}`, written.Format(time.RFC3339Nano)))
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	before := opens.Load()
	if enabled, since := v.ConfirmConsent(); !enabled || !since.Equal(written) {
		t.Fatalf("confirmed consent = (%v, %v); want (true, %v) without waiting for a recheck", enabled, since, written)
	}
	if enabled, since := Open(v.profile).CaptureConsent(); !enabled || !since.Equal(written) {
		t.Fatalf("shared view after confirmation = (%v, %v)", enabled, since)
	}
	if got := opens.Load() - before; got != 1 {
		t.Fatalf("confirmation and the next shared read opened the database %d times; want 1", got)
	}
}
