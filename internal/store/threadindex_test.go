package store

import "testing"

// THE SWITCHER IS AN INDEX, NOT A LIVENESS QUERY.
//
// This is the regression for the bug that made the chats feature look absent: a
// store holding three real conversations, all of them answered properly, drew a
// switcher with nothing in it — because the list was built from
// [Store.OpenThreads], which drops every thread without an unresolved arc. The
// only row left was the `new thread` door, preselected, and enter on it
// abandoned the conversation the reader was standing in.
//
// [Store.ThreadIndex] lists them all. Openness stays on the row as a decoration.
func TestThreadIndexListsSettledThreadsThatOpenThreadsDrops(t *testing.T) {
	graph := openThreadStore(t)

	// Two conversations that ended properly: the person asked, the head
	// answered, nothing is owed in either direction.
	postThreadMessage(t, graph, Message{SessionID: "settled-one", Role: RoleUser, Body: "what is the release date?"})
	postThreadMessage(t, graph, Message{SessionID: "settled-one", Role: RoleAgent, Body: "The fourteenth."})
	postThreadMessage(t, graph, Message{SessionID: "settled-two", Role: RoleUser, Body: "rename the column"})
	postThreadMessage(t, graph, Message{SessionID: "settled-two", Role: RoleAgent, Body: "Renamed and pushed."})

	// And one that is genuinely open, so the two reads can be told apart.
	postThreadMessage(t, graph, Message{SessionID: "open-one", Role: RoleUser, Body: "and the migration?"})

	open, err := graph.OpenThreads(20)
	if err != nil {
		t.Fatalf("open threads: %v", err)
	}
	if _, listed := arcFor(open, "settled-one"); listed {
		t.Fatal("OpenThreads listed a settled thread; it is the liveness query and must not")
	}
	if len(open) != 1 {
		t.Fatalf("OpenThreads returned %d arcs, want only the open one", len(open))
	}

	index, err := graph.ThreadIndex(20)
	if err != nil {
		t.Fatalf("thread index: %v", err)
	}
	for _, want := range []string{"settled-one", "settled-two", "open-one"} {
		arc, listed := arcFor(index, want)
		if !listed {
			t.Fatalf("the index dropped %q; a switcher built on it would not show it", want)
		}
		if arc.SessionID != want {
			t.Fatalf("the index returned a zeroed arc for %q: %+v", want, arc)
		}
		if arc.LastActive.IsZero() {
			t.Fatalf("the index returned %q with no activity mark; the row cannot be ordered or timed", want)
		}
	}

	// The decoration survives: the open one still says what is open in it, and
	// the settled ones say nothing rather than something invented.
	if arc, _ := arcFor(index, "open-one"); arc.Open != ThreadOpenUnanswered {
		t.Fatalf("the index lost the open kind: %q", arc.Open)
	}
	if arc, _ := arcFor(index, "settled-one"); arc.Open != "" {
		t.Fatalf("a settled thread claims an open arc: %q", arc.Open)
	}
	// A settled row still carries a line to recognise the conversation by, and
	// it is the AGENT's last word rather than the reader's own — "left at" is
	// what the conversation was saying when you walked away.
	if arc, _ := arcFor(index, "settled-one"); arc.Left != "The fourteenth." {
		t.Fatalf("a settled thread's left-at line is %q, want the agent's last word", arc.Left)
	}
}
