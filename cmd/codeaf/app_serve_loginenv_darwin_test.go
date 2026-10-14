package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppServeResolvesLoginPathBeforeLaunching(t *testing.T) {
	root := t.TempDir()
	shell := filepath.Join(root, "login-shell")
	receipt := filepath.Join(root, "login-environment")
	script := `#!/bin/sh
[ "$#" = 3 ] && [ "$1" = -l ] && [ "$2" = -c ] || exit 12
printf 'profile banner\n'
PATH="$CODEAF_LOGIN_TEST_BIN:/usr/bin:/bin"
printf '%s' "$PATH" > "$CODEAF_LOGIN_TEST_RECEIPT"
eval "$3"
printf 'profile footer\n'
`
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	launch := launchAppAtHomeWithEnvForTest(t, root, false, []string{
		"SHELL=" + shell,
		"CODEAF_LOGIN_TEST_BIN=" + filepath.Join(root, "tools"),
		"CODEAF_LOGIN_TEST_RECEIPT=" + receipt,
		"CODEAF_APP_TEST_LOGIN_ENV=1",
	})
	got, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatalf("app announced readiness before login PATH was read: %v", err)
	}
	if !strings.HasPrefix(string(got), filepath.Join(root, "tools")+":") {
		t.Fatalf("login shell receipt = %q", got)
	}
	if err := launch.input.Close(); err != nil {
		t.Fatal(err)
	}
	launch.wait(t)
	applied, err := os.ReadFile(receipt + ".applied")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(applied), string(got)) {
		t.Fatalf("app PATH = %q; want login prefix %q", applied, got)
	}
}
