package orientation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDigestContainsTreeAndOutlines(t *testing.T) {
	ClearCache()
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n\nfunc main() {}\n\ntype Config struct {\n\tName string\n}\n")
	writeTestFile(t, root, "parser/parser.go", "package parser\n\nfunc Parse(s string) (Node, error) {\n\treturn Node{}, nil\n}\n\nfunc (p *Parser) next() token {\n\treturn token{}\n}\n")
	writeTestFile(t, root, "parser/parser_test.go", "package parser\n\nfunc TestParse(t *testing.T) {}\n")
	writeTestFile(t, root, "README.md", "# Project\nA test project.\n")

	digest := BuildDigest(root, nil)

	// Must contain the directory tree
	if !strings.Contains(digest, "main.go") || !strings.Contains(digest, "parser/") {
		t.Fatalf("digest missing directory tree entries:\n%s", digest)
	}

	// Must contain code outlines (declarations, not bodies)
	if !strings.Contains(digest, "func main()") {
		t.Fatalf("digest missing main.go outline:\n%s", digest)
	}
	if !strings.Contains(digest, "func Parse(s string) (Node, error)") {
		t.Fatalf("digest missing parser.go Parse declaration:\n%s", digest)
	}

	// Must NOT contain bodies — the return statement inside Parse
	if strings.Contains(digest, "return Node{}, nil") {
		t.Fatalf("digest contains function body (should be declarations only):\n%s", digest)
	}

	// Must contain test files section
	if !strings.Contains(digest, "parser_test.go") {
		t.Fatalf("digest missing test file listing:\n%s", digest)
	}
}

func TestBuildDigestContainsFailingTests(t *testing.T) {
	ClearCache()
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n")
	writeTestFile(t, root, "main_test.go", "package main\n")

	failing := []string{"TestFoo", "TestBar"}
	digest := BuildDigest(root, failing)

	if !strings.Contains(digest, "TestFoo") || !strings.Contains(digest, "TestBar") {
		t.Fatalf("digest missing failing test names:\n%s", digest)
	}
	if !strings.Contains(digest, "Failing tests") {
		t.Fatalf("digest missing failing tests section header:\n%s", digest)
	}
}

func TestBuildDigestIsCached(t *testing.T) {
	ClearCache()
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\nfunc main() {}\n")

	first := BuildDigest(root, nil)
	// Add a file after the first call — the cached digest must not change
	writeTestFile(t, root, "new.go", "package main\nfunc New() {}\n")
	second := BuildDigest(root, nil)

	if first != second {
		t.Fatal("digest not cached: second call returned different output after cache should have hit")
	}
}

func TestBuildDigestCachesByFailingKey(t *testing.T) {
	ClearCache()
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n")

	noFailing := BuildDigest(root, nil)
	withFailing := BuildDigest(root, []string{"TestX"})

	if noFailing == withFailing {
		t.Fatal("digest should differ when failing tests differ")
	}
	if !strings.Contains(withFailing, "TestX") {
		t.Fatalf("digest with failing tests missing TestX:\n%s", withFailing)
	}
}

func TestBuildDigestBoundedSize(t *testing.T) {
	ClearCache()
	root := t.TempDir()
	// Create a large repo that would blow the bound
	for i := 0; i < 50; i++ {
		dir := filepath.Join(root, "pkg"+itoa(i))
		writeTestFile(t, root, filepath.Join("pkg"+itoa(i), "file.go"), "package pkg"+itoa(i)+"\n\nfunc A() {}\nfunc B() {}\nfunc C() {}\n")
		_ = dir
	}
	digest := BuildDigest(root, nil)
	if len(digest) > MaxBytes*2 {
		t.Fatalf("digest too large: %d bytes (MaxBytes=%d)", len(digest), MaxBytes)
	}
}

func TestBuildDigestSkipsNoise(t *testing.T) {
	ClearCache()
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\nfunc main() {}\n")
	writeTestFile(t, root, "node_modules/lib/index.js", "function lib() {}\n")
	writeTestFile(t, root, ".git/config", "[core]\n")

	digest := BuildDigest(root, nil)
	if strings.Contains(digest, "node_modules") {
		t.Fatalf("digest should skip node_modules:\n%s", digest)
	}
	if strings.Contains(digest, ".git") {
		t.Fatalf("digest should skip .git:\n%s", digest)
	}
	if !strings.Contains(digest, "main.go") {
		t.Fatalf("digest missing main.go:\n%s", digest)
	}
}

func TestBuildDigestEmptyDir(t *testing.T) {
	ClearCache()
	root := t.TempDir()
	digest := BuildDigest(root, nil)
	// An empty directory should still produce a tree (empty) and headers
	if digest == "" {
		t.Fatal("digest for empty dir should not be empty string")
	}
	if !strings.Contains(digest, "Repository orientation") {
		t.Fatalf("digest missing header:\n%s", digest)
	}
}

func TestDeclarationLineGoFunc(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"func Parse(s string) (Node, error) {", "func Parse(s string) (Node, error)"},
		{"type Config struct {", "type Config struct"},
		{"var Default = 42", "var Default = 42"},
		{"const Pi = 3.14", "const Pi = 3.14"},
		{"    func Indented() {", "func Indented()"},
		{"// comment", ""},
		{"", ""},
		{"result := compute()", ""},
	}
	for _, tc := range tests {
		got := declarationLine(".go", tc.line)
		if got != tc.want {
			t.Errorf("declarationLine(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestDeclarationLineJS(t *testing.T) {
	tests := []struct {
		ext  string
		line string
		want string
	}{
		{".ts", "export function parse(s: string): Node {", "export function parse(s: string): Node"},
		{".ts", "class Parser {", "class Parser"},
		{".js", "const lib = require('lib')", "const lib = require('lib')"},
		{".ts", "// comment", ""},
		{".js", "let x = 1", "let x = 1"},
	}
	for _, tc := range tests {
		got := declarationLine(tc.ext, tc.line)
		if got != tc.want {
			t.Errorf("declarationLine(%s, %q) = %q, want %q", tc.ext, tc.line, got, tc.want)
		}
	}
}

func TestDeclarationLinePython(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"def parse(s):", "def parse(s)"},
		{"class Parser:", "class Parser"},
		{"async def fetch(url):", "async def fetch(url)"},
		{"x = 1", ""},
		{"# comment", ""},
	}
	for _, tc := range tests {
		got := declarationLine(".py", tc.line)
		if got != tc.want {
			t.Errorf("declarationLine(.py, %q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"parser_test.go", true},
		{"foo.test.ts", true},
		{"bar.spec.js", true},
		{"test_parser.py", true},
		{"main.go", false},
		{"README.md", false},
		{"test.go", true}, // stem "test" starts with "test"
	}
	for _, tc := range tests {
		if got := isTestFile(tc.name); got != tc.want {
			t.Errorf("isTestFile(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
