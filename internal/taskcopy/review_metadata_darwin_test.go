//go:build darwin

package taskcopy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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

func requireDarwinProvenance(t *testing.T, path string) {
	t.Helper()
	var value [12]byte
	n, err := unix.Lgetxattr(path, "com.apple.provenance", value[:])
	if errors.Is(err, unix.ENOATTR) || errors.Is(err, unix.ENOTSUP) {
		t.Skip("fixture has no system provenance; the test binary may be untracked")
	}
	if err != nil || n != 11 {
		t.Fatalf("system provenance size = %d, error = %v; want 11 bytes", n, err)
	}
}

func darwinMetadataFixture(t *testing.T, kind string) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "entry")
	switch kind {
	case "file":
		write(t, path, "result\n")
	case "directory":
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	case "symlink", "dangling-symlink":
		if kind == "symlink" {
			write(t, filepath.Join(root, "target"), "target\n")
		}
		if err := os.Symlink("target", path); err != nil {
			t.Fatal(err)
		}
	}
	return root, path
}

func requireDarwinXattr(t *testing.T, path, name string, value []byte) {
	t.Helper()
	if err := unix.Lsetxattr(path, name, value, 0); err != nil {
		t.Skipf("kernel rejected fixture attribute %s: %v", name, err)
	}
	var actual [256]byte
	n, err := unix.Lgetxattr(path, name, actual[:])
	if errors.Is(err, unix.ENOATTR) {
		t.Skipf("kernel discarded fixture attribute %s despite a successful write", name)
	}
	if err != nil || string(actual[:n]) != string(value) {
		t.Fatalf("fixture attribute %s did not persist as requested (size %d, error %v)", name, n, err)
	}
}

func requireDarwinXattrRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "portable apply cannot preserve extended attributes or access-control metadata") {
		t.Fatalf("want extended-attributes refusal, got %v", err)
	}
}

func TestPortableMutationAllowsDarwinProvenance(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "dangling-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, path := darwinMetadataFixture(t, kind)
			requireDarwinProvenance(t, path)
			if err := CheckPortableMutationMetadata(root, []string{"entry"}); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("root", func(t *testing.T) {
		root := t.TempDir()
		requireDarwinProvenance(t, root)
		if err := CheckPortableRootInheritance(context.Background(), root); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPortableMutationRefusesOtherDarwinXattrs(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			for _, attr := range []struct {
				label, name string
				value       []byte
			}{
				{"quarantine", "com.apple.quarantine", []byte("0081;00000000;Safari;")},
				{"tags", "com.apple.metadata:_kMDItemUserTags", []byte("tags")},
				{"where-from", "com.apple.metadata:kMDItemWhereFroms", []byte("origin")},
				{"finder-zero", "com.apple.FinderInfo", make([]byte, 32)},
				{"finder-nonzero", "com.apple.FinderInfo", append([]byte{1}, make([]byte, 31)...)},
				{"last-used", "com.apple.lastuseddate#PS", make([]byte, 16)},
				{"encoding", "com.apple.TextEncoding", []byte("UTF-8;134217984")},
				{"macl", "com.apple.macl", make([]byte, 72)},
				{"empty-resource-fork", "com.apple.ResourceFork", []byte{}},
				{"user", "com.example.data", []byte("data")},
				{"same-length", "com.apple.provenancf", []byte("data")},
			} {
				t.Run(attr.label, func(t *testing.T) {
					root, path := darwinMetadataFixture(t, kind)
					requireDarwinXattr(t, path, attr.name, attr.value)
					requireDarwinXattrRefusal(t, CheckPortableMutationMetadata(root, []string{"entry"}))
					if kind == "symlink" {
						if err := CheckPortableMutationMetadata(root, []string{"target"}); err != nil {
							t.Fatalf("link attribute leaked onto target verdict: %v", err)
						}
					}
				})
			}
		})
	}
	t.Run("target-attribute-does-not-follow-link", func(t *testing.T) {
		root, path := darwinMetadataFixture(t, "symlink")
		requireDarwinXattr(t, filepath.Join(root, "target"), "com.example.data", []byte("target data"))
		if err := CheckPortableMutationMetadata(root, []string{filepath.Base(path)}); err != nil {
			t.Fatalf("target attribute leaked onto link verdict: %v", err)
		}
		requireDarwinXattrRefusal(t, CheckPortableMutationMetadata(root, []string{"target"}))
	})
	t.Run("provenance-overwrite", func(t *testing.T) {
		root, path := darwinMetadataFixture(t, "file")
		if err := unix.Lsetxattr(path, "com.apple.provenance", make([]byte, 33), 0); err != nil {
			t.Skipf("kernel rejected provenance overwrite fixture: %v", err)
		}
		var value [34]byte
		n, err := unix.Lgetxattr(path, "com.apple.provenance", value[:])
		if err != nil {
			t.Fatal(err)
		}
		switch n {
		case 33:
			t.Log("kernel retained the 33-byte provenance overwrite")
			requireDarwinXattrRefusal(t, CheckPortableMutationMetadata(root, []string{"entry"}))
		case 11:
			t.Log("kernel kept the 11-byte system provenance value")
			if err := CheckPortableMutationMetadata(root, []string{"entry"}); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected provenance size after overwrite: %d", n)
		}
	})
}

