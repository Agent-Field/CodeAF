package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── A WORKSTREAM'S FILES ───────────────────────────────────────────────────
//
// Files is a workstream's shared drive (api/workstream_files.go has the wire,
// factory/workstream_file.go the shape). A file POINTS at what it holds — a
// page, a canvas, an upload's bytes, an address, another file — so the
// routes here keep only where it sits, what it is called and who made it.
// The rules are the workstream's: you read the files of one you can see, and
// you change the files of one you can write and that is not archived.
//
// THE FOLD. A workstream made before Files existed kept pages, sources and
// canvases. The first read of its files folds every one of them in as a file
// pointing at it, and gives a workstream with no decisions page a Decisions
// document; nothing is copied and nothing is lost. Every later read folds in
// a page, source or canvas an older client added through its own route, so
// the two kinds of client never see different drives.

// The most an upload may be, and the most a request carrying one may be once
// base64 and JSON have grown it.
const (
	uploadMost     = 25 << 20
	uploadBodyMost = 36 << 20
	fileNameMost   = 300
)

var (
	errFileFolder   = workstreamInvalid("that folder is not one of this workstream's folders")
	errFileUnder    = workstreamInvalid("a folder cannot move into itself or a folder inside it")
	errFileNotGone  = workstreamInvalid("that file is not in Recently deleted")
	errFileKind     = workstreamInvalid("kind is one of folder, document, canvas, upload, link, repo or shortcut")
	errFileTarget   = workstreamInvalid("a shortcut names a file in a workstream you can see")
	errFileCanvas   = workstreamInvalid("canvas_id names a canvas made inside this workstream")
	errUploadTooBig = workstreamInvalid("an upload may be at most 25 MiB")
)

func (h *Handler) workstreamFileRoutes() {
	h.route("/v1/workstreams/{id}/files", map[string]http.HandlerFunc{http.MethodGet: h.listWorkstreamFiles, http.MethodPost: h.createWorkstreamFile})
	h.route("/v1/workstreams/{id}/files/move", map[string]http.HandlerFunc{http.MethodPost: h.moveWorkstreamFiles})
	h.route("/v1/workstreams/{id}/files/{fid}", map[string]http.HandlerFunc{http.MethodPatch: h.patchWorkstreamFile, http.MethodDelete: h.deleteWorkstreamFile})
	h.route("/v1/workstreams/{id}/files/{fid}/restore", map[string]http.HandlerFunc{http.MethodPost: h.restoreWorkstreamFile})
	h.route("/v1/workstreams/{id}/files/{fid}/content", map[string]http.HandlerFunc{http.MethodGet: h.workstreamFileContent})
}

// fileRefused writes the refusal a file change ended with.
func (h *Handler) fileRefused(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, store.ErrFileStale):
		h.fail(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, store.ErrFileGone):
		h.fail(w, http.StatusNotFound, err.Error())
	default:
		return h.partRefused(w, err)
	}
	return true
}

// ── the fold ──

