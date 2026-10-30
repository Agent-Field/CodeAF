package teams

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// A CONVERSATION IS FOUND BY ITS MEMBERSHIP'S FILE OR ITS KEY. The index
// names a membership by its file's folder; when its key is the same
// conversation spelled through another folder (a link to it), asking by that
// spelling still finds it, as reading every team did, so a conversation a
// team names is never taken for one nobody holds.
func TestAConversationIsFoundByItsFileOrItsKey(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	must(t, os.MkdirAll(real, 0o700))
	must(t, os.Symlink(real, link))
	realT, linkT := filepath.Join(real, "transcript.jsonl"), filepath.Join(link, "transcript.jsonl")
	must(t, os.WriteFile(realT, nil, 0o600))
	resolved, err := filepath.EvalSymlinks(realT)
	must(t, err)

	for name, c := range map[string]struct {
		m   Member
		ask string
	}{
		"the file through the link, asked by the key": {Member{Key: resolved, File: linkT}, realT},
		"the key through the link, asked by it":       {Member{Key: linkT, File: realT}, linkT},
	} {
		for _, stale := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, names stale %v", name, stale), func(t *testing.T) {
				dir := t.TempDir()
				must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "dock", Members: []Member{c.m}}}))
				if stale {
					// A store an earlier build wrote: the names are not trusted and
					// every membership is looked at.
					h, err := acquire(Path(dir), false)
					must(t, err)
					_, err = h.db.Exec("UPDATE meta SET indexed = 0; UPDATE members SET conv = 'stale'")
					h.release()
					must(t, err)
				}
				if ok, err := ConversationReferenced(dir, c.ask); err != nil || !ok {
					t.Fatalf("referenced %v, %v", ok, err)
				}
				var held bool
				must(t, InspectConversation(dir, c.ask, func(referenced bool) error { held = referenced; return nil }))
				if !held {
					t.Fatal("the locked look missed it")
				}
			})
		}
	}
}
