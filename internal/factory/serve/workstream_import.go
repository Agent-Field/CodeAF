package serve

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"net/http"
	"strings"
)

// File metadata and task relationships travel with an import. A path on the
// originating laptop never becomes an upload the organization cannot open.
func (h *Handler) importWorkstreamContents(w http.ResponseWriter, ws factory.OrgWorkstream, v wsViewer, pkg *api.WorkstreamPackage) bool {
	if pkg == nil {
		return true
	}
	files := make([]factory.WorkstreamFile, 0, len(pkg.Files))
	for _, item := range pkg.Files {
		f := item.File
		if f.ID == "" || f.DeletedAt != nil {
			h.fail(w, 422, "an imported file needs an identity and must not be deleted")
			return false
		}
		if f.MadeBy.Kind == factory.MakerPerson {
			f.MadeBy.ID = v.Handle
			f.MadeBy.Name = v.Handle
		} else {
			f.MadeBy.For = v.Handle
		}
		if f.Kind == factory.FileUpload {
			if len(item.Data) > uploadMost {
				h.fail(w, 422, "an upload may be at most 25 MiB")
				return false
			}
			sha, err := h.config.Store.PutBlob(item.Data)
			if h.failed(w, err) {
				return false
			}
			mime := ""
			if f.Upload != nil {
				mime = f.Upload.Mime
			}
			f.Upload = &factory.UploadBlob{Size: int64(len(item.Data)), Mime: mime, Blob: sha}
			f.URL = ""
		}
		files = append(files, f)
	}
	if len(pkg.Tasks) > factory.TasksMost {
		h.fail(w, 422, "too many tasks to bring along")
		return false
	}
	for i := range pkg.Tasks {
		t := &pkg.Tasks[i]
		if strings.TrimSpace(t.ID) == "" {
			h.fail(w, 422, "an imported task needs its identity")
			return false
		}
		switch t.State {
		case factory.TaskUpNext, factory.TaskNeeds, factory.TaskDone, factory.TaskFailed, factory.TaskInterrupted, factory.TaskStopped, factory.TaskNoReport:
		case factory.TaskRunning:
			h.fail(w, 422, "stop working tasks before bringing them along")
			return false
		default:
			h.fail(w, 422, "an imported task needs a known task state")
			return false
		}
		if strings.HasPrefix(t.ID, factory.TaskJobPrefix) {
			h.fail(w, 422, "Factory jobs stay where they ran")
			return false
		}
		if t.Owner.Kind == "person" && (t.Owner.ID == "you" || t.Owner.ID == "") {
			t.Owner.ID = v.Handle
			t.Owner.Name = v.Handle
		}
		if t.DoneBy.Kind == "person" && (t.DoneBy.ID == "you" || t.DoneBy.ID == "") {
			t.DoneBy.ID = v.Handle
			t.DoneBy.Name = v.Handle
		}
		if err := factory.CheckTask(*t, pkg.Tasks); err != nil {
			h.fail(w, 422, fmt.Sprintf("cannot bring task: %s", err))
			return false
		}
	}
	if h.failed(w, h.config.Store.ImportWorkstreamContents(ws.ID, files, pkg.Tasks, pkg.TaskEvents)) {
		return false
	}
	return true
}

// Bringing a chat into an existing workstream uses the same package as a
// whole-workstream import, limited by the desk to that chat's own work.
func (h *Handler) bringWorkstreamContents(w http.ResponseWriter, r *http.Request, v wsViewer, pkg *api.WorkstreamPackage) {
	all, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		ws, ok := all[pkg.To]
		if !ok || !v.canSee(ws) {
			return nil, errWorkstreamGone
		}
		if !v.canGovern(ws) {
			return nil, errWorkstreamNoWrite
		}
		if ws.ArchivedAt != nil {
			return nil, errWorkstreamArchived
		}
		for _, p := range pkg.Pages {
			found := false
			for _, old := range ws.Pages {
				if old.ID == p.ID {
					found = true
					break
				}
			}
			if !found {
				if p.Kind == "overview" {
					p.Kind = "page"
				}
				ws.Pages = append(ws.Pages, p)
			}
		}
		ws.UpdatedAt = h.clock().UTC()
		all[ws.ID] = ws
		return []string{ws.ID}, nil
	})
	if h.workstreamRefused(w, err) {
		return
	}
	ws := all[pkg.To]
	if !h.importWorkstreamContents(w, ws, v, pkg) {
		return
	}
	h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), nil, ws)
	h.writeWorkstream(w, http.StatusOK, ws, v)
}
