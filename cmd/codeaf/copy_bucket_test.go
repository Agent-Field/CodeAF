package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestCopyBucket_D3_TenAlternatingOriginsStayBoundedAndStable(t *testing.T) {
	homes := []string{t.TempDir(), t.TempDir()}
	for _, origin := range []string{"/work/project", "/work/" + strings.Repeat("project", 50), "/work/" + strings.Repeat("目录", 70)} {
		t.Run(origin[:min(len(origin), 24)], func(t *testing.T) {
			first := ""
			for hop := 0; hop < 10; hop++ {
				t.Setenv(home.EnvVar, homes[hop%2])
				bucket := takeCopyBucket(origin)
				name := filepath.Base(bucket)
				if hop == 0 {
					first = name
				} else if name != first {
					t.Errorf("D3 hop %d changed bucket: %q != %q", hop, name, first)
				}
				if !utf8.ValidString(name) {
					t.Error("D3 split a UTF-8 path component")
				}
				for _, component := range strings.Split(filepath.ToSlash(bucket), "/") {
					if len(component) > 255 {
						t.Fatalf("D3 hop %d component has %d bytes", hop, len(component))
					}
				}
				if err := os.MkdirAll(bucket, 0700); err != nil {
					t.Fatal(err)
				}
				previous := origin
				origin = filepath.Join(bucket, "01J0000000000000000000000A", "work")
				if err := os.MkdirAll(origin, 0700); err != nil {
					t.Fatal(err)
				}
				if err := session.SaveMeta(filepath.Dir(origin), session.Meta{ID: "01J0000000000000000000000A", Owned: true, Workspace: origin, Origin: previous}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestCopyBucket_D3_ExistingShortNamesStayTheSame(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	for _, origin := range []string{"/work/project", "/work/copy-not-a-codeaf-bucket"} {
		want := filepath.Join(home.Dir(), "v3", "projects", "copy-"+encodeWorkspace(origin))
		if got := takeCopyBucket(origin); got != want {
			t.Fatalf("existing short bucket %q became %q", want, got)
		}
	}
}
