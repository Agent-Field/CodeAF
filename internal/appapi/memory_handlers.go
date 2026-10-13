package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/teams"
)

var memoryContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/memory", Response: "MemoryResponse"}, handle: (*Server).memoryHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/memory/{id}", Request: "PatchMemoryRequest", Response: "MemoryItem"}, handle: (*Server).memoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/memory/{id}/forget", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/memory/{id}/restore", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memoryHandler},
}

// MemoryUndoWindow is owned by the engine so two windows agree on its deadline.
const MemoryUndoWindow = store.MemoryUndoWindow

// MemoryBackend lets the crew lane supply its owner-aware store without making
// this resource infer crew ownership from project memory or UI shelf labels.
// Mutations must compare expect and write atomically; restores enforce the deadline.
type MemoryBackend interface {
	ReadMemory(context.Context) (MemoryResponse, error)
	PatchMemory(context.Context, string, PatchMemoryRequest) (MemoryItem, error)
	ForgetMemory(context.Context, string, *AgentExpect, time.Time) error
	RestoreMemory(context.Context, string, *AgentExpect, time.Time) error
}

type memoryBackendKey struct{}

// WithMemoryBackend binds a home-scoped backend to Options.Context. No shared
// router or wire type needs to change when crew ownership becomes available.
func WithMemoryBackend(ctx context.Context, backend MemoryBackend) context.Context {
	return context.WithValue(ctx, memoryBackendKey{}, backend)
}

func (s *Server) resourceHome() string {
	if s.options.Agents != nil {
		return s.options.Agents.home
	}
	return config.ProfilePath(s.options.ProfileDir, "")
}

func (s *Server) resourceNow() time.Time {
	if s.options.Agents != nil {
		return s.options.Agents.now()
	}
	return time.Now()
}

func (s *Server) memoryHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.memoryAction)(w, r)
}

func (s *Server) memoryAction(w http.ResponseWriter, r *http.Request) {
	var backend MemoryBackend
	if s.options.Context != nil {
		backend, _ = s.options.Context.Value(memoryBackendKey{}).(MemoryBackend)
	}
	if backend == nil {
		db, err := store.Open(filepath.Join(s.resourceHome(), "graph.db"))
		if err != nil {
			resourceFailure(w, err)
			return
		}
		defer db.Close()
		backend = nativeMemory{db: db, home: s.resourceHome()}
	}
	id := r.PathValue("id")
	if r.Method == "GET" {
		rows, err := backend.ReadMemory(r.Context())
		if err != nil {
			resourceFailure(w, err)
			return
		}
		if rows.Shelves == nil {
			rows.Shelves = []MemoryShelf{}
		}
		if rows.Items == nil {
			rows.Items = []MemoryItem{}
		}
		shelf, q := r.URL.Query().Get("shelf"), strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		found := shelf == ""
		for _, row := range rows.Shelves {
			found = found || row.ID == shelf
		}
		if !found {
			fail(w, 422, "bad_input", "no such memory shelf")
			return
		}
		items := []MemoryItem{}
		for _, row := range rows.Items {
			if (shelf == "" || row.Shelf == shelf) && strings.Contains(strings.ToLower(row.Text), q) {
				items = append(items, row)
			}
		}
		rows.Items = items
		write(w, 200, rows)
		return
	}
	if r.Method == "PATCH" {
		var body PatchMemoryRequest
		if !decodeResourceObject(w, r, &body) {
			return
		}
		if (body.Text == nil && body.Shelf == nil) || (body.Text != nil && strings.TrimSpace(*body.Text) == "") || (body.Shelf != nil && strings.TrimSpace(*body.Shelf) == "") {
			fail(w, 422, "bad_input", "memory patch needs text or a shelf")
			return
		}
		row, err := backend.PatchMemory(r.Context(), id, body)
		if err != nil {
			resourceFailure(w, err)
			return
		}
		_, _ = s.events.Publish(EventMemoryChanged, IDChangedEvent{ID: id})
		write(w, 200, row)
		return
	}
	var body ExpectRequest
	if !decodeResourceObject(w, r, &body) {
		return
	}
	now := s.resourceNow()
	var err error
	if strings.HasSuffix(r.URL.Path, "/forget") {
		err = backend.ForgetMemory(r.Context(), id, body.Expect, now)
	} else {
		err = backend.RestoreMemory(r.Context(), id, body.Expect, now)
	}
	if err != nil {
		resourceFailure(w, err)
		return
	}
	_, _ = s.events.Publish(EventMemoryChanged, IDChangedEvent{ID: id})
	write(w, 200, ActionResponse{OK: true})
}

// The native adapter edits the canonical store; it never relabels a project
// memory as a crew memory, because a shelf is a retrieval permission boundary.
type nativeMemory struct {
	db   *store.Store
	home string
}

