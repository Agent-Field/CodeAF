package clientproto

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
)

type grantCase struct {
	Name       string `json:"name"`
	SealedB64U string `json:"sealed_b64url"`
	LinkKeyHex string `json:"link_key_hex"`
	Device     string `json:"phone_device"`
	Code       string `json:"request_code"`
	Opens      bool   `json:"opens"`
	Accepted   bool   `json:"accepted"`
	Why        string `json:"why,omitempty"`
}

type callCase struct {
	Name      string `json:"name"`
	SealedB64 string `json:"sealed_b64"`
	OuterBody string `json:"outer_body"`
	QueueNo   int64  `json:"outer_number"`
	OuterAt   int64  `json:"outer_at"`
	Method    string `json:"outer_method"`
	OuterTo   string `json:"outer_target"`
	Client    string `json:"queue_client"`
	Self      string `json:"computer"`
	NowMS     int64  `json:"now_ms"`
	Verdict   string `json:"verdict"`
	PlainJSON string `json:"plaintext_json"`
	SeedHex   string `json:"ephemeral_seed_hex,omitempty"`
}

// buildCallVectors makes the grant, queued-call and outcome vectors (design 3.5, 3.6 and 3.8a as
// amended 2026-11-27). Every key, seed and nonce is a fixed pattern so the file is reproducible.
func buildCallVectors(t *testing.T) map[string]any {
	t.Helper()
	id := testIdentity(t, 1)
	idPub := id.PublicKey()
	dev := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, 32))
	devPub := dev.Public().(ed25519.PublicKey)
	devCert := identity.IssueCert(id, devPub, time.UnixMilli(1700000000000))
	execPub, execPriv := keyPair(0x11)
	card := Card{V: 1, Writer: devCert.DeviceID(), Cert: devCert, BoxPub: execPub[:], Name: "desk", At: 1700000000500}.Sign(dev)
	phoneSign := ed25519.NewKeyFromSeed(seed(2)).Public().(ed25519.PublicKey)
	phoneCert := identity.IssueCert(id, phoneSign, time.UnixMilli(1700000000000))
	phone := phoneCert.DeviceID()
	link := bytes.Repeat([]byte{0x05}, ClientLinkKeyBytes)
	code := "k7m2q9xd4a"

	grant := ClientGrant{V: GrantVersion, Device: phone, Code: code, Cert: phoneCert, IdentityPub: idPub,
		Relay: "https://codeaf.agentfield.ai/fabric", Executors: []Card{card}}
	var gnonce [NonceSize]byte
	copy(gnonce[:], bytes.Repeat([]byte{0x61}, NonceSize))
	sealedGrant, err := SealGrant(grant, &gnonce, link)
	if err != nil {
		t.Fatal(err)
	}
	grantJSON, _ := json.Marshal(grant)
	gk := GrantKey(link)
	flip := func(b []byte, i int) []byte { c := append([]byte(nil), b...); c[i] ^= 1; return c }
	otherLink := bytes.Repeat([]byte{0x06}, ClientLinkKeyBytes)
	evil := grant
	evil.Relay = "https://evil.example"
	evilSealed, err := SealGrant(evil, &gnonce, otherLink)
	if err != nil {
		t.Fatal(err)
	}
	b64u := base64.RawURLEncoding.EncodeToString
	var grants []grantCase
	addGrant := func(name string, sealed, key []byte, dev, code, why string) {
		g, err := OpenGrant(sealed, key)
		opens := err == nil
		accepted := opens && g.Check(dev, code, phoneSign) == nil
		grants = append(grants, grantCase{Name: name, SealedB64U: b64u(sealed), LinkKeyHex: hx(key), Device: dev, Code: code, Opens: opens, Accepted: accepted, Why: why})
	}
	addGrant("accepted, the phone's own request", sealedGrant, link, phone, code, "")
	addGrant("refused, the grant answers another request code", sealedGrant, link, phone, "zzzzzzzzzz", "inner code")
	addGrant("refused, the grant is for another phone", sealedGrant, link, DeviceIDOf(bytes.Repeat([]byte{7}, 32)), code, "inner device")
	addGrant("does not open, sealed by a relay under its own key", evilSealed, link, phone, code, "secretbox")
	addGrant("does not open, a ciphertext byte flipped", flip(sealedGrant, len(sealedGrant)-1), link, phone, code, "secretbox")
	addGrant("does not open, a nonce byte flipped", flip(sealedGrant, 0), link, phone, code, "secretbox")
	addGrant("does not open, truncated", sealedGrant[:30], link, phone, code, "short")
	addGrant("does not open, the phone's link key is another one", sealedGrant, otherLink, phone, code, "secretbox")

	// Queued calls: one per method the phone sends, each sealed with its own fixed ephemeral seed.
	computer := devCert.DeviceID()
	at := int64(1700000100000)
	mk := func(number int64, method, params string, life time.Duration) CallPlain {
		return CallPlain{V: CallVersion, Client: phone, Number: number, At: at + number, NotAfter: at + number + life.Milliseconds(),
			Target: computer, Method: method, Params: json.RawMessage(params)}
	}
	plains := []CallPlain{
		mk(1, MethodCreate, `{"client_id":"c0ffee00-0000-4000-8000-000000000001"}`, CreateLifetime),
		mk(2, MethodSend, `{"id":"chat-7","text":"say hello","client_id":"c0ffee00-0000-4000-8000-000000000002"}`, CallLifetime),
		mk(3, MethodAnswer, `{"agent":"chat-7","id":"ask-1","option":"yes","text":""}`, CallLifetime),
		mk(4, MethodStop, `{"id":"chat-7"}`, CallLifetime),
	}
	var calls []callCase
	sealOf := func(p CallPlain, seedByte byte) []byte {
		s, err := SealCall(p, execPub, bytes.NewReader(bytes.Repeat([]byte{seedByte}, 32)))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	addCall := func(name string, p CallPlain, o OuterCall, client, self string, now int64, sealed []byte, seedByte byte) {
		verdict := "ok"
		opened, err := OpenCall(sealed, execPub, execPriv)
		switch {
		case err != nil:
			verdict = "bad_call"
		default:
			switch opened.Check(o, client, self, now) {
			case nil:
			case ErrCallExpired:
				verdict = "expired"
			default:
				verdict = "bad_call"
			}
		}
		body, _ := json.Marshal(struct {
			Number int64  `json:"number"`
			At     int64  `json:"at"`
			Method string `json:"method"`
			Target string `json:"target"`
			Sealed []byte `json:"sealed"`
		}{o.Number, o.At, o.Method, o.Target, sealed})
		raw, _ := json.Marshal(p)
		calls = append(calls, callCase{Name: name, SealedB64: base64.StdEncoding.EncodeToString(sealed), OuterBody: string(body), QueueNo: o.Number, OuterAt: o.At,
			Method: o.Method, OuterTo: o.Target, Client: client, Self: self, NowMS: now, Verdict: verdict, PlainJSON: string(raw), SeedHex: seedHexOf(seedByte)})
	}
	for i, p := range plains {
		sealed := sealOf(p, 0x42+byte(i))
		addCall("ok, "+p.Method, p, outerOf(p), phone, computer, at+60000, sealed, 0x42+byte(i))
	}
	create := plains[0]
	sealedCreate := sealOf(create, 0x42)
	tweak := func(f func(*OuterCall)) OuterCall { o := outerOf(create); f(&o); return o }
	addCall("expired, a create one millisecond past not_after", create, outerOf(create), phone, computer, create.NotAfter+1, sealedCreate, 0x42)
	addCall("bad_call, outer target changed in transit", create, tweak(func(o *OuterCall) { o.Target = "dev_00000000000000000000000000000000" }), phone, computer, at+60000, sealedCreate, 0x42)
	addCall("bad_call, outer number changed", create, tweak(func(o *OuterCall) { o.Number++ }), phone, computer, at+60000, sealedCreate, 0x42)
	addCall("bad_call, outer method changed", create, tweak(func(o *OuterCall) { o.Method = MethodSend }), phone, computer, at+60000, sealedCreate, 0x42)
	addCall("bad_call, sealed body copied into another phone's envelope", create, outerOf(create), "dev_11111111111111111111111111111111", computer, at+60000, sealedCreate, 0x42)
	addCall("bad_call, a ciphertext byte flipped", create, outerOf(create), phone, computer, at+60000, flip(sealedCreate, len(sealedCreate)-1), 0)
	long := mk(5, MethodSend, `{"id":"chat-7","text":"x"}`, CallLifetime+time.Millisecond)
	addCall("bad_call, a life longer than a day", long, outerOf(long), phone, computer, at+60000, sealOf(long, 0x50), 0x50)

	// The outcome slot: the payload and the slot box it travels in.
	outcomes := []Outcome{
		{Number: 1, Status: OutcomeDone, Agent: "chat-7"},
		{Number: 2, Status: OutcomeDone, Agent: "chat-7"},
		{Number: 3, Status: OutcomeRefused, Reason: ReasonNotHolder, Agent: "chat-9"},
		{Number: 4, Status: OutcomeRefused, Reason: ReasonMethodDenied},
		{Number: 5, Status: OutcomeExpired, Reason: ReasonExpired},
	}
	payload, err := EncodeOutcomes(outcomes)
	if err != nil {
		t.Fatal(err)
	}
	phoneBoxPub, _ := keyPair(0x12)
	slot := SlotPlain{V: SlotVersion, Slot: "outcomes." + computer, Client: phone, Executor: computer, At: 1700000200000, Ver: 1, Payload: payload}
	var onone [NonceSize]byte
	copy(onone[:], bytes.Repeat([]byte{0x52}, NonceSize))
	_, ePriv := keyPair(0x11)
	sealedSlot, err := SealSlot(slot, &onone, phoneBoxPub, ePriv)
	if err != nil {
		t.Fatal(err)
	}
	slotJSON, _ := json.Marshal(slot)

	return map[string]any{
		"grant": map[string]any{
			"about":          "the grant the approving computer hands the phone (design 5.1 step 4.3, 3.8a): sealed under a key derived from the link key, so a relay that never sees the link key cannot read, forge or swap it. It travels in the link request's grant field as base64url without padding.",
			"k_grant":        "sha256(\"codeaf/phone-grant/v1\\n\" || link_key), link_key the 16 raw bytes behind the token's 22 characters",
			"seal":           "sealed = nonce(24) || nacl secretbox(plaintext, nonce, k_grant) (xsalsa20 + poly1305), a fresh random nonce per grant",
			"open":           "secretbox.Open(sealed[24:], nonce = sealed[:24], k_grant)",
			"plaintext":      "JSON object with exactly the keys v, device, code, cert, identity_pub, relay, executors; v must be 2; identity_pub is standard padded base64 of the 32-byte identity PUBLIC key; cert is the phone's own device certificate; executors holds 1 to 4 cards; unknown keys or trailing data are refused. It never contains a seed, a dedup secret or a cell key.",
			"phone_rules":    "after opening: refuse unless device == the phone's own device id, code == the code the relay gave this request, cert.device is the phone's own sign key and the cert verifies under identity_pub, relay is not empty and at least one card verifies under identity_pub (cards that fail are dropped); keep nothing until every check passes. The phone stores relay as the address to use; it should refuse a grant whose relay names another host than the address it made the request at unless it was set up from a setup link that names it.",
			"max_bytes":      MaxGrantBytes,
			"link_key_hex":   hx(link),
			"k_grant_hex":    hx(gk[:]),
			"nonce_hex":      hx(gnonce[:]),
			"phone_device":   phone,
			"request_code":   code,
			"plaintext_json": string(grantJSON),
			"sealed_b64url":  b64u(sealedGrant),
			"cases":          grants,
		},
		"call": map[string]any{
			"about":          "the queued call (design 3.5): outer body {number, at, method, target, sealed} signed with codeaf-req-v1; sealed is the plaintext below anonymously sealed to ONE computer's executor box key (nacl SealAnonymous: ephemeral x25519 key, nonce = blake2b-24(eph_pub || computer_pub), box).",
			"plaintext":      "JSON object with exactly the keys v (1), client, number, at, not_after, target, method, params, face_id, in this order; params is the method's own JSON object",
			"computer_rules": "after opening: refuse (bad_call) unless client == the phone whose queue the entry came from, target == this computer, number, at, method and target equal the outer signed body, number > 0, not_after > at and not_after - at <= 86400000; then, if now > not_after the call is expired, never run. not_after is at + 3600000 for agents.create and at + 86400000 for every other method.",
			"params": map[string]any{
				"agents.create": "{\"client_id\": \"<uuid>\"}: nothing else. The same client_id from the same phone is the same chat, so a retry under another number maps to the chat the first one made. The chat is started on the computer in `target`, in its default folder, with its default model; the first message follows as agents.send.",
				"agents.send":   "{\"id\": \"<chat id>\", \"text\": \"...\", \"client_id\": \"<uuid>\"}: client_id is echoed on the person's entry, so the phone finds its own message again.",
				"asks.answer":   "{\"agent\": \"<chat id>\", \"id\": \"<ask id>\", \"option\": \"<option>\", \"text\": \"...\"}: option and text may be empty strings but not both.",
				"agents.stop":   "{\"id\": \"<chat id>\"}",
			},
			"executor_box_pub_hex":  hx(execPub[:]),
			"executor_box_priv_hex": hx(ePriv[:]),
			"reproduce":             "a case with ephemeral_seed_hex is reproducible: sealed_b64 is SealAnonymous(plaintext_json, executor_box_pub_hex) with that 32-byte ephemeral private key; a case without one was changed after sealing",
			"cases":                 calls,
		},
		"outcomes": map[string]any{
			"about":                "the outcome slot (design 3.6): a computer publishes one outcome for EVERY queued call it acknowledges, the last 32 in the slot outcomes.<writer>, oldest first. Statuses: done, refused, expired. Reasons: not_holder, needs_face_id, expired, method_denied, bad_target, bad_call (a call that did not open, did not match its outer body or carried params the method does not take). A transient failure publishes nothing: the call stays queued and is tried again.",
			"payload_json":         string(payload),
			"plaintext_json":       string(slotJSON),
			"nonce_hex":            hx(onone[:]),
			"sealed_b64":           base64.StdEncoding.EncodeToString(sealedSlot),
			"opens_with":           "the slot box of design 3.4: executor_box_pub_hex below, phone_box_priv_hex 0x12 x 32",
			"executor_box_pub_hex": hx(execPub[:]),
			"phone_box_pub_hex":    hx(phoneBoxPub[:]),
		},
	}
}

