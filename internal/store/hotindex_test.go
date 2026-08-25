package store

import (
	"path/filepath"
	"testing"
)

// THE HOT INDEXES MUST REACH A DATABASE THAT ALREADY EXISTS.
//
// Both of them stand beside base columns rather than migration columns, so they
// live in the schema strings themselves — which every open executes, not only
// the first — instead of beside nodes_charter in the column migration. That is
// the whole of the upgrade story and it is worth pinning, because the failure
// mode is silent: an existing brain would simply keep scanning every message it
// has ever held to answer a mailbox poll, and nothing would say so.
func TestHotIndexesInstallOnAnExistingStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "leaf", Brief: "work", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "errand-one", Intent: "do the thing"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	if _, err := graph.PostMessage(Message{SessionID: "errand-one", Role: RoleUser,
		Body: "steer left", NodeID: "leaf"}); err != nil {
		t.Fatalf("post message: %v", err)
	}

	// Stand the store back down to what an older build left behind.
	for _, index := range []string{"messages_node_seq", "nodes_session"} {
		if _, err := graph.db.Exec(`DROP INDEX IF EXISTS ` + index); err != nil {
			t.Fatalf("drop %s: %v", index, err)
		}
	}
	if err := graph.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	for _, index := range []string{"messages_node_seq", "nodes_session"} {
		var found int
		if err := reopened.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&found); err != nil {
			t.Fatalf("read schema: %v", err)
		}
		if found != 1 {
			t.Fatalf("%s did not reach the existing store", index)
		}
	}

	// And the reads they exist for still answer the same thing they always did.
	messages, err := reopened.NodeMessages("leaf", 0, 10)
	if err != nil || len(messages) != 1 || messages[0].Body != "steer left" {
		t.Fatalf("node messages = %+v err=%v", messages, err)
	}
	nodes, err := reopened.SessionMemberNodes("errand-one")
	if err != nil || len(nodes) != 1 || nodes[0].ID != "leaf" {
		t.Fatalf("session member nodes = %+v err=%v", nodes, err)
	}
	if others, err := reopened.SessionMemberNodes("somebody-else"); err != nil || len(others) != 0 {
		t.Fatalf("another errand's nodes leaked in: %+v err=%v", others, err)
	}
}
