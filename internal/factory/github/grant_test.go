package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadGrantIsPrivateAndNeverBecomesAWriteToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	profile := t.TempDir()
	path := GrantPath(filepath.Join(profile, "tokens.json"))
	grant := ReadGrant{Token: "app-read-token", Login: "owner", GrantedAt: time.Now().UTC()}
	if err := SaveReadGrant(path, grant); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadReadGrant(path); err != nil || got != grant {
		t.Fatalf("grant: %+v %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private: %v %v", info, err)
	}
	if token, via, err := ReadTokenAt(context.Background(), profile, path); err != nil || token != grant.Token || via != "grant" {
		t.Fatalf("read: %q %q %v", token, via, err)
	}
	if token, _ := TokenAt(context.Background(), profile); token != "" {
		t.Fatal("read grant escaped to write resolver")
	}
	for _, variable := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(variable, "host-override")
		if token, via, err := ReadTokenAt(context.Background(), profile, path); err != nil || token != "host-override" || via != "env" {
			t.Fatalf("override: %q %q %v", token, via, err)
		}
	}
}
