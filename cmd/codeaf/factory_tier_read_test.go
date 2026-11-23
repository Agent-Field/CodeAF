package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactoryServerReadsBlankTierBeforeAnyPoll(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	root := t.TempDir()
	tokensPath := filepath.Join(root, "tokens.json")
	// A malformed local grant fails before any credential fallback or HTTP call.
	if err := os.WriteFile(factorygithub.GrantPath(tokensPath), []byte("invalid grant"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Origin: factory.OriginForge, Product: "acme", Repo: "api", Author: "bob", Num: 7, Title: "legacy job"})
	if err != nil {
		t.Fatal(err)
	}
	_ = factoryServerSeam(st, tokensPath, &githubLimit{})
	got, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != "" || got.Failure == nil || !strings.Contains(got.Failure.Detail, "Author access unknown: factory read credential is unavailable; retry after ") {
		t.Fatalf("tier=%q failure=%+v", got.Tier, got.Failure)
	}
}
