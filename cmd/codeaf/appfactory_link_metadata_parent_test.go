package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/redact"
)

func TestInvitationMetadataCannotReintroduceKnownSecretsBeforeTruncation(t *testing.T) {
	secret := "b17f9a5c019e43d276aef875a94c3d08d99100bcba710ee1217b9df78923f911"
	redact.Register(secret)
	for _, max := range []int{39, 80} {
		for _, value := range []string{secret, secret[:16] + "\n" + secret[16:]} {
			safe := cleanLinkText(value, max)
			if strings.Contains(safe, secret[:16]) {
				t.Fatal("known invitation secret entered public metadata")
			}
		}
	}
}

func TestInvitationMetadataNeverShowsAnUnregisteredJoinURL(t *testing.T) {
	value := "From codeaf://factory/join?url=https%3A%2F%2Ffixture.invalid#token=" + strings.Repeat("c", 64)
	safe := cleanLinkText(value, 80)
	if strings.Contains(strings.ToLower(safe), "codeaf://") || strings.Contains(safe, "#token") {
		t.Fatal("an invite address was truncated into public metadata")
	}
}
