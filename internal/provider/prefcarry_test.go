package provider

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── WHETHER A BASE CARRIES A PREFERENCE IS LEARNED FROM THE BASE ────────────
//
// Issue #433. What is asserted here is the whole of the law, and every one of
// these scenarios runs against a PLAIN LOOPBACK ADDRESS with a model spelled
// like anybody's model: no `openrouter.ai` in the URL and no `openrouter/` on
// the id. Both of those used to be load-bearing — they were how this build
// decided whether a routing preference went on the wire at all — and a test
// that still wore either would be asserting against the costume rather than
// against the product.
//
// The behaviour they replace: on a proxy, a mirror, a self-hosted router or the
// shipped router reached by its IP, a person could rank lanes, pin one, and
// watch `pinned: X` go on standing in the settings panel while every request
// went out as `auto`. Nothing errored and nothing logged.

// plainModel is a model id with nothing router-shaped about it. It is spelled
// here rather than inline so that a reader cannot mistake it for an incidental
// choice: the spelling IS the assertion.
const plainModel = "acme/talk-v1"

// prefLanes is the scenario: two machines that answer, either of which a person
// might pin.
func prefLanes() []lanestub.Lane {
	quick := lanestub.Profile{TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 6, Tools: true}
	return []lanestub.Lane{
		{Name: "Harbor", Profile: quick},
		{Name: "Haven", Profile: quick},
	}
}

// plainBase starts a stub and points a client at whichever spelling of its
// address the test is about, with a model that is nobody's router.
//
// IT GIVES THE CLIENT ITS OWN STRIKE LEDGER for [stubbedRouter]'s reason: the
// shared one is process-wide, and a lane another test taught it about would
// arrive here as an order nobody asked for.
func plainBase(t *testing.T, base string, server *lanestub.Server) *Client {
	t.Helper()
	client, err := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: base,
		Model:   plainModel,
		Routing: StaticRouting(RoutingLatency),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	return client
}

// prefRig is a stub, a home nobody shares, and a clean set of parked sentences.
func prefRig(t *testing.T) *lanestub.Server {
	t.Helper()
	// NO TEST WRITES THE REAL HOME. The registry's own store lives under it.
	t.Setenv(home.EnvVar, t.TempDir())
	server := lanestub.New(plainModel, prefLanes()...)
	t.Cleanup(server.Close)
	forgotten(t)
	forgetUncarriedPins()
	t.Cleanup(forgetUncarriedPins)
	return server
}

// uncarried is the notes that are about a base which will not carry a pin.
func (l *noticeLog) uncarried() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var kept []string
	for _, line := range l.lines {
		if strings.Contains(line, "does not take a lane choice") {
			kept = append(kept, line)
		}
	}
	return kept
}

// ACCEPTANCE 1. A PINNED LANE ON A PLAIN LOOPBACK BASE REACHES THE WIRE.
//
// The stub serves an endpoints page, so it carries preferences by the router's
// own contract, and the demand goes out on the very first request — with no
// `openrouter.ai` anywhere near the base URL and no `openrouter/` on the model.
func TestAPinnedLaneReachesTheWireOnAPlainLoopbackBase(t *testing.T) {
	server := prefRig(t)
	client := plainBase(t, server.URL(), server)
	pinned(t, LanePin{Lane: "Harbor"})

	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	asks := server.Asks()
	if len(asks) != 1 {
		t.Fatalf("%d requests reached the base, want exactly the one turn: %+v", len(asks), asks)
	}
	if !demandedOnly(asks[0], "Harbor") {
		t.Fatalf("the request demanded %v, want the machine that was pinned", asks[0].Only)
	}
	if !lanes.PrefsCarried(server.URL()) {
		t.Fatal("a base that served an endpoints page is not believed to carry a preference")
	}
	if !BaseTakesLaneChoice() {
		t.Fatal("the settings row would say the pin is not taken on a base that took it")
	}
}

// ACCEPTANCE 2. THE SHIPPED ROUTER PAYS NO EXTRA REQUEST FOR THE LAW.
//
// Its hostname is the hint that skips the asking ([LaneSheetCertain] hands
// `known` to the sheet, which files the preference answer with it), so a turn
// against it costs exactly the turn: one completion, no endpoints fetch, no
// probe of any kind bought to discover a fact the hint already stated.
func TestTheShippedRouterPaysNoExtraRequestToLearnThatItCarriesAPreference(t *testing.T) {
	server := prefRig(t)
	client := plainBase(t, server.RouterURL(), server)
	pinned(t, LanePin{Lane: "Harbor"})

	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	if asks := server.Asks(); len(asks) != 1 || !demandedOnly(asks[0], "Harbor") {
		t.Fatalf("the shipped path sent %+v, want one request demanding the pinned machine", asks)
	}
	if fetched := server.Sheets(plainModel); fetched != 0 {
		t.Fatalf("the shipped path fetched the endpoints page %d times inside a turn, want none", fetched)
	}
}