// foldFiles is ws's files with everything it kept before Files folded in,
// and anything deleted longer ago than [factory.FileKeptDeleted] let go.
func (h *Handler) foldFiles(ws factory.OrgWorkstream) (map[string]factory.WorkstreamFile, error) {
	st := h.config.Store
	now := h.clock().UTC()
	seeded, err := st.FilesSeeded(ws.ID)
	if err != nil {
		return nil, err
	}
	// It makes NO Decisions document. The decisions are the workstream's own
	// kept list, drawn as its Decisions row (screen 12); a second, empty page
	// of that name was one more thing called Decisions that every chat opened
	// and found blank (live E6b, B4).
	var canvases []factory.OrgCanvas
	if all, err := st.Canvases(); err == nil {
		for _, c := range all {
			if c.Workstream == ws.ID {
				canvases = append(canvases, c)
			}
		}
	} else {
		return nil, err
	}
	slices.SortFunc(canvases, func(a, b factory.OrgCanvas) int { return a.CreatedAt.Compare(b.CreatedAt) })

	var purged []factory.WorkstreamFile
	files, err := st.EditWorkstreamFiles(ws.ID, now.Format(time.RFC3339Nano), func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		var changed []string
		pages, sources, canvasRefs := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for id, f := range files {
			if f.DeletedAt != nil && now.Sub(*f.DeletedAt) > factory.FileKeptDeleted {
				purged = append(purged, f)
				delete(files, id)
				changed = append(changed, id)
				continue
			}
			pages[f.PageID], sources[f.Source], canvasRefs[f.CanvasID] = true, true, true
		}
		for _, f := range purged {
			// A purged document's page goes with it below; it must not be
			// folded back in by this same read.
			pages[f.PageID] = true
		}
		add := func(f factory.WorkstreamFile) {
			if _, ok := files[f.ID]; ok {
				return
			}
			f.WS = ws.ID
			if f.CreatedAt.IsZero() {
				f.CreatedAt = f.ChangedAt
			}
			files[f.ID] = f
			changed = append(changed, f.ID)
		}
		// Pages that hold pages become folders, so the nesting is kept.
		hasChildren := map[string]bool{}
		for _, p := range ws.Pages {
			if p.Parent != "" {
				hasChildren[p.Parent] = true
			}
		}
		byID := map[string]factory.WorkstreamPage{}
		for _, p := range ws.Pages {
			byID[p.ID] = p
		}
		folderOf := func(p factory.WorkstreamPage) string {
			if p.Parent != "" && byID[p.Parent].ID != "" {
				return "fd_" + p.Parent
			}
			return ""
		}
		for _, p := range ws.Pages {
			if pages[p.ID] {
				continue
			}
			at := pageTime(p.UpdatedAt, ws.CreatedAt)
			by := firstNonEmpty(p.UpdatedBy, ws.CreatedBy)
			doc := factory.WorkstreamFile{
				ID: "fl_" + p.ID, Kind: factory.FileDocument, Name: firstNonEmpty(p.Title, "Untitled"), Folder: folderOf(p),
				MadeBy: factory.FileMaker{Kind: factory.MakerPerson, ID: by, Name: by}, ChangedAt: at, ChangedBy: by, PageID: p.ID,
			}
			if hasChildren[p.ID] {
				add(factory.WorkstreamFile{
					ID: "fd_" + p.ID, Kind: factory.FileFolder, Name: doc.Name, Folder: folderOf(p),
					MadeBy: doc.MadeBy, ChangedAt: at, ChangedBy: by,
				})
				doc.Folder = "fd_" + p.ID
			}
			add(doc)
		}
		for _, s := range ws.Sources {
			if sources[s.ID] {
				continue
			}
			f := factory.WorkstreamFile{
				ID: "fl_" + s.ID, Name: firstNonEmpty(s.Title, s.Ref), Source: s.ID,
				MadeBy:    factory.FileMaker{Kind: factory.MakerPerson, ID: ws.CreatedBy, Name: ws.CreatedBy},
				ChangedAt: ws.CreatedAt, ChangedBy: ws.CreatedBy,
			}
			switch s.Kind {
			case "repo":
				f.Kind, f.Repo = factory.FileRepo, s.Ref
			case "file":
				// A file source named a path before uploads existed; it is
				// kept as an upload with no bytes, its path as its address.
				f.Kind, f.URL, f.Upload = factory.FileUpload, s.Ref, &factory.UploadBlob{}
			default:
				f.Kind, f.URL = factory.FileLink, s.Ref
			}
			add(f)
		}
		for _, c := range canvases {
			if canvasRefs[c.ID] {
				continue
			}
			by := firstNonEmpty(c.CreatedBy, ws.CreatedBy)
			add(factory.WorkstreamFile{
				ID: "fl_" + c.ID, Kind: factory.FileCanvas, Name: h.canvasTitle(c.ID), CanvasID: c.ID,
				MadeBy: factory.FileMaker{Kind: factory.MakerPerson, ID: by, Name: by}, ChangedAt: c.CreatedAt, ChangedBy: c.UpdatedBy,
			})
		}
		return changed, !seeded, nil
	})
	if err != nil {
		return nil, err
	}
	for _, f := range purged {
		h.letGo(ws.ID, f)
	}
	return files, nil
}

