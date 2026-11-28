package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// errNoPhoneServer is returned when a phone approval is requested before this
// computer has a valid device key to sign an executor card.
var errNoPhoneServer = errors.New("this computer cannot serve a phone yet")

// phoneCardProvider is the seam for building this computer's signed executor card.
var phoneCardProvider = defaultPhoneCard

// phoneCard returns this computer's signed executor card for a phone grant.
func phoneCard(home string) (clientproto.Card, error) {
	return phoneCardProvider(home)
}

func defaultPhoneCard(home string) (clientproto.Card, error) {
	dev, err := identity.Device(home)
	if err != nil {
		return clientproto.Card{}, err
	}
	raw, err := os.ReadFile(filepath.Join(home, identity.DeviceFile))
	if err != nil {
		return clientproto.Card{}, err
	}
	var doc struct {
		Seed string `json:"device_seed"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return clientproto.Card{}, err
	}
	seed, err := hex.DecodeString(doc.Seed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return clientproto.Card{}, errNoPhoneServer
	}
	name := devname.Name(home)
	if !clientproto.ValidCardName(name) {
		name = "computer"
	}
	return clientrun.ExecutorCard(home, ed25519.NewKeyFromSeed(seed), dev.Cert, name, time.Now())
}
