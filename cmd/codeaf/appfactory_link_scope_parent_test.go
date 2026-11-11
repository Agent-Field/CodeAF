package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

func TestMintedInviteCannotExtendTheRequestedExpiryWithinClockSlack(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	expiry := now.Add(time.Hour)
	longer := expiry.Add(30 * time.Second)
	row := appwire.FactoryLink{CreatedAt: now, MaxUses: 1, Status: "open", ExpiresAt: &longer}
	if madeMatches(row, linkCreateBody{ExpiresAt: &expiry}, now, expiry) {
		t.Fatal("a wider explicit expiry was copied as the chosen scope")
	}
	row.ExpiresAt = &expiry
	if !madeMatches(row, linkCreateBody{ExpiresAt: &expiry}, now, expiry) {
		t.Fatal("exact requested expiry rejected")
	}
}

func TestMintedInviteDefaultDoesNotOutliveItsSevenDayLifetime(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	until := now.Add(tokens.HumanLinkTTL)
	longer := until.Add(30 * time.Second)
	row := appwire.FactoryLink{CreatedAt: now, MaxUses: 1, Status: "open", ExpiresAt: &longer}
	if madeMatches(row, linkCreateBody{}, now, until) {
		t.Fatal("a wider default lifetime was copied")
	}
	row.ExpiresAt = &until
	if !madeMatches(row, linkCreateBody{}, now, until) {
		t.Fatal("valid default lifetime rejected")
	}
	row.CreatedAt = time.Time{}
	if madeMatches(row, linkCreateBody{}, now, until) {
		t.Fatal("missing creation clock was accepted")
	}
}
