package connect

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestConnectingAToolServerAsksItsWayIn(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{stampsIssuer: true})
	manager, plug := withToolServer(t, fake)
	ctx := context.Background()

	if manager.Connected("example") {
		t.Fatalf("nothing is connected before anybody signs in")
	}
	flow, err := manager.BeginAuth(ctx, "example", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	// The address is ready before the wait begins, and it is one short
	// loopback address rather than the long one with every parameter in it.
	if !strings.HasPrefix(flow.URL(), "http://localhost:") {
		t.Errorf("the address to open is %q", flow.URL())
	}
	openInBrowser(t, flow)

	status, err := flow.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !status.Connected || status.Name != "Example" {
		t.Errorf("Wait gave %+v", status)
	}
	if !manager.Connected("example") {
		t.Errorf("the service is connected once the keys are stored")
	}

	// The keys are in the one store file every other service's keys are in,
	// and they say what kind of connection they came from.
	entry, held, err := manager.store.get("example")
	if err != nil || !held {
		t.Fatalf("store.get: %v, held %v", err, held)
	}
	if entry.Auth != authMCP {
		t.Errorf("the entry says it is a %q connection", entry.Auth)
	}
	if entry.Keys == nil || entry.Keys.AccessToken == "" || entry.Keys.RefreshToken == "" {
		t.Errorf("a connection that cannot be renewed was stored")
	}

	// The identity aforge was issued is written down beside it, keyed to the
	// sign-in that issued it and the thing it was issued for.
	record, kept := manager.registrations().get("example")
	if !kept {
		t.Fatalf("nothing was written down about who aforge is to this service")
	}
	if record.Issuer != fake.URL {
		t.Errorf("the sign-in was recorded as %q, want %q", record.Issuer, fake.URL)
	}
	if record.Resource != plug.address {
		t.Errorf("the service was recorded as %q, want %q", record.Resource, plug.address)
	}
	if record.ClientID != fake.clientID || record.Server != plug.address {
		t.Errorf("the identity reads %+v", record)
	}

	// The trip carried a proof key and the service's own name, on both legs.
	if !fake.askedProof {
		t.Errorf("the browser trip carried no proof key")
	}
	for _, asked := range fake.resources() {
		if asked != plug.address {
			t.Errorf("a leg of the trip asked for %q, want %q", asked, plug.address)
		}
	}
	if introductions, _, _ := fake.counted(); introductions != 1 {
		t.Errorf("aforge introduced itself %d times", introductions)
	}
}

// A build configured with nothing still offers these services, because there is
// nothing anybody could have failed to configure.
func TestAToolServerIsOfferedInABuildWithNoCredentials(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)

	listed := manager.Services()
	if len(listed) != 1 {
		t.Fatalf("Services() gave %d rows", len(listed))
	}
	if listed[0].Auth != AuthBrowser {
		t.Errorf("a tool server is signed in to in a browser, got %q", listed[0].Auth)
	}
	if listed[0].Category == "" || listed[0].Blurb == "" {
		t.Errorf("a row nobody can read: %+v", listed[0])
	}
	// A browser service has no one address, and the emptiness law says an
	// unknown is empty.
	if listed[0].Address != "" {
		t.Errorf("Address = %q, want nothing", listed[0].Address)
	}
}

// An address answer outside the service's own list is refused before there is
// anything listening and before the service is asked a single question.
func TestAWrongAddressAnswerStopsBeforeAListenerOrRequest(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{blank: true})
	manager, _ := withToolServer(t, fake)
	saved := localServerAddresses
	localServerAddresses = []string{"this is not a listener address"}
	t.Cleanup(func() { localServerAddresses = saved })

	flow, err := manager.BeginAuth(context.Background(), "example", "somewhere-else")
	if err == nil {
		t.Fatal("an answer outside the published list started a connection")
	}
	if flow != nil {
		t.Errorf("a refused answer returned a flow")
	}
	for _, want := range []string{"Example", "here", "elsewhere"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if paths := fake.paths(); len(paths) != 0 {
		t.Errorf("a refused answer reached the service at %v", paths)
	}
}

