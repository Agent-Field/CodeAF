package shell

import (
	"runtime"
	"testing"
)

// Unix parity contract 2: output bytes that already worked stay unchanged.
func TestUnixParityOutputBytes(t *testing.T) {
	text := "version\r\nprogress\rnext\r\n"
	want := text
	if runtime.GOOS == "windows" {
		want = "version\nprogress\rnext\n"
	}
	if got := OutputText(text); got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
}
