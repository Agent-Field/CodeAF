package appwire

import (
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"strings"
)

const FeatureKnowledge = "knowledge"
const EventKnowledgeChanged = "knowledge.changed"

type KnowledgeHead struct {
	Summary              string   `json:"summary,omitempty"`
	UpdatedAt            string   `json:"updated_at,omitempty"`
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	TitleEdited          bool     `json:"title_edited,omitempty"`
	Folder               []string `json:"folder"`
	Kind                 string   `json:"kind"`
	Revision             string   `json:"revision"`
	HumanEdited          bool     `json:"human_edited"`
	NeedsEditorialUpdate bool     `json:"needs_editorial_update"`
	HasConflict          bool     `json:"has_conflict"`
	// FromChats is set when learning from chats wrote some of this page, not
	// only what a person added on purpose. The Library marks it "From your
	// chats", and a person can remove it.
	FromChats bool     `json:"from_chats,omitempty"`
	Sections  []string `json:"sections"`
}
type KnowledgeBuild struct {
	Phase    knowledge.BuildPhase `json:"phase"`
	Chats    int                  `json:"chats"`
	Imported int                  `json:"imported"`
	Sources  int                  `json:"sources"`
	Problems int                  `json:"problems"`
}
type KnowledgeSnapshot struct {
	CanRename  bool            `json:"can_rename"`
	SavedChats *int            `json:"saved_chats,omitempty"`
	Revision   string          `json:"revision"`
	Enabled    bool            `json:"enabled"`
	Build      KnowledgeBuild  `json:"build"`
	Pending    int             `json:"pending"`
	Model      string          `json:"model"`
	Pages      []KnowledgeHead `json:"pages"`
}
type KnowledgeRevision struct {
	Revision string `json:"revision"`
}

// KnowledgeCapture keeps words in the Library. Its first form is a passage
// from a saved agent reply (chat, message, text). Its second form is a
// workstream file the person chose to keep (file, text) and names no chat: the
// desk sends the file's own words, and the engine records which file they came
// from so the Library can lead back to it.
type KnowledgeCapture struct {
	Chat    string         `json:"chat,omitempty"`
	Message string         `json:"message,omitempty"`
	Text    string         `json:"text"`
	File    *KnowledgeFile `json:"file,omitempty"`
}

// KnowledgeFile names a workstream file: the desk's workstream address (a
// Personal id, or org:<connection>/<id>), the file's id and its name.
type KnowledgeFile struct {
	Workstream string `json:"workstream"`
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
}

// FeatureKnowledgeCaptureFile is announced with the Library by an engine whose
// knowledge.capture takes the file form. A desk that does not see it leaves
// "Add to Library" off a file's menus, because an older engine would refuse
// every call for want of a chat.
const FeatureKnowledgeCaptureFile = "knowledge.capture_file"

// FeatureKnowledgeKeepAlways is announced by an engine whose knowledge.capture
// keeps a person's deliberate add (a file or a passage) with "Learn from chats
// and jobs" off, and writes it into the Library then (owner decision 5). A desk
// that does not see it offers Add to Library only while learning is on,
// because an older engine refuses the call otherwise.
const FeatureKnowledgeKeepAlways = "knowledge.keep_always"

// MaxKnowledgeCaptureFile bounds a kept file's words. A saved reply's passage is
// capped at 16000 characters; a file is a whole document, so it gets four times
// that, which the vault stores in spans.
const MaxKnowledgeCaptureFile = 64000

// KnowledgeLocation is where a Library source came from: a chat message, or,
// for a kept workstream file, the file (and then chat and message are empty).
type KnowledgeLocation struct {
	Chat    string         `json:"chat"`
	Message string         `json:"message"`
	File    *KnowledgeFile `json:"file,omitempty"`
}
type KnowledgeSettlement struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Conflict string `json:"conflict"`
	Keep     string `json:"keep"`
	Note     string `json:"note"`
}
type KnowledgeMutation struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Markdown string `json:"markdown"`
}
type KnowledgeRename struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Title    string `json:"title"`
}
type KnowledgeSearchHit struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Folder  []string `json:"folder"`
	Snippet string   `json:"snippet"`
}

// KnowledgeLibrary is the same profile vault the terminal reads, reached through
// explicit commands so an older engine never advertises an unusable Library.
type KnowledgeLibrary interface {
	KnowledgeSnapshot() (KnowledgeSnapshot, error)
	KnowledgeRead(string) (knowledge.PageDetail, error)
	KnowledgeSearch(string, int) ([]KnowledgeSearchHit, error)
	KnowledgeRecall(string, string) (knowledge.Answer, error)
	KnowledgeEdit(KnowledgeMutation) (KnowledgeRevision, error)
	KnowledgeRename(KnowledgeRename) (KnowledgeRevision, error)
	KnowledgeDelete(KnowledgeMutation) error
	KnowledgeBuild() error
	KnowledgeCapture(KnowledgeCapture) error
	KnowledgeSettle(KnowledgeSettlement) (KnowledgeRevision, error)
	KnowledgeSource(string, string) (KnowledgeLocation, error)
}