// letGo drops what a purged file pointed at that nothing else holds: a
// document's page, its versions and its live document, and an upload's bytes
// when no other file names them. One left behind is unreachable, never wrong.
func (h *Handler) letGo(ws string, f factory.WorkstreamFile) {
	st := h.config.Store
	if f.Kind == factory.FileDocument && f.PageID != "" {
		_, _ = st.MirrorWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
			cur, ok := all[ws]
			if !ok {
				return nil, nil
			}
			at := slices.IndexFunc(cur.Pages, func(p factory.WorkstreamPage) bool { return p.ID == f.PageID })
			if at < 0 {
				return nil, nil
			}
			cur.Pages = slices.Delete(slices.Clone(cur.Pages), at, at+1)
			all[ws] = cur
			return []string{ws}, nil
		})
		_ = st.DropPageVersions(ws, f.PageID)
		_ = h.dropDoc("ws/" + ws + "/page/" + f.PageID)
	}
	if f.Upload != nil && f.Upload.Blob != "" {
		_ = st.DropUnusedBlob(f.Upload.Blob)
	}
}

// canvasTitle is a canvas's own title, "Canvas" when canvas-core is not
// running or does not answer.
func (h *Handler) canvasTitle(id string) string {
	if h.config.Canvas == nil {
		return "Canvas"
	}
	raw, err := h.config.Canvas.Raw(context.Background(), id, map[string]any{"cmd": "summary"})
	if err != nil {
		return "Canvas"
	}
	var s struct {
		Title string `json:"title"`
	}
	if json.Unmarshal(raw, &s) != nil || strings.TrimSpace(s.Title) == "" {
		return "Canvas"
	}
	return s.Title
}

func pageTime(at string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
		return t
	}
	return fallback
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ── reads ──

// filesAsRead are ws's files the way a reader sees them: a document named by
// its page's title and counting its versions and open comment threads, a
// shortcut named after what it shows when it has no name of its own.
func (h *Handler) filesAsRead(ws factory.OrgWorkstream, files map[string]factory.WorkstreamFile, q string) (live, deleted []factory.WorkstreamFile, err error) {
	pages := map[string]factory.WorkstreamPage{}
	for _, p := range ws.Pages {
		pages[p.ID] = p
	}
	open, err := h.config.Store.OpenPageComments(ws.ID)
	if err != nil {
		return nil, nil, err
	}
	q = strings.ToLower(strings.TrimSpace(q))
	for _, f := range files {
		if p, ok := pages[f.PageID]; ok && f.Kind == factory.FileDocument {
			f.Name = firstNonEmpty(p.Title, f.Name)
			vs, err := h.config.Store.PageVersions(ws.ID, p.ID)
			if err != nil {
				return nil, nil, err
			}
			f.Versions = len(vs)
			f.OpenComments = open[p.ID]
		}
		if q != "" && !fileMatches(f, pages[f.PageID], q) && !h.uploadSays(f, q) {
			continue
		}
		if f.DeletedAt != nil {
			deleted = append(deleted, f)
		} else {
			live = append(live, f)
		}
	}
	store.SortFiles(live)
	slices.SortFunc(deleted, func(a, b factory.WorkstreamFile) int { return b.DeletedAt.Compare(*a.DeletedAt) })
	return nonNil(live), deleted, nil
}

