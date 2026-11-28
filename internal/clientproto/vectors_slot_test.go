package clientproto

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/identity"
)

type cardCase struct {
	Name        string `json:"name"`
	IdentityPub string `json:"identity_pub_hex"`
	Card        Card   `json:"card"`
	SignText    string `json:"sign_text"`
	SignTextHex string `json:"sign_text_hex"`
	Valid       bool   `json:"valid"`
	Why         string `json:"why_invalid,omitempty"`
}

type slotCase struct {
	Name           string `json:"name"`
	SealedB64      string `json:"sealed_b64"`
	ExecutorBoxPub string `json:"executor_box_pub_hex"`
	PhoneBoxPriv   string `json:"phone_box_priv_hex"`
	FetchedAs      string `json:"fetched_as"`
	PhoneClient    string `json:"phone_client"`
	Writer         string `json:"writer"`
	LastAcceptedAt int64  `json:"last_accepted_at"`
	Opens          bool   `json:"opens"`
	Accepted       bool   `json:"accepted"`
	Why            string `json:"why,omitempty"`
}

func hx(b []byte) string { return hex.EncodeToString(b) }

// buildSlotVectors makes the card, sealed-card and slot-box vectors (design 3.3 and 3.4 as
// revised 2026-11-27). Every key and nonce is a fixed pattern so the file is reproducible.
func buildSlotVectors(t *testing.T) map[string]any {
	t.Helper()
	id := testIdentity(t, 1)
	idPub := id.PublicKey()
	otherIdentityPub := testIdentity(t, 9).PublicKey()
	dev := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, 32))
	devPub := dev.Public().(ed25519.PublicKey)
	cert := identity.IssueCert(id, devPub, time.UnixMilli(1700000000000))
	execPub, execPriv := keyPair(0x11)
	phoneBoxPub, phoneBoxPriv := keyPair(0x12)
	phoneSign := ed25519.NewKeyFromSeed(seed(2)).Public().(ed25519.PublicKey)
	phoneClient := DeviceIDOf(phoneSign)
	writer := cert.DeviceID()

	card := Card{V: 1, Writer: writer, Cert: cert, BoxPub: execPub[:], Name: "desk", At: 1700000000500}.Sign(dev)
	mkCard := func(name string, c Card, pub ed25519.PublicKey, why string) cardCase {
		return cardCase{Name: name, IdentityPub: hx(pub), Card: c, SignText: string(c.SignText()), SignTextHex: hx(c.SignText()),
			Valid: c.Verify(pub) == nil, Why: why}
	}
	unicode := Card{V: 1, Writer: writer, Cert: cert, BoxPub: execPub[:], Name: "büro é", At: 1700000000501}.Sign(dev)
	badName := func(n string) Card {
		return Card{V: 1, Writer: writer, Cert: cert, BoxPub: execPub[:], Name: n, At: 1700000000502}.Sign(dev)
	}
	tampered := func(f func(*Card)) Card { c := card; f(&c); return c }
	otherDev := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x32}, 32))
	attackerBox, _ := keyPair(0x66)
	cards := []cardCase{
		mkCard("valid", card, idPub, ""),
		mkCard("valid, a name with non-ASCII letters", unicode, idPub, ""),
		mkCard("invalid, name changed after signing", tampered(func(c *Card) { c.Name = "desk2" }), idPub, "signature"),
		mkCard("invalid, at changed after signing", tampered(func(c *Card) { c.At++ }), idPub, "signature"),
		mkCard("invalid, box_pub replaced after signing (what a substituting relay would do)", tampered(func(c *Card) { c.BoxPub = attackerBox[:] }), idPub, "signature"),
		mkCard("invalid, writer is not the certificate's device", tampered(func(c *Card) { c.Writer = DeviceIDOf(bytes.Repeat([]byte{7}, 32)) }), idPub, "writer"),
		mkCard("invalid, verified under another identity key", card, otherIdentityPub, "certificate"),
		mkCard("invalid, signed by a device key that is not the certificate's", card.Sign(otherDev), idPub, "signature"),
		mkCard("invalid, signature is the non-canonical S+L twin", tampered(func(c *Card) { c.Sig = malleate(c.Sig) }), idPub, "signature"),
		mkCard("invalid, name has a control character (correctly signed)", badName("a\u0007b"), idPub, "name"),
		mkCard("invalid, name is 33 runes (correctly signed)", badName("abcdefghijklmnopqrstuvwxyz0123456"), idPub, "name"),
	}

	sealedCard, err := SealCard(card, phoneBoxPub, bytes.NewReader(bytes.Repeat([]byte{0x41}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	cardJSON, _ := json.Marshal(card)

	plain := SlotPlain{V: SlotVersion, Slot: "agents.entries." + writer, Client: phoneClient, Executor: writer, At: 1700000001000, Ver: 1,
		Payload: []byte(`[{"kind":"text","text":"hello from desk"}]`)}
	var nonce [NonceSize]byte
	copy(nonce[:], bytes.Repeat([]byte{0x51}, NonceSize))
	sealed, err := SealSlot(plain, &nonce, phoneBoxPub, execPriv)
	if err != nil {
		t.Fatal(err)
	}
	plainJSON, _ := json.Marshal(plain)
	flip := func(b []byte, i int) []byte { c := append([]byte(nil), b...); c[i] ^= 1; return c }
	attackerPub, attackerPriv := keyPair(0x66)
	forged, err := SealSlot(plain, &nonce, phoneBoxPub, attackerPriv)
	if err != nil {
		t.Fatal(err)
	}
	slots := []slotCase{}
	add := func(name string, sealed []byte, execPub *[32]byte, fetchedAs, client, w string, last int64, why string) {
		p, err := OpenSlot(sealed, execPub, phoneBoxPriv)
		opens := err == nil
		slots = append(slots, slotCase{Name: name, SealedB64: base64.StdEncoding.EncodeToString(sealed), ExecutorBoxPub: hx(execPub[:]),
			PhoneBoxPriv: hx(phoneBoxPriv[:]), FetchedAs: fetchedAs, PhoneClient: client, Writer: w, LastAcceptedAt: last,
			Opens: opens, Accepted: opens && p.Check(fetchedAs, client, w, last) == nil, Why: why})
	}
	name := plain.Slot
	add("accepted, first slot from this computer", sealed, execPub, name, phoneClient, writer, 0, "")
	add("accepted, newer than the last accepted", sealed, execPub, name, phoneClient, writer, plain.At-1, "")
	add("refused, at equals the last accepted (replay)", sealed, execPub, name, phoneClient, writer, plain.At, "at")
	add("refused, at below the last accepted (rollback)", sealed, execPub, name, phoneClient, writer, plain.At+60000, "at")
	add("refused, served under another slot name", sealed, execPub, "agents.list."+writer, phoneClient, writer, 0, "inner slot name")
	add("refused, fetched for another phone", sealed, execPub, name, "dev_00000000000000000000000000000000", writer, 0, "inner client")
	add("refused, relay says another writer", sealed, execPub, name, phoneClient, "dev_11111111111111111111111111111111", 0, "inner executor")
	add("does not open, wrong executor key", sealed, attackerPub, name, phoneClient, writer, 0, "box")
	add("does not open, a ciphertext byte flipped", flip(sealed, len(sealed)-1), execPub, name, phoneClient, writer, 0, "box")
	add("does not open, a nonce byte flipped", flip(sealed, 0), execPub, name, phoneClient, writer, 0, "box")
	add("does not open, truncated", sealed[:30], execPub, name, phoneClient, writer, 0, "short")
	add("does not open, forged by a relay under its own key", forged, execPub, name, phoneClient, writer, 0, "box")

	var shared [32]byte
	box.Precompute(&shared, execPub, phoneBoxPriv)
	return map[string]any{
		"card": map[string]any{
			"label":     "codeaf-card-v1\\n",
			"sign_text": "label + writer + \\n + hex(box_pub) + \\n + decimal(at) + \\n + name",
			"verify":    "cert verifies under the identity key (hex signature over \"codeaf/device-cert/v1\\n\" + the cert JSON with sig empty); cert.device is the signing key; writer == dev_ + hex(sha256(cert device key)[:16]); the card signature verifies under that device key, strictly (a signature whose S is not below the group order is refused); name is 1 to 32 runes with no control characters; v == 1",
			"cases":     cards,
		},
		"card_slot": map[string]any{
			"about":              "a card travels in slot card.<writer> sealed ANONYMOUSLY to the phone box key (nacl SealAnonymous: ephemeral x25519 key, nonce = blake2b-24(eph_pub || phone_pub), box). The phone has no key for the computer yet; the card's own signature chain authenticates it.",
			"identity_pub_hex":   hx(idPub),
			"phone_box_pub_hex":  hx(phoneBoxPub[:]),
			"phone_box_priv_hex": hx(phoneBoxPriv[:]),
			"ephemeral_seed_hex": hx(bytes.Repeat([]byte{0x41}, 32)),
			"sealed_b64":         base64.StdEncoding.EncodeToString(sealedCard),
			"card_json":          string(cardJSON),
			"expect":             "opens to card_json, then Card verification under identity_pub_hex succeeds",
		},
		"slot_box": map[string]any{
			"about":                "every slot except the card is nonce(24) || nacl box (curve25519 + xsalsa20 + poly1305) between the computer's executor key and the phone's box key, so only those two can compute the shared key. There is no certificate and no signature in a slot (design 3.4, revised 2026-11-27 after the Hermes gate). The phone gets the executor public key ONLY from that computer's verified card.",
			"seal":                 "sealed = nonce || box.Seal(plaintext, nonce, phone_box_pub, executor_box_priv), a fresh random 24-byte nonce per slot",
			"open":                 "box.Open(sealed[24:], nonce = sealed[:24], executor_box_pub (from the card), phone_box_priv)",
			"plaintext":            "JSON object with exactly the keys v, slot, client, executor, at, ver, payload; payload is standard padded base64 of the entries (or outcomes) JSON array; v must be 1; unknown keys or trailing data are refused",
			"phone_rules":          "after opening: refuse unless slot == the name it was fetched as, client == the phone's own device id, executor == the slot's writer (the relay's writer field and the name suffix), and at > the last accepted at for that slot name; ver is recorded and never refused on in v1",
			"executor_box_pub_hex": hx(execPub[:]),
			"phone_box_pub_hex":    hx(phoneBoxPub[:]),
			"phone_box_priv_hex":   hx(phoneBoxPriv[:]),
			"nonce_hex":            hx(nonce[:]),
			"shared_key_hex":       hx(shared[:]),
			"plaintext_json":       string(plainJSON),
			"sealed_b64":           base64.StdEncoding.EncodeToString(sealed),
			"cases":                slots,
		},
	}
}

// The verdict each vector states is the one its name promises, so a name and the code cannot drift apart.
func TestSlotAndCardVectorVerdictsMatchTheirNames(t *testing.T) {
	v := buildSlotVectors(t)
	for _, c := range v["card"].(map[string]any)["cases"].([]cardCase) {
		if want := strings.HasPrefix(c.Name, "valid"); c.Valid != want {
			t.Errorf("card %q: verdict %v", c.Name, c.Valid)
		}
	}
	for _, c := range v["slot_box"].(map[string]any)["cases"].([]slotCase) {
		if want := strings.HasPrefix(c.Name, "accepted"); c.Accepted != want {
			t.Errorf("slot %q: accepted %v", c.Name, c.Accepted)
		}
		if want := !strings.HasPrefix(c.Name, "does not open"); c.Opens != want {
			t.Errorf("slot %q: opens %v", c.Name, c.Opens)
		}
	}
}
