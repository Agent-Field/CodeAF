//go:build linux || darwin

package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func rangeFixture(t *testing.T) (MediaRow, string, string, []byte) {
	t.Helper()
	root, dest := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "standup call.wav")
	data := rangeWAV(2000)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fact := GeneratedMedia(path, data, "")
	fact.ID = NewMediaID()
	if fact.DurationMS != 2000 {
		t.Fatalf("the fixture's recorded length is %d ms", fact.DurationMS)
	}
	return MediaRow{Fact: fact, Kind: "audio", Recorded: true}, root, dest, data
}

func leftOver(t *testing.T, dest string) {
	t.Helper()
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("a refused or removed snapshot left %d entries", len(entries))
	}
}

func TestMediaRangeSnapshotIsTheExactRecordingNamedForItsSpan(t *testing.T) {
	row, root, dest, data := rangeFixture(t)
	got, err := SnapshotMediaRange(context.Background(), row, []string{root}, row.Version(), 250, 2000, dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got.Path) != "standup_call.range-250-2000.wav" || !strings.HasPrefix(got.Path, dest+string(os.PathSeparator)) {
		t.Fatalf("the snapshot is %s", got.Path)
	}
	copied, err := os.ReadFile(got.Path)
	if err != nil || string(copied) != string(data) || got.SHA256 != row.Fact.SHA256 {
		t.Fatalf("the snapshot is not the exact recording: %v", err)
	}
	if err := os.WriteFile(row.Fact.Canonical, []byte("edited after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(got.Path); string(again) != string(data) {
		t.Fatal("editing the original altered the snapshot")
	}
	if err := got.Remove(); err != nil {
		t.Fatal(err)
	}
	leftOver(t, dest)
	if original, _ := os.ReadFile(row.Fact.Canonical); string(original) != "edited after" {
		t.Fatal("removing the snapshot touched the original")
	}
}

func TestMediaRangeRefusesEveryWayThatIsNotTheRecordedRecording(t *testing.T) {
	cases := []struct {
		name string
		want error
		edit func(t *testing.T, row *MediaRow, roots *[]string, version *string, from, to *int64, dest string)
	}{
		{"negative start", ErrMediaRange, func(_ *testing.T, _ *MediaRow, _ *[]string, _ *string, from, _ *int64, _ string) { *from = -1 }},
		{"empty span", ErrMediaRange, func(_ *testing.T, _ *MediaRow, _ *[]string, _ *string, from, to *int64, _ string) {
			*from, *to = 500, 500
		}},
		{"backwards span", ErrMediaRange, func(_ *testing.T, _ *MediaRow, _ *[]string, _ *string, from, to *int64, _ string) {
			*from, *to = 900, 100
		}},
		{"absurd end", ErrMediaRange, func(_ *testing.T, _ *MediaRow, _ *[]string, _ *string, _, to *int64, _ string) {
			*to = MaxMediaRangeMS + 1
		}},
		{"past the recorded length", ErrMediaRange, func(_ *testing.T, _ *MediaRow, _ *[]string, _ *string, _, to *int64, _ string) { *to = 2001 }},
		{"unknown length", ErrMediaRange, func(_ *testing.T, row *MediaRow, _ *[]string, _ *string, _, _ *int64, _ string) {
			row.Fact.DurationMS = 0
		}},
		{"an image", ErrMediaReference, func(_ *testing.T, row *MediaRow, _ *[]string, _ *string, _, _ *int64, _ string) { row.Kind = "image" }},
		{"a legacy row", ErrMediaReference, func(_ *testing.T, row *MediaRow, _ *[]string, _ *string, _, _ *int64, _ string) { row.Recorded = false }},
		{"bytes of another kind", ErrMediaReference, func(_ *testing.T, row *MediaRow, _ *[]string, _ *string, _, _ *int64, _ string) { row.Kind = "video" }},
		{"another version", ErrMediaReference, func(_ *testing.T, _ *MediaRow, _ *[]string, version *string, _, _ *int64, _ string) {
			*version = "sha256:old"
		}},
		{"no version", ErrMediaReference, func(_ *testing.T, _ *MediaRow, _ *[]string, version *string, _, _ *int64, _ string) { *version = "" }},
		{"outside the session", ErrMediaReference, func(_ *testing.T, _ *MediaRow, roots *[]string, _ *string, _, _ *int64, dest string) {
			*roots = []string{dest}
		}},
		{"changed", ErrMediaReference, func(t *testing.T, row *MediaRow, _ *[]string, _ *string, _, _ *int64, _ string) {
			if err := os.WriteFile(row.Fact.Canonical, rangeWAV(2000)[:900], 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"deleted", ErrMediaReference, func(t *testing.T, row *MediaRow, _ *[]string, _ *string, _, _ *int64, _ string) {
			if err := os.Remove(row.Fact.Canonical); err != nil {
				t.Fatal(err)
			}
		}},
		{"a symlink", ErrMediaReference, func(t *testing.T, row *MediaRow, _ *[]string, _ *string, _, _ *int64, _ string) {
			moved := row.Fact.Canonical + ".moved"
			if err := os.Rename(row.Fact.Canonical, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, row.Fact.Canonical); err != nil {
				t.Fatal(err)
			}
		}},
		{"a recorded hash the bytes no longer match", ErrMediaReference, func(_ *testing.T, row *MediaRow, _ *[]string, version *string, _, _ *int64, _ string) {
			row.Fact.SHA256 = strings.Repeat("0", 64)
			*version = row.Version()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row, root, dest, _ := rangeFixture(t)
			roots, version, from, to := []string{root}, row.Version(), int64(0), int64(2000)
			c.edit(t, &row, &roots, &version, &from, &to, dest)
			_, err := SnapshotMediaRange(context.Background(), row, roots, version, from, to, dest)
			if !errors.Is(err, c.want) {
				t.Fatalf("wanted %v, got %v", c.want, err)
			}
			leftOver(t, dest)
		})
	}
}

func TestMediaRangeUnknownLengthSaysSoAndOversizeAndCancelLeaveNothing(t *testing.T) {
	row, root, dest, _ := rangeFixture(t)
	row.Fact.DurationMS = 0
	if _, err := SnapshotMediaRange(context.Background(), row, []string{root}, row.Version(), 0, 1, dest); err == nil || !strings.Contains(err.Error(), "not recorded") {
		t.Fatalf("an unknown length was not named: %v", err)
	}
	row, root, dest, _ = rangeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SnapshotMediaRange(ctx, row, []string{root}, row.Version(), 0, 1000, dest); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	big := append(rangeWAV(2000), make([]byte, MaxMediaRangeSourceBytes)...)
	if err := os.WriteFile(row.Fact.Canonical, big, 0o600); err != nil {
		t.Fatal(err)
	}
	row.Fact = GeneratedMedia(row.Fact.Canonical, big, "")
	row.Fact.ID, row.Fact.DurationMS = NewMediaID(), 2000
	if _, err := SnapshotMediaRange(context.Background(), row, []string{root}, row.Version(), 0, 1000, dest); !errors.Is(err, ErrMediaRange) {
		t.Fatalf("an oversize recording was accepted: %v", err)
	}
	leftOver(t, dest)
}

// A source swapped for a FIFO between authorization and open must be refused,
// not wait for a writer that never comes while holding the staging admission.
func TestMediaRangeNeverBlocksOnASourceSwappedForAFIFO(t *testing.T) {
	row, root, _, _ := rangeFixture(t)
	live := ResolveMedia(row, []string{root})
	if err := os.Remove(live.Path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(live.Path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		file, _, err := openLiveMedia(context.Background(), live, MaxMediaRangeSourceBytes, func([]byte) bool { return true }, func() error { return ErrMediaRange })
		if file != nil {
			_ = file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrMediaReference) {
			t.Fatalf("wanted the changed-source refusal, got %v", err)
		}
	case <-time.After(time.Second):
		if unblock, err := os.OpenFile(live.Path, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
			defer unblock.Close()
		}
		t.Fatal("a FIFO blocked the range worker")
	}
}

func rangedAttachment(from, to int64) MessageAttachment {
	return MessageAttachment{ID: "at1", Kind: "file", Name: "call.range-1-2.wav", Path: "/o/call.range-1-2.wav",
		Source: &MediaReference{SourceID: "src_1", Path: "/w/call.wav", Version: "sha256:abc", FromMS: &from, ToMS: &to}}
}

func TestMessageMarkKeepsAWellFormedRangeAndRefusesTheRest(t *testing.T) {
	mark, err := Submission{Text: "t", Attachments: []MessageAttachment{rangeAttachment(t, 1000, 2500)}}.mark()
	if err != nil || mark.Attachments[0].Source == nil || *mark.Attachments[0].Source.FromMS != 1000 || *mark.Attachments[0].Source.ToMS != 2500 || mark.Attachments[0].Source.Version != "sha256:abc" {
		t.Fatalf("the range was not kept: %+v %v", mark, err)
	}
	one := int64(1)
	bad := map[string]func(*MessageAttachment){
		"only a start":    func(a *MessageAttachment) { a.Source.ToMS = nil },
		"only an end":     func(a *MessageAttachment) { a.Source.FromMS = nil },
		"backwards":       func(a *MessageAttachment) { a.Source.FromMS = &one; a.Source.ToMS = &one },
		"negative":        func(a *MessageAttachment) { n := int64(-5); a.Source.FromMS = &n },
		"also a moment":   func(a *MessageAttachment) { a.Source.AtMS = &one },
		"no source id":    func(a *MessageAttachment) { a.Source.SourceID = "" },
		"no version":      func(a *MessageAttachment) { a.Source.Version = "" },
		"on a link":       func(a *MessageAttachment) { a.Kind = "link" },
		"past a year":     func(a *MessageAttachment) { n := MaxMediaRangeMS + 1; a.Source.ToMS = &n },
		"a long version":  func(a *MessageAttachment) { a.Source.Version = strings.Repeat("v", maxAttachmentField+1) },
		"a rangeless one": func(a *MessageAttachment) { a.Source.FromMS, a.Source.ToMS = nil, nil },
	}
	for name, edit := range bad {
		t.Run(name, func(t *testing.T) {
			a := rangeAttachment(t, 1000, 2500)
			edit(&a)
			if _, err := (Submission{Text: "t", Attachments: []MessageAttachment{a}}).mark(); !errors.Is(err, ErrMessageMark) {
				t.Fatalf("a malformed range was kept: %v", err)
			}
		})
	}
}

func rangeAttachment(t *testing.T, from, to int64) MessageAttachment {
	t.Helper()
	return rangedAttachment(from, to)
}

// A reading's copy shares no pointer with the index behind it, and the cost
// counts the range it holds.
func TestAnAttachmentsRangeIsCopiedAndCosted(t *testing.T) {
	original := []MessageAttachment{rangedAttachment(10, 20)}
	copied := cloneAttachments(original)
	*copied[0].Source.FromMS = 99
	copied[0].Source.Version = "x"
	if *original[0].Source.FromMS != 10 {
		t.Fatal("a reading's copy reached the record's range")
	}
	plain := attachmentSizes(MessageAttachment{ID: "at1", Kind: "file"})
	if got := attachmentSizes(original[0]); got < plain+int64(len("src_1")+len("/w/call.wav")+len("sha256:abc"))+16 {
		t.Fatalf("the range costs %d beside %d", got, plain)
	}
}
