package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// ── AN ORGANIZATION'S LIBRARY (factory/org_library.go, org-space DESIGN §2.1) ──
//
//	GET    /v1/library          every page's title, newest first
//	GET    /v1/library/{id}     one page with its words
//	POST   /v1/library          a person adds a page (Add to Library, or words they wrote)
//	PATCH  /v1/library/{id}     the person who added it, or the operator, changes it
//	DELETE /v1/library/{id}     the person who added it, or the operator, removes it
//
// EVERYONE IN THE ORGANIZATION READS EVERY PAGE, and every chat the server runs
// may search it (agentDoor.Library). NOTHING IS ADDED BUT BY A PERSON
// (OWNER-DECISIONS §5): there is no learning here, so Add to Library is the
// one door in. Where a page came from is said only to a person who may see that
// workstream: a page added from a private workstream does not name it.

var (
	errLibraryGone     = errors.New("that page is not in the Library")
	errLibraryNotYours = errors.New("only the person who added this page, or the operator, can change it")
	errLibraryFull     = errors.New("the Library holds 500 pages; remove one first")
)

func (h *Handler) orgLibraryRoutes() {
	h.route("/v1/library", map[string]http.HandlerFunc{http.MethodGet: h.listOrgLibrary, http.MethodPost: h.addOrgLibrary})
	h.route("/v1/library/{page}", map[string]http.HandlerFunc{http.MethodGet: h.getOrgLibrary,
		http.MethodPatch: h.patchOrgLibrary, http.MethodDelete: h.deleteOrgLibrary})
}

// libraryCaller is the signed-in member asking, or a refusal written. The
// Library is served where My space is: an organization's server.
func (h *Handler) libraryCaller(w http.ResponseWriter, r *http.Request) (Viewer, bool) {
	if !h.requireVerb(w, h.spacesServed()) {
		return Viewer{}, false
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return v, false
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "the Library is the organization's; sign in first")
		return v, false
	}
	return v, true
}

// libraryChangeable is whether v may change or remove p.
func libraryChangeable(v Viewer, p factory.OrgLibraryPage) bool {
	return v.Operator || factory.NormalizeHandle(p.By) == v.Handle
}

