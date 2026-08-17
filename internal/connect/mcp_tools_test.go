package connect

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAToolListIsFetchedOnceAndRemembered(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	ctx := context.Background()
	connectFake(t, manager)

	tools, err := manager.MCPTools(ctx, "example")
	if err != nil {
		t.Fatalf("MCPTools: %v", err)
	}
	byName := map[string]MCPTool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if len(byName) != 4 {
		t.Fatalf("the service offers %d tools: %v", len(byName), byName)
	}

	found := byName["find_pages"]
	if found.Description == "" {
		t.Errorf("a tool with nothing said about it: %+v", found)
	}
	// The arguments come through exactly as the service published them.
	var schema map[string]any
	if err := json.Unmarshal(found.Schema, &schema); err != nil {
		t.Fatalf("the arguments are not readable: %v", err)
	}
	if schema["type"] != "object" || schema["properties"] == nil {
		t.Errorf("the arguments read %v", schema)
	}

	// ReadOnly comes from what the service said, and saying nothing is not
	// saying read-only.
	if !found.ReadOnly {
		t.Errorf("find_pages was marked as one that only looks")
	}
	if byName["write_page"].ReadOnly {
		t.Errorf("write_page said nothing, so it acts")
	}
	if got := MCPToolCapability(found); got != CapabilityRead {
		t.Errorf("a tool that only looks is %q, want %q", got, CapabilityRead)
	}
	if got := MCPToolCapability(byName["write_page"]); got != CapabilityAct {
		t.Errorf("a tool that acts is %q, want %q", got, CapabilityAct)
	}

	// Asking again costs the service nothing.
	again, err := manager.MCPTools(ctx, "example")
	if err != nil {
		t.Fatalf("MCPTools again: %v", err)
	}
	if len(again) != len(tools) {
		t.Errorf("the second answer had %d tools, the first had %d", len(again), len(tools))
	}
	if _, lists, _ := fake.counted(); lists != 1 {
		t.Errorf("the service was asked for its tools %d times, want once", lists)
	}

	// What a caller does to the list it was handed cannot reach the list
	// everybody else gets.
	again[0].Name = "changed"
	third, err := manager.MCPTools(ctx, "example")
	if err != nil {
		t.Fatalf("MCPTools a third time: %v", err)
	}
	if third[0].Name == "changed" {
		t.Errorf("the remembered list was edited from outside")
	}
}

func TestCallingATool(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	ctx := context.Background()
	connectFake(t, manager)

	said, err := manager.MCPCall(ctx, "example", "find_pages", json.RawMessage(`{"query":"minutes"}`))
	if err != nil {
		t.Fatalf("MCPCall: %v", err)
	}
	if !strings.Contains(said, "Found:") || !strings.Contains(said, "minutes") {
		t.Errorf("the answer reads %q", said)
	}

	// A tool the service does not have is the service's answer, not a guess
	// made here from a list that may be out of date.
	if _, err := manager.MCPCall(ctx, "example", "nothing_like_this", nil); err == nil {
		t.Errorf("a tool nobody has is an error")
	}
	// A tool with no name never leaves this process.
	if _, err := manager.MCPCall(ctx, "example", "  ", nil); err == nil {
		t.Errorf("a tool with no name is an error")
	}
}

func TestARefusedCallCarriesTheServicesOwnSentence(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	connectFake(t, manager)

	said, err := manager.MCPCall(context.Background(), "example", "refuse", nil)
	if err == nil {
		t.Fatalf("a tool that refuses is an error")
	}
	if said != "" {
		t.Errorf("a refused call answered with %q", said)
	}
	if !strings.Contains(err.Error(), "not yours to read") {
		t.Errorf("the service's own words were lost: %v", err)
	}
	if !strings.Contains(err.Error(), "Example") {
		t.Errorf("the sentence does not say who refused: %v", err)
	}
}

func TestAnAnswerTooLongToReadIsCutAndSaysSo(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	connectFake(t, manager)

	said, err := manager.MCPCall(context.Background(), "example", "flood", nil)
	if err != nil {
		t.Fatalf("MCPCall: %v", err)
	}
	if len(said) > maxToolText+200 {
		t.Errorf("the answer is %d characters, which is more than the ceiling", len(said))
	}
	if !strings.Contains(said, "Shortened here") {
		t.Errorf("the cut was not announced: %q", said[max(0, len(said)-200):])
	}
}

