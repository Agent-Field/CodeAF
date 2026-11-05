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
	Folder               []string `json:"folder"`
	Kind                 string   `json:"kind"`
	Revision             string   `json:"revision"`
	HumanEdited          bool     `json:"human_edited"`
	NeedsEditorialUpdate bool     `json:"needs_editorial_update"`
	HasConflict          bool     `json:"has_conflict"`
	Sections             []string `json:"sections"`
}
type KnowledgeBuild struct {
	Phase    knowledge.BuildPhase `json:"phase"`
	Chats    int                  `json:"chats"`
	Imported int                  `json:"imported"`
	Sources  int                  `json:"sources"`
	Problems int                  `json:"problems"`
}
type KnowledgeSnapshot struct {
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
type KnowledgeCapture struct {
	Chat    string `json:"chat"`
	Message string `json:"message"`
	Text    string `json:"text"`
}
type KnowledgeLocation struct {
	Chat    string `json:"chat"`
	Message string `json:"message"`
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
	KnowledgeDelete(KnowledgeMutation) error
	KnowledgeBuild() error
	KnowledgeCapture(KnowledgeCapture) error
	KnowledgeSettle(KnowledgeSettlement) (KnowledgeRevision, error)
	KnowledgeSource(string, string) (KnowledgeLocation, error)
}

func withKnowledgeFeature(h Hello, e Engine) Hello {
	if _, ok := e.(KnowledgeLibrary); ok {
		return withFeature(h, FeatureKnowledge)
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
