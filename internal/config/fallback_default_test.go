package config

import "testing"

func TestDefaultFallbackIsDeepSeekFlash(t *testing.T) {
	dir := t.TempDir()
	if got := ModelFallbacksAt(dir); got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("default=%q", got)
	}
	if err := writeText(dir, KeyModelFallbacks, "custom/backup,custom/second"); err != nil {
		t.Fatal(err)
	}
	if got := ModelFallbacksAt(dir); got != "custom/backup,custom/second" {
		t.Fatalf("explicit choice overwritten: %q", got)
	}
	if err := writeText(dir, KeyModelFallbacks, ""); err != nil {
		t.Fatal(err)
	}
	if got := ModelFallbacksAt(dir); got != DefaultFallbackModel {
		t.Fatalf("blank default=%q", got)
	}
}

func TestLegacyFallbackDisplayLabelUsesTheDefault(t *testing.T) {
	dir := t.TempDir()
	if err := writeText(dir, KeyModelFallbacks, "nearest in the catalog"); err != nil {
		t.Fatal(err)
	}
	if got := ModelFallbacksAt(dir); got != DefaultFallbackModel {
		t.Fatalf("display label reached a model setting: %q", got)
	}
}