// fileMatches is whether f answers a search, by its name or by what it says.
func fileMatches(f factory.WorkstreamFile, page factory.WorkstreamPage, q string) bool {
	for _, s := range []string{f.Name, f.URL, f.Repo, page.BodyMD} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

func (h *Handler) listWorkstreamFiles(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	files, err := h.foldFiles(ws)
	if h.fileRefused(w, err) {
		return
	}
	// The fold may have added the Decisions page; read the pages again so
	// the document is named by it.
	if fresh, ok, err := h.config.Store.Workstream(ws.ID); err == nil && ok {
		ws = fresh
	}
	q := r.URL.Query().Get("q")
	out := api.WorkstreamFiles{}
	var deleted []factory.WorkstreamFile
	out.Files, deleted, err = h.filesAsRead(ws, files, q)
	if h.failed(w, err) {
		return
	}
	if r.URL.Query().Get("deleted") == "1" {
		out.Deleted = nonNil(deleted)
	}
	// A child reads its parent's files, read-only, when the reader can see
	// the parent.
	parentID, err := h.config.Store.WorkstreamParent(ws.ID)
	if h.failed(w, err) {
		return
	}
	if parentID != "" {
		if parent, ok, err := h.config.Store.Workstream(parentID); err == nil && ok && v.canSee(parent) {
			pf, err := h.foldFiles(parent)
			if h.failed(w, err) {
				return
			}
			if fresh, ok, err := h.config.Store.Workstream(parent.ID); err == nil && ok {
				parent = fresh
			}
			from, _, err := h.filesAsRead(parent, pf, q)
			if h.failed(w, err) {
				return
			}
			out.From = &api.FilesFrom{ID: parent.ID, Name: parent.Name, Files: from}
		}
	}
	h.write(w, http.StatusOK, out)
}

// workstreamFileContent answers an upload's bytes.
func (h *Handler) workstreamFileContent(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	files, err := h.config.Store.WorkstreamFiles(ws.ID)
	if h.failed(w, err) {
		return
	}
	f, ok := files[r.PathValue("fid")]
	if !ok || f.Upload == nil || f.Upload.Blob == "" {
		h.fail(w, http.StatusNotFound, store.ErrFileGone.Error())
		return
	}
	data, err := h.config.Store.Blob(f.Upload.Blob)
	if h.fileRefused(w, err) {
		return
	}
	typ := f.Upload.Mime
	if typ == "" {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ── writes ──

// liveFolder is whether id names a live folder among files ("" is the top).
func liveFolder(files map[string]factory.WorkstreamFile, id string) bool {
	if id == "" {
		return true
	}
	f, ok := files[id]
	return ok && f.Kind == factory.FileFolder && f.DeletedAt == nil
}

// under is whether folder is id or sits somewhere under it.
func under(files map[string]factory.WorkstreamFile, folder, id string) bool {
	for seen := 0; folder != "" && seen <= len(files); seen++ {
		if folder == id {
			return true
		}
		folder = files[folder].Folder
	}
	return false
}

// readFileNew decodes POST /files, whose body may carry an upload's bytes and
// so may be larger than the usual JSON body.
func (h *Handler) readFileNew(w http.ResponseWriter, r *http.Request, out *api.FileNew) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, uploadBodyMost))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(w, http.StatusRequestEntityTooLarge, errUploadTooBig.Error())
		} else {
			h.fail(w, http.StatusUnprocessableEntity, "could not read JSON body")
		}
		return false
	}
	d := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (h *Handler) createWorkstreamFile(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	var body api.FileNew
	if !h.readFileNew(w, r, &body) {
		return
	}
	// Fold first, so a canvas or page made a moment ago through its own
	// route is the file this one names rather than a second one.
	folded, err := h.foldFiles(ws)
	if h.fileRefused(w, err) {
		return
	}
	// The folder is checked before a document's page is made, so a refused
	// document never leaves a page behind.
	if !liveFolder(folded, body.Folder) {
		h.fail(w, http.StatusUnprocessableEntity, errFileFolder.Error())
		return
	}
	now := h.clock().UTC()
	id, err := cell.NewID(now)
	if h.failed(w, err) {
		return
	}
	maker := factory.FileMaker{Kind: factory.MakerPerson, ID: v.Handle, Name: h.personNames()(v.Handle)}
	if body.MadeBy != nil && (body.MadeBy.Kind == factory.MakerAfi || body.MadeBy.Kind == factory.MakerChat) {
		// An agent's file is stamped with the person whose agent made it;
		// the name is the agent's own.
		maker = factory.FileMaker{Kind: body.MadeBy.Kind, ID: body.MadeBy.ID, Name: firstNonEmpty(strings.TrimSpace(body.MadeBy.Name), "Afi"), For: v.Handle}
	}
	f := factory.WorkstreamFile{
		ID: "fl_" + id, Kind: body.Kind, Name: strings.TrimSpace(body.Name), Folder: body.Folder, FromTask: body.FromTask,
		MadeBy: maker, CreatedAt: now, ChangedAt: now, ChangedBy: v.Handle,
	}
	if !slices.Contains(factory.FileKinds, f.Kind) {
		h.fail(w, http.StatusUnprocessableEntity, errFileKind.Error())
		return
	}
	switch f.Kind {
	case factory.FileLink:
		u, err := url.Parse(strings.TrimSpace(body.URL))
		if err != nil || u.Scheme == "" || u.Host == "" {
			h.fail(w, http.StatusUnprocessableEntity, "url is a web address, https://…")
			return
		}
		f.URL = u.String()
		f.Name = firstNonEmpty(f.Name, u.Host+u.Path)
	case factory.FileRepo:
		f.Repo = strings.TrimSpace(body.Repo)
		if !strings.Contains(f.Repo, "/") {
			h.fail(w, http.StatusUnprocessableEntity, `repo is "owner/name"`)
			return
		}
		f.Name = firstNonEmpty(f.Name, f.Repo)
	case factory.FileUpload:
		if len(body.Data) > uploadMost {
			h.fail(w, http.StatusRequestEntityTooLarge, errUploadTooBig.Error())
			return
		}
		sha, err := h.config.Store.PutBlob(body.Data)
		if h.failed(w, err) {
			return
		}
		f.Upload = &factory.UploadBlob{Size: int64(len(body.Data)), Mime: body.Mime, Blob: sha}
	case factory.FileShortcut:
		if body.Target == nil {
			h.fail(w, http.StatusUnprocessableEntity, errFileTarget.Error())
			return
		}
		target, ok, err := h.config.Store.Workstream(body.Target.WS)
		if h.failed(w, err) {
			return
		}
		if !ok || !v.canSee(target) {
			h.fail(w, http.StatusUnprocessableEntity, errFileTarget.Error())
			return
		}
		tf, err := h.foldFiles(target)
		if h.failed(w, err) {
			return
		}
		t, ok := tf[body.Target.File]
		if !ok || t.DeletedAt != nil {
			h.fail(w, http.StatusUnprocessableEntity, errFileTarget.Error())
			return
		}
		f.Target = &factory.FileTarget{WS: target.ID, File: t.ID}
		f.Name = firstNonEmpty(f.Name, t.Name)
	case factory.FileCanvas:
		c, ok, err := h.config.Store.Canvas(body.CanvasID)
		if h.failed(w, err) {
			return
		}
		if !ok || c.Workstream != ws.ID {
			h.fail(w, http.StatusUnprocessableEntity, errFileCanvas.Error())
			return
		}
		f.CanvasID = c.ID
	}
	if err := plainWords("name", f.Name, 1, fileNameMost); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if f.Kind == factory.FileDocument {
		// A document is a page: its words live in the page's live document,
		// its versions are the page's versions.
		pid, err := cell.NewID(now)
		if h.failed(w, err) {
			return
		}
		page := factory.WorkstreamPage{ID: "pg_" + pid, Title: f.Name, Kind: "page", BodyMD: body.BodyMD,
			UpdatedAt: now.Format(time.RFC3339Nano), UpdatedBy: v.Handle, Rev: 1}
		saved, err := h.editPartPages(ws.ID, v, now, page)
		if h.partRefused(w, err) {
			return
		}
		ws = saved
		f.PageID = page.ID
		// A DOCUMENT'S FIRST WORDS ARE ITS FIRST VERSION. Without one, a
		// document Afi wrote opened History to nothing, and "Show versions"
		// showed nothing either (L3 F-21); the first save is the one every
		// later version is compared with.
		if strings.TrimSpace(page.BodyMD) != "" {
			_, _, _ = h.config.Store.AddPageVersion(ws.ID, page.ID, store.PageVersion{At: page.UpdatedAt, By: v.Handle, Title: page.Title, BodyMD: page.BodyMD, Reason: "manual"})
		}
	}
	status := http.StatusCreated
	files, err := h.config.Store.EditWorkstreamFiles(ws.ID, now.Format(time.RFC3339Nano), func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		if !liveFolder(files, f.Folder) {
			return nil, false, errFileFolder
		}
		if f.CanvasID != "" {
			// The fold already made a file for this canvas: name and place
			// that one rather than keeping two.
			for cid, cur := range files {
				if cur.CanvasID == f.CanvasID {
					cur.Name, cur.Folder, cur.FromTask, cur.MadeBy = f.Name, f.Folder, firstNonEmpty(f.FromTask, cur.FromTask), f.MadeBy
					files[cid] = cur
					f.ID, status = cid, http.StatusOK
					return []string{cid}, false, nil
				}
			}
		}
		files[f.ID] = f
		return []string{f.ID}, false, nil
	})
	if h.fileRefused(w, err) {
		return
	}
	saved := files[f.ID]
	h.recordWorkstream(r, ws.ID, "added file", saved.ID)
	h.publishWorkstreamPart(ws, api.PartFiles, saved.ID, h.orgActor(r), saved.Rev, false)
	h.write(w, status, saved)
}