func withKnowledgeFeature(h Hello, e Engine) Hello {
	if _, ok := e.(KnowledgeLibrary); ok {
		return withFeature(withFeature(withFeature(h, FeatureKnowledge), FeatureKnowledgeCaptureFile), FeatureKnowledgeKeepAlways)
	}
	return h
}
func libraryCall[P, R any](call func(KnowledgeLibrary, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		lib, ok := e.(KnowledgeLibrary)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This version of codeaf cannot show its Library")
		}
		return call(lib, p)
	})
}
func init() {
	methods["knowledge.capture"] = libraryCall(func(l KnowledgeLibrary, p KnowledgeCapture) (*Empty, error) {
		if p.File != nil {
			if p.Chat != "" || p.Message != "" || p.File.Workstream == "" || p.File.ID == "" || len(p.File.Workstream) > 256 || len(p.File.ID) > 256 || len(p.File.Name) > 512 {
				return nil, Errorf(CodeBadParams, "Choose a file in a workstream")
			}
			if strings.TrimSpace(p.Text) == "" || len(p.Text) > MaxKnowledgeCaptureFile {
				return nil, Errorf(CodeBadParams, "Choose a file with words in it, under %d characters", MaxKnowledgeCaptureFile)
			}
			return done(l.KnowledgeCapture(p))
		}
		if p.Chat == "" || p.Message == "" || strings.TrimSpace(p.Text) == "" || len(p.Text) > 16000 {
			return nil, Errorf(CodeBadParams, "Choose a passage under 16000 characters from a saved reply")
		}
		return done(l.KnowledgeCapture(p))
	})

	methods["knowledge.settle"] = libraryCall(func(l KnowledgeLibrary, p KnowledgeSettlement) (*KnowledgeRevision, error) {
		if p.ID == "" || p.Revision == "" || p.Conflict == "" || p.Keep == "" || len(p.Note) > 16000 {
			return nil, Errorf(CodeBadParams, "Choose an account in the current page revision")
		}
		r, e := l.KnowledgeSettle(p)
		return &r, e
	})
	methods["knowledge.source"] = libraryCall(func(l KnowledgeLibrary, p struct {
		ID     string `json:"id"`
		Source string `json:"source"`
	}) (*KnowledgeLocation, error) {
		if p.ID == "" || p.Source == "" {
			return nil, Errorf(CodeBadParams, "Choose a page source")
		}
		r, e := l.KnowledgeSource(p.ID, p.Source)
		return &r, e
	})

	methods["knowledge.snapshot"] = libraryCall(func(l KnowledgeLibrary, _ struct{}) (*KnowledgeSnapshot, error) {
		r, e := l.KnowledgeSnapshot()
		return &r, e
	})
	methods["knowledge.read"] = libraryCall(func(l KnowledgeLibrary, p IDParams) (*knowledge.PageDetail, error) {
		if e := needID("knowledge.read", p.ID); e != nil {
			return nil, e
		}
		r, e := l.KnowledgeRead(p.ID)
		return &r, e
	})
	methods["knowledge.search"] = libraryCall(func(l KnowledgeLibrary, p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}) (*[]KnowledgeSearchHit, error) {
		if len(p.Query) > 4096 {
			return nil, Errorf(CodeBadParams, "Search is too long")
		}
		if p.Limit <= 0 || p.Limit > 100 {
			p.Limit = 30
		}
		r, e := l.KnowledgeSearch(p.Query, p.Limit)
		return &r, e
	})
	methods["knowledge.recall"] = libraryCall(func(l KnowledgeLibrary, p struct {
		Question string `json:"question"`
		Scope    string `json:"scope"`
	}) (*knowledge.Answer, error) {
		if strings.TrimSpace(p.Question) == "" || len(p.Question) > 16000 {
			return nil, Errorf(CodeBadParams, "Ask a question under 16000 characters")
		}
		r, e := l.KnowledgeRecall(p.Question, p.Scope)
		return &r, e
	})
	methods["knowledge.edit"] = libraryCall(func(l KnowledgeLibrary, p KnowledgeMutation) (*KnowledgeRevision, error) {
		if e := needID("knowledge.edit", p.ID); e != nil {
			return nil, e
		}
		if p.Revision == "" {
			return nil, Errorf(CodeBadParams, "A page revision is required")
		}
		r, e := l.KnowledgeEdit(p)
		return &r, e
	})
	methods["knowledge.rename"] = libraryCall(func(l KnowledgeLibrary, p KnowledgeRename) (*KnowledgeRevision, error) {
		if e := needID("knowledge.rename", p.ID); e != nil {
			return nil, e
		}
		if p.Revision == "" {
			return nil, Errorf(CodeBadParams, "A page revision is required")
		}
		r, e := l.KnowledgeRename(p)
		return &r, e
	})
	methods["knowledge.delete"] = libraryCall(func(l KnowledgeLibrary, p KnowledgeMutation) (*Empty, error) {
		if e := needID("knowledge.delete", p.ID); e != nil {
			return nil, e
		}
		if p.Revision == "" {
			return nil, Errorf(CodeBadParams, "A page revision is required")
		}
		return done(l.KnowledgeDelete(p))
	})
	methods["knowledge.build"] = libraryCall(func(l KnowledgeLibrary, _ struct{}) (*Empty, error) { return done(l.KnowledgeBuild()) })
}