// ACCEPTANCE 3. A BASE THAT REFUSES THE FIELD IS SAID ONCE, AND THE WORK GOES
// OUT ANYWAY.
//
// The stub answers `Unrecognized request argument supplied: provider` to any
// request carrying the object, which is OpenAI's own sentence for an argument
// it does not know. Three things are owed to the person and all three are here:
// the answer is learned, the request in hand is sent again without the
// preference rather than failing, and they are told once — in a line that names
// the machine they pinned and the address that would not take it.
func TestABaseThatRefusesTheFieldSaysSoOnceAndTheRequestStillGoesOut(t *testing.T) {
	server := prefRig(t)
	server.RefusesPreference()
	client := plainBase(t, server.URL(), server)
	pinned(t, LanePin{Lane: "Harbor"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	if _, err := client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the turn died on a refusal the widened retry was supposed to absorb: %v", err)
	}
	asks := server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests reached the base, want the refused one and the widened one: %+v", len(asks), asks)
	}
	if !demandedOnly(asks[0], "Harbor") {
		t.Fatalf("the first request demanded %v, want the pin — the base has to be ASKED", asks[0].Only)
	}
	if len(asks[1].Only) != 0 || asks[1].Sort != "" {
		t.Fatalf("the widened request still carried a preference: %+v", asks[1])
	}

	// AND THE NEXT TURN CARRIES NONE AT ALL. The answer is remembered, so the
	// 400 is paid once for the base rather than once a turn.
	if _, err := client.CompleteWithMessages(turn, userMessages("again")); err != nil {
		t.Fatalf("the second turn failed: %v", err)
	}
	for _, later := range server.Asks()[2:] {
		if len(later.Only) != 0 || later.Sort != "" {
			t.Fatalf("a request after the answer still carried a preference: %+v", later)
		}
	}

	// AND THE PERSON WAS TOLD ONCE, in one line naming both.
	said := notes.uncarried()
	if len(said) != 1 {
		t.Fatalf("the conversation was told %d times, want exactly once: %q", len(said), said)
	}
	if want := UncarriedPinLine("Harbor", server.URL()); said[0] != want {
		t.Fatalf("the conversation reads %q, want %q", said[0], want)
	}
	if !strings.Contains(said[0], "Harbor") {
		t.Fatalf("the line does not name the pin: %q", said[0])
	}
	if !BaseTakesLaneChoice() {
		return
	}
	t.Fatal("the settings row would still read a bare `pinned:` on a base that refused it")
}

// AND THE HONEST LIMIT, ASSERTED AS A LIMIT.
//
// A base that serves the completion and says NOTHING about which machine did it
// — a plain OpenAI-compatible endpoint, and this stub with [Server.Anonymous] —
// cannot be told apart from one that honoured the preference silently. There is
// no evidence in such an answer either way. This build takes the reading that
// produces a sentence rather than the one that produces a silence, and this is
// the test that pins that choice: the person is told, and the work still goes
// out.
func TestABaseThatNamesNoLaneIsReadAsNotCarryingAndTheReasonIsSaid(t *testing.T) {
	server := prefRig(t)
	server.Sheetless()
	server.Anonymous()
	client := plainBase(t, server.URL(), server)
	pinned(t, LanePin{Lane: "Haven"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	// THE FIRST TURN IS THE ASKING, and it carries the preference: there is no
	// other way to learn, and the request goes out either way.
	if _, err := client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the first turn failed: %v", err)
	}
	if asks := server.Asks(); len(asks) != 1 || !demandedOnly(asks[0], "Haven") {
		t.Fatalf("the first request was %+v, want one carrying the pin", asks)
	}
	if lanes.PrefsCarried(server.URL()) {
		t.Fatal("a base that named no lane is still believed to carry a preference")
	}

	// AND THE SECOND CARRIES NONE. The answer is remembered per base.
	if _, err := client.CompleteWithMessages(turn, userMessages("again")); err != nil {
		t.Fatalf("the second turn failed: %v", err)
	}
	asks := server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests reached the base, want two turns' worth: %+v", len(asks), asks)
	}
	if len(asks[1].Only) != 0 || asks[1].Sort != "" {
		t.Fatalf("the second request still carried a preference: %+v", asks[1])
	}
	if said := notes.uncarried(); len(said) != 1 {
		t.Fatalf("the conversation was told %d times over two turns, want once: %q", len(said), said)
	}
}

