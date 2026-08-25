package relay

// A MACHINE'S NAME IS DERIVED FROM ITS KEY, NEVER CHOSEN.
//
// The name is what a person types — `aforge chat --at otter-lamp-42` — so it has
// to be sayable, typable over a phone call, and short enough to read off a
// screen. But it is also what the relay matches a dial against, so if it were
// chosen there would be a land rush for the good ones and a way to sit on
// somebody else's. Deriving it from the machine's own long-term public key
// makes the name a FACT ABOUT THE MACHINE rather than a claim it registered,
// and lets the relay refuse a registration whose name and key do not agree
// without holding an account for anybody.
//
// WHAT THE NAME IS NOT is an identity. It is 22-odd bits of a hash: two words
// out of 256 and a two-digit number. Somebody who wants a key that hashes to
// YOUR name can grind for one in seconds. That is fine, and it is fine for a
// stated reason: the name is a rendezvous label and the trust is elsewhere. A
// dial that lands on the wrong machine fails the Noise handshake in
// internal/pair, because the surface pinned the key it paired with and this
// machine does not hold it. The squatter gets a name and a refused handshake.
// See [Names] for the whole of the policy, which the relay implements and this
// file's derivation is only the first half of.

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// nameSalt keeps this derivation from ever colliding with another use of the
// same key. Every hash of a device key in this tree carries the label of what
// it is being hashed FOR.
const nameSalt = "aforge relay name\x00"

// NameFor is the name a machine holding this public key registers under.
//
// The shape is two words and a two-digit number — `otter-lamp-42` — because
// that is a thing a person reads off one screen and types into another without
// writing it down. The number exists so that two machines whose keys collide on
// both words still differ, and so that the name never looks like an English
// phrase somebody might try to guess at.
func NameFor(publicKey []byte) string {
	sum := sha256.Sum256(append([]byte(nameSalt), publicKey...))
	// 10..99 rather than 0..99, because a leading zero is a digit people drop
	// when they retype a name from memory.
	return fmt.Sprintf("%s-%s-%d", nameWords[sum[0]], nameWords[sum[1]], 10+int(sum[2])%90)
}

// ValidName says whether a string could be a name this package ever minted. It
// is a cheap door on the dial path so that a garbage path never reaches the
// registration table, and it is deliberately a SHAPE check rather than a
// membership one: the relay does not hold a list of the names that exist, and
// asking it to would be asking it to hold a directory of everybody's machines.
func ValidName(name string) bool {
	if len(name) > 64 {
		return false
	}
	parts := strings.Split(name, "-")
	if len(parts) != 3 {
		return false
	}
	for _, word := range parts[:2] {
		if word == "" || len(word) > 12 {
			return false
		}
		for _, r := range word {
			if r < 'a' || r > 'z' {
				return false
			}
		}
	}
	if len(parts[2]) != 2 {
		return false
	}
	for _, r := range parts[2] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Names is the name-squatting policy, written down where the code that enforces
// it can be read beside it. It is a constant so that the relay's own operator
// documentation and the manual page quote the same words rather than two
// paraphrases that drift.
//
// THE POLICY IS FOUR RULES:
//
//  1. A name must agree with the key that claims it. The relay recomputes
//     [NameFor] over the offered key and refuses a registration where the two
//     disagree, so a name cannot be claimed by a machine that does not hold a
//     key for it.
//  2. Possession is proved, not asserted. The registering machine answers a
//     challenge that can only be answered with the private half of the key it
//     offered, so a name cannot be claimed by replaying somebody's public key.
//  3. A live registration is never taken over. While a machine is connected
//     under a name, a second machine offering the same name is refused — even
//     with a valid key. Whoever is there stays there until they leave.
//  4. The name is not the trust root, and the relay says so out loud. Grinding
//     a key whose hash lands on a given name costs seconds; it buys the name
//     and nothing else, because the surface dialling it has pinned the key it
//     paired with and the handshake fails against any other.
const Names = `a name is derived from the machine's own key, proved on registration, held while that machine is connected, and never a substitute for the pinned key the two ends actually trust`

// nameWords is 256 short words with no two that sound alike over a phone. Two
// of them and two digits is a little over 22 bits, which is a rendezvous label
// and never a secret — see the header of this file.
var nameWords = [256]string{
	"acorn", "alder", "amber", "anchor", "apple", "arbor", "arrow", "ash",
	"aspen", "atlas", "aurora", "autumn", "azure", "badger", "bamboo", "banjo",
	"basil", "beacon", "beetle", "birch", "bishop", "bison", "blossom", "bluff",
	"bobcat", "bonfire", "boulder", "bramble", "brass", "breeze", "bridge", "bronze",
	"brook", "buffalo", "burrow", "cabin", "cactus", "camel", "canary", "candle",
	"canoe", "canyon", "cardinal", "carrot", "cascade", "cedar", "cello", "chalk",
	"cherry", "chestnut", "chimney", "cinder", "citrus", "clover", "cobalt", "comet",
	"compass", "copper", "coral", "cottage", "cougar", "crane", "crater", "cricket",
	"crimson", "crocus", "cypress", "daffodil", "dahlia", "daisy", "dapple", "dawn",
	"delta", "denim", "dingo", "dolphin", "domino", "donkey", "dragon", "driftwood",
	"dune", "eagle", "ember", "emerald", "falcon", "fennel", "fern", "ferry",
	"fiddle", "finch", "flint", "flute", "forest", "fossil", "fountain", "foxglove",
	"gadget", "gallery", "garnet", "gazelle", "geode", "geyser", "ginger", "glacier",
	"glider", "granite", "grotto", "guitar", "gulch", "gypsum", "harbor", "harvest",
	"hazel", "heather", "hedge", "heron", "hickory", "hollow", "honey", "hornet",
	"ibis", "iceberg", "indigo", "iris", "island", "ivory", "jackal", "jasmine",
	"jasper", "jetty", "jigsaw", "juniper", "kayak", "kelp", "kestrel", "kettle",
	"kingfisher", "kite", "koala", "lagoon", "lamp", "lantern", "larch", "lattice",
	"lavender", "ledger", "lemon", "lichen", "lighthouse", "lilac", "linen", "lobster",
	"lotus", "lumber", "lupine", "lynx", "magnet", "magnolia", "mahogany", "mallard",
	"mango", "maple", "marble", "marigold", "marlin", "marsh", "meadow", "mesa",
	"meteor", "mica", "midnight", "mimosa", "mineral", "mint", "mirror", "mist",
	"mongoose", "moonlight", "moraine", "mosaic", "moss", "mulberry", "mustang", "nectar",
	"nettle", "nimbus", "nomad", "nutmeg", "oasis", "obsidian", "ocelot", "octopus",
	"olive", "onyx", "opal", "orchard", "orchid", "oriole", "osprey", "otter",
	"paddle", "pampas", "pansy", "papaya", "parsley", "pebble", "pelican", "pepper",
	"periwinkle", "petal", "pewter", "phoenix", "piano", "pigeon", "pilot", "pine",
	"pistachio", "plateau", "plover", "plum", "pollen", "pomelo", "poppy", "porcelain",
	"prairie", "puffin", "pumice", "quail", "quartz", "quiver", "rabbit", "raccoon",
	"radish", "rafter", "rapids", "raven", "redwood", "reef", "rhubarb", "ribbon",
	"river", "robin", "rosemary", "rudder", "saffron", "sage", "salmon", "sandal",
	"sapphire", "sardine", "satchel", "scarlet", "seagull", "sequoia", "shale", "willow",
}
