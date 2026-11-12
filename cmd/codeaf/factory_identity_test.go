package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFactoryHostConfigPreservesDeviceSignInThroughTokenRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeFactoryPrivate(path, []byte("listen: 127.0.0.1:7420\ngithub_client_id: client\nteam: [priya, '@sam']\nrepos: []\n")); err != nil {
		t.Fatal(err)
	}
	for _, rotate := range []bool{false, true} {
		cfg, err := factoryHostConfigAt(path, rotate)
		if err != nil || cfg.GitHubClientID != "client" || !reflect.DeepEqual(cfg.Team, []string{"priya", "@sam"}) || cfg.Repos == nil {
			t.Fatalf("config = %+v %v", cfg, err)
		}
	}
	if err := writeFactoryPrivate(path, []byte("team: ['not a handle']\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := readFactoryHostConfig(path); err == nil {
		t.Fatal("invalid team handle accepted")
	}
}
