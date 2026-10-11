package audit

import (
	"reflect"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// plantTree fills a Tree from another package. The mutators are unexported
// on purpose — only the runner applies ops — and a test still has to hand
// the auditor a tree. The fields are set through the value's own address.
func plantTree(roots []string, nodes map[string]*placeorg.Node, home map[string]string, inbox []string) *placeorg.Tree {
	t := placeorg.NewTree()
	if home == nil {
		home = map[string]string{}
	}
	in := map[string]bool{}
	for _, id := range inbox {
		in[id] = true
	}
	rv := reflect.ValueOf(t).Elem()
	setUnexported(rv.FieldByName("nodes"), nodes)
	setUnexported(rv.FieldByName("roots"), roots)
	setUnexported(rv.FieldByName("home"), home)
	setUnexported(rv.FieldByName("links"), map[string][]string{})
	setUnexported(rv.FieldByName("inbox"), in)
	return t
}

func setUnexported(field reflect.Value, v any) {
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(v))
}

func placeNode(id, parent, name string, depth int, children, members []string) *placeorg.Node {
	return &placeorg.Node{
		ID: id, Parent: parent, Name: name, Depth: depth,
		Children: append([]string(nil), children...),
		Members:  append([]string(nil), members...),
	}
}

func chatsOf(ids ...string) []placeorg.Chat {
	out := make([]placeorg.Chat, len(ids))
	for i, id := range ids {
		out[i] = placeorg.Chat{ID: id, Title: id}
	}
	return out
}

func corpusOf(ids ...string) *placeorg.Corpus {
	return &placeorg.Corpus{Chats: chatsOf(ids...)}
}
