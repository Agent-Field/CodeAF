package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/search"
)

// A SERVER WORKSTREAM AGENT'S web_fetch READS ONLY AN ADDRESS IT WAS GIVEN OR
// FOUND: one in the person's or task's words, or in this run's web_search
// results, exactly. An address a page made up (with the workstream's data in
// it) is refused with a sentence the model can read, and never fetched.
func TestAServerAgentsWebFetchReadsOnlyAddressesItWasGivenOrFound(t *testing.T) {
	fetch := &scriptedFetch{text: "Hall A seats 120."}
	finder := &scriptedSearch{results: []search.Result{{Title: "Hall B", URL: "https://venues.example/hall-b?city=austin", Snippet: "Seats 80."}}}
	agent := &Agent{config: Config{Workstream: &fakeWorkstreamDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true,
		SearchProvider: finder, SearchFetcher: fetch}}
	agent.tools = agent.belt()
	tool := func(name string) func(string) (string, bool) {
		for _, tl := range agent.tools {
			if tl.Name == name {
				return func(args string) (string, bool) {
					out, failed, err := tl.Execute(context.Background(), json.RawMessage(args))
					if err != nil {
						t.Fatalf("%s: %v", name, err)
					}
					return out, failed
				}
			}
		}
		t.Fatalf("no %s on the belt", name)
		return nil
	}
	webFetch, webSearch := tool("web_fetch"), tool("web_search")

	agent.keepPersonWordsLocked("Shortlist venues. Start with (https://Venues.example/hall-a), then look around.")
	for _, refused := range []string{
		"https://evil.example/?d=the+notes",
		"https://venues.example/hall-a?d=the+notes",
		"https://venues.example/hall-b?city=austin",
		"file:///etc/passwd",
	} {
		out, failed := webFetch(`{"url":"` + refused + `"}`)
		if !failed || !strings.Contains(out, "did not read") || !strings.Contains(out, "web_search") {
			t.Errorf("web_fetch of %s answered %q failed=%v, want a refusal", refused, out, failed)
		}
	}
	if got := fetch.fetched(); len(got) != 0 {
		t.Fatalf("refused addresses reached the fetcher: %v", got)
	}

	if out, failed := webFetch(`{"url":"https://venues.example/hall-a#seating"}`); failed || !strings.Contains(out, "Hall A seats 120") {
		t.Fatalf("an address in the person's words was refused: %q", out)
	}
	if _, failed := webSearch(`{"query":"austin venues"}`); failed {
		t.Fatal("web_search failed")
	}
	if out, failed := webFetch(`{"url":"https://venues.example/hall-b?city=austin"}`); failed {
		t.Fatalf("an address from this run's web_search was refused: %q", out)
	}
	if got := fetch.fetched(); strings.Join(got, ",") != "https://venues.example/hall-a#seating,https://venues.example/hall-b?city=austin" {
		t.Fatalf("the fetcher was asked for %v", got)
	}

	// A person's own conversation (no workstream) keeps the plain web_fetch.
	plain := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), SearchFetcher: fetch}}
	for _, tl := range plain.searchTools() {
		if tl.Name == "web_fetch" {
			if _, failed, _ := tl.Execute(context.Background(), json.RawMessage(`{"url":"https://anything.example/"}`)); failed {
				t.Fatal("a person's own web_fetch was leashed")
			}
		}
	}
}