// A valid answer is kept as the missing piece, while both the registration and
// every later connection use the filled address.
func TestAnAddressAnswerIsKeptAndUsedOnEveryOpen(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{blank: true})
	manager, plug := withToolServer(t, fake)
	connectFakeAt(t, manager, "here")

	entry, held, err := manager.store.get("example")
	if err != nil || !held {
		t.Fatalf("store.get: held=%v err=%v", held, err)
	}
	if entry.Blank != "here" {
		t.Errorf("the stored answer is %q, want here", entry.Blank)
	}
	filled := fake.address()
	record, held := manager.registrations().get("example")
	if !held || record.Server != filled {
		t.Errorf("the registration is for %q, want %q", record.Server, filled)
	}
	rows := manager.Services()
	if len(rows) != 1 || rows[0].Address != filled {
		t.Errorf("the connected row reads %+v, want address %q", rows, filled)
	}
	session, err := manager.open(context.Background(), plug)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = session.Close()
}

// Keys and a stored answer that name different sites are not spent anywhere:
// the person is asked to connect the service again.
func TestAStoredSiteAndRegistrationThatDisagreeAreRefused(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{blank: true})
	manager, plug := withToolServer(t, fake)
	connectFakeAt(t, manager, "here")

	entry, _, err := manager.store.get("example")
	if err != nil {
		t.Fatalf("store.get: %v", err)
	}
	entry.Blank = "elsewhere"
	if err := manager.store.put("example", entry); err != nil {
		t.Fatalf("store.put: %v", err)
	}
	before := len(fake.paths())
	if _, err := manager.open(context.Background(), plug); err == nil {
		t.Fatal("keys for one site were used at another")
	} else if !strings.Contains(err.Error(), "Example has to be connected again") {
		t.Errorf("the refusal reads %q", err)
	}
	if after := len(fake.paths()); after != before {
		t.Errorf("the mismatch made %d requests", after-before)
	}
}

// Reconnecting the same row at a different site obtains a fresh identity tied
// to that address, and the new connection is ready to use there.
func TestReconnectingAtAnotherSiteObtainsAFreshIdentity(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{blank: true})
	manager, plug := withToolServer(t, fake)
	connectFakeAt(t, manager, "here")
	connectFakeAt(t, manager, "elsewhere")

	if introductions, _, _ := fake.counted(); introductions != 2 {
		t.Errorf("aforge introduced itself %d times, want once at each site", introductions)
	}
	record, held := manager.registrations().get("example")
	want := fake.URL + "/elsewhere"
	if !held || record.Server != want {
		t.Errorf("the identity is for %q, want %q", record.Server, want)
	}
	session, err := manager.open(context.Background(), plug)
	if err != nil {
		t.Fatalf("open at the new site: %v", err)
	}
	_ = session.Close()
}

// The widened door is harmless to an existing entry: without a declared blank,
// even a non-empty answer changes neither the address nor the trip.
func TestAToolServerWithoutABlankIgnoresAnAnswer(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, plug := withToolServer(t, fake)
	connectFakeAt(t, manager, "not for this service")

	record, held := manager.registrations().get("example")
	if !held || record.Server != fake.address() {
		t.Errorf("the answer changed the registration: %+v", record)
	}
	session, err := manager.open(context.Background(), plug)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = session.Close()
}

// The one failure a person cannot work around is said in one sentence, and
// nothing is written down when it happens.
func TestAServiceThatWillNotBeIntroducedToSaysSoPlainly(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{refuseIntroductions: true})
	manager, _ := withToolServer(t, fake)

	_, err := manager.BeginAuth(context.Background(), "example", "")
	if err == nil {
		t.Fatalf("a service that cannot be connected must not hand back a flow")
	}
	said := err.Error()
	if !strings.Contains(said, "Example") || !strings.Contains(said, "introduce itself") {
		t.Errorf("the sentence reads %q", said)
	}
	for _, word := range machineryWords {
		if strings.Contains(strings.ToLower(said), word) {
			t.Errorf("the sentence says %q: %q", word, said)
		}
	}
	if manager.Connected("example") {
		t.Errorf("nothing is connected")
	}
	if _, kept := manager.registrations().get("example"); kept {
		t.Errorf("nothing is written down")
	}
}

