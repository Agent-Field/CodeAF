package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// BenchmarkReplayTwoHundredPictures is the cost of reopening a conversation that
// holds two hundred 1MiB pictures: reading the journal's parts back into the
// live transcript. Run with -benchmem.
func BenchmarkReplayTwoHundredPictures(b *testing.B) {
	directory := b.TempDir()
	var parts []journalPart
	for i := 0; i < 200; i++ {
		data := make([]byte, 1<<20)
		copy(data, strconv.Itoa(i))
		path := filepath.Join(directory, "p"+strconv.Itoa(i)+".png")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			b.Fatal(err)
		}
		sum := sha256.Sum256(data)
		parts = append(parts, journalPart{Type: journalPartImage, Path: path, SHA256: hex.EncodeToString(sum[:]), MIME: "image/png"})
	}
	entry := sessionEntry{Role: "user", Content: "two hundred", Parts: parts}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = replayedMessage(entry, true)
	}
}
