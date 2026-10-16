package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func framePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngChunk builds one well-formed chunk.
func pngChunk(kind string, body []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	out = append(out, kind...)
	out = append(out, body...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(kind), body...)))
}

// headerOnly is a PNG whose IHDR claims w by h; the pixels are never decoded.
func headerOnly(w, h uint32) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 6, 0, 0, 0)
	out := append([]byte(nil), pngSignature...)
	out = append(out, pngChunk("IHDR", ihdr)...)
	out = append(out, pngChunk("IDAT", []byte{0})...)
	return append(out, pngChunk("IEND", nil)...)
}

func TestCheckFramePNGBoundsAndCorruption(t *testing.T) {
	good := framePNG(t, 3, 2)
	if w, h, err := CheckFramePNG(good); err != nil || w != 3 || h != 2 {
		t.Fatalf("good frame refused: %d %d %v", w, h, err)
	}
	flip := func(at int) []byte {
		b := append([]byte(nil), good...)
		b[at] ^= 0xff
		return b
	}
	cases := map[string][]byte{
		"empty":      nil,
		"signature":  append([]byte("x"), good[1:]...),
		"ihdr crc":   flip(8 + 8 + 2),
		"truncated":  good[:len(good)-5],
		"trailing":   append(append([]byte(nil), good...), 0),
		"oversize":   append(append([]byte(nil), good...), make([]byte, MaxFrameBytes)...),
		"wide":       headerOnly(MaxFrameSide+1, 1),
		"tall":       headerOnly(1, MaxFrameSide+1),
		"pixels":     headerOnly(4096, 2049),
		"zero":       headerOnly(0, 4),
		"no data":    append(append(append([]byte(nil), pngSignature...), good[8:8+25]...), pngChunk("IEND", nil)...),
		"iend first": append(append([]byte(nil), pngSignature...), pngChunk("IEND", nil)...),
	}
	for name, data := range cases {
		if _, _, err := CheckFramePNG(data); !errors.Is(err, ErrMediaFrame) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	// The limits are inclusive: exactly 4096 a side and exactly 8 Mpx pass.
	if _, _, err := CheckFramePNG(headerOnly(MaxFrameSide, MaxFramePixels/MaxFrameSide)); err != nil {
		t.Fatalf("limit refused: %v", err)
	}
}

func frameFixture(t *testing.T) (MediaRow, string, string) {
	t.Helper()
	root, dest := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "clip one.mp4")
	data := append([]byte{0, 0, 0, 0x18}, []byte("ftypisom\x00\x00\x02\x00isomiso2")...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fact := GeneratedMedia(path, data, "")
	fact.ID = NewMediaID()
	return MediaRow{Fact: fact, Kind: "video", Recorded: true}, root, dest
}

func TestSnapshotMediaFrameIsOwnedAndTheClipIsUntouched(t *testing.T) {
	row, root, dest := frameFixture(t)
	data := framePNG(t, 4, 3)
	got, err := SnapshotMediaFrame(context.Background(), row, []string{root}, row.Version(), 3200, data, dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Path, dest+string(os.PathSeparator)) || filepath.Base(got.Path) != "clip_one.frame-3200.png" {
		t.Fatalf("not an owned, named snapshot: %s", got.Path)
	}
	saved, err := os.ReadFile(got.Path)
	if err != nil || !bytes.Equal(saved, data) || len(got.SHA256) != 64 {
		t.Fatalf("saved bytes differ: %v", err)
	}
	if err := got.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(row.Fact.Canonical); err != nil {
		t.Fatal("cleanup touched the clip")
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatal("snapshot directory leaked")
	}
}

func TestSnapshotMediaFrameRefusesWithoutLeavingAFile(t *testing.T) {
	for _, name := range []string{"legacy", "image", "version", "outside", "changed", "symlink", "moment", "negative", "bytes"} {
		t.Run(name, func(t *testing.T) {
			row, root, dest := frameFixture(t)
			version, roots, at, data := row.Version(), []string{root}, int64(10), framePNG(t, 2, 2)
			switch name {
			case "legacy":
				row.Recorded = false
			case "image":
				row.Kind = "image"
			case "version":
				version = "sha256:other"
			case "outside":
				roots = []string{dest}
			case "changed":
				if err := os.WriteFile(row.Fact.Canonical, []byte("rewritten clip, different size"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				moved := row.Fact.Canonical + ".moved"
				if err := os.Rename(row.Fact.Canonical, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, row.Fact.Canonical); err != nil {
					t.Fatal(err)
				}
			case "moment":
				at = MaxFrameAtMS + 1
			case "negative":
				at = -1
			case "bytes":
				data = []byte("not a png")
			}
			_, err := SnapshotMediaFrame(context.Background(), row, roots, version, at, data, dest)
			if !errors.Is(err, ErrMediaReference) && !errors.Is(err, ErrMediaFrame) {
				t.Fatalf("wanted a refusal: %v", err)
			}
			if entries, _ := os.ReadDir(dest); len(entries) != 0 {
				t.Fatal("refused snapshot leaked")
			}
		})
	}
}

func TestSnapshotMediaFrameCancelledLeavesNothing(t *testing.T) {
	row, root, dest := frameFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SnapshotMediaFrame(ctx, row, []string{root}, row.Version(), 1, framePNG(t, 2, 2), dest); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatal("cancelled snapshot leaked")
	}
}