// The identity survives being disconnected, exactly as a client credential does,
// so that connecting again is one browser trip and not a second registration.
func TestTheIdentityIsUsedAgainAndSurvivesDisconnect(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	ctx := context.Background()

	connectFake(t, manager)
	if err := manager.Disconnect("example"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if manager.Connected("example") {
		t.Fatalf("the keys are gone")
	}
	if _, kept := manager.registrations().get("example"); !kept {
		t.Errorf("who aforge is to this service is not a thing to forget")
	}

	flow, err := manager.BeginAuth(ctx, "example", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	openInBrowser(t, flow)
	if _, err := flow.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if introductions, _, _ := fake.counted(); introductions != 1 {
		t.Errorf("aforge introduced itself %d times, want once", introductions)
	}
}

// A person who says no in their browser is told what the service said, and
// nothing is written down.
func TestSayingNoInTheBrowserConnectsNothing(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	ctx := context.Background()

	flow, err := manager.BeginAuth(ctx, "example", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	// What the service sends back when the person presses cancel.
	refused := flow.URL() + "?error=access_denied&error_description=You+said+no"
	response, err := http.Get(refused)
	if err != nil {
		t.Fatalf("come back refused: %v", err)
	}
	_ = response.Body.Close()

	status, err := flow.Wait(ctx)
	if err == nil {
		t.Fatalf("a refusal is an error, got %+v", status)
	}
	if !strings.Contains(err.Error(), "You said no") {
		t.Errorf("the service's own words were lost: %v", err)
	}
	if status.Connected || manager.Connected("example") {
		t.Errorf("nothing is connected")
	}
	if _, held, _ := manager.store.get("example"); held {
		t.Errorf("nothing is written down")
	}
	// Asking again gives the same answer rather than starting anything new.
	again, againErr := flow.Wait(ctx)
	if againErr == nil || again.Connected {
		t.Errorf("Wait answered differently the second time: %+v, %v", again, againErr)
	}
}

// The sign-in has to be the one the person was sent to.
func TestAnAnswerFromSomewhereElseIsRefused(t *testing.T) {
	found := signIn{issuer: "https://example.test", stampsIssuer: true}
	cases := []struct {
		name    string
		named   string
		promise bool
		wantErr bool
	}{
		{name: "the sign-in named itself", named: "https://example.test", promise: true},
		{name: "a trailing slash is the same sign-in", named: "https://example.test/", promise: true},
		{name: "it promised a name and sent none", promise: true, wantErr: true},
		{name: "it named somebody else", named: "https://elsewhere.test", promise: true, wantErr: true},
		{name: "it promised nothing and sent nothing"},
		{name: "it promised nothing and named somebody else", named: "https://elsewhere.test", wantErr: true},
	}
	for _, c := range cases {
		found.stampsIssuer = c.promise
		err := checkIssuer(c.named, found)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, want error %v", c.name, err, c.wantErr)
		}
	}
}

// Every loopback address this build may land on is registered, so that a busy
// port does not cost a second registration.
func TestEveryLoopbackAddressIsRegistered(t *testing.T) {
	registered := loopbacks("http://localhost:18765")
	if registered[0] != "http://localhost:18765" {
		t.Errorf("the address in hand comes first, got %v", registered)
	}
	want := map[string]bool{"http://localhost:8765": false, "http://localhost:18765": false}
	for _, address := range registered {
		if _, known := want[address]; !known {
			t.Errorf("%q is not an address this build listens on", address)
		}
		want[address] = true
	}
	for address, found := range want {
		if !found {
			t.Errorf("%q was not registered", address)
		}
	}
}

// connectFake takes one stand-in service all the way through a sign-in.
func connectFake(t *testing.T, manager *Manager) {
	connectFakeAt(t, manager, "")
}

// connectFakeAt takes one stand-in through a sign-in with its address answer.
func connectFakeAt(t *testing.T, manager *Manager, answer string) {
	t.Helper()
	ctx := context.Background()
	flow, err := manager.BeginAuth(ctx, "example", answer)
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	openInBrowser(t, flow)
	if _, err := flow.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}
