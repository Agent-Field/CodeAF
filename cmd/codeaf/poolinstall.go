// The install's own identity at the relay: one nonce, drawn once and kept.
//
// Every batch the outbox sends to the relay rides the header
// X-Codeaf-Install with this word, so the relay can tell one install's
// measurements from another's without ever learning who the install is: the
// nonce is 32 random hex characters, held in a file only this install writes,
// and nothing else about the machine travels with it. The file is replaced,
// not repaired, when it stops being that word — a mangled file costs the
// install its old identity and nothing else.
package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// installFile is the one file an install's nonce is kept in, under the pool
// directory, beside the outbox and the own sheet.
const installFile = "install"

// nonceLen is 16 random bytes, spelled as 32 lowercase hex — the shape the
// relay's header check asks for.
const (
	nonceLen        = 16
	nonceHexLen     = 32
	installDirMode  = 0700
	installFileMode = 0600
)

// nonceWord is the only shape installNonce accepts back off the disk.
var nonceWord = regexp.MustCompile(`^[0-9a-f]{32}$`)

// poolPushBudget is the bound one push is given, at start-up and after a
// judged run alike: an errand nobody is waiting for gets one bound, the same
// bound at both doors so a slow relay cannot hold either open longer.
const poolPushBudget = 5 * time.Second

// installNonce answers this install's nonce: the word in <poolDir>/install,
// drawn from the system's randomness and written there the first time, mode
// 0600 in a directory of mode 0700. A file that is missing, or one that does
// not hold the one accepted shape, is replaced the same way, so a mangled
// file cannot wedge an install out of sending.
func installNonce(poolDir string) (string, error) {
	path := filepath.Join(poolDir, installFile)
	if raw, err := os.ReadFile(path); err == nil {
		if word := string(raw); nonceWord.MatchString(word) {
			return word, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	raw := make([]byte, nonceLen)
	if _, err := cryptorand.Read(raw); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(raw)
	if err := os.MkdirAll(poolDir, installDirMode); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(nonce), installFileMode); err != nil {
		return "", err
	}
	return nonce, nil
}

// poolPush hands the outbox's pending rows to the relay: the rows are the
// install's judged scores, and the relay wants the install's nonce beside
// them. Quiet by design, for poolindex.go's reason — a relay that does not
// answer yet, or a nonce that could not be drawn, is an ordinary state for an
// errand nothing waits on, so the error is said only under the debug record's
// switch and never at a person. The rows a batch did not reach stay pending,
// where the next judged run and the next start-up try them again.
func poolPush(ctx context.Context, profileDir string, cfg poolcfg.Config, budget time.Duration) {
	if !cfg.CanSend() {
		return
	}
	poolDir := config.ProfilePath(profileDir, "pool")
	box, err := outbox.Open(outboxPath(poolDir))
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: outbox: %v", err)
		}
		return
	}
	defer box.Close()
	nonce, err := installNonce(poolDir)
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: install nonce: %v", err)
		}
		return
	}
	box.Install = nonce
	box.Budget = budget
	if _, err := box.Send(ctx, cfg.SubmitURL); err != nil && trace.Enabled() {
		log.Printf("model pool: send: %v", err)
	}
}