func memoryView(row store.Memory) MemoryItem {
	made := row.SourceSession
	return MemoryItem{ID: row.ID, Text: row.Text, Shelf: row.Owner, MadeBy: made, At: row.UpdatedAt}
}
func (n nativeMemory) ReadMemory(context.Context) (MemoryResponse, error) {
	rows, err := n.db.ListMemories(nil, 0)
	if err != nil {
		return MemoryResponse{}, err
	}
	result := MemoryResponse{Shelves: []MemoryShelf{{ID: store.OwnerUser, Name: "Yours"}}, Items: []MemoryItem{}}
	names := map[string]string{store.OwnerUser: "Yours"}
	crews, err := teams.Snapshot(n.home)
	if err != nil {
		return MemoryResponse{}, err
	}
	for _, crew := range crews.Teams {
		if !crew.Root && !crew.Closed() {
			names[store.OwnerCrew(crew.ID)] = crew.Name
		}
	}
	for _, row := range rows {
		if _, ok := names[row.Owner]; !ok {
			names[row.Owner] = row.Owner
		}
		if row.Owner == store.OwnerUser {
			names[row.Owner] = "Yours"
		}

		if row.Owner == store.OwnerMachine {
			names[row.Owner] = "This machine"
		}
		result.Items = append(result.Items, memoryView(row))
	}
	ids := []string{}
	for id := range names {
		if id != store.OwnerUser {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		result.Shelves = append(result.Shelves, MemoryShelf{ID: id, Name: names[id]})
	}
	return result, nil
}

func memoryEditExpect(edit *store.MemoryEdit, expect *AgentExpect) {
	if expect != nil {
		edit.State, edit.Version = expect.State, expect.Version
	}
}
func (n nativeMemory) PatchMemory(_ context.Context, id string, body PatchMemoryRequest) (MemoryItem, error) {
	if body.Shelf != nil && store.OwnerKindOf(*body.Shelf) == "crew" {
		crews, err := teams.Snapshot(n.home)
		if err != nil {
			return MemoryItem{}, err
		}
		crew, ok := crews.Team(strings.TrimPrefix(*body.Shelf, "crew:"))
		if !ok || crew.Root || crew.Closed() {
			return MemoryItem{}, agentError(422, "bad_input", "no such memory shelf")
		}
	}
	edit := store.MemoryEdit{Action: "patch", Text: body.Text, Owner: body.Shelf}
	memoryEditExpect(&edit, body.Expect)
	if err := n.db.EditMemory(id, edit); err != nil {
		return MemoryItem{}, err
	}
	row, _, err := n.db.MemoryRecord(id)
	return memoryView(row), err
}
func (n nativeMemory) ForgetMemory(_ context.Context, id string, expect *AgentExpect, now time.Time) error {
	edit := store.MemoryEdit{Action: "forget", Now: now}
	memoryEditExpect(&edit, expect)
	return n.db.EditMemory(id, edit)
}
func (n nativeMemory) RestoreMemory(_ context.Context, id string, expect *AgentExpect, now time.Time) error {
	edit := store.MemoryEdit{Action: "restore", Now: now}
	memoryEditExpect(&edit, expect)
	return n.db.EditMemory(id, edit)
}

func resourceFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrMemoryMissing):
		fail(w, 404, "not_found", err.Error())
		return
	case errors.Is(err, store.ErrMemoryStale):
		fail(w, 409, "stale", err.Error())
		return
	case errors.Is(err, store.ErrMemoryConflict), errors.Is(err, store.ErrMemoryUndoEnded):
		fail(w, 409, "conflict", err.Error())
		return
	}
	var reply *agentReplyError
	if errors.As(err, &reply) {
		agentFailure(w, err)
		return
	}
	if errors.Is(err, store.ErrInvalid) {
		fail(w, 422, "bad_input", err.Error())
		return
	}
	fail(w, 500, "conflict", "could not read or save this resource")
}

// Resource receipts use private atomic replacement and a home-wide flock, so
// an undo in another window sees the same deadline without a per-window timer.
func resourceDisk(home, name string, initial any, fn func([]byte) (any, error)) error {
	dir := filepath.Join(home, "v3", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, name+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	path := filepath.Join(dir, name+".json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw, err = json.Marshal(initial)
	}
	if err != nil {
		return err
	}
	value, err := fn(raw)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(raw), encoded) {
		return nil
	}
	raw = encoded
	temp, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("save resource: %w", err)
	}
	return nil
}

// Resource mutations distinguish an omitted field from null, because silently
// ignoring a broken expectation could turn a conditional edit into an overwrite.
func decodeResourceObject(w http.ResponseWriter, r *http.Request, value any) bool {
	var fields map[string]json.RawMessage
	if !decodeObject(w, r, &fields) {
		return false
	}
	for _, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			fail(w, 422, "bad_input", "resource fields cannot be null")
			return false
		}
	}
	raw, _ := json.Marshal(fields)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		fail(w, 422, "bad_input", "unknown field or invalid value")
		return false
	}
	return true
}
