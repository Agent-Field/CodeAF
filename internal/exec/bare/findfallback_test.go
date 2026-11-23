package bare

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFindWorksWithoutFD(t *testing.T) {
	root := grepTree(t)
	t.Setenv("PATH", t.TempDir())
	tool := newFindTool(root, DefaultCaps())
	text, bad, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil || bad {
		t.Fatalf("find: %s %v %v", text, bad, err)
	}
	for _, want := range []string{"main.go", "internal/belt.go"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, ".git/") || strings.Contains(text, "node_modules/") {
		t.Fatalf("searched noise: %s", text)
	}
	if !strings.Contains(tool.Description, "No fd") {
		t.Fatal(tool.Description)
	}
}

func TestFindGlobMatchesNestedPaths(t *testing.T) {
	for _, name := range []string{"src/main.go", "src/deep/main.go", "other/src/deep/main.go"} {
		if !findGlob("src/**/main.go", name) {
			t.Fatal(name)
		}
	}
	if findGlob("src/**/main.go", "src/main.ts") {
		t.Fatal("matched wrong extension")
	}
}

func TestFindFallbackHonoursRootLimitAndCancellation(t *testing.T) {
	root := grepTree(t)
	dir := "internal"
	limit := 1
	text, bad, err := findByWalking(context.Background(), root, "*", &dir, &limit, DefaultCaps())
	if err != nil || bad || !strings.HasPrefix(text, "belt.go") || !strings.Contains(text, "limit reached") {
		t.Fatalf("root/limit: %q %v %v", text, bad, err)
	}
	text, bad, _ = findByWalking(context.Background(), root, "[", nil, nil, DefaultCaps())
	if !bad || !strings.Contains(text, "Invalid pattern") {
		t.Fatalf("invalid pattern: %q", text)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	text, bad, _ = findByWalking(ctx, root, "*", nil, nil, DefaultCaps())
	if !bad || text != "Operation aborted" {
		t.Fatalf("cancellation: %q", text)
	}
}

func TestFindBracePatternsMatchNestedFiles(t *testing.T) {
	for _, name := range []string{"src/main.ts", "ui/View.tsx", "scripts/run.py"} {
		if !findGlob("*.{ts,tsx,py}", name) {
			t.Fatalf("brace glob missed %s", name)
		}
	}
	if findGlob("*.{ts,tsx,py}", "main.go") {
		t.Fatal("brace glob matched wrong extension")
	}
	if !findGlob("{src,ui}/**/*.{ts,tsx}", "ui/deep/View.tsx") {
		t.Fatal("multiple braces failed")
	}
}
