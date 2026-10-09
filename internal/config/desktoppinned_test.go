package config

import "testing"

func TestPinnedModelsStartOnTheThreeAndSurviveAWrite(t *testing.T) {
	dir := t.TempDir()
	got, chosen := DesktopPinned(dir)
	if chosen || len(got) != 3 || got[0] != "z-ai/glm-5.3-flash" || got[1] != DesktopDefaultModel || got[2] != "z-ai/glm-5.3" {
		t.Fatalf("default: %v %v", got, chosen)
	}
	if err := writeProfileValue(dir, "other.row", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := WriteDesktopPinned(dir, []string{"a/b", "c/d", "e/f"}); err != nil {
		t.Fatal(err)
	}
	got, chosen = DesktopPinned(dir)
	if !chosen || got[0] != "a/b" || got[2] != "e/f" {
		t.Fatalf("stored: %v %v", got, chosen)
	}
	if v, ok := persistedString(dir, "other.row"); !ok || v != "kept" {
		t.Fatal("another row was lost")
	}
	if err := WriteDesktopPinned(dir, nil); err != nil {
		t.Fatal(err)
	}
	if _, chosen = DesktopPinned(dir); chosen {
		t.Fatal("reset left the list")
	}
}

func TestPinnedWriteRefusesWhatIsNotThreeDistinctModels(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range [][]string{{"a/b"}, {"a/b", "a/b", "c/d"}, {"a/b", "c d", "e/f"}, {"a/b", "c/d", "e/f", "g/h"}} {
		if WriteDesktopPinned(dir, bad) == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestPinnedLabelsAreShort(t *testing.T) {
	for id, want := range map[string]string{"z-ai/glm-5.3-flash": "GLM Flash", DesktopDefaultModel: "DS Flash", "z-ai/glm-5.3": "GLM 5.3", "x/other": "other"} {
		if got := DesktopPinnedLabel(id); got != want {
			t.Fatalf("%s: %q", id, got)
		}
	}
}
