package config

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// The fixtures here are UNIT DATA: keys are this test's own strings, refusals
// are written into the temporary profile's own router log as a refused seat
// call writes them, and nothing reaches a network or a model.

const (
	crewOldKey = "sk-or-v1-crewtest-0123456789"
	crewNewKey = "sk-or-v1-crewtest-reconnected-9876543210"
)

// glmOnOpenRouter is the pinned worker's pick, as a decision carries it.
func glmOnOpenRouter() (crewroute.Decision, crewroute.Pick) {
	pick := crewroute.Pick{Seat: crewroute.Worker, Model: "z-ai/glm-5.3-flash", Provider: "openrouter", Send: "z-ai/glm-5.3-flash", Kind: crewroute.Metered}
	return crewroute.Decision{Class: crewroute.OpenEnded, Crew: []crewroute.Pick{pick}}, pick
}

// refuseOnOpenRouter writes one refused first call on openrouter's GLM route,
// as a task's seat does: the dispatch fact taken as the call is sent, the
// refusal written with it.
func refuseOnOpenRouter(t *testing.T, dir, kind string) {
	t.Helper()
	refuseSent(t, dir, kind, CrewDispatchFor(dir, "openrouter"))
}

// refuseSent writes a refusal of a call sent under the dispatch fact sent.
func refuseSent(t *testing.T, dir, kind string, sent CrewDispatch) {
	t.Helper()
	d, pick := glmOnOpenRouter()
	LogCrewRouteSent(dir, "call-"+kind+"-"+time.Now().Format("150405.000000000"), d, dir, "fixture", crewroute.Worker, pick, kind, time.Time{}, sent)
}

// refuseAsAnOldBuild writes the refusal exactly as a build before prints wrote
// it: straight into the router's log, with no credential named.
func refuseAsAnOldBuild(t *testing.T, dir, kind string) {
	t.Helper()
	d, pick := glmOnOpenRouter()
	router.LogCrewRoute(ProfilePath(dir, ""), "old-"+kind+"-"+time.Now().Format("150405.000000000"), CrewRecordOf(d, dir, "fixture"), router.CrewRouteOutcome{
		Seat: string(crewroute.Worker), Send: pick.Send, Provider: pick.Provider, Paid: true, Kind: kind,
	})
}

// reopen forgets everything this process keeps of the profile between reads,
// as closing codeaf and opening it again does. What survives is on disk.
func reopen() { forgetCrewHealth() }

// pinnedWorkerAdmits answers whether a task admits with the worker pinned to
// GLM on openrouter, and fails the test if it admitted on anything else.
func pinnedWorkerAdmits(t *testing.T, dir string) bool {
	t.Helper()
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	var refusal *crewroute.NoEligibleError
	if errors.As(err, &refusal) {
		if refusal.Seat != crewroute.Worker {
			t.Fatalf("refused the %s, want the pinned worker", refusal.Seat)
		}
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	if w := d.Seat(crewroute.Worker); !w.Pinned || w.Model != "z-ai/glm-5.3-flash" || w.Provider != "openrouter" {
		t.Fatalf("the pinned worker was substituted: %+v", w)
	}
	return true
}

func pinWorkerOnOpenRouter(t *testing.T, dir string) {
	t.Helper()
	applyTaskModels(t, dir, TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Worker: {Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}}})
}

