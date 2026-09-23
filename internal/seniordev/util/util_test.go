//go:build !windows

package util

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLocalContext(t *testing.T) {
	local := CreateLocalContext[string]("test")
	if _, err := local.Use(context.Background()); err == nil || err.Error() != "No context found for test" {
		t.Fatalf("missing context: %v", err)
	}
	ctx := local.Provide(context.Background(), "value")
	if got := local.MustUse(ctx); got != "value" {
		t.Fatalf("context = %q", got)
	}
}

func TestFindUpRootFirst(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"root.txt", "a/one.txt", "a/b/two.txt"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := WriteText(path, relative); err != nil {
			t.Fatal(err)
		}
	}
	start := filepath.Join(root, "a", "b")
	got := FindUp([]string{"root.txt", "one.txt", "two.txt"}, start, "", FindUpOptions{RootFirst: true})
	want := []string{
		filepath.Join(root, "root.txt"),
		filepath.Join(root, "a", "one.txt"),
		filepath.Join(root, "a", "b", "two.txt"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findUp = %v, want %v", got, want)
	}
}

func TestNamedSchemaErrorFactory(t *testing.T) {
	factory := NamedSchemaErrorFactory("Boom")
	err := factory.New(map[string]any{"message": "x"}, errors.New("cause"))
	if !factory.IsInstance(err) || err.Error() != "Boom" || !errors.Is(err, err.Cause) {
		t.Fatalf("named error: %+v", err)
	}
}
