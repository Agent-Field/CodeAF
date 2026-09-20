package standing

// A ROW THAT NEEDS SOMEBODY IS PUT DOWN BY THEM, NOT ONLY BY THE NEXT FIRING.
//
// The pass writes this line whole every time it fires an item, so for most of
// this field's life the only way a person could be rid of it was to wait for
// the item to run again. An item that had spent its allowance for the day could
// not run again, so its row stayed on home saying somebody was needed, and
// nothing they did could put it down.

import (
	"reflect"
	"testing"
)

func TestChangingAnItemPutsDownTheLineItStoppedOn(t *testing.T) {
	item := Item{ID: "an-item", NeedsPerson: NeedsPermissionLead + "something"}
	if changed := item.ClearNeedsPerson(); changed.NeedsPerson != "" {
		t.Fatalf("the line survived the person's change: %q", changed.NeedsPerson)
	}
}

// AND A QUESTION IS NOT A LINE ABOUT A PERMISSION. The same field carries a
// question the firing put to the person in its own words, and pausing a watch
// is not answering it. Losing one behind their back would be the surface
// throwing away the one thing it exists to carry.
func TestChangingAnItemKeepsAQuestionTheFiringAsked(t *testing.T) {
	const asked = "should I send it to the whole team?"
	item := Item{ID: "an-item", NeedsPerson: asked}
	if changed := item.ClearNeedsPerson(); changed.NeedsPerson != asked {
		t.Fatalf("a question the firing asked was thrown away: %q", changed.NeedsPerson)
	}
}

func TestClearingTheLineChangesNothingElseAboutTheItem(t *testing.T) {
	item := Item{
		ID:          "an-item",
		Words:       "keep an eye on it",
		Status:      StatusActive,
		SpentUSD:    1.25,
		CleanRuns:   3,
		LastOutcome: "needs-you",
		NeedsPerson: NeedsPermissionLead + "something",
	}
	changed := item.ClearNeedsPerson()
	want := item
	want.NeedsPerson = ""
	if !reflect.DeepEqual(changed, want) {
		t.Fatalf("clearing the line moved something else:\n got %+v\nwant %+v", changed, want)
	}
	// AND THE CALLER'S OWN COPY IS UNTOUCHED, because this answers with a
	// document rather than editing one somebody else is holding.
	if item.NeedsPerson == "" {
		t.Fatal("the caller's own item was edited in place")
	}
}

func TestAnItemNeedingNobodyIsUnmovedByTheChange(t *testing.T) {
	item := Item{ID: "an-item", Status: StatusActive}
	if changed := item.ClearNeedsPerson(); !reflect.DeepEqual(changed, item) {
		t.Fatalf("an item with no line was changed: %+v", changed)
	}
}
