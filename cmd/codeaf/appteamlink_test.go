package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A LINK MADE BY THE CREW'S MANAGER REACHES THE CREW: in the app it is told to
// the window at once (crew.item), and from an engine process it is written to
// the same layer and read by the app on the next ask. The same ref linked
// twice is one item.
func TestAppCrewManagerLinksReachTheCrew(t *testing.T) {
	backend, rec, _, id := crewBackend(t)
	item, err := backend.CrewLink(id, "file", "/w/launch-post-final.md", "launch-post-final.md")
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		got, ok := data.(appwire.CrewItemEvent)
		return event == appwire.EventCrewItem && ok && got.Item.ID == item.ID && got.Item.Crew == id
	})

	// An engine process (a terminal's host) links into the same file.
	engine := &appCrewLayer{path: backend.crewLayer().path}
	other, err := linkCrewItem(engine, session.TeamLink{Team: id, Kind: "file", Ref: "/w/reviewed-patch-plan.md", Title: "reviewed-patch-plan.md"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	again, err := linkCrewItem(engine, session.TeamLink{Team: id, Kind: "file", Ref: "/w/launch-post-final.md"}, time.Now())
	if err != nil || again.ID != item.ID {
		t.Fatalf("linking the same file again made a second item: %+v %v", again, err)
	}
	items, err := backend.CrewItems(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != item.ID || items[1].ID != other.ID {
		t.Fatalf("the crew's items = %+v", items)
	}
}