// editPartPages adds page to the workstream id, the way a page made through
// its own route is added.
func (h *Handler) editPartPages(id string, v wsViewer, now time.Time, page factory.WorkstreamPage) (factory.OrgWorkstream, error) {
	_, saved, err := h.editPart(id, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		cur.Pages = append(cur.Pages, page)
		cur.UpdatedAt = now
		return true, nil
	})
	return saved, err
}

func (h *Handler) patchWorkstreamFile(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	rev, ok := h.ifMatch(w, r)
	if !ok {
		return
	}
	var body api.FilePatch
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Edited != "" && body.Edited != "person" && body.Edited != "agent" {
		h.fail(w, http.StatusUnprocessableEntity, `edited is "person" or "agent"`)
		return
	}
	if body.Name != nil {
		if err := plainWords("name", strings.TrimSpace(*body.Name), 1, fileNameMost); err != nil {
			h.fail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	fid := r.PathValue("fid")
	now := h.clock().UTC()
	files, err := h.config.Store.EditWorkstreamFiles(ws.ID, now.Format(time.RFC3339Nano), func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		f, ok := files[fid]
		if !ok || f.DeletedAt != nil {
			return nil, false, store.ErrFileGone
		}
		if f.Rev != rev {
			return nil, false, store.ErrFileStale
		}
		if body.Name != nil {
			f.Name = strings.TrimSpace(*body.Name)
		}
		if body.FromTask != nil {
			f.FromTask = *body.FromTask
		}
		if body.UsedBy != nil {
			f.UsedBy = slices.Compact(slices.Sorted(slices.Values(*body.UsedBy)))
		}
		if body.Edited == "person" {
			f.PersonEdited = true
		}
		f.ChangedAt, f.ChangedBy = now, v.Handle
		files[fid] = f
		return []string{fid}, false, nil
	})
	if h.fileRefused(w, err) {
		return
	}
	saved := files[fid]
	if body.Name != nil && saved.Kind == factory.FileDocument && saved.PageID != "" {
		// A document is named by its page: the page takes the new name too,
		// so every client that still reads pages sees it.
		_, _, _ = h.editPart(ws.ID, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
			at := slices.IndexFunc(cur.Pages, func(p factory.WorkstreamPage) bool { return p.ID == saved.PageID })
			if at < 0 || cur.Pages[at].Title == saved.Name {
				return false, nil
			}
			cur.Pages[at].Title, cur.Pages[at].Rev = saved.Name, cur.Pages[at].Rev+1
			cur.Pages[at].UpdatedAt, cur.Pages[at].UpdatedBy = now.Format(time.RFC3339Nano), v.Handle
			return true, nil
		})
	}
	verb := "changed file"
	if body.Name != nil {
		verb = "renamed file"
	}
	h.recordWorkstream(r, ws.ID, verb, fid)
	h.publishFileChange(ws, saved, h.orgActor(r), body.Edited != "")
	if body.Edited != "" {
		h.flagTasksOfFile(r, ws, saved, h.orgActor(r))
	}
	h.write(w, http.StatusOK, saved)
}

