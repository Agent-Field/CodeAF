package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactoryNameConfigPreservesSettingsAndOmission(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "listen: 127.0.0.1:9876\naccess_token: secret\ngithub_client_id: client\nteam: [priya]\n"
	if err := writeFactoryPrivate(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"Agent-Field/codeaf"}); err != nil {
		t.Fatal(err)
	}
	name, err := factoryNameAt(path, nil)
	if err != nil || name != "Agent-Field" || string(readFactoryTestFile(t, path)) != body {
		t.Fatalf("read = %q %v", name, err)
	}
	text := "  Our team  "
	if name, err := factoryNameAt(path, &text); err != nil || name != "Our team" {
		t.Fatalf("set = %q %v", name, err)
	}
	for _, rotate := range []bool{false, true} {
		cfg, err := factoryHostConfigAt(path, rotate)
		if err != nil || cfg.Name != "Our team" || cfg.Listen != "127.0.0.1:9876" || cfg.GitHubClientID != "client" || len(cfg.Team) != 1 || cfg.Repos != nil {
			t.Fatalf("config = %+v %v", cfg, err)
		}
	}
	text = ""
	if name, err := factoryNameAt(path, &text); err != nil || name != "Agent-Field" {
		t.Fatalf("clear = %q %v", name, err)
	}
	if strings.Contains(string(readFactoryTestFile(t, path)), "name:") {
		t.Fatal("cleared name was persisted")
	}
	if err := writeFactoryPrivate(path, []byte("repos: []\n")); err != nil {
		t.Fatal(err)
	}
	if name, err := factoryNameAt(path, nil); err != nil || name != factory.DisplayName("", nil) {
		t.Fatalf("explicit no repositories = %q %v", name, err)
	}
}

func TestFactoryNameRejectsInvalidTextWithoutWriting(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "name: '  Our team  '\nrepos: []\n"
	if err := writeFactoryPrivate(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if cfg, err := readFactoryHostConfig(path); err != nil || cfg.Name != "Our team" {
		t.Fatalf("trimmed config = %+v %v", cfg, err)
	}
	for _, text := range []string{strings.Repeat("a", factory.MaxNameChars+1), "team\n", "te\x00am"} {
		if _, err := factoryNameAt(path, &text); err == nil {
			t.Fatalf("accepted %q", text)
		}
		if got := string(readFactoryTestFile(t, path)); got != body {
			t.Fatal("invalid command changed the config")
		}
	}
	for _, body := range []string{"name: " + strings.Repeat("a", factory.MaxNameChars+1), `name: "team\n"`, `name: "te\u0000am"`} {
		if err := writeFactoryPrivate(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := readFactoryHostConfig(path); err == nil {
			t.Fatalf("accepted YAML %q", body)
		}
	}
}
