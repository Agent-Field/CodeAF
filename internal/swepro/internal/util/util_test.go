package util

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type intValidator struct{}

func (intValidator) Parse(value int) (int, error) {
	if value < 0 {
		return 0, errors.New("negative")
	}
	return value + 1, nil
}

func TestLazyValidatedAndLocalContext(t *testing.T) {
	calls := 0
	lazy := Lazy(func() int {
		calls++
		return calls
	})
	if lazy.Loaded() {
		t.Fatal("lazy starts loaded")
	}
	if lazy.Get() != 1 || lazy.Get() != 1 || calls != 1 {
		t.Fatalf("lazy get calls=%d", calls)
	}
	lazy.Reset()
	if lazy.Get() != 2 || calls != 2 {
		t.Fatalf("lazy reset calls=%d", calls)
	}

	fn := Fn[int, int](intValidator{}, func(value int) int { return value * 2 })
	if got, err := fn.Call(2); err != nil || got != 6 {
		t.Fatalf("validated call = %d, %v", got, err)
	}
	if got := fn.Force(2); got != 4 {
		t.Fatalf("force = %d", got)
	}

	local := CreateLocalContext[string]("test")
	if _, err := local.Use(context.Background()); err == nil || err.Error() != "No context found for test" {
		t.Fatalf("missing context: %v", err)
	}
	ctx := local.Provide(context.Background(), "value")
	if got := local.MustUse(ctx); got != "value" {
		t.Fatalf("context = %q", got)
	}
}

func TestUtilFilesystemAndBOM(t *testing.T) {
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

	path := filepath.Join(root, "bom.txt")
	if err := WriteText(path, "\ufeffhello"); err != nil {
		t.Fatal(err)
	}
	text, err := SyncBOMFile(path, false)
	if err != nil || text != "hello" {
		t.Fatalf("sync bom = %q, %v", text, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "hello" {
		t.Fatalf("raw = %q", raw)
	}
}

func TestWhichIncludesManagedBin(t *testing.T) {
	bin := t.TempDir()
	command := filepath.Join(bin, "codeaf-test-bin")
	if err := os.WriteFile(command, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := Which("codeaf-test-bin", map[string]string{"PATH": ""}, bin)
	if !ok || got != command {
		t.Fatalf("which = %q, %v", got, ok)
	}
}

func TestSchemaAdapters(t *testing.T) {
	schema := ValidatorFunc{
		ParseFunc: func(value any) (any, error) { return value, nil },
		JSONSchemaFunc: func() any {
			return map[string]any{"type": "object"}
		},
	}
	if value, err := Zod(schema).Parse("x"); err != nil || value != "x" {
		t.Fatalf("zod parse = %v, %v", value, err)
	}
	if _, err := ZodObject(schema); err != nil {
		t.Fatal(err)
	}
	if value, ok := PositiveInt(float64(2)); !ok || value != 2 {
		t.Fatalf("positive = %d, %v", value, ok)
	}
	if _, ok := NonNegativeInt(-1); ok {
		t.Fatal("-1 accepted")
	}
	factory := NamedSchemaErrorFactory("Boom")
	err := factory.New(map[string]any{"message": "x"}, errors.New("cause"))
	if !factory.IsInstance(err) || err.Error() != "Boom" || !errors.Is(err, err.Cause) {
		t.Fatalf("named error: %+v", err)
	}
}