// AND THE ANSWER IS THE BASE'S, NOT THE CLIENT'S.
//
// A second client built against the same address in the same process inherits
// what the first one learned: it sends no preference and says nothing, because
// the fact is about the machine on the other end and the two clients are
// talking to one machine. A build that kept this per client would re-pay the
// discovery for every adapter a session constructs — the conversation's, a tool
// loop's own, every task node's — and say the same sentence once per adapter.
func TestTheAnswerIsLearnedOncePerBaseAndSurvivesASecondClient(t *testing.T) {
	server := prefRig(t)
	server.Sheetless()
	server.Anonymous()
	pinned(t, LanePin{Lane: "Haven"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	first := plainBase(t, server.URL(), server)
	if _, err := first.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the first client's turn failed: %v", err)
	}
	before := len(server.Asks())

	second := plainBase(t, server.URL(), server)
	if _, err := second.CompleteWithMessages(turn, userMessages("hello again")); err != nil {
		t.Fatalf("the second client's turn failed: %v", err)
	}
	asks := server.Asks()
	if len(asks) != before+1 {
		t.Fatalf("the second client sent %d requests, want one: %+v", len(asks)-before, asks[before:])
	}
	if len(asks[before].Only) != 0 || asks[before].Sort != "" {
		t.Fatalf("a second client re-asked a question this base has answered: %+v", asks[before])
	}
	if said := notes.uncarried(); len(said) != 1 {
		t.Fatalf("two clients said the sentence %d times, want once: %q", len(said), said)
	}
}

// AND A BASE THAT MOVES FORGETS. What one address answered is not evidence
// about another, which is the sheet's own law (internal/lane's wire) read
// through this question: a person who repoints AFORGE_BASE_URL at a real router
// after a proxy that dropped their pin gets their pin back.
func TestABaseThatMovesForgetsWhatTheOldOneAnswered(t *testing.T) {
	dropped := prefRig(t)
	dropped.Sheetless()
	dropped.Anonymous()
	pinned(t, LanePin{Lane: "Harbor"})

	client := plainBase(t, dropped.URL(), dropped)
	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the turn on the dropping base failed: %v", err)
	}
	if lanes.PrefsCarried(dropped.URL()) {
		t.Fatal("the base that named no lane is still believed to carry a preference")
	}

	carrying := lanestub.New(plainModel, prefLanes()...)
	t.Cleanup(carrying.Close)
	moved := plainBase(t, carrying.URL(), carrying)
	if _, err := moved.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the turn on the new base failed: %v", err)
	}
	if asks := carrying.Asks(); len(asks) != 1 || !demandedOnly(asks[0], "Harbor") {
		t.Fatalf("the new base was sent %+v, want the pin demanded afresh", asks)
	}
}

// AND SOMEBODY WHO PINNED NOTHING IS TOLD NOTHING.
//
// The law is about a person's own answer not reaching the wire. A session that
// named no machine has had nothing taken from it, and a note about a `provider`
// field they never asked for would be machinery talking — which this build's
// design laws forbid in anything a person reads.
func TestABaseThatCarriesNoPreferenceSaysNothingToSomebodyWhoPinnedNothing(t *testing.T) {
	server := prefRig(t)
	server.Sheetless()
	server.Anonymous()
	pinned(t, LanePin{})
	notes := &noticeLog{}
	client := plainBase(t, server.URL(), server)

	turn := WithStreamObserver(talking(), notes.observe)
	if _, err := client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	if _, err := client.CompleteWithMessages(turn, userMessages("again")); err != nil {
		t.Fatalf("the second turn failed: %v", err)
	}
	if said := notes.uncarried(); len(said) != 0 {
		t.Fatalf("somebody who pinned nothing was told %q", said)
	}
}

