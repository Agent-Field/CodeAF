//go:build !windows

package ports

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParseLsofFailsClosed(t *testing.T) {
	ok := "p42\ncnode\n"
	if h := ParseLsof([]byte(ok)); h == nil || h.PID != 42 || h.Name != "node" {
		t.Fatalf("%+v", h)
	}
	if h := ParseLsof([]byte("p42\ncnode\np42\ncnode\n")); h == nil || h.PID != 42 {
		t.Fatalf("same process on v4 and v6: %+v", h)
	}
	for name, in := range map[string]string{
		"empty": "", "truncated": "p42\ncno", "no name": "p42\n", "no pid": "cnode\n",
		"two": "p42\ncnode\np43\ncnode\n", "zero": "p0\ncnode\n", "negative": "p-4\ncx\n",
		"junk": "p42\ncnode\nxjunk\n", "word": "pabc\ncx\n", "blank line": "p42\n\ncnode\n",
		"control": "p42\nc\x1b[31m\n",
	} {
		if h := ParseLsof([]byte(in)); h != nil {
			t.Errorf("%s: held %+v", name, h)
		}
	}
	if h := ParseLsof([]byte("p9\nc/usr/local/bin/my server\n")); h == nil || h.Name != "my server" {
		t.Fatalf("name is a base name: %+v", h)
	}
}

func script(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix helper")
	}
	p := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHelperTimeoutDoesNotHangOrLeak(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "alive")
	p := script(t, "(/bin/sleep 1; echo x > "+marker+") &\n/bin/sleep 30\n")
	start := time.Now()
	_, err := runCapped(context.Background(), p, nil, 200*time.Millisecond, 1024)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a descendant of the helper outlived its cancellation")
	}
}

func TestHelperOutputOverCapFailsClosed(t *testing.T) {
	p := script(t, "/bin/yes pxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n")
	start := time.Now()
	out, err := runCapped(context.Background(), p, nil, 2*time.Second, 4096)
	if err == nil || out != nil || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("%d bytes, %v after %v", len(out), err, time.Since(start))
	}
}