func (h *Handler) libraryWire(v Viewer, p factory.OrgLibraryPage, words bool, nameOf func(string) string, seeWS func(string) (string, bool)) api.OrgLibraryPage {
	by := factory.NormalizeHandle(p.By)
	out := api.OrgLibraryPage{ID: p.ID, Title: p.Title, By: by, Mine: by == v.Handle, Changeable: libraryChangeable(v, p),
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if words {
		out.Body = p.Body
	}
	if name := nameOf(by); name != by {
		out.ByName = name
	}
	if p.From != nil && seeWS != nil {
		if name, ok := seeWS(p.From.Workstream); ok {
			out.FromWorkstream, out.FromName = p.From.Workstream, name
		}
	}
	return out
}

// workstreamsSeenBy answers a workstream's name when v may see it.
func (h *Handler) workstreamsSeenBy(v Viewer) func(string) (string, bool) {
	var all factory.OrgWorkstreams
	read := false
	return func(id string) (string, bool) {
		if !read {
			all, _ = h.config.Store.Workstreams()
			read = true
		}
		ws, ok := all[id]
		if !ok || !v.canSee(ws) {
			return "", false
		}
		if ws.Space {
			return "My space", true
		}
		return ws.Name, true
	}
}

// libraryOrder is newest change first, then title.
func libraryOrder(a, b factory.OrgLibraryPage) int {
	if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

func (h *Handler) listOrgLibrary(w http.ResponseWriter, r *http.Request) {
	v, ok := h.libraryCaller(w, r)
	if !ok {
		return
	}
	lib, err := h.config.Store.OrgLibrary()
	if h.failed(w, err) {
		return
	}
	pages := make([]factory.OrgLibraryPage, 0, len(lib))
	for _, p := range lib {
		pages = append(pages, p)
	}
	slices.SortFunc(pages, libraryOrder)
	nameOf, seeWS := h.personNames(), h.workstreamsSeenBy(v)
	out := api.OrgLibraryList{Pages: make([]api.OrgLibraryPage, 0, len(pages))}
	for _, p := range pages {
		out.Pages = append(out.Pages, h.libraryWire(v, p, false, nameOf, seeWS))
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) getOrgLibrary(w http.ResponseWriter, r *http.Request) {
	v, ok := h.libraryCaller(w, r)
	if !ok {
		return
	}
	lib, err := h.config.Store.OrgLibrary()
	if h.failed(w, err) {
		return
	}
	p, ok := lib[r.PathValue("page")]
	if !ok {
		h.fail(w, http.StatusNotFound, errLibraryGone.Error())
		return
	}
	h.write(w, http.StatusOK, h.libraryWire(v, p, true, h.personNames(), h.workstreamsSeenBy(v)))
}

// libraryFields checks a title and words as a person sent them.
func libraryFields(title, body string) (string, string, error) {
	title = strings.Join(strings.Fields(title), " ")
	body = strings.TrimSpace(body)
	switch {
	case title == "":
		return "", "", errors.New("a Library page needs a title")
	case utf8.RuneCountInString(title) > api.OrgLibraryTitleMost:
		return "", "", errors.New("a Library page's title is at most 200 characters")
	case body == "":
		return "", "", errors.New("a Library page needs words; this one has none")
	case len(body) > api.OrgLibraryBodyMost:
		return "", "", errors.New("a Library page is at most 64000 bytes; add a shorter part")
	case !utf8.ValidString(body):
		return "", "", errors.New("a Library page is text; this is not")
	}
	return title, body, nil
}

func (h *Handler) addOrgLibrary(w http.ResponseWriter, r *http.Request) {
	v, ok := h.libraryCaller(w, r)
	if !ok {
		return
	}
	var body api.OrgLibraryNew
	if !h.decode(w, r, &body, false) {
		return
	}
	title, words, err := libraryFields(body.Title, body.Body)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var from *factory.OrgLibraryFrom
	if ws := strings.TrimSpace(body.FromWorkstream); ws != "" {
		// A page said to come from a file comes from one the person can read:
		// the name of the place is shown to everyone who may see it.
		got, ok, err := h.config.Store.Workstream(ws)
		if h.failed(w, err) {
			return
		}
		if !ok || !v.canSee(got) {
			h.fail(w, http.StatusNotFound, "that workstream is not one you can see")
			return
		}
		from = &factory.OrgLibraryFrom{Workstream: ws}
		if file := strings.TrimSpace(body.FromFile); file != "" {
			files, err := h.config.Store.WorkstreamFiles(ws)
			if h.failed(w, err) {
				return
			}
			if _, ok := files[file]; !ok {
				h.fail(w, http.StatusNotFound, "that file is not in the workstream")
				return
			}
			from.File = file
		}
	}
	now := h.clock().UTC()
	id, err := cell.NewID(now)
	if h.failed(w, err) {
		return
	}
	p := factory.OrgLibraryPage{ID: "lb_" + id, Title: title, Body: words, By: v.Handle, From: from, CreatedAt: now, UpdatedAt: now}
	if _, err := h.config.Store.EditOrgLibrary(func(all factory.OrgLibrary) error {
		if len(all) >= api.OrgLibraryPagesMost {
			return errLibraryFull
		}
		all[p.ID] = p
		return nil
	}); err != nil {
		h.libraryFailed(w, err)
		return
	}
	h.recordOrg(r, record.LibraryChanged, "added library page", orgObjectLibrary+p.ID, "")
	h.write(w, http.StatusCreated, h.libraryWire(v, p, true, h.personNames(), h.workstreamsSeenBy(v)))
}

// libraryFailed writes a store refusal in the person's words.
func (h *Handler) libraryFailed(w http.ResponseWriter, err error) {
	var bad libraryInvalid
	switch {
	case errors.As(err, &bad):
		h.fail(w, http.StatusUnprocessableEntity, bad.Error())
	case errors.Is(err, errLibraryFull):
		h.fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, errLibraryGone):
		h.fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errLibraryNotYours):
		h.fail(w, http.StatusForbidden, err.Error())
	default:
		h.failed(w, err)
	}
}

// libraryInvalid is a field refusal raised inside a store edit.
type libraryInvalid struct{ error }

func (h *Handler) patchOrgLibrary(w http.ResponseWriter, r *http.Request) {
	v, ok := h.libraryCaller(w, r)
	if !ok {
		return
	}
	var body api.OrgLibraryPatch
	if !h.decode(w, r, &body, false) {
		return
	}
	id := r.PathValue("page")
	var out factory.OrgLibraryPage
	if _, err := h.config.Store.EditOrgLibrary(func(all factory.OrgLibrary) error {
		p, ok := all[id]
		if !ok {
			return errLibraryGone
		}
		if !libraryChangeable(v, p) {
			return errLibraryNotYours
		}
		title, words := p.Title, p.Body
		if body.Title != nil {
			title = *body.Title
		}
		if body.Body != nil {
			words = *body.Body
		}
		var err error
		if p.Title, p.Body, err = libraryFields(title, words); err != nil {
			return libraryInvalid{err}
		}
		p.UpdatedAt = h.clock().UTC()
		all[id] = p
		out = p
		return nil
	}); err != nil {
		h.libraryFailed(w, err)
		return
	}
	h.recordOrg(r, record.LibraryChanged, "changed library page", orgObjectLibrary+id, "")
	h.write(w, http.StatusOK, h.libraryWire(v, out, true, h.personNames(), h.workstreamsSeenBy(v)))
}

func (h *Handler) deleteOrgLibrary(w http.ResponseWriter, r *http.Request) {
	v, ok := h.libraryCaller(w, r)
	if !ok {
		return
	}
	id := r.PathValue("page")
	if _, err := h.config.Store.EditOrgLibrary(func(all factory.OrgLibrary) error {
		p, ok := all[id]
		if !ok {
			return errLibraryGone
		}
		if !libraryChangeable(v, p) {
			return errLibraryNotYours
		}
		delete(all, id)
		return nil
	}); err != nil {
		h.libraryFailed(w, err)
		return
	}
	h.recordOrg(r, record.LibraryChanged, "removed library page", orgObjectLibrary+id, "")
	w.WriteHeader(http.StatusNoContent)
}

const orgObjectLibrary = "library:"

// ── the Library as a chat on the server reads it ──

// libraryDoor is an agent door that also reads the organization's Library;
// a server without one hands its agents the bare door, so the verb is absent.
type libraryDoor struct{ *agentDoor }

// withLibrary is door with the Library beside it where the server keeps one.
func (h *Handler) withLibrary(door *agentDoor) AgentDoor {
	if h.spacesServed() {
		return libraryDoor{door}
	}
	return door
}

// The amounts one call reads, so a search or a long page never floods the
// conversation.
const (
	libraryHitsMost     = 8
	librarySnippetBytes = 400
	libraryReadBytes    = 12000
)

type libraryHit struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	By      string `json:"by"`
	Snippet string `json:"snippet,omitempty"`
}

