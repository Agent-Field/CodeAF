package desktopbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

// The workspace routes keep each window place's tab set in the engine, so two
// windows on one place mirror one tab set (design Places 6d) instead of each
// webview's localStorage forking its own. The store is a compare-and-swap
// register (internal/workspacestore); the renderer's controller replays its own
// actions over whatever a refused write returns
// (desktop/src/features/workspace-sync).
//
//	GET /workspaces/{key}                     → Record, at once
//	GET /workspaces/{key}?after=N&wait=1      → Record as soon as its revision is not N,
//	                                            or the unchanged Record after workspaceWait
//	PUT /workspaces/{key} {revision, writer, workspace}
//	                                          → 200 Record
//	                                          → 409 {error, code:"conflict", current: Record}
//
// The expected revision travels in the body, not an If-Match header: the
// bridge's CORS answer names only Authorization and Content-Type, and widening
// it is not this lane's to do.
//
// GET never changes the disk (it creates no file, no lock and no directory),
// and nothing here calls a model.
//
// THE LIVE MIRROR IS A LONG POLL, NOT A WORLD RECORD. The architecture names a
// `workspace` record on the engine-wide stream, but the renderer's world reader
// (features/chat/world-client.ts parseWorldRecord) refuses any record type it
// does not know and drops the whole stream. Publishing one here would cut every
// window's world feed. Once that reader skips unknown types, a PUT can publish
// {key, revision, writer} there and the controller can stop polling.

// workspaceWait is how long one long poll is held open. It sits well under the
// renderer's request timeout and the server's idle timeout.
const workspaceWait = 25 * time.Second

// UseWorkspaces attaches the tab-set store. Without it every /workspaces route
// is absent (the ordinary 404), never a route that fails.
func (b *Bridge) UseWorkspaces(store *workspacestore.Store) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.workspaces = store
}

func (b *Bridge) workspaceStore() *workspacestore.Store {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.workspaces
}

// nativeOrigin is the same four origins ServeHTTP answers CORS for.
func nativeOrigin(origin string) bool {
	switch origin {
	case "tauri://localhost", "http://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420":
		return true
	}
	return false
}

// workspaceRoutes serves /workspaces/{key}. It reports whether the path was its
// own; with no store attached it claims nothing.
func (b *Bridge) workspaceRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	rest, ok := strings.CutPrefix(path, "/workspaces/")
	if !ok {
		return false
	}
	store := b.workspaceStore()
	if store == nil {
		return false
	}
	// A page from anywhere else never reaches a tab set, token or not. Requests
	// with no Origin are the native shell and the dev proxy.
	if origin := r.Header.Get("Origin"); origin != "" && !nativeOrigin(origin) {
		failWorkspace(w, http.StatusForbidden, "origin", "this page may not read codeaf's tabs")
		return true
	}
	if key, found := strings.CutSuffix(rest, "/open-elsewhere"); found && workspacestore.ValidKey(key) {
		b.workspaceOpenElsewhere(w, r, store, key)
		return true
	}
	if key, found := strings.CutSuffix(rest, "/favicon"); found && workspacestore.ValidKey(key) {
		b.workspaceFavicon(w, r, store, key)
		return true
	}
	key := rest
	if !workspacestore.ValidKey(key) {
		failWorkspace(w, http.StatusNotFound, "unknown_key", "there is no such tab set")
		return true
	}
	switch r.Method {
	case http.MethodGet:
		b.getWorkspace(w, r, store, key)
	case http.MethodPut:
		b.putWorkspace(w, r, store, key)
	default:
		fail(w, 405, "GET or PUT required")
	}
	return true
}

func (b *Bridge) getWorkspace(w http.ResponseWriter, r *http.Request, store *workspacestore.Store, key string) {
	q := r.URL.Query()
	if q.Get("wait") == "" {
		rec, err := store.Get(key)
		if err != nil {
			workspaceError(w, err)
			return
		}
		write(w, rec)
		return
	}
	after, err := strconv.ParseUint(q.Get("after"), 10, 64)
	if err != nil {
		failWorkspace(w, 400, "invalid", "after must be a revision")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), workspaceWait)
	defer cancel()
	rec, err := store.Wait(ctx, key, after)
	if err != nil {
		workspaceError(w, err)
		return
	}
	write(w, rec)
}

type workspacePut struct {
	Revision  uint64          `json:"revision"`
	Writer    string          `json:"writer"`
	Workspace json.RawMessage `json:"workspace"`
}

func (b *Bridge) putWorkspace(w http.ResponseWriter, r *http.Request, store *workspacestore.Store, key string) {
	var put workspacePut
	if !decodeMax(w, r, &put, workspacestore.MaxDocumentBytes+8<<10) {
		return
	}
	rec, err := store.Put(key, put.Revision, put.Writer, put.Workspace)
	var conflict *workspacestore.ConflictError
	if errors.As(err, &conflict) {
		writeStatus(w, http.StatusConflict, map[string]any{
			"error": "these tabs changed in another window", "code": "conflict", "current": conflict.Current,
		})
		return
	}
	if err != nil {
		workspaceError(w, err)
		return
	}
	write(w, rec)
}

// workspaceError turns a store refusal into one sentence and a code the
// renderer can act on. Only a fault the person cannot cause is a 500.
func workspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacestore.ErrTooLarge):
		failWorkspace(w, http.StatusRequestEntityTooLarge, "too_large", "these tabs are larger than codeaf can save; close some tabs or shorten a draft")
	case errors.Is(err, workspacestore.ErrInvalid):
		failWorkspace(w, http.StatusBadRequest, "invalid", "these tabs could not be saved: "+strings.TrimPrefix(err.Error(), workspacestore.ErrInvalid.Error()+": "))
	case errors.Is(err, workspacestore.ErrInvalidKey):
		failWorkspace(w, http.StatusNotFound, "unknown_key", "there is no such tab set")
	case errors.Is(err, workspacestore.ErrLocked):
		failWorkspace(w, http.StatusServiceUnavailable, "busy", "another window is saving these tabs; try again in a moment")
	case errors.Is(err, workspacestore.ErrUnsupportedVersion):
		failWorkspace(w, http.StatusConflict, "newer", "these tabs were saved by a newer codeaf; this one will not overwrite them")
	default:
		failWorkspace(w, http.StatusInternalServerError, "unavailable", "codeaf could not reach its saved tabs")
	}
}

func failWorkspace(w http.ResponseWriter, status int, code, sentence string) {
	writeStatus(w, status, map[string]string{"error": sentence, "code": code})
}
