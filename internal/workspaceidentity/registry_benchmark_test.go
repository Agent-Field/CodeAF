package workspaceidentity_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// Metadata-only admission measurements deliberately exclude file-tree copying,
// terminal rendering, and model latency. Every fixture uses the public registry.
func BenchmarkWorkspaceRegistry(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			root := b.TempDir()
			registry, err := workspaceidentity.Open(filepath.Join(root, "registry"))
			if err != nil {
				b.Fatal(err)
			}
			var last workspaceidentity.Record
			for index := range count {
				path := filepath.Join(root, "workspaces", fmt.Sprint(index))
				if err := os.MkdirAll(path, 0700); err != nil {
					b.Fatal(err)
				}
				last, err = registry.Register(workspaceidentity.Run, path)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.Run("RegisterExisting", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					if _, err := registry.Register(workspaceidentity.Run, last.Path); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("RegisterNew", func(b *testing.B) {
				paths := make([]string, b.N)
				for index := range b.N {
					paths[index] = filepath.Join(root, "new", fmt.Sprint(index))
					if err := os.MkdirAll(paths[index], 0700); err != nil {
						b.Fatal(err)
					}
				}
				b.ResetTimer()
				b.ReportAllocs()
				for _, path := range paths {
					if _, err := registry.Register(workspaceidentity.Run, path); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Resolve", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					if _, err := registry.Resolve(last.URI); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
