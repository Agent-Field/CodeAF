package clientproto

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

const testNow = int64(1700000100000)

func sampleCall() CallPlain {
	return CallPlain{V: CallVersion, Client: "dev_phone", Number: 7, At: testNow - 1000, NotAfter: testNow - 1000 + CreateLifetime.Milliseconds(),
		Target: "dev_computer", Method: MethodCreate, Params: json.RawMessage(`{"client_id":"c0ffee00-0000-4000-8000-000000000001"}`)}
}

func outerOf(p CallPlain) OuterCall {
	return OuterCall{Number: p.Number, At: p.At, Method: p.Method, Target: p.Target}
}

// S8: a call opens only for the one computer it was sealed to, and any changed byte refuses it.
func TestCallOpensOnlyForTheComputerItWasSealedTo(t *testing.T) {
	pub, priv := keyPair(0x11)
	otherPub, otherPriv := keyPair(0x22)
	sealed, err := SealCall(sampleCall(), pub, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenCall(sealed, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 7 || got.Method != MethodCreate || string(got.Params) != string(sampleCall().Params) {
		t.Fatalf("the call changed in transit: %+v", got)
	}
	if _, err := OpenCall(sealed, otherPub, otherPriv); err == nil {
		t.Fatal("another computer must not open the call")
	}
	for _, i := range []int{0, 33, len(sealed) - 1} {
		c := append([]byte(nil), sealed...)
		c[i] ^= 1
		if _, err := OpenCall(c, pub, priv); err == nil {
			t.Errorf("a call with byte %d changed must not open", i)
		}
	}
}

// S7, S8: the computer refuses a call whose inner fields do not equal the outer signed ones, or that is not
// from the phone whose queue it came out of, or is not addressed to this computer.
func TestCallMustRepeatItsOuterBodyExactly(t *testing.T) {
	p := sampleCall()
	if err := p.Check(outerOf(p), "dev_phone", "dev_computer", testNow); err != nil {
		t.Fatalf("a good call must pass: %v", err)
	}
	cases := map[string]func(*OuterCall, *CallPlain, *string, *string){
		"another outer number":              func(o *OuterCall, _ *CallPlain, _, _ *string) { o.Number++ },
		"another outer time":                func(o *OuterCall, _ *CallPlain, _, _ *string) { o.At++ },
		"another outer method":              func(o *OuterCall, _ *CallPlain, _, _ *string) { o.Method = MethodSend },
		"another outer target (S7)":         func(o *OuterCall, _ *CallPlain, _, _ *string) { o.Target = "dev_other" },
		"another phone's queue (S8)":        func(_ *OuterCall, _ *CallPlain, client, _ *string) { *client = "dev_phone2" },
		"another computer (S7)":             func(_ *OuterCall, _ *CallPlain, _, self *string) { *self = "dev_other" },
		"a version other than one":          func(_ *OuterCall, c *CallPlain, _, _ *string) { c.V = 2 },
		"a number of zero":                  func(o *OuterCall, c *CallPlain, _, _ *string) { o.Number, c.Number = 0, 0 },
		"no method":                         func(o *OuterCall, c *CallPlain, _, _ *string) { o.Method, c.Method = "", "" },
		"a life that ends before it begins": func(_ *OuterCall, c *CallPlain, _, _ *string) { c.NotAfter = c.At },
		"a life longer than a day":          func(_ *OuterCall, c *CallPlain, _, _ *string) { c.NotAfter = c.At + CallLifetime.Milliseconds() + 1 },
	}
	for name, change := range cases {
		c, o, client, self := sampleCall(), outerOf(sampleCall()), "dev_phone", "dev_computer"
		change(&o, &c, &client, &self)
		if err := c.Check(o, client, self, testNow); err != ErrCall {
			t.Errorf("a call with %s: got %v, want ErrCall", name, err)
		}
	}
}

// S4: a call past not_after is told apart from a forged one, and never passes; one second early still does.
func TestCallPastNotAfterIsExpiredNotForged(t *testing.T) {
	p := sampleCall()
	if err := p.Check(outerOf(p), "dev_phone", "dev_computer", p.NotAfter); err != nil {
		t.Fatalf("a call at exactly not_after must still pass: %v", err)
	}
	if err := p.Check(outerOf(p), "dev_phone", "dev_computer", p.NotAfter+1); err != ErrCallExpired {
		t.Fatalf("a call a millisecond past not_after: got %v, want ErrCallExpired", err)
	}
	forged := p
	forged.Client = "dev_phone2"
	if err := forged.Check(outerOf(forged), "dev_phone", "dev_computer", p.NotAfter+1); err != ErrCall {
		t.Fatalf("a forged call must be a bad call even when it is also late: got %v", err)
	}
}

// S9: only the methods the owner may enroll are names a phone can be given.
func TestPhoneMethodsAreTheSixTheRunnerRoutes(t *testing.T) {
	want := []string{"agents.create", "agents.send", "agents.stop", "asks.answer", "inbox.answer", "tasks.start"}
	got := PhoneMethods()
	if len(got) != len(want) {
		t.Fatalf("PhoneMethods = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PhoneMethods[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The inner plaintext is exactly the keys the phone writes, in the phone's order: a computer refuses an unknown key.
func TestCallPlaintextIsStrict(t *testing.T) {
	raw, err := json.Marshal(sampleCall())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"client":"dev_phone","number":7,"at":1700000099000,"not_after":1700003699000,"target":"dev_computer","method":"agents.create","params":{"client_id":"c0ffee00-0000-4000-8000-000000000001"},"face_id":false}`
	if string(raw) != want {
		t.Fatalf("the call plaintext is\n%s\nwant\n%s", raw, want)
	}
	pub, priv := keyPair(0x11)
	extra := bytes.Replace(raw, []byte(`"face_id":false`), []byte(`"face_id":false,"extra":1`), 1)
	sealed, err := box.SealAnonymous(nil, extra, pub, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCall(sealed, pub, priv); err == nil {
		t.Fatal("a call with an unknown key must not open")
	}
}

// The reads a phone is enrolled for fit in the slots one computer may write, with room for the card and the outcomes.
func TestPhoneReadsFitTheComputersSlotBudget(t *testing.T) {
	reads := PhoneReads()
	if len(reads) > MaxComputerReads || len(reads)+2 > MaxComputerSlots {
		t.Fatalf("%d reads do not fit %d read slots and %d slots in all", len(reads), MaxComputerReads, MaxComputerSlots)
	}
	if len(reads) > MaxEnrollNames {
		t.Fatalf("an enrollment names at most %d reads, got %d", MaxEnrollNames, len(reads))
	}
}
