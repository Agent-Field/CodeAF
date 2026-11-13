package textfile

import (
	"runtime"
	"testing"
)

// Unix parity contracts 1 and 2: UTF-16 is a fix; existing byte writes stay exact.
func TestUnixParityWriteContent(t *testing.T) {
	for _, fixture := range []struct{ original, windows string }{
		{"a\nb\n", "mixed\nnext\nprogress\n"},
		{"a\r\nb\r\n", "mixed\r\nnext\r\nprogress\r\n"},
	} {
		content := "mixed\r\nnext\nprogress\r"
		want := content
		if runtime.GOOS == "windows" {
			want = fixture.windows
		}
		got, err := WriteContent([]byte(fixture.original), content)
		if err != nil || string(got) != want {
			t.Fatalf("write over %q = %q, %v; want %q", fixture.original, got, err, want)
		}
	}
}

// Unix parity contract 2: a full Unix rewrite can replace malformed old bytes.
func TestUnixParityRewriteMalformedBytes(t *testing.T) {
	got, err := WriteContent([]byte{0xff, 0xfe, 0}, "replacement\r\n")
	if runtime.GOOS == "windows" {
		if err == nil {
			t.Fatal("malformed UTF-16 was accepted")
		}
		return
	}
	if err != nil || string(got) != "replacement\r\n" {
		t.Fatalf("rewrite = %q, %v", got, err)
	}
}
