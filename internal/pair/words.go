package pair

// The join words: three words two screens show, so a person can see that the
// device asking is the device they are holding.
//
// A NAME PROVES NOTHING HERE. The name a joining device gives itself is chosen
// by that device and is copied for free by anyone who has the six digits. What
// an impostor cannot copy is the Noise handshake it takes part in: the words are
// derived from the handshake hash, which covers both devices' ephemeral keys, so
// two sessions that differ in a single message show different words. The person
// compares two screens; agreeing screens mean one session.
//
// THEY ARE A CHECK ON THE CODE'S SHORTNESS AND NOT A SECOND SECRET. Six digits
// survive one attempt per code and a human look; three words from the machine
// name list are 24 bits, which is what the look has to be fooled out of.

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/relay"
	"golang.org/x/crypto/blake2s"
)

// joinWordsLabel keeps this derivation from ever colliding with another hash of
// a handshake, as every hash in this tree carries the label of what it is for.
const joinWordsLabel = "codeaf-join-words"

// wordsShown is how many words a person compares.
const wordsShown = 3

// joinWords is the words for one handshake, given its hash at the moment the
// joining device's first Noise message has been written and read.
func joinWords(handshakeHash []byte) string {
	sum := blake2s.Sum256(append([]byte(joinWordsLabel), handshakeHash...))
	words := make([]string, wordsShown)
	for i := range words {
		words[i] = relay.WordFor(sum[i])
	}
	return strings.Join(words, " ")
}
