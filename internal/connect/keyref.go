package connect

// THE OTHER THING A PERSON CAN PASTE: the NAME of an environment variable,
// instead of the key itself.
//
// A key pasted whole is written to the store file and read back from it forever
// (key.go). That is the right shape for somebody who was handed a key once and
// wants to stop thinking about it, and the wrong shape for three kinds of person
// this program keeps meeting:
//
//   - the one whose keys already live in their shell, their direnv file or their
//     secret manager, and who would have to copy each of them into a second file
//     to use aforge at all;
//   - the one on a machine they share, or a machine whose disk they do not own,
//     for whom "aforge wrote my Stripe key down" is the reason they stop here;
//   - the one who rotates. A key in an environment is rotated where it is set. A
//     key copied into a store file is rotated twice, and the second time is the
//     one they forget.
//
// So a pasted value that reads as `$NAME` or `${NAME}` is stored AS THE
// REFERENCE. Nothing resolves it at connect time except the one check that
// proves the key works; every request afterwards reads the variable again, so a
// key rotated in the environment is rotated here with no further step.
//
// ── THE REFERENCE IS MARKED, NEVER GUESSED AT ──
//
// It is written to its own field ([stored.KeyEnv]) and the key field is left
// empty. A store that kept `$STRIPE_KEY` in the same field as a literal key
// would have to guess, on every read, which of the two a value that begins with
// a dollar is — and the day somebody's real key begins with one, that guess
// sends a variable name to a payment processor. One field per meaning, and no
// reading of a value decides what it is.
//
// ── AND A MISSING VARIABLE IS A REFUSAL, NOT A SILENCE ──
//
// A connection whose variable is not set is not a connection that quietly signs
// nothing: it says which variable, by name, in the sentence the caller shows.
// The alternative is a request that goes out unsigned and comes back as
// somebody else's 401, which is the far end explaining our own bookkeeping.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// envReferencePattern is the two shapes a reference may be written in, and no
// third. `$NAME` is what a person types; `${NAME}` is what they paste out of a
// script. The name itself is the shape every shell agrees on for an exported
// variable — a letter or an underscore, then letters, digits and underscores —
// and it is UPPER CASE because that is what an environment variable is called
// everywhere a person has seen one, and because a lower-case word after a dollar
// is far more likely to be part of a key than the name of anything.
var envReferencePattern = regexp.MustCompile(`^\$(?:([A-Z_][A-Z0-9_]*)|\{([A-Z_][A-Z0-9_]*)\})$`)

// envReference reads one pasted answer as the name of an environment variable,
// or says it is not one.
//
// Anything that is not exactly one of the two shapes is a literal key, including
// `$` alone, `$name`, `${NAME` and `$NAME extra`. THE DEFAULT IS ALWAYS THE
// LITERAL: a value this cannot read is a key, which is the reading that can only
// fail at the far end, while the other way round writes a person's key into a
// field that means "the name of a variable".
func envReference(answer string) (string, bool) {
	found := envReferencePattern.FindStringSubmatch(strings.TrimSpace(answer))
	if found == nil {
		return "", false
	}
	if found[1] != "" {
		return found[1], true
	}
	return found[2], true
}

// envReferenceWord is a reference as a screen says it out loud: the dollar and
// the name, which is what the person typed and what they will go and look for
// when it is not set.
func envReferenceWord(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return "$" + name
}

// secret is the key one stored entry signs with: the value the person pasted, or
// whatever the variable they named holds RIGHT NOW.
//
// The variable is read at every use and never cached. That is the whole point of
// the reference — a key rotated in the environment is rotated here — and the
// cost of it is one map lookup per client, which is nothing beside the request
// the client is about to make.
//
// name is the service as a person reads it, for the one sentence this can fail
// with. THE SENTENCE NAMES THE VARIABLE AND NOT THE KEY: what is missing is a
// variable, the person can go and set it, and nothing about the key itself is
// known here or worth saying.
func (s stored) secret(name string) (string, error) {
	if variable := strings.TrimSpace(s.KeyEnv); variable != "" {
		if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
			return value, nil
		}
		return "", fmt.Errorf("%s reads its key from %s, and nothing is set there",
			name, envReferenceWord(variable))
	}
	return s.Key, nil
}
