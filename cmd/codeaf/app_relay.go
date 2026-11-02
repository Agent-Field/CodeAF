package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func (b *appBackend) relayHome() string {
	if b.devices != nil {
		return b.devices.home
	}
	return home.Dir()
}
func (*appBackend) RelayKnown() bool { return true }

// The resolver owns precedence; these facts name which of its inputs won.
func appRelay(dir string) *appwire.Relay {
	resolved := syncsetup.Resolve(dir)
	source := "none"
	switch {
	case env.Get(syncsetup.URLVar) != "":
		source = "environment"
	case resolved.Hosted:
		source = "default"
	case resolved.URL != "":
		source = "paired"
	}
	url := resolved.URL
	if resolved.Off {
		url = ""
	}
	return &appwire.Relay{URL: url, Source: source, Off: resolved.Off}
}
