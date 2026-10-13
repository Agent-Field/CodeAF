package loginenv

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"testing/fstest"
)

func TestParsePath(t *testing.T) {
	for _, test := range []struct {
		name, output, want string
		fails              bool
	}{
		{"plain", pathStart + "/opt/homebrew/bin:/usr/bin" + pathEnd, "/opt/homebrew/bin:/usr/bin", false},
		{"banners", "Welcome\nnoise:before\n" + pathStart + "/a:/b" + pathEnd + "\nnoise:after", "/a:/b", false},
		{"empty", pathStart + pathEnd, "", false},
		{"spaces", pathStart + "/a folder/bin:/b" + pathEnd, "/a folder/bin:/b", false},
		{"missing start", "/a" + pathEnd, "", true},
		{"missing end", pathStart + "/a", "", true},
		{"no markers", "/a:/b", "", true},
		{"reversed", pathEnd + "/a" + pathStart, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parsePath(test.output)
			if got != test.want || (err != nil) != test.fails {
				t.Fatalf("parsePath = %q, %v; want %q, failure %v", got, err, test.want, test.fails)
			}
		})
	}
}

func TestMergePath(t *testing.T) {
	for _, test := range []struct {
		name, login, current, home, want string
		dirs, files                      []string
	}{
		{name: "login first with stable deduplication", login: "/brew:/usr/bin:/brew", current: "/custom:/usr/bin:/bin:/custom", want: "/brew:/usr/bin:/custom:/bin"},
		{name: "empty components", login: ":/a::", current: ":/b:", want: "/a:/b"},
		{name: "existing directories in fallback order", current: "/usr/bin", home: "/home/person", dirs: []string{"/home/person/.cargo/bin", "/home/person/.local/bin", "/usr/local/bin", "/opt/homebrew/bin", "/opt/homebrew/sbin", "/home/person/go/bin"}, want: "/usr/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/home/person/.local/bin:/home/person/go/bin:/home/person/.cargo/bin"},
		{name: "missing and regular files excluded", current: "/bin", home: "/home/person", dirs: []string{"/home/person/go/bin"}, files: []string{"/opt/homebrew/bin", "/usr/local/bin"}, want: "/bin:/home/person/go/bin"},
		{name: "fallback already present", login: "/opt/homebrew/bin:/bin", current: "/bin:/usr/local/bin", dirs: []string{"/opt/homebrew/bin", "/usr/local/bin"}, want: "/opt/homebrew/bin:/bin:/usr/local/bin"},
		{name: "unknown home has no relative fallback", dirs: []string{"go/bin", ".local/bin", ".cargo/bin"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixtures := make(fstest.MapFS)
			for _, dir := range test.dirs {
				fixtures[dir] = &fstest.MapFile{Mode: os.ModeDir | 0755}
			}
			for _, file := range test.files {
				fixtures[file] = &fstest.MapFile{Mode: 0644}
			}
			stat := func(path string) (os.FileInfo, error) {
				file, present := fixtures[path]
				if !present {
					return nil, os.ErrNotExist
				}
				return fstest.MapFS{"entry": file}.Stat("entry")
			}
			if got := mergePath(test.login, test.current, test.home, stat); got != test.want {
				t.Fatalf("mergePath = %q; want %q", got, test.want)
			}
		})
	}
}

func TestResolvePathFailureKeepsFallback(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, errors.New("shell failed"), errors.New("missing markers")} {
		t.Run(failure.Error(), func(t *testing.T) {
			read := func(context.Context) (string, error) { return "/partial", failure }
			stat := func(path string) (os.FileInfo, error) {
				if path == "/opt/homebrew/bin" {
					return fstest.MapFS{"dir": &fstest.MapFile{Mode: os.ModeDir}}.Stat("dir")
				}
				return nil, os.ErrNotExist
			}
			got, err := resolvePath(context.Background(), "/usr/bin:/bin", "", read, stat)
			if got != "/usr/bin:/bin:/opt/homebrew/bin" || !errors.Is(err, failure) {
				t.Fatalf("resolvePath = %q, %v", got, err)
			}
		})
	}
}

func TestResolverOptOutAndOnce(t *testing.T) {
	for _, value := range []string{"", "1", "0"} {
		t.Run("opt-out="+value, func(t *testing.T) {
			t.Setenv("CODEAF_NO_LOGIN_ENV", value)
			t.Setenv("PATH", ":/inherited:/inherited:")
			t.Setenv("SHELL", "/must/not/run")
			var r resolver
			if got := r.resolve(context.Background()); got != ":/inherited:/inherited:" {
				t.Fatalf("opt-out changed PATH: %q", got)
			}
			t.Setenv("PATH", "/later")
			if got := r.resolve(context.Background()); got != ":/inherited:/inherited:" {
				t.Fatalf("second resolution changed cached PATH: %q", got)
			}
		})
	}
}

func TestResolverOtherPlatformsAreNoOp(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS restores the login PATH")
	}
	t.Setenv("CODEAF_NO_LOGIN_ENV", "")
	if err := os.Unsetenv("CODEAF_NO_LOGIN_ENV"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", ":/inherited:/inherited:")
	t.Setenv("SHELL", "/must/not/run")
	var r resolver
	if got := r.resolve(context.Background()); got != ":/inherited:/inherited:" {
		t.Fatalf("non-macOS resolution changed PATH: %q", got)
	}
}
