package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// THE RELAY AN ORGANIZATION ADVERTISES IS AN ADDRESS EVERY MEMBER IS HANDED, so the config takes a web address and nothing that
// would be passed on with it: no sign-in in the address, no query, and a scheme a relay speaks.
func TestFactoryConfigAcceptsARelayAddressAndRefusesWhatCannotBeHandedOn(t *testing.T) {
	for _, tc := range []struct {
		in, want, err string
	}{
		{"", "", ""},
		{"  https://relay.example.com/  ", "https://relay.example.com", ""},
		{"http://127.0.0.1:8788", "http://127.0.0.1:8788", ""},
		{"https://example.com/fabric", "https://example.com/fabric", ""},
		{"relay.example.com", "", "relay_url must be a web address"},
		{"ftp://relay.example.com", "", "relay_url must be a web address"},
		{"https://user:secret@relay.example.com", "", "no sign-in or query"},
		{"https://relay.example.com/?token=x", "", "no sign-in or query"},
		{"https://relay.example.com/#x", "", "no sign-in or query"},
		{"https://", "", "relay_url must be a web address"},
	} {
		got, err := checkFactoryRelayURL(tc.in)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%q: want an error naming %q, got %q, %v", tc.in, tc.err, got, err)
			}
			if err != nil && strings.Contains(tc.in, "secret") && strings.Contains(err.Error(), "secret") {
				t.Errorf("%q: the refusal repeats the sign-in it was given: %v", tc.in, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q: want %q, got %q, %v", tc.in, tc.want, got, err)
		}
	}
}

// relay_url lives in the host config, which a token rotation rewrites from the struct: the address must survive that, or the
// next rotation would quietly stop telling members where their relay is.
func TestFactoryConfigKeepsTheRelayAddressThroughATokenRotation(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeFactoryPrivate(path, []byte("mode: team\nlisten: 127.0.0.1:9876\nrelay_url: https://relay.example.com/\n")); err != nil {
		t.Fatal(err)
	}
	for _, rotate := range []bool{false, true} {
		cfg, err := factoryHostConfigAt(path, rotate)
		if err != nil || cfg.RelayURL != "https://relay.example.com" {
			t.Fatalf("rotate=%v: config = %+v %v", rotate, cfg, err)
		}
	}
	if !strings.Contains(string(readFactoryTestFile(t, path)), "relay_url: https://relay.example.com") {
		t.Fatalf("the rotation dropped the address:\n%s", readFactoryTestFile(t, path))
	}
	if err := writeFactoryPrivate(path, []byte("relay_url: https://user:pw@relay.example.com\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := readFactoryHostConfig(path); err == nil || strings.Contains(err.Error(), "pw@") {
		t.Fatalf("a relay address with a sign-in in it must be refused without being printed: %v", err)
	}
}
