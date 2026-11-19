package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestQAOfferingComputerKeepsItsApprovedRelay(t *testing.T) {
	home := t.TempDir()
	const address = "https://relay.example.test"
	grant, err := pairGrantAt(home, pair.Mailbox{URL: address})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, syncsetup.SavedURLFile)); !os.IsNotExist(err) {
		t.Fatal("showing a code changed the saved relay before approval")
	}
	if err := grant.BeforeSend(); err != nil {
		t.Fatal(err)
	}
	if got := syncsetup.ResolveUnnamed(home).URL; got != address {
		t.Fatalf("reopening without an environment variable chose %q, want %q", got, address)
	}
}
