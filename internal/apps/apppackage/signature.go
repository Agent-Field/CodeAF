package apppackage

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const BuiltinPublisher = "codeaf"
const signatureDomain = "codeaf app package v1\x00"

// Release archives are signed outside the engine. The verification key is
// public so an embedded or imported archive goes through the same check.
const builtinPublicKey = "2f5079f91f328b441581125d98b084a0c29079fed966ec2c3679f7a6d33b0062"

// SealBuiltin signs a built-in archive for release. The caller provides the
// private key from the release environment.
func SealBuiltin(files map[string]string, version string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("a release signing key is required")
	}
	raw, err := pack(files, nil, PackOptions{Publisher: BuiltinPublisher, Version: version})
	if err != nil {
		return nil, err
	}
	var a Archive
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	a.Signature = ed25519.Sign(privateKey, a.signatureMessage())
	if err := a.verifyPublisher(); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(a)
	if len(raw) > maxArchive {
		return nil, fmt.Errorf("the signed app package exceeds %d MiB", maxArchive>>20)
	}
	return raw, err
}

func (a Archive) signatureMessage() []byte {
	return []byte(signatureDomain + a.Manifest.Hash)
}

func (a Archive) verifyPublisher() error {
	if a.Manifest.Publisher != BuiltinPublisher {
		if len(a.Signature) != 0 {
			return fmt.Errorf("this package has an unknown signing publisher")
		}
		return nil
	}
	publicKey, _ := hex.DecodeString(builtinPublicKey)
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, a.signatureMessage(), a.Signature) {
		return fmt.Errorf("the codeaf package has an invalid release signature")
	}
	return nil
}
