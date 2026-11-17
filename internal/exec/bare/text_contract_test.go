package bare

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf16"
)

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C2/C4: tool text is decoded, while file bytes keep their BOM,
// encoding. Chat edits preserve line endings on every OS; ordinary rewrites
// preserve EOL style only on Windows.
func TestTextContractReadEditWriteEncoding(t *testing.T) {
	for _, encoding := range []string{"utf8", "utf8bom", "le", "be"} {
		t.Run(encoding, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notes.txt")
			initial := "hello 🌍\r\nnext\r\n"
			if err := os.WriteFile(path, textContractBytes(initial, encoding), 0o644); err != nil {
				t.Fatal(err)
			}
			got, bad := runTool(t, newReadTool(dir, DefaultCaps()), map[string]any{"path": "notes.txt"})
			wantRead := initial
			if encoding == "utf8bom" {
				wantRead = "\ufeff" + wantRead
			}
			if bad || got != wantRead {
				t.Errorf("read = %q, error=%v; want %q", got, bad, wantRead)
			}
			got, bad = runTool(t, newEditTool(dir), map[string]any{"path": "notes.txt", "oldText": "hello 🌍\nnext\n", "newText": "hi 🌍\nnext\nlast\n"})
			if bad {
				t.Fatalf("edit: %s", got)
			}
			assertTextContractBytes(t, path, textContractBytes("hi 🌍\r\nnext\r\nlast\r\n", encoding))
			got, bad = runTool(t, newWriteTool(dir), map[string]any{"path": "notes.txt", "content": "rewritten 🌍\nlast\n"})
			if bad {
				t.Fatalf("write: %s", got)
			}
			// UTF-8 write keeps its existing BOM behavior; UTF-16 keeps its BOM.
			writeEncoding := encoding
			if encoding == "utf8bom" {
				writeEncoding = "utf8"
			}
			wantWrite := "rewritten 🌍\r\nlast\r\n"
			if runtime.GOOS != "windows" && (encoding == "utf8" || encoding == "utf8bom") {
				wantWrite = "rewritten 🌍\nlast\n"
			}
			assertTextContractBytes(t, path, textContractBytes(wantWrite, writeEncoding))
		})
	}
}

func textContractBytes(text, encoding string) []byte {
	if encoding == "utf8bom" {
		return []byte("\ufeff" + text)
	}
	if encoding == "utf8" {
		return []byte(text)
	}
	var order binary.ByteOrder = binary.LittleEndian
	if encoding == "be" {
		order = binary.BigEndian
	}
	raw := []byte{0, 0}
	order.PutUint16(raw, 0xfeff)
	for _, unit := range utf16.Encode([]rune(text)) {
		var encoded [2]byte
		order.PutUint16(encoded[:], unit)
		raw = append(raw, encoded[:]...)
	}
	return raw
}

func assertTextContractBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("file = %x, %v; want %x", got, err, want)
	}
}

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C2: no UTF-16 BOM means the read tool keeps its byte behavior.
func TestTextContractReadWithoutUTF16BOM(t *testing.T) {
	dir := t.TempDir()
	raw := "h\x00i\x00\n"
	mustWriteFile(t, dir, "raw.txt", raw)
	got, bad := runTool(t, newReadTool(dir, DefaultCaps()), map[string]any{"path": "raw.txt"})
	if bad || got != raw {
		t.Fatalf("read = %q, error=%v", got, bad)
	}
}

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C3: only Windows changes CRLF to LF; lone CR stays a CR.
func TestTextContractBashOutput(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	got, bad := runTool(t, newBashTool(t.TempDir(), DefaultCaps()), map[string]any{"command": `printf 'version\r\nprogress\rnext\r\n'`})
	want := "version\r\nprogress\rnext\r\n"
	if runtime.GOOS == "windows" {
		want = "version\nprogress\rnext\n"
	}
	if bad || got != want {
		t.Fatalf("bash = %q, error=%v", got, bad)
	}
}