// AND THE HINT IS NOT THE DECISION, said as a unit.
//
// [Client.shippedRouterHint] is what is left of `isOpenRouter`, and it must
// answer about ONE MACHINE and never about whether a preference may be sent. A
// reader who wires a new gate to it would reopen the defect class of #373 and
// #433 both, so the two are pinned apart here.
func TestTheShippedRouterHintIsNotThePreferenceDecision(t *testing.T) {
	server := prefRig(t)
	plain := plainBase(t, server.URL(), server)
	if plain.shippedRouterHint() {
		t.Fatal("a loopback base reads as the shipped router")
	}
	if !plain.carriesPreferences() {
		t.Fatal("a base nobody has asked refuses a preference, so it can never be asked")
	}

	shipped := plainBase(t, server.RouterURL(), server)
	if !shipped.shippedRouterHint() {
		t.Fatal("the shipped router's own address does not read as it")
	}
	if !shipped.carriesPreferences() {
		t.Fatal("the shipped router does not carry a preference")
	}

	// AND AN EMPTY BASE IS NOT A BASE. Nothing was ever asked of it and nothing
	// can be, so it carries nothing. It is built by hand because [NewClient]
	// refuses a config with no base at all, which is the right refusal and is
	// not what this line is about.
	none := &Client{config: Config{APIKey: "test-key", Model: plainModel}}
	if none.carriesPreferences() {
		t.Fatal("a client with no base at all would put a preference on the wire")
	}
}

// AND THE QUESTION IS NEVER EVEN ASKED OF A ROUTER REFUSING ONE LANE, OF A
// RELAYED FAULT, OR OF A 404.
//
// Three structural facts keep the field's question apart from every other 400,
// and none of them is a word: the status, whose refusal it is, and whether the
// message names the machine the person pinned. `…provider.only preference
// permits only: harbor` and a gateway's `invalid provider name: harbor` are both
// the LANE being refused — #533's retirement is the answer to those, and
// widening on them would swallow the sentence the person is owed about their pin.
func TestTheFieldQuestionIsNotAskedOfARefusalAboutALane(t *testing.T) {
	server := prefRig(t)
	server.Sheetless()
	client := plainBase(t, server.URL(), server)
	pinned(t, LanePin{Lane: "Harbor"})
	client.prefWentOut(true)

	permits := []byte(`{"error":{"message":"No endpoints found. Your request's provider.only preference permits only: harbor","code":404}}`)
	if client.prefsMayBeRefused(plainModel, 404, permits) {
		t.Fatal("a router's 404 about its endpoints was read as a question about the field")
	}
	badName := []byte(`{"error":{"message":"invalid provider name: Harbor","code":400}}`)
	if client.prefsMayBeRefused(plainModel, 400, badName) {
		t.Fatal("a 400 naming the pinned machine was read as the field being unknown; the pin's own refusal would be swallowed")
	}
	relayed := []byte(`{"error":{"message":"Provider returned error","code":400,"metadata":{"provider_name":"Harbor"}}}`)
	if client.prefsMayBeRefused(plainModel, 400, relayed) {
		t.Fatal("a refusal the router RELAYED was read as the base's own")
	}

	// AND THE ONE THAT IS WORTH A ROUND TRIP: a 400 of the base's own that does
	// not name the machine. The WORDS are not what makes it so — an unfamiliar
	// sentence is asked exactly the same question.
	for _, body := range [][]byte{
		[]byte(`{"error":{"message":"Unrecognized request argument supplied: provider","code":400}}`),
		[]byte(`unknown field "provider"`),
		[]byte(`{"detail":[{"loc":["body","provider"],"msg":"Extra inputs are not permitted"}]}`),
		[]byte(`{"error":{"message":"something nobody has written a list entry for","code":400}}`),
	} {
		if !client.prefsMayBeRefused(plainModel, 400, body) {
			t.Fatalf("a base-own 400 was not worth asking about: %s", body)
		}
	}
}

// AND A BASE THAT HAS ALREADY SHOWN IT CARRIES IS NEVER ASKED, whatever a later
// refusal says: the first definite answer stands (internal/lane's prefAnswer),
// so the shipped router's own refusals are the retirement's business.
func TestABaseThatHasNamedItsLaneIsNotTalkedOutOfIt(t *testing.T) {
	server := prefRig(t)
	client := plainBase(t, server.URL(), server)
	pinned(t, LanePin{Lane: "Harbor"})
	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	unknown := []byte(`{"error":{"message":"Unrecognized request argument supplied: provider","code":400}}`)
	if client.prefsMayBeRefused(plainModel, 400, unknown) {
		t.Fatal("a base that had named its lane was asked whether it understands the field")
	}
	if !lanes.PrefsCarried(server.URL()) {
		t.Fatal("a base that named its lane stopped being believed")
	}
}

