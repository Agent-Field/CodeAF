package keys

import "testing"

func TestCleanReplacesTheValueOfASecretAndNothingElse(t *testing.T) {
	const key = "sk-abcdefghijklmnopqrstuvwx"
	for line, want := range map[string]string{
		"npm run dev":                                 "npm run dev",
		"API_KEY=" + key + " npm run dev":             "API_KEY=… npm run dev",
		"curl --token abc123 https://x":               "curl --token … https://x",
		"curl --token=abc123 https://x":               "curl --token=… https://x",
		"deploy --password 'a b' --env prod":          "deploy --password … --env prod",
		"echo " + key:                                 "echo …",
		"git commit --author=me -m msg":               "git commit --author=me -m msg",
		`DB_PASSWORD="two words" ./run`:               "DB_PASSWORD=… ./run",
		"PORT=3000 node server.js":                    "PORT=3000 node server.js",
		"aws --access-key-id AKIAABCDEFGHIJKLMNOP ls": "aws --access-key-id … ls",
		"": "",
	} {
		if got := Clean(line); got != want {
			t.Errorf("Clean(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestCleanCutsALineItCannotReadToItsFirstWord(t *testing.T) {
	if got := Clean(`run --note "never closed TOKEN=abc`); got != "run" {
		t.Fatalf("Clean = %q", got)
	}
}