func TestArgumentsThatAreNotAnObjectAreRefusedHere(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	connectFake(t, manager)
	ctx := context.Background()

	for _, args := range []string{`[1,2]`, `"words"`, `{ not json`} {
		if _, err := manager.MCPCall(ctx, "example", "find_pages", json.RawMessage(args)); err == nil {
			t.Errorf("%s was accepted as arguments", args)
		}
	}
	// Nothing at all is a legitimate argument list.
	if _, err := manager.MCPCall(ctx, "example", "find_pages", nil); err != nil {
		t.Errorf("no arguments is not a mistake: %v", err)
	}
}

func TestNothingIsAskedOfAServiceThatIsNotConnected(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	ctx := context.Background()

	if _, err := manager.MCPTools(ctx, "example"); err == nil {
		t.Errorf("an unconnected service has no tools to list")
	} else if !strings.Contains(err.Error(), "Example is not connected") {
		t.Errorf("the sentence reads %q", err)
	}
	if _, err := manager.MCPCall(ctx, "example", "find_pages", nil); err == nil {
		t.Errorf("an unconnected service runs nothing")
	}
	if _, err := manager.MCPTools(ctx, "nothing-like-this"); err == nil {
		t.Errorf("a service this build has never heard of is an error")
	}
	// The question a caller arming a belt asks first.
	if !manager.MCPService("example") {
		t.Errorf("the stand-in brings its own tools")
	}
	if manager.MCPService("nothing-like-this") {
		t.Errorf("a service nobody has brings nothing")
	}
}

// A connection whose keys have aged out renews itself underneath the caller,
// and the renewal is written down.
func TestKeysAreRenewedAndTheRenewalIsWrittenDown(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{briefKeys: true})
	manager, _ := withToolServer(t, fake)
	connectFake(t, manager)

	before, _, err := manager.store.get("example")
	if err != nil {
		t.Fatalf("store.get: %v", err)
	}
	if _, err := manager.MCPTools(context.Background(), "example"); err != nil {
		t.Fatalf("MCPTools: %v", err)
	}
	if _, _, renewals := fake.counted(); renewals == 0 {
		t.Fatalf("a stale key was used without being renewed")
	}
	after, _, err := manager.store.get("example")
	if err != nil {
		t.Fatalf("store.get: %v", err)
	}
	if after.Keys == nil || after.Keys.AccessToken == before.Keys.AccessToken {
		t.Errorf("the renewed key was not written down")
	}
	if after.Auth != authMCP {
		t.Errorf("the renewal lost what kind of connection this is: %q", after.Auth)
	}
}

// Keys with nothing to renew them and nothing left to spend are not a
// connection, and the sentence says what to do about it.
func TestAConnectionThatCannotBeRenewedAsksToBeMadeAgain(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	manager, _ := withToolServer(t, fake)
	connectFake(t, manager)

	entry, _, err := manager.store.get("example")
	if err != nil {
		t.Fatalf("store.get: %v", err)
	}
	entry.Keys.RefreshToken = ""
	entry.Keys.Expiry = entry.Keys.Expiry.AddDate(-1, 0, 0)
	if err := manager.store.put("example", entry); err != nil {
		t.Fatalf("store.put: %v", err)
	}
	if _, err := manager.MCPTools(context.Background(), "example"); err == nil {
		t.Errorf("dead keys are not a connection")
	} else if !strings.Contains(err.Error(), "connected again") {
		t.Errorf("the sentence reads %q", err)
	}
}

// The renderer turns everything a tool can say into text, and never into bytes
// nobody can read.
func TestWhatATextlessAnswerReadsAs(t *testing.T) {
	if got := callArgumentsText(t, `  `); got != "" {
		t.Errorf("empty arguments read as %q", got)
	}
	if got := schemaOf(nil); string(got) != `{"type":"object"}` {
		t.Errorf("a tool that published nothing reads as %q", got)
	}
	if got := schemaOf(map[string]any{"type": "object"}); string(got) != `{"type":"object"}` {
		t.Errorf("a published schema is passed through, got %q", got)
	}
}

// callArgumentsText is what one argument list becomes on the wire.
func callArgumentsText(t *testing.T, args string) string {
	t.Helper()
	read, err := callArguments(json.RawMessage(args))
	if err != nil {
		t.Fatalf("callArguments(%q): %v", args, err)
	}
	if read == nil {
		return ""
	}
	encoded, err := json.Marshal(read)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}