func TestPortableMutationRefusesDarwinResourceFork(t *testing.T) {
	root, path := darwinMetadataFixture(t, "file")
	fork := path + "/..namedfork/rsrc"
	const contents = "resource fork project bytes\n"
	if err := os.WriteFile(fork, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	requireDarwinXattrRefusal(t, CheckPortableMutationMetadata(root, []string{"entry"}))
	actual, err := os.ReadFile(fork)
	if err != nil || string(actual) != contents {
		t.Fatalf("resource fork changed after refusal: size %d, error %v", len(actual), err)
	}
}

func TestPortableMutationRefusesDarwinACLBesideProvenance(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, path := darwinMetadataFixture(t, kind)
			requireDarwinProvenance(t, path)
			args := []string{"+a", "everyone deny delete", path}
			cleanup := []string{"-N", path}
			if kind == "symlink" {
				args = append([]string{"-h"}, args...)
				cleanup = append([]string{"-h"}, cleanup...)
			}
			if out, err := exec.Command("/bin/chmod", args...).CombinedOutput(); err != nil {
				t.Skipf("fixture ACL unavailable: %v %s", err, out)
			}
			t.Cleanup(func() {
				if out, err := exec.Command("/bin/chmod", cleanup...).CombinedOutput(); err != nil {
					t.Errorf("remove fixture ACL: %v %s", err, out)
				}
			})
			paths := []string{"entry"}
			for _, batched := range []bool{false, true} {
				if batched {
					paths = nil
					for i := range 200 {
						rel := fmt.Sprintf("clean-%03d", i)
						write(t, filepath.Join(root, rel), "clean\n")
						paths = append(paths, rel)
					}
					// Put the ACL in the second subprocess batch too.
					paths = append(paths, "entry")
				}
				err := CheckPortableMutationMetadata(root, paths)
				if err == nil || !strings.Contains(err.Error(), "access-control metadata") || strings.Contains(err.Error(), "extended attributes") {
					t.Fatalf("batched=%v: want ACL refusal, got %v", batched, err)
				}
			}
		})
	}
	t.Run("escaped-names-without-acls", func(t *testing.T) {
		root := t.TempDir()
		paths := []string{" 0: fake", "name\nwith-newline"}
		for _, name := range paths {
			write(t, filepath.Join(root, name), "clean\n")
		}
		if err := CheckPortableMutationMetadata(root, paths); err != nil {
			t.Fatalf("ordinary names mistaken for ACLs: %v", err)
		}
	})
}

