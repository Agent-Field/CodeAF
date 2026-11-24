package chatlist

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestASealedCrewOpensOnlyAsACrewAndTravelsOnTheRow(t *testing.T) {
	key := directory.MetadataKey([]byte("cell-key"))
	sealed, err := SealCrew(key, Crew{ID: "da48d479ea82", Name: "Todo crew", Manager: true, Kind: CrewKindFactory})
	if err != nil {
		t.Fatal(err)
	}
	if got := OpenCrew(key, sealed); got != (Crew{V: 1, ID: "da48d479ea82", Name: "Todo crew", Manager: true, Kind: CrewKindFactory}) {
		t.Fatalf("OpenCrew = %+v", got)
	}
	// A crew is sealed under its own associated data: it never reads back as a
	// title, and a title never reads back as a crew.
	if name, err := directory.OpenName(key, sealed); err == nil {
		t.Fatalf("a sealed crew opened as a name: %q", name)
	}
	title, _ := directory.SealTitle(key, `{"v":1,"id":"x"}`)
	if got := OpenCrew(key, title); got != (Crew{}) {
		t.Fatalf("a sealed title opened as a crew: %+v", got)
	}
	// "In no crew" is sealed on purpose and opens as no crew.
	none, _ := SealCrew(key, Crew{})
	if none == "" || OpenCrew(key, none) != (Crew{}) {
		t.Fatalf("no crew sealed as %q", none)
	}
	if got := OpenCrew(directory.MetadataKey([]byte("another identity")), sealed); got != (Crew{}) {
		t.Fatalf("another identity opened the crew: %+v", got)
	}
	long, _ := SealCrew(key, Crew{ID: "c", Name: strings.Repeat("é", 200)})
	if got := OpenCrew(key, long); len(got.Name) > directory.MaxTitleBytes || !strings.HasPrefix(got.Name, "é") {
		t.Fatalf("a long name was kept as %q", got.Name)
	}

	l := directory.Listing{Cells: map[string]directory.Cell{"a": {Crew: sealed}, "b": {}}}
	rows := []Row{{Cell: "a"}, {Cell: "b"}}
	WithCrews(rows, l, key)
	if rows[0].Crew.ID != "da48d479ea82" || rows[1].Crew != (Crew{}) {
		t.Fatalf("WithCrews = %+v", rows)
	}
}
