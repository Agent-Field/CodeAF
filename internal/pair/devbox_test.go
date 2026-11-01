package pair

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// Round 2: Terraform receives the exact files released serve loads, independently of identity pairing.
func TestDevboxRemoteFilesLoadWithServeReaders(t *testing.T) {
	client, err := ThisDevice(FileKeeper{Path: filepath.Join(t.TempDir(), "client.key")})
	if err != nil {
		t.Fatal(err)
	}
	key, devices, known, err := DevboxRemote("build-box", "https://relay.example", client, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string][]byte{DeviceKeyPath(): key, filepath.Join(Dir(), "devices.json"): devices} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	machine, err := ThisDevice(OpenKeeper())
	if err != nil {
		t.Fatal(err)
	}
	if machine.Name() != known.Name || base64.RawURLEncoding.EncodeToString(machine.Public()) != known.Key || known.Alias != "build-box" || known.Service != "https://relay.example" {
		t.Fatalf("wrong pin: %+v", known)
	}
	allowed, ok, err := DeviceBook().Allows(client.Public())
	if err != nil || !ok || allowed.Label != "laptop" {
		t.Fatalf("admission: %+v %v %v", allowed, ok, err)
	}
	if raw, err := os.ReadFile(DeviceKeyPath()); err != nil || !bytes.Equal(raw, key) {
		t.Fatal("serve regenerated its key", err)
	}
	book := BookAt(filepath.Join(t.TempDir(), "machines.json"))
	if err := book.Remember(known); err != nil {
		t.Fatal(err)
	}
	if err := book.ForgetAlias("build-box"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := book.Machine("build-box"); err != nil || found {
		t.Fatalf("destroyed alias retained: %v %v", found, err)
	}
}
