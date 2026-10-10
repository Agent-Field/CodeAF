package decide

import "testing"

func TestSentenceNamesThePlaceAndTheReason(t *testing.T) {
	got := Sentence("permission", "Marketing", "You allowed go test 6 times.")
	if want := "Allowed automatically by Marketing · You allowed go test 6 times. · Why?"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := Sentence("choice", "", ""); got != "Decided automatically by this place · Why?" {
		t.Fatalf("no reason, no place: %q", got)
	}
}

func TestComposeSingleGroupAndNothing(t *testing.T) {
	one := ReceiptOf(Decision{ID: "a", AskKind: "permission", By: "mkt", Because: "seen before", Percent: 93, Reversible: true,
		QuestionRef: QuestionRef{Kind: "consent", ID: "7"}}, "Marketing")
	if one.Why != (Why{By: "Marketing", Because: "seen before", Percent: 93, Reversible: true}) || one.Question.ID != "7" {
		t.Fatalf("receipt = %+v", one)
	}
	single, ok := Compose([]Receipt{one})
	if !ok || single.Kind != AsideKind || single.Text != one.Text || single.Why == nil || len(single.Children) != 0 {
		t.Fatalf("single = %+v", single)
	}
	group, ok := Compose([]Receipt{one, one, one})
	if !ok || group.Text != "Did 3 things" || len(group.Children) != 3 || group.Why != nil {
		t.Fatalf("group = %+v", group)
	}
	if _, ok := Compose(nil); ok {
		t.Fatal("no receipts must compose to nothing")
	}
}
