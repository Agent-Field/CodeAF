package catalog

import "testing"

// L4-A: The cache owner preserves the existing legacy and keyed filenames and
// refuses look-alikes, paths and keys the catalog could never have generated.
func TestL4Fix4PickerCacheNames(t *testing.T) {
	if got := PickerCacheName("", DefaultBaseURL); got != "models.json" {
		t.Fatal(got)
	}
	if got := PickerCacheName("ollama", "http://localhost:11434/v1"); got != "models-2c2a3009790cef0b.json" {
		t.Fatal(got)
	}
	for _, name := range []string{"models.json", "models-2c2a3009790cef0b.json"} {
		if !IsPickerCacheName(name) {
			t.Fatalf("picker does not recognize %q", name)
		}
	}
	for _, name := range []string{
		"models-my-notes.json", "models-.json", "models-0123.json",
		"models-0123456789abcdeF.json", "models-0123456789abcdef-more.json",
		"models-0123456789abcdef.json.bak", "elsewhere/models.json", "model-catalog.json",
	} {
		if IsPickerCacheName(name) {
			t.Fatalf("unowned picker file accepted: %q", name)
		}
	}
}
