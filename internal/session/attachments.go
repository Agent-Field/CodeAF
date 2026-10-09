package session

import (
	"mime"
	"path/filepath"
	"strings"
)

// Attachment kinds a surface draws differently: a picture is a thumbnail, a file
// is a chip.
const (
	AttachmentImage = "image"
	AttachmentFile  = "file"
)

// journalPartFile is the journal's word for a file a person attached. It lives in
// a sessionEntry's Files, never in Parts: Parts line up with a message's content
// parts so a replay can rebuild the pictures, and a file is never a content part
// (the model is told its path, not its bytes).
const journalPartFile = "file"

// AttachmentRef is one thing a person attached to a message, as a person-facing
// surface needs it: where it is, what to call it and how to draw it.
//
// It is read from the journal's own record of the message and never from the
// message's words, so a line that merely MENTIONS "attached file:" is not one. It
// is nil on every message that carried nothing, and on every message from a file
// written before files were recorded structurally (the emptiness law: a surface
// draws nothing for nothing).
type AttachmentRef struct {
	Path string
	Name string
	// MIME is "" when nobody can say; a guessed type is how a file opens in the
	// wrong program.
	MIME string
	// Kind is [AttachmentImage] or [AttachmentFile].
	Kind string
}

// AttachedSentence is what the MODEL is told about the files on a message, and
// it is a PATH rather than a payload: a file is a file, and the engine already
// has tools to read one.
//
// IT IS HERE, NOT IN THE REMOTE PACKAGE, BECAUSE THREE READERS NEED ONE SENTENCE:
// the surface that composes it for a local session, the engine that composes it
// for a hosted one, and the display shaping that takes it back OFF a message
// whose attachments were recorded structurally. Three copies would be three
// chances to drift apart, and the third would then fail to find the words.
func AttachedSentence(text string, paths []string) string {
	if len(paths) == 0 {
		return text
	}
	block := attachedFilesBlock(paths)
	if strings.TrimSpace(text) == "" {
		return block
	}
	return strings.TrimRight(text, " \t\n") + "\n\n" + block
}

// attachedFilesBlock is the sentence's file half, the part that is the same whatever
// the person said beside it.
func attachedFilesBlock(paths []string) string {
	if len(paths) == 1 {
		return "attached file: " + paths[0]
	}
	return "attached files:\n" + strings.Join(paths, "\n")
}

// withoutAttachedBlock is the person's own words: the message with the
// model-facing file sentence taken off. Text that does not hold the block is
// returned as it was.
func withoutAttachedBlock(text, block string) string {
	at := strings.LastIndex(text, block)
	if at < 0 {
		return text
	}
	before := strings.TrimRight(text[:at], " \t\n")
	return strings.TrimSpace(before + text[at+len(block):])
}

// fileJournalParts is the journal's record of the files one message named.
func fileJournalParts(paths []string) []journalPart {
	parts := make([]journalPart, 0, len(paths))
	for _, path := range paths {
		parts = append(parts, journalPart{Type: journalPartFile, Path: path, MIME: guessMIME(path)})
	}
	return parts
}

// guessMIME names a type by extension, "" when the extension says nothing.
func guessMIME(path string) string {
	kind := mime.TypeByExtension(filepath.Ext(path))
	cut, _, _ := strings.Cut(kind, ";")
	return strings.TrimSpace(cut)
}

// fileAttachments turns the journal's file records into what a surface draws.
func fileAttachments(parts []journalPart) []AttachmentRef {
	refs := make([]AttachmentRef, 0, len(parts))
	for _, part := range parts {
		refs = append(refs, AttachmentRef{
			Path: part.Path,
			Name: filepath.Base(part.Path),
			MIME: part.MIME,
			Kind: AttachmentFile,
		})
	}
	return refs
}

// imageAttachments turns a message's picture paths into what a surface draws.
func imageAttachments(paths []string) []AttachmentRef {
	refs := make([]AttachmentRef, 0, len(paths))
	for _, path := range paths {
		refs = append(refs, AttachmentRef{
			Path: path,
			Name: filepath.Base(path),
			MIME: imageMediaTypes[strings.ToLower(filepath.Ext(path))],
			Kind: AttachmentImage,
		})
	}
	return refs
}

// rememberFiles indexes one message's file records under the block that names
// them in its words. The block is the key because the message the display
// shaping sees can carry the session's own parts in front of the person's (a plan
// digest, a skills block), so no whole-message fingerprint matches between the
// moment of writing and the moment of drawing; the block is the one piece both
// share.
func rememberFiles(files map[string][]journalPart, parts []journalPart) {
	if files == nil || len(parts) == 0 {
		return
	}
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		paths = append(paths, part.Path)
	}
	files[attachedFilesBlock(paths)] = parts
}

// attachmentsOf is everything the journal recorded as attached to one user
// message, and the words with the file sentence off when files were recorded.
// The NIL RECEIVER answers the words unchanged and the pictures only, for the
// reason [sessionFile.imageRefs] answers nil.
func (s *sessionFile) attachmentsOf(words string, pictures []string) (string, []AttachmentRef) {
	refs := imageAttachments(pictures)
	if s == nil {
		return words, nilIfEmpty(refs)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for block, parts := range s.files {
		if strings.Contains(words, block) {
			words = withoutAttachedBlock(words, block)
			refs = append(refs, fileAttachments(parts)...)
			break
		}
	}
	return words, nilIfEmpty(refs)
}

func nilIfEmpty(refs []AttachmentRef) []AttachmentRef {
	if len(refs) == 0 {
		return nil
	}
	return refs
}

// splitFileParts parts the references a message was handed into the pictures,
// which line up with its content parts, and the files, which do not. They travel
// through the one variadic so that no writer between the door and the file has to
// learn a second argument.
func splitFileParts(refs []journalPart) (pictures, files []journalPart) {
	for _, ref := range refs {
		if ref.Type == journalPartFile {
			files = append(files, ref)
			continue
		}
		pictures = append(pictures, ref)
	}
	return pictures, files
}

// rememberFileParts indexes files as they are written, so a page drawn in this
// process agrees with the one a replay draws.
func (s *sessionFile) rememberFileParts(files []journalPart) {
	if s == nil || len(files) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = make(map[string][]journalPart)
	}
	rememberFiles(s.files, files)
}