// Library answers org_library: a search, or one page from offset from.
func (d libraryDoor) Library(_ context.Context, query, page string, from int) (string, error) {
	lib, err := d.h.config.Store.OrgLibrary()
	if err != nil {
		return "", err
	}
	nameOf := d.h.personNames()
	if page != "" {
		p, ok := lib[page]
		if !ok {
			return "", errors.New("no Library page has that id; search the Library for it again")
		}
		if from > len(p.Body) {
			from = len(p.Body)
		}
		for from > 0 && from < len(p.Body) && !utf8.RuneStart(p.Body[from]) {
			from--
		}
		end := min(from+libraryReadBytes, len(p.Body))
		for end < len(p.Body) && !utf8.RuneStart(p.Body[end]) {
			end--
		}
		out := map[string]any{"id": p.ID, "title": p.Title, "by": nameOf(factory.NormalizeHandle(p.By)), "words": p.Body[from:end]}
		if end < len(p.Body) {
			out["more_from"] = end
		}
		b, err := json.Marshal(out)
		return string(b), err
	}
	terms := strings.Fields(strings.ToLower(query))
	type scored struct {
		p     factory.OrgLibraryPage
		score int
		at    int
	}
	var hits []scored
	for _, p := range lib {
		title, body := strings.ToLower(p.Title), strings.ToLower(p.Body)
		s, at := 0, -1
		for _, t := range terms {
			if strings.Contains(title, t) {
				s += 5
			}
			if n := strings.Count(body, t); n > 0 {
				s += min(n, 5)
				if i := strings.Index(body, t); at < 0 || i < at {
					at = i
				}
			}
		}
		if len(terms) == 0 || s > 0 {
			hits = append(hits, scored{p, s, at})
		}
	}
	slices.SortFunc(hits, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return libraryOrder(a.p, b.p)
	})
	out := struct {
		Pages []libraryHit `json:"pages"`
		Total int          `json:"total"`
	}{Pages: []libraryHit{}, Total: len(hits)}
	for _, x := range hits {
		if len(out.Pages) == libraryHitsMost {
			break
		}
		hit := libraryHit{ID: x.p.ID, Title: x.p.Title, By: nameOf(factory.NormalizeHandle(x.p.By))}
		if len(terms) > 0 {
			hit.Snippet = librarySnippet(x.p.Body, x.at)
		}
		out.Pages = append(out.Pages, hit)
	}
	if len(lib) == 0 {
		return "The organization's Library has no pages yet. A person adds one with Add to Library on a file.", nil
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// librarySnippet is the words around at (a byte offset into body, lowercased
// the same length), or the page's opening when at is unknown.
func librarySnippet(body string, at int) string {
	start := max(at-librarySnippetBytes/4, 0)
	if at < 0 || start >= len(body) {
		start = 0
	}
	for start > 0 && !utf8.RuneStart(body[start]) {
		start--
	}
	end := min(start+librarySnippetBytes, len(body))
	for end < len(body) && !utf8.RuneStart(body[end]) {
		end--
	}
	return strings.Join(strings.Fields(body[start:end]), " ")
}
