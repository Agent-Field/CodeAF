package cellstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIdleRelease_R8_RepeatedPathRetainsItsFirstFingerprint(t *testing.T) {
	dir := t.TempDir()
	s := &SavedState{files: map[string]os.FileInfo{}, members: map[string]string{}}
	if err := s.remember(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new"), []byte("new state"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(100, 0)
	if err := os.Chtimes(dir, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := s.remember(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if s.Unchanged(t.Context()) {
		t.Fatal("a repeated ancestor replaced the first fingerprint and hid an edit")
	}
}

func TestIdleRelease_R8_WideWorkspaceAndReceiptsAllocateLinearly(t *testing.T) {
	for _, n := range []int{100, 500, 5000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			e, c := realEngine(t), newCell(t)
			for _, dir := range []string{filepath.Join(c.Root, "src"), filepath.Join(c.Root, ReceiptsDir)} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < n; i++ {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%06d", i)), []byte("plain content"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
				t.Fatal(err)
			}
			if err := Await(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			proof, err := e.SavedState(t.Context(), c)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if proof == nil || len(proof.Changes) != 0 {
				t.Fatalf("proof=%+v", proof)
			}
			// Each directory entry and candidate is visited once, including
			// the receipt history. Shared ancestors must not multiply work.
			if len(proof.files) > 2*n+100 || proof.entries > 2*n+100 {
				t.Fatalf("nonlinear proof work: paths=%d entries=%d", len(proof.files), proof.entries)
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			limit := uint64(6<<20) + uint64(n)*32_000
			t.Logf("files=%d receipts=%d allocated_bytes=%d limit=%d", n, n, allocated, limit)
			if allocated > limit {
				t.Fatalf("certification allocates %d bytes; linear allowance is %d", allocated, limit)
			}
		})
	}
}

func TestIdleRelease_R8_ProofBudgetsRefuseIncompleteCertification(t *testing.T) {
	t.Run("unique paths", func(t *testing.T) {
		s := &SavedState{files: map[string]os.FileInfo{}, members: map[string]string{}}
		for i := 0; i < savedStatePaths; i++ {
			s.files[fmt.Sprint(i)] = nil
		}
		if err := s.remember(t.Context(), filepath.Join(t.TempDir(), "absent")); !errors.Is(err, ErrSavedStateTooLarge) {
			t.Fatalf("budget error=%v", err)
		}
	})
	t.Run("directory entries", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "entry"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		s := &SavedState{files: map[string]os.FileInfo{}, members: map[string]string{}, entries: savedStateEntries}
		if err := s.remember(t.Context(), dir); !errors.Is(err, ErrSavedStateTooLarge) {
			t.Fatalf("budget error=%v", err)
		}
	})
	t.Run("chat bytes", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "journal")
		if err := os.WriteFile(file, []byte("whole journal"), 0600); err != nil {
			t.Fatal(err)
		}
		remaining := int64(1)
		if _, err := savedFile(t.Context(), file, &remaining); !errors.Is(err, ErrSavedStateTooLarge) {
			t.Fatalf("budget error=%v", err)
		}
	})
	t.Run("native reply", func(t *testing.T) {
		b := &savedStateReply{}
		if _, err := b.Write(bytes.Repeat([]byte("x"), savedStateReplyBytes+1)); !errors.Is(err, ErrSavedStateTooLarge) || b.buffer.Len() != 0 {
			t.Fatalf("oversized reply retained: bytes=%d err=%v", b.buffer.Len(), err)
		}
		run := savedStateRunner(func(context.Context, string, []string, ...string) ([]byte, error) {
			return bytes.Repeat([]byte("x"), savedStateReplyBytes+1), nil
		})
		if _, err := run(t.Context(), "", nil, "unused"); !errors.Is(err, ErrSavedStateTooLarge) {
			t.Fatalf("runner budget error=%v", err)
		}
	})
}

func TestIdleRelease_R8_NativeReplyStreamCannotBypassBudget(t *testing.T) {
	reply := &savedStateReply{}
	// os/exec copies a pipe into this writer with io.Copy. Exercise that door,
	// including io.ReaderFrom optimization, rather than only direct Write calls.
	stream := io.LimitReader(bytes.NewReader(bytes.Repeat([]byte("x"), savedStateReplyBytes+1)), savedStateReplyBytes+1)
	_, err := io.Copy(reply, stream)
	if !errors.Is(err, ErrSavedStateTooLarge) || reply.buffer.Len() > savedStateReplyBytes {
		t.Fatalf("stream bytes=%d error=%v", reply.buffer.Len(), err)
	}
}
