// Package relayconf holds the one definition of "a relay works": the store and
// directory conformance suites, run over real signed HTTP against a live relay.
// The self-hosted relay and any hosted one are held to it alike.
//
// Its tests are behind the relayurl build tag, because they need a relay to
// talk to:
//
//	go test -tags relayurl ./internal/relayconf -relay-url=http://127.0.0.1:18787 -count=1
package relayconf