// A REFUSED KEY HOLDS A PIN BACK UNTIL A DIFFERENT KEY IS CONNECTED — the
// same key written again changes nothing — and a refusal of the new key holds
// it back again. Read back from disk, the lift stands.
func TestARefusedKeyHoldsAPinUntilANewCredentialIsConnected(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	if !pinnedWorkerAdmits(t, dir) {
		t.Fatal("a healthy pin was refused")
	}
	refuseOnOpenRouter(t, dir, "auth")
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("a pin whose key was just refused admitted")
	}
	if err := WriteAPIKey(dir, crewOldKey); err != nil {
		t.Fatal(err)
	}
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("the refused key, written again, lifted its own refusal")
	}
	if err := WriteAPIKey(dir, crewNewKey); err != nil {
		t.Fatal(err)
	}
	if !pinnedWorkerAdmits(t, dir) {
		t.Fatal("a new key did not lift the old key's refusal")
	}
	forgetCrewHealth()
	if !pinnedWorkerAdmits(t, dir) || !CrewRouteAnswers(dir, "z-ai/glm-5.3-flash") {
		t.Fatal("the lift did not survive reading the profile again")
	}
	// The log holds prints, never a key.
	raw, err := os.ReadFile(ProfilePath(dir, "router-events.jsonl"))
	if err == nil && (strings.Contains(string(raw), crewOldKey) || strings.Contains(string(raw), crewNewKey)) {
		t.Fatal("the router log carries a key")
	}
	refuseOnOpenRouter(t, dir, "auth")
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("a refusal of the new key did not hold the pin back")
	}
}

// A KEY THE APP HANDS OVER IS A NEW CREDENTIAL TOO, seen at once even by the
// cached reading auxiliary calls use.
func TestAnAppHandedKeyLiftsTheOldKeysRefusal(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	refuseOnOpenRouter(t, dir, "auth")
	if CrewRouteAnswers(dir, "z-ai/glm-5.3-flash") || pinnedWorkerAdmits(t, dir) {
		t.Fatal("a refused key still answers")
	}
	keys := NewAppCredentials()
	t.Cleanup(UseAppCredentials(keys))
	keys.Set("openrouter", crewNewKey)
	if !CrewRouteAnswers(dir, "z-ai/glm-5.3-flash") || !pinnedWorkerAdmits(t, dir) {
		t.Fatal("the app's new key did not lift the refusal")
	}
}

// A POSITIVE CREDIT READING LIFTS AN EARLIER PAYMENT REFUSAL; a fresh reading
// of no credit keeps it, and a payment refusal after the reading holds again.
func TestAPositiveCreditReadingLiftsAnEarlierPaymentRefusal(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	refuseOnOpenRouter(t, dir, "payment")
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("an account out of credit admitted its pin")
	}
	if err := WriteCreditsReading(dir, crewOldKey, credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("a fresh reading of no credit lifted the refusal")
	}
	if err := WriteCreditsReading(dir, crewNewKey, credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("credit read on another key lifted this key's refusal")
	}
	time.Sleep(time.Millisecond)
	if err := WriteCreditsReading(dir, crewOldKey, credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	if !pinnedWorkerAdmits(t, dir) {
		t.Fatal("a positive reading did not lift the earlier payment refusal")
	}
	time.Sleep(time.Millisecond)
	refuseOnOpenRouter(t, dir, "payment")
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("a payment refusal after the reading did not hold")
	}
}

// ONE MODEL FOR EVERY TASK RECOVERS THE SAME WAY.
func TestAOneModelRecoversWhenANewKeyIsConnected(t *testing.T) {
	dir := crewProfile(t)
	applyTaskModels(t, dir, TaskModelsChange{Mode: modePtr(crewroute.ModeOne), One: &CrewPin{Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}})
	refuseOnOpenRouter(t, dir, "auth")
	if _, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}}); err == nil {
		t.Fatal("a refused one model admitted")
	}
	if err := WriteAPIKey(dir, crewNewKey); err != nil {
		t.Fatal(err)
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	if err != nil || d.Mode != crewroute.ModeOne || d.Seat(crewroute.Checker).Model != "z-ai/glm-5.3-flash" {
		t.Fatalf("after a new key: %+v, %v", d.Seat(crewroute.Checker), err)
	}
}