// publishFileChange records one change to one file. A change to its
// CONTENTS flags the tasks that use it: the event names them, so a task
// still running learns that what its agent read has moved under it.
func (h *Handler) publishFileChange(ws factory.OrgWorkstream, f factory.WorkstreamFile, by string, contents bool) {
	e := workstreamLive{WorkstreamEvent: api.WorkstreamEvent{
		Kind: api.EventWorkstreamChanged, ID: ws.ID, Part: api.PartFiles, PartID: f.ID, By: by, Rev: f.Rev,
	}, itemAudience: audienceOf(workstreamItem(ws))}
	if contents {
		e.Flagged = f.UsedBy
	}
	_ = live.Append(h.eventRoot(), live.New(api.EventWorkstreamChanged, 0, e))
}

func (h *Handler) moveWorkstreamFiles(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	var body api.FilesMove
	if !h.decode(w, r, &body, false) {
		return
	}
	if len(body.Files) == 0 {
		h.fail(w, http.StatusUnprocessableEntity, "files names at least one file to move")
		return
	}
	if body.To != "" && body.To != ws.ID {
		h.moveFilesAcross(w, r, ws, v, body)
		return
	}
	now := h.clock().UTC()
	ids := slices.Compact(slices.Sorted(slices.Values(body.Files)))
	files, err := h.config.Store.EditWorkstreamFiles(ws.ID, now.Format(time.RFC3339Nano), func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		if !liveFolder(files, body.Folder) {
			return nil, false, errFileFolder
		}
		var changed []string
		for _, id := range ids {
			f, ok := files[id]
			if !ok || f.DeletedAt != nil {
				return nil, false, store.ErrFileGone
			}
			if f.Kind == factory.FileFolder && under(files, body.Folder, id) {
				return nil, false, errFileUnder
			}
			if f.Folder == body.Folder {
				continue
			}
			f.Folder, f.ChangedAt, f.ChangedBy = body.Folder, now, v.Handle
			files[id] = f
			changed = append(changed, id)
		}
		return changed, false, nil
	})
	if h.fileRefused(w, err) {
		return
	}
	out := api.WorkstreamFiles{Files: []factory.WorkstreamFile{}}
	for _, id := range ids {
		out.Files = append(out.Files, files[id])
		h.publishWorkstreamPart(ws, api.PartFiles, id, h.orgActor(r), files[id].Rev, false)
	}
	h.recordWorkstream(r, ws.ID, "moved files", strings.Join(ids, " "))
	h.write(w, http.StatusOK, out)
}