// AND A SECOND FAILURE TEACHES NOTHING.
//
// The retry is the test, so a base whose 400 was NOT about the field — a bad
// request this build would have made either way — must leave the question
// exactly where it was: the answer stays unasked, the next request carries the
// preference again, and nothing is said to anybody. A build that filed the
// refusal on the first 400 alone would retire a person's pin over an unrelated
// fault and never ask again.
func TestASecondFailureTeachesNothingAboutTheBase(t *testing.T) {
	server := prefRig(t)
	server.Sheetless()
	server.RefusesEverything()
	client := plainBase(t, server.URL(), server)
	pinned(t, LanePin{Lane: "Harbor"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	if _, err := client.CompleteWithMessages(turn, userMessages("hello")); err == nil {
		t.Fatal("a request the base refused twice answered successfully")
	}
	asks := server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests reached the base, want the refused one and the widened one: %+v", len(asks), asks)
	}
	if !demandedOnly(asks[0], "Harbor") || len(asks[1].Only) != 0 {
		t.Fatalf("the pair was not ask-then-widen: %+v", asks)
	}
	if !lanes.PrefsCarried(server.URL()) {
		t.Fatal("a base whose second refusal proved nothing was filed as refusing the field")
	}
	if said := notes.uncarried(); len(said) != 0 {
		t.Fatalf("the person was told %q about a base that established nothing", said)
	}
	// AND THE NEXT REQUEST ASKS AGAIN, because nothing was learnt.
	if _, err := client.CompleteWithMessages(turn, userMessages("again")); err == nil {
		t.Fatal("the second turn answered successfully")
	}
	if later := server.Asks()[2]; !demandedOnly(later, "Harbor") {
		t.Fatalf("the next turn went out bare on a question nobody answered: %+v", later)
	}
}

// AND NOBODY WHO PINNED NOTHING GETS A FIELD THEY NEVER HAD.
//
// THE LAW IS ABOUT A PREFERENCE THE PERSON HAS. `sort`, `allow_fallbacks` and
// `require_parameters` are this adapter's own knobs for breaking a tie among
// machines a ROUTER already knows about, and a base that has not shown it has
// any such machines has no tie to break. So on an unasked base with no pin the
// request is byte-for-byte the request it was before this law existed — and
// nothing is asked, because there is nothing to ask with.
func TestAnUnaskedBaseWithNoPinCarriesNoProviderObjectAtAll(t *testing.T) {
	server := prefRig(t)
	server.Sheetless()
	client := plainBase(t, server.URL(), server)
	pinned(t, LanePin{})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	for _, said := range []string{"hello", "again", "and again"} {
		if _, err := client.CompleteWithMessages(turn, userMessages(said)); err != nil {
			t.Fatalf("the turn %q failed: %v", said, err)
		}
	}
	asks := server.Asks()
	if len(asks) != 3 {
		t.Fatalf("%d requests reached the base, want three turns' worth: %+v", len(asks), asks)
	}
	for index, ask := range asks {
		if ask.Sort != "" || len(ask.Order) != 0 || len(ask.Only) != 0 || len(ask.Ignore) != 0 {
			t.Fatalf("request %d carried a preference nobody asked for: %+v", index, ask)
		}
	}
	if said := notes.uncarried(); len(said) != 0 {
		t.Fatalf("a base nobody asked anything of said %q", said)
	}
}

// AND THE MOMENT SOMEBODY PINS, THAT PIN IS THE ASKING. The two halves are one
// rule and are asserted together: no pin, no object; a pin, and the object goes
// out on the very next request.
func TestAPinOnAnUnaskedBaseIsTheAsking(t *testing.T) {
	server := prefRig(t)
	server.Sheetless()
	client := plainBase(t, server.URL(), server)

	pinned(t, LanePin{})
	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the unpinned turn failed: %v", err)
	}
	RepinLane(LanePin{Lane: "Haven"})
	t.Cleanup(func() { RepinLane(LanePin{}) })
	if _, err := client.CompleteWithMessages(talking(), userMessages("again")); err != nil {
		t.Fatalf("the pinned turn failed: %v", err)
	}
	asks := server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests reached the base: %+v", len(asks), asks)
	}
	if asks[0].Sort != "" || len(asks[0].Only) != 0 {
		t.Fatalf("the unpinned request carried %+v", asks[0])
	}
	if !demandedOnly(asks[1], "Haven") {
		t.Fatalf("the pinned request demanded %v, want the machine the person named", asks[1].Only)
	}
}
