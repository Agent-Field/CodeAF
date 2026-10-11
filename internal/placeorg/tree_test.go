package placeorg

import (
	"reflect"
	"testing"
)

// treeOf builds   top -> mid -> leaf   with chats a,b at leaf, c at mid, d at top.
func treeOf() *Tree {
	return TreeFromNodes([]Node{
		{ID: "top", Depth: 1, Children: []string{"mid"}, Members: []string{"d"}},
		{ID: "mid", Parent: "top", Depth: 2, Children: []string{"leaf"}, Members: []string{"c"}},
		{ID: "leaf", Parent: "mid", Depth: 3, Members: []string{"a", "b"}},
	})
}

func TestApplyFilesParksAndSendsToNow(t *testing.T) {
	tr := treeOf()
	for _, op := range []Op{{Kind: OpFile, Chat: "c", Place: "leaf"}, {Kind: OpPark, Chat: "a", Place: "top"}, {Kind: OpInbox, Chat: "b"}} {
		if err := tr.apply(op); err != nil {
			t.Fatal(err)
		}
	}
	if h, _ := tr.Home("c"); h != "leaf" {
		t.Errorf("c is at %q", h)
	}
	if h, _ := tr.Home("a"); h != "top" {
		t.Errorf("a is at %q", h)
	}
	if _, ok := tr.Home("b"); ok || len(tr.Inbox()) != 1 {
		t.Error("b is not in Now")
	}
	if n, _ := tr.Node("leaf"); !reflect.DeepEqual(n.Members, []string{"c"}) {
		t.Errorf("leaf lists %v", n.Members)
	}
}

func TestCollapseFoldsTheWholeSubtreeIntoTheParent(t *testing.T) {
	tr := treeOf()
	if err := tr.apply(Op{Kind: OpCollapse, Place: "mid"}); err != nil {
		t.Fatal(err)
	}
	top, _ := tr.Node("top")
	if len(top.Children) != 0 || len(tr.nodes) != 1 {
		t.Errorf("the places under the folded one should be gone: %+v, %d nodes", top, len(tr.nodes))
	}
	for _, chat := range []string{"a", "b", "c", "d"} {
		if h, _ := tr.Home(chat); h != "top" {
			t.Errorf("%s is at %q, want top", chat, h)
		}
	}
}

func TestDissolvingATopLevelPlaceSendsEveryChatUnderItToNow(t *testing.T) {
	tr := treeOf()
	if err := tr.apply(Op{Kind: OpDissolve, Place: "top"}); err != nil {
		t.Fatal(err)
	}
	if len(tr.nodes) != 0 || len(tr.Roots()) != 0 || len(tr.Inbox()) != 4 {
		t.Errorf("%d places, roots %v, %d in Now", len(tr.nodes), tr.Roots(), len(tr.Inbox()))
	}
}

func TestSplitAndMergeMoveChatsBetweenPlaces(t *testing.T) {
	tr := treeOf()
	if err := tr.apply(Op{Kind: OpSplit, Place: "leaf+1", Parent: "leaf", Members: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := tr.Node("leaf+1"); n.Depth != 4 || !reflect.DeepEqual(n.Members, []string{"a"}) {
		t.Errorf("split child %+v", n)
	}
	if err := tr.apply(Op{Kind: OpMerge, Place: "leaf+1", Into: "leaf"}); err != nil {
		t.Fatal(err)
	}
	if h, _ := tr.Home("a"); h != "leaf" {
		t.Errorf("a is at %q after the merge", h)
	}
	if _, ok := tr.Node("leaf+1"); ok {
		t.Error("the merged place is still there")
	}
	if err := tr.apply(Op{Kind: OpMerge, Place: "top", Into: "leaf"}); err == nil {
		t.Error("a place was merged into one of its own children")
	}
}

func TestNamesLinksAndLensesAreRecordedOnceAndUnknownOpsAreRefused(t *testing.T) {
	tr := treeOf()
	tr.apply(Op{Kind: OpName, Place: "top", Name: "garden"})
	tr.apply(Op{Kind: OpLink, Chat: "a", Place: "top"})
	tr.apply(Op{Kind: OpLink, Chat: "a", Place: "top"})
	tr.apply(Op{Kind: OpLens, Place: "top", Name: "by month", Into: "month", Members: []string{"a"}})
	if n, _ := tr.Node("top"); n.Name != "garden" || len(n.Lenses) != 1 || n.Lenses[0].Axis != "month" {
		t.Errorf("top %+v", n)
	}
	if got := tr.Links("a"); !reflect.DeepEqual(got, []string{"top"}) {
		t.Errorf("links %v", got)
	}
	if err := tr.apply(Op{Kind: "invented"}); err == nil {
		t.Error("an invented op kind was applied")
	}
	if err := tr.apply(Op{Kind: OpName, Place: "nowhere", Name: "x"}); err == nil {
		t.Error("a missing place was named")
	}
}

func TestACloneSharesNothingWithItsOriginal(t *testing.T) {
	a := treeOf()
	b := a.clone(nil)
	b.apply(Op{Kind: OpDissolve, Place: "leaf"})
	if _, ok := a.Node("leaf"); !ok {
		t.Fatal("changing the clone changed the original")
	}
	if h, _ := a.Home("a"); h != "leaf" {
		t.Errorf("a moved in the original to %q", h)
	}
}
