package config

import (
	"reflect"
	"testing"
)

func TestChatModelRotationPersistsOrderAndPreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	if err := WriteChatModel(dir, "vendor/current"); err != nil {
		t.Fatal(err)
	}
	registry := NewSettings(SettingsOptions{ProfileDir: dir})
	row, ok := registry.Row(KeyChatModelRotation)
	if !ok {
		t.Fatal("missing rotation setting")
	}
	if err := row.Apply("vendor/b, vendor/a, vendor/b, , vendor/c"); err != nil {
		t.Fatal(err)
	}
	if got := ChatModelRotationAt(dir); !reflect.DeepEqual(got, []string{"vendor/b", "vendor/a", "vendor/c"}) {
		t.Fatalf("order = %v", got)
	}
	if got := ChatModelAt(dir); got != "vendor/current" {
		t.Fatalf("setting rotation changed chat: %s", got)
	}
	if err := row.Apply(""); err != nil {
		t.Fatal(err)
	}
	if got := ChatModelRotationAt(dir); len(got) != 0 {
		t.Fatalf("cleared list = %v", got)
	}
}
