//go:build darwin

package taskcopy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPortableMutationRefusesNativeDarwinMetadata(t *testing.T) {
	for _, kind := range []string{"acl", "flags"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "result.txt")
			write(t, path, "result\n")
			if err := os.Chown(path, -1, os.Getgid()); err != nil {
				t.Fatal(err)
			}
			if err := CheckPortableMutationMetadata(root, []string{"result.txt"}); err != nil {
				t.Fatalf("unadorned fixture must be supported: %v", err)
			}
			if kind == "acl" {
				if out, err := exec.Command("/bin/chmod", "+a", "everyone deny delete", path).CombinedOutput(); err != nil {
					t.Skipf("fixture ACL unavailable: %v %s", err, out)
				}
				defer exec.Command("/bin/chmod", "-N", path).Run()
			} else {
				if out, err := exec.Command("/usr/bin/chflags", "nodump", path).CombinedOutput(); err != nil {
					t.Skipf("fixture flags unavailable: %v %s", err, out)
				}
				defer exec.Command("/usr/bin/chflags", "nonodump", path).Run()
			}
			if err := CheckPortableMutationMetadata(root, []string{"result.txt"}); err == nil {
				t.Fatalf("native %s would be dropped by replacement", kind)
			} else {
				want := "native filesystem flags"
				if kind == "acl" {
					want = "access-control metadata"
				}
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("native %s refused for wrong reason: %v", kind, err)
				}
			}
		})
	}
}

func TestPortableMutationPreflightBatchesDarwinPaths(t *testing.T) {
	for _, count := range []int{100, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			root := t.TempDir()
			paths := make([]string, count)
			for i := range paths {
				paths[i] = fmt.Sprintf("file-%04d.txt", i)
				write(t, filepath.Join(root, paths[i]), "result\n")
				if err := os.Chown(filepath.Join(root, paths[i]), -1, os.Getgid()); err != nil {
					t.Fatal(err)
				}
			}
			started := time.Now()
			if err := CheckPortableMutationMetadata(root, paths); err != nil {
				t.Fatal(err)
			}
			t.Logf("native metadata preflight: %d paths in %s", count, time.Since(started))
		})
	}
}