// A KEY CHANGED WHILE A CALL WAS IN FLIGHT IS NOT BLAMED FOR THE OLD KEY'S
// REFUSAL. The call was sent with the old key; the person connected a new one
// before the refusal came back. The refusal is the old key's — the dispatch
// fact says so — and the same pin, on the same route, is admitted at once on
// the new key; a refusal of a call sent with the new key holds it back.
func TestAKeyChangedWhileACallWasInFlightKeepsTheNewKeyEligible(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	sent := CrewDispatchFor(dir, "openrouter")
	if sent.Cred == "" || strings.Contains(sent.Cred, crewOldKey) {
		t.Fatalf("dispatch print %q: want a print, never the key", sent.Cred)
	}
	if err := WriteAPIKey(dir, crewNewKey); err != nil {
		t.Fatal(err)
	}
	refuseSent(t, dir, "auth", sent)
	if !pinnedWorkerAdmits(t, dir) {
		t.Fatal("the old key's in-flight refusal held back the new key")
	}
	reopen()
	if !pinnedWorkerAdmits(t, dir) || !CrewRouteAnswers(dir, "z-ai/glm-5.3-flash") {
		t.Fatal("after reopening, the old key's refusal held back the new key")
	}
	refuseOnOpenRouter(t, dir, "auth")
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("a refusal of a call sent with the new key did not hold")
	}
}

// AN OLD BUILD'S REFUSAL, WHICH NAMES NO CREDENTIAL, is lifted by a credential
// change after it and by nothing else: written again, the same key stays held
// back across reopening; a new key lifts it, and the lift survives reopening.
// The record of changes carries prints, never a key.
func TestAnOldBuildsRefusalIsLiftedByAReconnectAfterIt(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	refuseAsAnOldBuild(t, dir, "auth")
	reopen()
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("an old build's refusal of the key in force was dropped")
	}
	if err := WriteAPIKey(dir, crewOldKey); err != nil {
		t.Fatal(err)
	}
	reopen()
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("the refused key, written again, lifted an old build's refusal")
	}
	if err := WriteAPIKey(dir, crewNewKey); err != nil {
		t.Fatal(err)
	}
	reopen()
	if !pinnedWorkerAdmits(t, dir) {
		t.Fatal("a new key did not lift an old build's refusal")
	}
	reopen()
	if !pinnedWorkerAdmits(t, dir) || !CrewRouteAnswers(dir, "z-ai/glm-5.3-flash") {
		t.Fatal("the lift did not survive reopening")
	}
	raw, err := os.ReadFile(ProfilePath(dir, crewCredentialsFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), crewOldKey) || strings.Contains(string(raw), crewNewKey) || strings.Contains(string(raw), "crewtest") {
		t.Fatalf("the record of changes carries a key: %s", raw)
	}
	// A refusal of the new key, from this build or an old one, holds again.
	refuseAsAnOldBuild(t, dir, "auth")
	reopen()
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("an unprinted refusal after the change did not hold")
	}
}

// THE FIRST CREDENTIAL AN INSTALL RECORDS IS NOT A CHANGE: an old build's
// refusal met by a profile that has never been read since holds, read after
// read, however long the record has existed.
func TestTheFirstCredentialRecordedIsNotAReconnect(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	refuseAsAnOldBuild(t, dir, "auth")
	for range 3 {
		reopen()
		if pinnedWorkerAdmits(t, dir) {
			t.Fatal("an untouched credential's refusal was lifted")
		}
	}
	keys := NewAppCredentials()
	t.Cleanup(UseAppCredentials(keys))
	keys.Set("openrouter", crewOldKey)
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("the refused key handed over again lifted its refusal")
	}
	keys.Set("openrouter", crewNewKey)
	if !pinnedWorkerAdmits(t, dir) {
		t.Fatal("a new key the app handed over did not lift an old build's refusal")
	}
}

