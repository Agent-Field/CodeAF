// Package agentdid names an agent with a decentralized identifier (DID).
//
// Every conversation (agent) gets one DID, minted once and kept with the
// conversation (internal/session's AgentIdentity). Commands the agent runs see
// it as $CODEAF_AGENT_DID, so a tool that records who did something can name
// the agent rather than a folder. A worker the agent starts for one task is
// the agent's DID with a fragment, `<did>#w<task id>`.
//
// THE SPELLING IS AGENTFIELD'S. AgentField names its agents with did:key over
// an Ed25519 public key: the multicodec prefix 0xed01 followed by the key,
// base64url without padding, after the `z` (control-plane's DIDService). Using
// the same spelling lets the two products read each other's identifiers.
//
// THE KEY IS NOT KEPT. A DID here is an identifier, not a credential: nothing
// signs with it today, so only the public half is minted and the private half
// is dropped at once. Nothing secret is ever written to disk for it.
package agentdid

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Env is the variable every command an agent runs carries its DID in.
const Env = "CODEAF_AGENT_DID"

// prefix is did:key with the multibase marker AgentField writes.
const prefix = "did:key:z"

// multicodec is the Ed25519 public key code, as an unsigned varint.
var multicodec = []byte{0xed, 0x01}

// New mints a fresh DID from random bytes.
func New() (string, error) { return newFrom(rand.Reader) }

func newFrom(random io.Reader) (string, error) {
	public, _, err := ed25519.GenerateKey(random)
	if err != nil {
		return "", fmt.Errorf("mint agent identity: %w", err)
	}
	return FromPublicKey(public), nil
}

// FromPublicKey spells the DID of one Ed25519 public key.
func FromPublicKey(public ed25519.PublicKey) string {
	key := append(append([]byte(nil), multicodec...), public...)
	return prefix + base64.RawURLEncoding.EncodeToString(key)
}

// Valid reports whether did is an agent DID this package could have minted.
// A worker DID (one with a fragment) is not: it names a worker, not an agent.
func Valid(did string) bool {
	rest, ok := strings.CutPrefix(did, prefix)
	if !ok {
		return false
	}
	key, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil || len(key) != len(multicodec)+ed25519.PublicKeySize {
		return false
	}
	return key[0] == multicodec[0] && key[1] == multicodec[1]
}

// Worker is the DID of the worker running task n for the agent named did.
// It is empty when there is no agent DID to hang it from.
func Worker(did string, n uint64) string {
	if did == "" || n == 0 {
		return did
	}
	return did + "#w" + strconv.FormatUint(n, 10)
}