func TestPortableDarwinXattrDecision(t *testing.T) {
	const provenanceList = "com.apple.provenance\x00"
	for _, tc := range []struct {
		name                       string
		first, last                string
		valueSize                  int
		listErr, getErr, relistErr error
		refuse                     bool
		wantErr                    error
	}{
		{name: "exact", first: provenanceList, last: provenanceList, valueSize: 11},
		{name: "empty-value", first: provenanceList, valueSize: 0, refuse: true},
		{name: "short-value", first: provenanceList, valueSize: 10, refuse: true},
		{name: "long-value", first: provenanceList, valueSize: 12, refuse: true},
		{name: "get-range", first: provenanceList, getErr: unix.ERANGE, refuse: true},
		{name: "get-vanished", first: provenanceList, getErr: unix.ENOATTR, refuse: true},
		{name: "list-range", listErr: unix.ERANGE, refuse: true},
		{name: "different-name", first: "com.apple.provenancf\x00", refuse: true},
		{name: "finder-info-name", first: "com.apple.FinderInfo\x00", refuse: true},
		{name: "empty-resource-fork-name", first: "com.apple.ResourceFork\x00", refuse: true},
		{name: "long-list", first: provenanceList + "com.example.data\x00", refuse: true},
		{name: "missing-terminator", first: "com.apple.provenance", refuse: true},
		{name: "relist-added", first: provenanceList, last: provenanceList + "com.example.data\x00", valueSize: 11, refuse: true},
		{name: "relist-different", first: provenanceList, last: "com.apple.provenancf\x00", valueSize: 11, refuse: true},
		{name: "relist-empty", first: provenanceList, valueSize: 11, refuse: true},
		{name: "relist-range", first: provenanceList, valueSize: 11, relistErr: unix.ERANGE, refuse: true},
		{name: "relist-unsupported", first: provenanceList, valueSize: 11, relistErr: unix.ENOTSUP, refuse: true},
		{name: "no-attributes"},
		{name: "unsupported", listErr: unix.ENOTSUP},
		{name: "list-error", listErr: unix.EACCES, wantErr: unix.EACCES},
		{name: "get-error", first: provenanceList, getErr: unix.EIO, wantErr: unix.EIO},
		{name: "relist-error", first: provenanceList, valueSize: 11, relistErr: unix.EIO, wantErr: unix.EIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lists, gets := 0, 0
			list := func(path string, buf []byte) (int, error) {
				if path != "fixture" || len(buf) != len(provenanceList) {
					t.Fatalf("unexpected list path or buffer size: %q, %d", path, len(buf))
				}
				lists++
				raw, err := tc.first, tc.listErr
				if lists > 1 {
					raw, err = tc.last, tc.relistErr
				}
				copy(buf, raw)
				return len(raw), err
			}
			get := func(path, name string, buf []byte) (int, error) {
				if path != "fixture" || name != "com.apple.provenance" || len(buf) != 12 {
					t.Fatalf("unexpected get path, name or buffer size: %q, %q, %d", path, name, len(buf))
				}
				gets++
				return tc.valueSize, tc.getErr
			}
			err := portableDarwinExtendedAttributes("fixture", list, get)
			switch {
			case tc.refuse:
				if err == nil || err.Error() != "portable apply cannot preserve extended attributes or access-control metadata" {
					t.Fatalf("want exact extended-attributes refusal, got %v", err)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want syscall error %v, got %v", tc.wantErr, err)
				}
			case err != nil:
				t.Fatal(err)
			}
			if lists > 2 || gets > 1 {
				t.Fatalf("unbounded inspection: %d list calls, %d get calls", lists, gets)
			}
			if tc.name == "exact" && (lists != 2 || gets != 1) {
				t.Fatalf("allowed provenance must be read and re-listed: %d lists, %d gets", lists, gets)
			}
			if tc.first != provenanceList && gets != 0 {
				t.Fatal("an unsupported attribute name must refuse without reading its value")
			}
		})
	}
}

func TestPortableDarwinACLListing(t *testing.T) {
	const main = "-rw-r--r--@ 1 501 20 7 Oct 29 12:00 /tmp/result"
	const acl = " 0: ABCDEFAB-CDEF-ABCD-EFAB-CDEF0000000C deny delete"
	for _, tc := range []struct {
		name, listing string
		paths         int
		refuse        bool
	}{
		{"at-with-acl", main + "\n" + acl + "\n", 1, true},
		{"acl-even-with-matching-count", main + "\n" + acl + "\n", 2, true},
		{"leading-acl", acl + "\n", 1, true},
		{"unindented-acl", "0: ABCDEFAB-CDEF-ABCD-EFAB-CDEF0000000C deny delete\n", 1, true},
		{"tabbed-acl", "\t0: ABCDEFAB-CDEF-ABCD-EFAB-CDEF0000000C deny delete\n", 1, true},
		{"plus-mode", strings.Replace(main, "@", "+", 1) + "\n", 1, true},
		{"plain-at", main + "\n" + main + "\n", 2, false},
		{"acl-looking-filename", main + "/ 0: fake\n", 1, false},
		{"escaped-newline", main + "\\nname\n", 1, false},
		{"count-mismatch", main + "\n", 2, true},
		{"blank-line", main + "\n\n" + main + "\n", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := portableDarwinACLListing(tc.listing, tc.paths)
			if tc.refuse {
				if err == nil || err.Error() != "portable apply cannot preserve native access-control metadata" {
					t.Fatalf("want ACL refusal, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
