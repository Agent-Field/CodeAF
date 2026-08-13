package chat

import "strings"

// The COMPANION TURNS, and the one law that decides whether they stream.
//
// A room's own turn is not the only model text that lands in it. The head runs
// two other short turns against the same room, each on its own stream key so its
// deltas can never be mistaken for the reply the person is waiting on:
//
//   - `#delivered` — the absorption answer (internal/head/absorb.go). Work the
//     person commissioned has settled, and this turn says what came of it, in the
//     head's own voice, as an ordinary agent row in their room.
//   - `#receipt` — the receipt wake (internal/head/wake.go). A change the head
//     put in hand has been applied or refused, and this turn says what the
//     workforce made of it. Also an ordinary agent row in their room.
//   - `#aside` — the ephemeral ask (internal/head/aside.go). This one journals a
//     COLLAPSED STUB — `▸ asked → answered` — with both halves inside it as a
//     part. What lands on screen is a fold, and folds do not stream.
//
// THE LAW IS THE VISIBLE ROW AND NOT THE TURN. Any model-authored text that will
// render visibly on the surface that is open renders as its deltas arrive; text
// that arrives folded, collapsed, or into a room nobody has open arrives whole.
// The first two keys above end in a row of prose a reader will read, so they
// stream. The third ends in a shut disclosure, so it does not — and a surface
// that streamed it would be typing an aside's words into the main transcript,
// which is the exact bug the key was invented to prevent (8.2.9).
//
// Before this, ALL THREE POPPED. applyStream's session filter is an equality
// test against the open room, so every one of these keys — which is the room's
// id plus a suffix, by construction — failed it and was dropped. The deltas were
// already crossing the bridge, already carrying the right room in them, and the
// surface threw them away and then drew the durable row whole a second later.
// That is the whole of what "the answer just appears" meant here.

const (
	// companionAbsorb keys the absorption answer. It must equal
	// internal/head's absorbStreamSuffix; cmd/aforge pins the pair, because that
	// is the one package that imports both vocabularies (see
	// TestCompanionStreamKeysMatchTheHead).
	companionAbsorb = "#delivered"
	// companionReceipt keys the receipt wake, matching head's
	// receiptStreamSuffix.
	companionReceipt = "#receipt"
	// companionAside keys the ephemeral ask, matching head's asideStreamSuffix.
	// It is listed here PRECISELY SO IT CAN BE REFUSED: a suffix this table does
	// not know is refused too, but silently, and an exclusion nobody wrote down
	// is indistinguishable from an omission nobody noticed.
	companionAside = "#aside"
)

// companionDrawn maps each companion key onto whether its text becomes a row a
// reader reads. It is a table rather than a switch so the exclusions are as
// legible as the inclusions.
var companionDrawn = map[string]bool{
	companionAbsorb:  true,
	companionReceipt: true,
	companionAside:   false,
}

// streamRoom splits a stream key into the room it belongs to and the companion
// suffix that keyed it, if any.
//
// suffix is the LIVE REGION'S OWNER IDENTITY and not merely a flag: it is what
// [App.applyStream] compares a turn's owner against, so a companion's deltas can
// only ever grow the block that companion opened. Empty means the room's own
// turn, which is what an unsuffixed key — and the empty key every existing
// emitter uses for "no room asserted" — has always meant.
//
// drawn is false for a key this surface will not render: an aside, or a
// companion suffix invented after this build. Refusing the unknown one is
// deliberate and is the same reasoning headStreamKind states about unknown
// boundaries (cmd/aforge/chat.go) — a surface that guessed would draw a turn
// whose landing place it cannot know into the one place it is sure to be wrong.
func streamRoom(key string) (room, suffix string, drawn bool) {
	if key == "" {
		return "", "", true
	}
	hash := strings.LastIndex(key, "#")
	if hash < 0 {
		return key, "", true
	}
	suffix = key[hash:]
	render, known := companionDrawn[suffix]
	if !known {
		// A `#` in a room id is not a companion key. Room ids are minted as
		// `chat-<timestamp>` (newRoomID) and never carry one, so this is the
		// unknown-suffix arm and not a false positive — but treating it as the
		// room's own turn would put a future companion's words in the main
		// transcript, so it is refused instead.
		return key[:hash], suffix, false
	}
	return key[:hash], suffix, render
}

// StreamRoomOf is streamRoom's exported half, for the one place that can check
// the two vocabularies against each other. It answers the same three things.
func StreamRoomOf(key string) (room, suffix string, drawn bool) { return streamRoom(key) }
