package main

// ── `codeaf pair --org https://org.example.com` ─────────────────────────────
//
// An organization that runs a relay for its members' computers (deploy/org) tells it to everyone signed in (GET /v1/org), so a
// member pairs their computers through it with the one address they already have, and not with a second one to look up:
//
//	laptop$  codeaf pair --org https://org.example.com --code
//	desktop$ codeaf pair <code> --via <the address the laptop printed>
//
// IT IS `--via` WITH THE ADDRESS LOOKED UP, AND NOTHING MORE. The pairing that follows is the one `--via` has always made: it
// remembers the relay, hands it to the computer it pairs, and from then on every start of those computers uses it. A computer
// that never says --org never asks an organization anything, so nobody's relay moves unless the person typed it; and
// CODEAF_SYNC_URL=off still stops the pairing, exactly as it stops `--via` (pairMailboxAt).
//
// THE ADDRESS IS READ WITH THE SIGN-IN THE PERSON ALREADY HAS (`codeaf attach`, CODEAF_ORG_TOKEN), over the door `--org` of
// `codeaf chat` uses, so one rule covers both: https anywhere, plain http only for this computer, no redirect followed.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// orgNoRelayWords is what an organization that runs no relay is asked about: it is not an error of the person's, so it says what
// to do instead.
func orgNoRelayWords(host string) string {
	return fmt.Sprintf("%s does not run a relay for your computers. Pair through another with --via <address>, or leave --org off to use the usual one.", host)
}

// orgPairRelay is the relay the organization at target runs for its members' computers.
func orgPairRelay(ctx context.Context, target string, say func(string)) (string, error) {
	door, err := newOrgDoor(target, say)
	if err != nil {
		return "", err
	}
	var profile struct {
		Relay string `json:"relay"`
	}
	if err := door.call(ctx, "GET", "/v1/org", &profile); err != nil {
		return "", err
	}
	return advertisedRelay(door.base, door.host, profile.Relay)
}

// advertisedRelay checks the address an organization named before a pairing is sent through it. The organization is a server the
// person was told to trust with a sign-in, not with where their computers go, so the address must be a web address, must not
// carry a sign-in of its own, and must not be plain http when the organization was reached over https: a server that could turn
// the lookup into an unprotected road would be choosing the person's security for them.
func advertisedRelay(orgBase, host, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New(orgNoRelayWords(host))
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s named a relay address this program cannot use.", host)
	}
	if u.Scheme == "http" && strings.HasPrefix(orgBase, "https://") {
		return "", fmt.Errorf("%s named a relay that is not https, so nothing was paired through it.", host)
	}
	return strings.TrimRight(u.String(), "/"), nil
}
