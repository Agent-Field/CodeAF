package placeorg

import "testing"

func TestAssembleRejectsACycleAndAMissingParent(t *testing.T) {
	_, err := Assemble([]BuiltNode{
		{ID: "a", Parent: "b", Depth: 2},
		{ID: "b", Parent: "a", Depth: 3},
	}, nil, nil)
	if err == nil {
		t.Fatal("a cycle was assembled")
	}
	_, err = Assemble([]BuiltNode{
		{ID: "a", Parent: "missing", Depth: 2},
	}, nil, nil)
	if err == nil {
		t.Fatal("a missing parent was assembled")
	}
}

func TestAssembleHomesAndCentroidRoundTrip(t *testing.T) {
	tree, err := Assemble([]BuiltNode{
		{ID: "r", Depth: 1},
		{ID: "f", Parent: "r", Depth: 2, Members: []string{"a"}},
	}, map[string]string{"a": "f"}, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Roots(); len(got) != 1 || got[0] != "r" {
		t.Fatalf("roots = %v", got)
	}
	root, ok := tree.Node("r")
	if !ok || len(root.Children) != 1 || root.Children[0] != "f" {
		t.Fatalf("root children = %+v", root)
	}
	if home, ok := tree.Home("a"); !ok || home != "f" {
		t.Fatalf("home = %q %v", home, ok)
	}
	inbox := tree.Inbox()
	if len(inbox) != 1 || inbox[0] != "b" {
		t.Fatalf("inbox = %v", inbox)
	}
	sum := []float32{1, 2}
	if err := SetCentroid(tree, "f", SpacePrimary, Centroid{Sum: sum, Count: 1}); err != nil {
		t.Fatal(err)
	}
	sum[0] = 9
	node, _ := tree.Node("f")
	if node.Centroids[SpacePrimary].Sum[0] != 1 || node.Centroids[SpacePrimary].Count != 1 {
		t.Fatalf("centroid = %+v", node.Centroids[SpacePrimary])
	}
	ClearCentroid(tree, "f", SpacePrimary)
	node, _ = tree.Node("f")
	if _, ok := node.Centroids[SpacePrimary]; ok {
		t.Fatal("centroid survived ClearCentroid")
	}
}

func TestAssembleRejectsAChatThatIsBothHomedAndInNow(t *testing.T) {
	_, err := Assemble([]BuiltNode{
		{ID: "r", Depth: 1, Members: []string{"a"}},
	}, map[string]string{"a": "r"}, []string{"a"})
	if err == nil {
		t.Fatal("a chat was both homed and in Now")
	}
}