// errFileNotMover refuses a move between workstreams by someone who neither
// looks after the workstream the files leave nor made them.
var errFileNotMover = errors.New("only whoever looks after this workstream, or the person who made a file, can move it to another workstream")

// moveFilesAcross sends files to another workstream's Files (Bring along:
// what a chat made lands in the new workstream once). It asks what a task's
// move asks (factory.MayStop): edit on both workstreams, and the caller
// either looks after this one or made the file, a chat or Afi's file
// counting as made for the person it was made for. A document's live text
// follows its page.
func (h *Handler) moveFilesAcross(w http.ResponseWriter, r *http.Request, ws factory.OrgWorkstream, v wsViewer, body api.FilesMove) {
	other, found, err := h.config.Store.Workstream(body.To)
	if h.failed(w, err) {
		return
	}
	if !found || !v.canSee(other) {
		h.fail(w, http.StatusNotFound, errWorkstreamGone.Error())
		return
	}
	if !v.canWrite(other) || other.ArchivedAt != nil {
		h.fail(w, http.StatusForbidden, "moving a file needs edit on the workstream it moves to")
		return
	}
	// Both sides folded first, so a fold that runs later never makes a second
	// copy of a page that moved.
	if _, err := h.foldFiles(ws); h.failed(w, err) {
		return
	}
	if _, err := h.foldFiles(other); h.failed(w, err) {
		return
	}
	ids := slices.Compact(slices.Sorted(slices.Values(body.Files)))
	if !v.canGovern(ws) {
		have, err := h.config.Store.WorkstreamFiles(ws.ID)
		if h.failed(w, err) {
			return
		}
		for _, id := range ids {
			f, ok := have[id]
			if !ok || f.DeletedAt != nil {
				h.fail(w, http.StatusNotFound, store.ErrFileGone.Error())
				return
			}
			if !madeBy(f, v.Handle) {
				h.fail(w, http.StatusForbidden, errFileNotMover.Error())
				return
			}
		}
	}
	now := h.clock().UTC()
	moved, err := h.config.Store.MoveWorkstreamFiles(ws.ID, other.ID, ids, body.Folder, now.Format(time.RFC3339Nano), v.Handle)
	switch {
	case errors.Is(err, store.ErrFileFolderMove), errors.Is(err, store.ErrFileNoFolder), errors.Is(err, store.ErrFileIDTaken):
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	case h.fileRefused(w, err):
		return
	}
	for _, f := range moved {
		if f.Kind == factory.FileDocument && f.PageID != "" {
			if ds, err := h.docStore(); err == nil {
				from := "ws/" + ws.ID + "/page/" + f.PageID
				if ds.Rename(from, "ws/"+other.ID+"/page/"+f.PageID) == nil {
					h.docs.hub.Remove(from)
				}
			}
		}
	}
	out := api.WorkstreamFiles{Files: []factory.WorkstreamFile{}}
	actor := h.orgActor(r)
	for _, f := range moved {
		out.Files = append(out.Files, f)
		h.publishWorkstreamPart(ws, api.PartFiles, f.ID, actor, f.Rev, true) // gone from here
		h.publishWorkstreamPart(other, api.PartFiles, f.ID, actor, f.Rev, false)
	}
	h.recordWorkstream(r, ws.ID, "moved files to "+other.ID, strings.Join(ids, " "))
	h.recordWorkstream(r, other.ID, "received files from "+ws.ID, strings.Join(ids, " "))
	h.write(w, http.StatusOK, out)
}

