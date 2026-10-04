// Package relayconf holds the one definition of "a relay works": the store and
// directory conformance suites, run over real signed HTTP against a live relay,
// and the pairing mailbox suite, run over plain HTTP. The self-hosted relay and
// any hosted one are held to it alike.
//
// The pairing suite gives every case a network of its own through
// X-Forwarded-For, so a live relay must be started with --trust-proxy (or with
// its pairing limits raised past what the suite spends from one address).
// Without it every case shares the tester's address and the rate cases fail
// on budget the earlier cases used.
//
// Its tests are behind the relayurl build tag, because they need a relay to
// talk to:
//
//	go test -tags relayurl ./internal/relayconf -relay-url=http://127.0.0.1:18787 -count=1
package relayconf