// seedHexOf is the 32-byte ephemeral seed a test case sealed with, or empty when the sealed bytes were changed afterwards.
func seedHexOf(b byte) string {
	if b == 0 {
		return ""
	}
	return hx(bytes.Repeat([]byte{b}, 32))
}

// K1: the verdict each grant and call vector states is the one its name promises, so a name and the code cannot drift apart.
func TestGrantAndCallVectorVerdictsMatchTheirNames(t *testing.T) {
	v := buildCallVectors(t)
	for _, c := range v["grant"].(map[string]any)["cases"].([]grantCase) {
		if want := len(c.Name) >= 8 && c.Name[:8] == "accepted"; c.Accepted != want {
			t.Errorf("grant %q: accepted %v", c.Name, c.Accepted)
		}
		if want := !(len(c.Name) >= 8 && c.Name[:8] == "does not"); c.Opens != want {
			t.Errorf("grant %q: opens %v", c.Name, c.Opens)
		}
	}
	for _, c := range v["call"].(map[string]any)["cases"].([]callCase) {
		want := "ok"
		switch {
		case strings.HasPrefix(c.Name, "expired"):
			want = "expired"
		case strings.HasPrefix(c.Name, "bad_call"):
			want = "bad_call"
		}
		if c.Verdict != want {
			t.Errorf("call %q: verdict %q, want %q", c.Name, c.Verdict, want)
		}
	}
	if payload := v["outcomes"].(map[string]any)["payload_json"].(string); payload == "" {
		t.Error("the outcomes vector must carry a payload")
	} else if _, err := DecodeOutcomes([]byte(payload)); err != nil {
		t.Errorf("the outcomes vector payload must decode: %v", err)
	}
}
