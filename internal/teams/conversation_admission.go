package teams

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConversationDeletedFile is the durable lifetime marker shared with the
// conversation engine. A stale membership picker cannot undo deletion.
const ConversationDeletedFile = ".conversation-deleted"

// Admission is checked under the store writer's lock, on the owning engine.
// Existing historical references remain readable; newly added members or
// newly appointed managers must not point at a permanently deleted journal.
func (f *File) checkIntroducedConversations(previous *File) error {
	for _, t := range f.Teams {
		if t.Closed() {
			continue
		}
		old, _ := previous.Team(t.ID)
		for _, m := range t.Members {
			prior, existed := old.Member(m.Key)
			if existed && prior.File == m.File && (t.Manager != m.Key || old.Manager == m.Key) {
				continue
			}
			file := m.File
			if file == "" {
				file = m.Key
			}
			if !filepath.IsAbs(file) {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(file), ConversationDeletedFile)); err == nil {
				return fmt.Errorf("that conversation was permanently deleted; choose another conversation")
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("cannot check whether that conversation was deleted: %w", err)
			}
		}
	}
	return nil
}
