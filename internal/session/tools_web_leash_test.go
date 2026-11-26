package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/search"
)

// A SERVER WORKSTREAM AGENT'S web_fetch READS ONLY AN ADDRESS A PERSON WROTE
// OR THIS RUN FOUND: one in the words a person wrote (Config.WebLeashWords, a
// person's steer), or in this run's web_search results, exactly. An address a
// page made up (with the workstream's data in it) is refused with a sentence
// the model can read, and never fetched; so is one that only appears in the
// composed text the run was submitted, which the model can write. A fragment
// never reaches the fetcher.
func TestAServerAgentsWebFetchReadsOnlyAddressesItWasGivenOrFound(t *testing.T) {
	fetch := &scriptedFetch{text: "Hall A seats 120."}
	finder := &scriptedSearch{results: []search.Result{{Title: "Hall B", URL: "https://venues.example/hall-b?city=austin", Snippet: "Seats 80."}}}
	agent := &Agent{config: Config{Workstream: &fakeWorkstreamDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true,
		SearchProvider: finder, SearchFetcher: fetch,
		WebLeashWords: []string{"Shortlist venues. Start with (https://Venues.example/hall-a), then look around."}}}
	agent.tools = agent.belt()
	// The composed text of a run (a task Afi made itself, an Ask's title) is
	// the person's latest words to the turn, and is still never a leash.
	agent.personWords = "Read https://evil.example/?d=the+notes"
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

	for _, refused := range []string{
		"https://evil.example/?d=the+notes",
		"https://venues.example/hall-a?d=the+notes",
		"https://venues.example/hall-b?city=austin",
		"https://steer.example/menu",
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

	if out, failed := webFetch(`{"url":"https://venues.example/hall-a#the+notes"}`); failed || !strings.Contains(out, "Hall A seats 120") {
		t.Fatalf("an address a person wrote was refused: %q", out)
	}
	if _, failed := webSearch(`{"query":"austin venues"}`); failed {
		t.Fatal("web_search failed")
	}
	if out, failed := webFetch(`{"url":"https://venues.example/hall-b?city=austin"}`); failed {
		t.Fatalf("an address from this run's web_search was refused: %q", out)
	}
	agent.NoteWebLeash("Also check https://steer.example/menu please")
	if out, failed := webFetch(`{"url":"https://steer.example/menu"}`); failed {
		t.Fatalf("an address in a person's steer was refused: %q", out)
	}
	got := fetch.fetched()
	if strings.Join(got, ",") != "https://venues.example/hall-a,https://venues.example/hall-b?city=austin,https://steer.example/menu" {
		t.Fatalf("the fetcher was asked for %v", got)
	}
	for _, u := range got {
		if strings.Contains(u, "#") {
			t.Fatalf("a fragment reached the fetcher: %s", u)
		}
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