// A CREDIT READING OLDER THAN A PAYMENT REFUSAL LIFTS NOTHING: credit was
// read, then the account refused for want of it. A fresh reading of none keeps
// it held back; only a positive reading after the refusal lifts it.
func TestAStalePositiveCreditReadingCannotLiftANewerPaymentRefusal(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	if err := WriteCreditsReading(dir, crewOldKey, credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	refuseOnOpenRouter(t, dir, "payment")
	reopen()
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("a positive reading from before the refusal lifted it")
	}
	time.Sleep(time.Millisecond)
	if err := WriteCreditsReading(dir, crewOldKey, credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	reopen()
	if pinnedWorkerAdmits(t, dir) {
		t.Fatal("a fresh reading of no credit lifted the refusal")
	}
	time.Sleep(time.Millisecond)
	if err := WriteCreditsReading(dir, crewOldKey, credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	reopen()
	if !pinnedWorkerAdmits(t, dir) {
		t.Fatal("a positive reading after the refusal did not lift it")
	}
}

// AN ACCOUNT ADAPTER'S BALANCE LIFTS A PAYMENT REFUSAL ONLY WITH A TIME AFTER
// IT: a balance with no time, or read before the refusal, is no evidence; a
// fresh balance of none holds it back.
func TestAnAccountBalanceLiftsAPaymentRefusalOnlyWhenReadAfterIt(t *testing.T) {
	dir := crewProfile(t)
	pinWorkerOnOpenRouter(t, dir)
	refuseOnOpenRouter(t, dir, "payment")
	refused := time.Now()
	previous := CrewAccounts
	t.Cleanup(func() { CrewAccounts = previous })
	for _, c := range []struct {
		name  string
		state CrewAccountState
		lifts bool
	}{
		{"a balance with no time", CrewAccountState{CreditKnown: true, CreditUSD: 5}, false},
		{"a balance read before the refusal", CrewAccountState{CreditKnown: true, CreditUSD: 5, ReadAt: refused.Add(-time.Hour)}, false},
		{"a fresh balance of none", CrewAccountState{CreditKnown: true, CreditUSD: 0, ReadAt: refused.Add(time.Second)}, false},
		{"a balance read after the refusal", CrewAccountState{CreditKnown: true, CreditUSD: 5, ReadAt: refused.Add(time.Second)}, true},
	} {
		state := c.state
		CrewAccounts = func(_, provider string) (CrewAccountState, bool) { return state, provider == "openrouter" }
		reopen()
		if got := pinnedWorkerAdmits(t, dir); got != c.lifts {
			t.Fatalf("%s: admitted %v, want %v", c.name, got, c.lifts)
		}
	}
}

// glmFailure is one failure on the pinned worker's route, as the router's log
// reads it back.
func glmFailure(kind string, at time.Time, sent CrewDispatch) router.CrewRouteOutcome {
	return router.CrewRouteOutcome{At: at, Seat: string(crewroute.Worker), Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Paid: true, Kind: kind, Cred: sent.Cred}
}

// ONE FAILURE IS JUDGED BY THE SAME FACTS AS ROUTE HEALTH: a key refusal stops
// applying when a different key is in force and goes on applying when the
// same key is written again; an unprinted one is lifted only by a change after
// it, and one with no time is never lifted by a change at all.
func TestCrewRefusalAppliesFollowsTheCredentialInForce(t *testing.T) {
	dir := crewProfile(t)
	at := time.Now().Add(-time.Minute)
	printed := glmFailure("auth", at, CrewDispatchFor(dir, "openrouter"))
	unprinted := glmFailure("auth", at, CrewDispatch{})
	undated := glmFailure("auth", time.Time{}, CrewDispatch{})
	for _, f := range []router.CrewRouteOutcome{printed, unprinted, undated} {
		if !CrewRefusalApplies(dir, f) {
			t.Fatalf("a refusal of the key in force does not apply: %+v", f)
		}
	}
	if err := WriteAPIKey(dir, crewOldKey); err != nil {
		t.Fatal(err)
	}
	reopen()
	if !CrewRefusalApplies(dir, printed) || !CrewRefusalApplies(dir, unprinted) {
		t.Fatal("the refused key, written again, lifted its refusal")
	}
	if err := WriteAPIKey(dir, crewNewKey); err != nil {
		t.Fatal(err)
	}
	reopen()
	if CrewRefusalApplies(dir, printed) || CrewRefusalApplies(dir, unprinted) {
		t.Fatal("a new key did not lift the old key's refusal")
	}
	if !CrewRefusalApplies(dir, undated) {
		t.Fatal("a change was taken to be after a refusal with no time")
	}
	if !CrewRefusalApplies(dir, glmFailure("auth", time.Time{}, CrewDispatchFor(dir, "openrouter"))) {
		t.Fatal("a refusal printed with the key in force does not apply")
	}
}

// ONLY A PAYMENT OR KEY REFUSAL CAN STOP APPLYING: a limit, a route gone, a
// timeout apply whatever the credentials and the credit say.
func TestCrewRefusalAppliesKeepsEveryOtherFailure(t *testing.T) {
	dir := crewProfile(t)
	at := time.Now().Add(-time.Minute)
	if err := WriteAPIKey(dir, crewNewKey); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreditsReadingAt(dir, crewNewKey, credits.Reading{Known: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"rate_limit", "unavailable", "transient", "model", ""} {
		if !CrewRefusalApplies(dir, glmFailure(kind, at, CrewDispatch{Cred: "0000000000000000"})) {
			t.Fatalf("a %q failure stopped applying", kind)
		}
	}
}

// CREDIT LIFTS A PAYMENT REFUSAL ONLY WHEN IT WAS OBSERVED AFTER IT: a read
// that began before the refusal — however late it answered — a reading of no
// credit, and a reading with no time all leave it applying; so does any
// reading against a refusal with no time.
func TestCrewRefusalAppliesUntilCreditIsObservedAfterIt(t *testing.T) {
	dir := crewProfile(t)
	refused := time.Now().Add(-time.Minute)
	failure := glmFailure("payment", refused, CrewDispatchFor(dir, "openrouter"))
	undated := glmFailure("payment", time.Time{}, CrewDispatchFor(dir, "openrouter"))
	for _, c := range []struct {
		name     string
		reading  credits.Reading
		observed time.Time
		lifts    bool
	}{
		{"credit observed before the refusal", credits.Reading{Known: true}, refused.Add(-time.Second), false},
		{"credit observed with no time", credits.Reading{Known: true}, time.Time{}, false},
		{"no credit observed after the refusal", credits.Reading{Known: true, Low: true}, refused.Add(time.Second), false},
		{"an unknown balance after the refusal", credits.Reading{}, refused.Add(time.Second), false},
		{"credit observed after the refusal", credits.Reading{Known: true}, refused.Add(time.Second), true},
	} {
		if err := WriteCreditsReadingAt(dir, crewOldKey, c.reading, c.observed); err != nil {
			t.Fatal(err)
		}
		reopen()
		if got := !CrewRefusalApplies(dir, failure); got != c.lifts {
			t.Fatalf("%s: lifted %v, want %v", c.name, got, c.lifts)
		}
		if !CrewRefusalApplies(dir, undated) {
			t.Fatalf("%s: lifted a payment refusal with no time", c.name)
		}
	}
}

// A READING DATED AFTER NOW IS REFUSED and leaves the record as it was: it
// would otherwise outrank refusals that have not happened yet.
func TestACreditReadingFromTheFutureIsRefused(t *testing.T) {
	dir := crewProfile(t)
	if err := WriteCreditsReadingAt(dir, crewOldKey, credits.Reading{Known: true, Low: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ProfilePath(dir, "credits.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteCreditsReadingAt(dir, crewOldKey, credits.Reading{Known: true}, time.Now().Add(time.Hour)); !errors.Is(err, ErrCreditsObservedAhead) {
		t.Fatalf("a future reading wrote with %v", err)
	}
	if after, _ := os.ReadFile(ProfilePath(dir, "credits.json")); string(after) != string(before) {
		t.Fatal("a refused future reading changed the record")
	}
}
