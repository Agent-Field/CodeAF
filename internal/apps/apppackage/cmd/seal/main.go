// Command seal signs a codeaf release package using the private key on stdin.
package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return errors.New("usage: seal DEFINITION_DIR VERSION < signing-key.pem > release.codeaf-app")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
	if err != nil {
		return err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return errors.New("expected a PEM-encoded Ed25519 key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("expected a PKCS#8 Ed25519 key")
	}
	private, ok := key.(ed25519.PrivateKey)
	if !ok {
		return errors.New("expected an Ed25519 key")
	}
	def, err := schema.Load(os.Args[1])
	if err != nil {
		return err
	}
	if report := schema.Validate(def, nil); !report.OK() {
		return errors.New(report.String())
	}
	files := make(map[string]string, len(def.Files))
	for path, content := range def.Files {
		files[path] = string(content)
	}
	archive, err := apppackage.SealBuiltin(files, os.Args[2], private)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(archive)
	return err
}
