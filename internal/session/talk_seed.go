package session

import (
	"errors"
	"os"
	"strings"
)

// SeedConversation writes a conversation that has not started yet: its
// journal's header, its name, and one opening note the model reads as the
// first thing in the conversation — and NO TURN. It is how the factory floor's
// `T` makes an item's own conversation (cmd/codeaf's talk maker): the item is
// in front of the model the moment the person says anything, and nothing was
// spent making it.
//
// THE NOTE IS THE SESSION'S, NOT THE PERSON'S. It is journaled as a note line
// ([sessionEntry.Note]), so a reopened conversation draws it as the session's
// own words rather than putting them in the person's mouth, and the model
// still reads it as a user-role message, which is what it must read it as.
//
// A path that already holds a conversation is refused: a seed is a first line,
// and writing one under somebody's conversation would be words in the middle of
// it. The folder's meta.json, when the path is a session folder's journal,
// takes the name too, so every list that reads names from it shows this one.
func SeedConversation(path, cwd, title, note string) error {
	path, note = strings.TrimSpace(path), strings.TrimSpace(note)
	if path == "" || note == "" {
		return errors.New("a seeded conversation needs a file and an opening note")
	}
	journal, replayed, err := openSessionFile(path, cwd, "", sessionIDOfFolder(path))
	if err != nil {
		return err
	}
	defer journal.Close()
	if replayed.existed {
		return errors.New("that conversation has already started")
	}
	journal.appendTitle(title, "")
	if !journal.appendNote(textMessage("user", note), noteMarks{}) {
		return errors.New("the opening note could not be written")
	}
	dir := DirOf(path)
	if title = strings.TrimSpace(title); title != "" {
		if meta, err := LoadMeta(dir); err == nil && strings.TrimSpace(meta.ID) != "" {
			meta.Title = title
			_ = SaveMeta(dir, meta)
		}
	}
	return nil
}

// RetitleConversation gives a conversation NOBODY HAS OPEN the name title: one
// literal title line appended to its journal, which is where an open reads a
// name from, and the folder's row beside it. It is for a caller that must
// rename a conversation without assembling it (a workspace it should not
// open again, a provider it does not have). A journal somebody holds is
// refused, as any second claim on one is; that conversation is renamed
// through its own door ([Agent.RenameByPerson]). A path that holds no
// conversation is refused too: this names one, it never makes one.
func RetitleConversation(path, title string) error {
	path, title = strings.TrimSpace(path), strings.Join(strings.Fields(title), " ")
	if path == "" || title == "" {
		return errors.New("a conversation is renamed by its file and a name")
	}
	if _, err := os.Stat(path); err != nil {
		return errors.New("there is no conversation there to rename")
	}
	journal, replayed, err := openSessionFile(path, "", "", sessionIDOfFolder(path))
	if err != nil {
		return err
	}
	defer journal.Close()
	if !replayed.existed {
		return errors.New("there is no conversation there to rename")
	}
	journal.appendNamedTitle(title, true)
	dir := DirOf(path)
	if meta, err := LoadMeta(dir); err == nil && strings.TrimSpace(meta.ID) != "" {
		meta.Title, meta.LiteralName = title, true
		_ = SaveMeta(dir, meta)
	}
	return nil
}

// sessionIDOfFolder is the id a seeded journal's header carries: its folder's
// meta id when the journal sits in a session folder, and "" (a fresh id) when
// it does not, which is [openSessionFile]'s own rule for a flat file.
func sessionIDOfFolder(path string) string {
	meta, err := LoadMeta(DirOf(path))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(meta.ID)
}