// madeBy is whether handle made f: a person's own file, or one a chat or Afi
// made for them.
func madeBy(f factory.WorkstreamFile, handle string) bool {
	if handle == "" {
		return false
	}
	h := factory.NormalizeHandle(handle)
	if f.MadeBy.Kind == factory.MakerPerson {
		return factory.NormalizeHandle(firstNonEmpty(f.MadeBy.ID, f.MadeBy.Name)) == h
	}
	return f.MadeBy.For != "" && factory.NormalizeHandle(f.MadeBy.For) == h
}

func (h *Handler) deleteWorkstreamFile(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	fid := r.PathValue("fid")
	now := h.clock().UTC()
	files, err := h.config.Store.EditWorkstreamFiles(ws.ID, now.Format(time.RFC3339Nano), func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		f, ok := files[fid]
		if !ok || f.DeletedAt != nil {
			return nil, false, store.ErrFileGone
		}
		f.DeletedAt, f.DeletedWith, f.ChangedAt, f.ChangedBy = &now, "", now, v.Handle
		files[fid] = f
		changed := []string{fid}
		if f.Kind == factory.FileFolder {
			for id, c := range files {
				if id != fid && c.DeletedAt == nil && under(files, c.Folder, fid) {
					c.DeletedAt, c.DeletedWith = &now, fid
					files[id] = c
					changed = append(changed, id)
				}
			}
		}
		return changed, false, nil
	})
	if h.fileRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "deleted file", fid)
	h.publishWorkstreamPart(ws, api.PartFiles, fid, h.orgActor(r), files[fid].Rev, true)
	h.write(w, http.StatusOK, files[fid])
}

func (h *Handler) restoreWorkstreamFile(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	fid := r.PathValue("fid")
	now := h.clock().UTC()
	files, err := h.config.Store.EditWorkstreamFiles(ws.ID, now.Format(time.RFC3339Nano), func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		f, ok := files[fid]
		if !ok {
			return nil, false, store.ErrFileGone
		}
		if f.DeletedAt == nil {
			return nil, false, errFileNotGone
		}
		// Back where it was, unless that folder is itself gone: then at the
		// top, so a restore never lands somewhere nobody can see.
		if !liveFolder(files, f.Folder) {
			f.Folder = ""
		}
		f.DeletedAt, f.DeletedWith, f.ChangedAt, f.ChangedBy = nil, "", now, v.Handle
		files[fid] = f
		changed := []string{fid}
		for id, c := range files {
			if c.DeletedWith == fid {
				c.DeletedAt, c.DeletedWith = nil, ""
				files[id] = c
				changed = append(changed, id)
			}
		}
		return changed, false, nil
	})
	if h.fileRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "restored file", fid)
	h.publishWorkstreamPart(ws, api.PartFiles, fid, h.orgActor(r), files[fid].Rev, false)
	h.write(w, http.StatusOK, files[fid])
}
