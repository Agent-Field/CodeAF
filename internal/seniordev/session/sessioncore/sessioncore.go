//go:build !windows

// Package sessioncore owns the durable session lifecycle: sessions, messages
// and parts are written to the flat JSON storage and every change is published
// on the bus.
package sessioncore

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/id"
	"github.com/Agent-Field/codeaf/internal/seniordev/storage"
)

const (
	parentTitlePrefix = "New session - "
	childTitlePrefix  = "Child session - "
)

var defaultTitlePattern = regexp.MustCompile(`^(New session - |Child session - )\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

var (
	EventCreated = bus.Define("session.created", nil)
	EventUpdated = bus.Define("session.updated", nil)
	EventDeleted = bus.Define("session.deleted", nil)
	EventDiff    = bus.Define("session.diff", nil)
	EventError   = bus.Define("session.error", nil)

	EventMessageUpdated     = bus.Define(msgmodel.EventMessageUpdated, nil)
	EventMessageRemoved     = bus.Define(msgmodel.EventMessageRemoved, nil)
	EventMessagePartUpdated = bus.Define(msgmodel.EventMessagePartUpdated, nil)
	EventMessagePartDelta   = bus.Define(msgmodel.EventMessagePartDelta, nil)
	EventMessagePartRemoved = bus.Define(msgmodel.EventMessagePartRemoved, nil)
)

type Summary struct {
	Additions uint64              `json:"additions"`
	Deletions uint64              `json:"deletions"`
	Files     uint64              `json:"files"`
	Diffs     []msgmodel.FileDiff `json:"diffs,omitempty"`
}

type Share struct {
	URL string `json:"url"`
}

type Revert struct {
	MessageID string  `json:"messageID"`
	PartID    *string `json:"partID,omitempty"`
	Snapshot  *string `json:"snapshot,omitempty"`
	Diff      *string `json:"diff,omitempty"`
}

type Model struct {
	ID         string  `json:"id"`
	ProviderID string  `json:"providerID"`
	Variant    *string `json:"variant,omitempty"`
}

type Time struct {
	Created    uint64   `json:"created"`
	Updated    uint64   `json:"updated"`
	Compacting *uint64  `json:"compacting,omitempty"`
	Archived   *float64 `json:"archived,omitempty"`
}

// Info is the durable session record.
type Info struct {
	ID          string          `json:"id"`
	Slug        string          `json:"slug"`
	ProjectID   string          `json:"projectID"`
	WorkspaceID *string         `json:"workspaceID,omitempty"`
	Directory   string          `json:"directory"`
	Path        *string         `json:"path,omitempty"`
	ParentID    *string         `json:"parentID,omitempty"`
	Title       string          `json:"title"`
	Agent       *string         `json:"agent,omitempty"`
	Model       *Model          `json:"model,omitempty"`
	Version     string          `json:"version"`
	Summary     *Summary        `json:"summary,omitempty"`
	Share       *Share          `json:"share,omitempty"`
	Revert      *Revert         `json:"revert,omitempty"`
	Permission  json.RawMessage `json:"permission,omitempty"`
	Time        Time            `json:"time"`
}

// Row is the storage-row projection consumed by FromRow and produced by ToRow.
type Row struct {
	ID               string              `json:"id"`
	ProjectID        string              `json:"project_id"`
	WorkspaceID      *string             `json:"workspace_id,omitempty"`
	ParentID         *string             `json:"parent_id,omitempty"`
	Slug             string              `json:"slug"`
	Directory        string              `json:"directory"`
	Path             *string             `json:"path,omitempty"`
	Title            string              `json:"title"`
	Agent            *string             `json:"agent,omitempty"`
	Model            *Model              `json:"model,omitempty"`
	Version          string              `json:"version"`
	ShareURL         *string             `json:"share_url,omitempty"`
	SummaryAdditions *uint64             `json:"summary_additions,omitempty"`
	SummaryDeletions *uint64             `json:"summary_deletions,omitempty"`
	SummaryFiles     *uint64             `json:"summary_files,omitempty"`
	SummaryDiffs     []msgmodel.FileDiff `json:"summary_diffs,omitempty"`
	Revert           *Revert             `json:"revert"`
	Permission       json.RawMessage     `json:"permission,omitempty"`
	TimeCreated      uint64              `json:"time_created"`
	TimeUpdated      uint64              `json:"time_updated"`
	TimeCompacting   *uint64             `json:"time_compacting,omitempty"`
	TimeArchived     *float64            `json:"time_archived,omitempty"`
}

func IsDefaultTitle(title string) bool { return defaultTitlePattern.MatchString(title) }

func FromRow(row Row) Info {
	var summary *Summary
	if row.SummaryAdditions != nil || row.SummaryDeletions != nil || row.SummaryFiles != nil {
		summary = &Summary{Diffs: row.SummaryDiffs}
		if row.SummaryAdditions != nil {
			summary.Additions = *row.SummaryAdditions
		}
		if row.SummaryDeletions != nil {
			summary.Deletions = *row.SummaryDeletions
		}
		if row.SummaryFiles != nil {
			summary.Files = *row.SummaryFiles
		}
	}
	var share *Share
	if row.ShareURL != nil && *row.ShareURL != "" {
		share = &Share{URL: *row.ShareURL}
	}
	permission := row.Permission
	if string(permission) == "null" {
		permission = nil
	}
	return Info{
		ID: row.ID, Slug: row.Slug, ProjectID: row.ProjectID,
		WorkspaceID: row.WorkspaceID, Directory: row.Directory, Path: row.Path,
		ParentID: row.ParentID, Summary: summary, Share: share, Title: row.Title,
		Agent: row.Agent, Model: row.Model, Version: row.Version,
		Time:       Time{Created: row.TimeCreated, Updated: row.TimeUpdated, Compacting: row.TimeCompacting, Archived: row.TimeArchived},
		Permission: permission, Revert: row.Revert,
	}
}

func ToRow(info Info) Row {
	row := Row{
		ID: info.ID, ProjectID: info.ProjectID, WorkspaceID: info.WorkspaceID,
		ParentID: info.ParentID, Slug: info.Slug, Directory: info.Directory,
		Path: info.Path, Title: info.Title, Agent: info.Agent, Model: info.Model,
		Version: info.Version, Revert: info.Revert, Permission: info.Permission,
		TimeCreated: info.Time.Created, TimeUpdated: info.Time.Updated,
		TimeCompacting: info.Time.Compacting, TimeArchived: info.Time.Archived,
	}
	if info.Share != nil {
		row.ShareURL = &info.Share.URL
	}
	if info.Summary != nil {
		row.SummaryAdditions = &info.Summary.Additions
		row.SummaryDeletions = &info.Summary.Deletions
		row.SummaryFiles = &info.Summary.Files
		row.SummaryDiffs = info.Summary.Diffs
	}
	return row
}

func GetUsage(input calc.GetUsageInput) calc.UsageResult { return calc.GetUsage(input) }

type CreateInput struct {
	ID          string
	ParentID    string
	Title       string
	Agent       string
	Model       *Model
	Permission  json.RawMessage
	WorkspaceID string
	Directory   string
	Path        string
}

type Options struct {
	Store       *storage.Store
	Bus         *bus.Bus
	ProjectID   string
	Worktree    string
	Directory   string
	WorkspaceID string
	Version     string
	Now         func() time.Time
	Slug        func() string
}

// Service is safe for concurrent processor and observer use.
type Service struct {
	store       *storage.Store
	bus         *bus.Bus
	projectID   string
	worktree    string
	directory   string
	workspaceID string
	version     string
	now         func() time.Time
	slug        func() string
}

func New(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, errors.New("sessioncore: Store is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Version == "" {
		opts.Version = "0.0.0"
	}
	if opts.Slug == nil {
		opts.Slug = func() string {
			value, err := id.Ascending("entry")
			if err != nil {
				return ""
			}
			return strings.TrimPrefix(value, "ent_")
		}
	}
	s := &Service{
		store: opts.Store, bus: opts.Bus, projectID: opts.ProjectID,
		worktree: opts.Worktree, directory: opts.Directory,
		workspaceID: opts.WorkspaceID, version: opts.Version, now: opts.Now,
		slug: opts.Slug,
	}
	return s, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Info, error) {
	_ = ctx
	sessionID, err := id.Descending("session", input.ID)
	if err != nil {
		return Info{}, err
	}
	now := uint64(s.now().UnixMilli())
	directory := input.Directory
	if directory == "" {
		directory = s.directory
	}
	path := input.Path
	if path == "" && s.worktree != "" {
		rel, relErr := filepath.Rel(filepath.Clean(s.worktree), directory)
		if relErr == nil {
			path = filepath.ToSlash(rel)
		}
	}
	title := input.Title
	if title == "" {
		title = createDefaultTitle(input.ParentID != "", time.UnixMilli(int64(now)).UTC())
	}
	info := Info{
		ID: sessionID, Slug: s.slug(), ProjectID: s.projectID, Directory: directory,
		Title: title, Model: input.Model, Version: s.version,
		Time: Time{Created: now, Updated: now}, Permission: input.Permission,
	}
	if path != "" {
		info.Path = &path
	}
	if input.ParentID != "" {
		info.ParentID = &input.ParentID
	}
	workspace := input.WorkspaceID
	if workspace == "" {
		workspace = s.workspaceID
	}
	if workspace != "" {
		info.WorkspaceID = &workspace
	}
	if input.Agent != "" {
		info.Agent = &input.Agent
	}
	if err := s.store.Write(sessionKey(info.ID), info); err != nil {
		return Info{}, err
	}
	s.publish(EventCreated, createdEvent{SessionID: info.ID, Info: info})
	// A session.updated follows session.created so subscribers that only
	// track updates also see the new session.
	s.publish(EventUpdated, createdEvent{SessionID: info.ID, Info: info})
	return info, nil
}

func createDefaultTitle(child bool, now time.Time) string {
	prefix := parentTitlePrefix
	if child {
		prefix = childTitlePrefix
	}
	return prefix + now.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (s *Service) Get(_ context.Context, sessionID string) (Info, error) {
	var info Info
	if err := s.store.ReadInto(sessionKey(sessionID), &info); err != nil {
		var miss *storage.NotFoundError
		if errors.As(err, &miss) {
			return Info{}, &storage.NotFoundError{Message: "Session not found: " + sessionID}
		}
		return Info{}, err
	}
	return info, nil
}

func (s *Service) List(ctx context.Context) ([]Info, error) {
	_ = ctx
	keys, err := s.store.List([]string{"session"})
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(keys))
	for _, key := range keys {
		var info Info
		if err := s.store.ReadInto(key, &info); err == nil && (s.projectID == "" || info.ProjectID == s.projectID) {
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Updated > out[j].Time.Updated })
	return out, nil
}

func (s *Service) Children(ctx context.Context, parentID string) ([]Info, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, item := range all {
		if item.ParentID != nil && *item.ParentID == parentID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *Service) Touch(ctx context.Context, sessionID string) error {
	return s.patch(ctx, sessionID, func(info *Info) { info.Time.Updated = uint64(s.now().UnixMilli()) })
}

func (s *Service) patch(ctx context.Context, sessionID string, mutate func(*Info)) error {
	_ = ctx
	info, err := storage.UpdateAs(s.store, sessionKey(sessionID), func(info *Info) {
		mutate(info)
	})
	if err != nil {
		var miss *storage.NotFoundError
		if errors.As(err, &miss) {
			return &storage.NotFoundError{Message: "Session not found: " + sessionID}
		}
		return err
	}
	s.publish(EventUpdated, createdEvent{SessionID: sessionID, Info: info})
	return nil
}

func (s *Service) Remove(ctx context.Context, sessionID string) error {
	info, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	children, _ := s.Children(ctx, sessionID)
	for _, child := range children {
		_ = s.Remove(ctx, child.ID)
	}
	for _, prefix := range [][]string{{"message", sessionID}, {"part", sessionID}} {
		keys, _ := s.store.List(prefix)
		for _, key := range keys {
			_ = s.store.Remove(key)
		}
	}
	if err := s.store.Remove(sessionKey(sessionID)); err != nil {
		return err
	}
	s.publish(EventDeleted, createdEvent{SessionID: sessionID, Info: info})
	return nil
}

func (s *Service) UpdateMessage(_ context.Context, info msgmodel.Info) error {
	sessionID := messageSessionID(info)
	if err := s.store.Write(messageKey(sessionID, info.MessageID()), info); err != nil {
		return err
	}
	s.publish(EventMessageUpdated, msgmodel.UpdatedEvent{SessionID: sessionID, Info: info})
	return nil
}

func (s *Service) UpdatePart(_ context.Context, part msgmodel.Part) error {
	base := part.Base()
	if err := s.store.Write(partKey(base.SessionID, base.MessageID, base.ID), part); err != nil {
		return err
	}
	s.publish(EventMessagePartUpdated, msgmodel.PartUpdatedEvent{
		SessionID: base.SessionID, Part: part, Time: uint64(s.now().UnixMilli()),
	})
	return nil
}

// UpdateMessageWithParts makes the message visible only after all of its parts
// are durable. The files are written part-first under one store operation, then
// projected message-first so SQLite foreign keys observe a complete turn.
func (s *Service) UpdateMessageWithParts(
	_ context.Context, info msgmodel.Info, parts ...msgmodel.Part,
) error {
	sessionID := messageSessionID(info)
	messageID := info.MessageID()
	items := make([]storage.WriteItem, 0, len(parts)+1)
	for _, part := range parts {
		base := part.Base()
		items = append(items, storage.WriteItem{
			Key: partKey(base.SessionID, base.MessageID, base.ID), Content: part,
		})
	}
	items = append(items, storage.WriteItem{
		Key: messageKey(sessionID, messageID), Content: info,
	})
	if err := s.store.WriteBatch(items); err != nil {
		return err
	}
	s.publish(EventMessageUpdated, msgmodel.UpdatedEvent{SessionID: sessionID, Info: info})
	for _, part := range parts {
		base := part.Base()
		s.publish(EventMessagePartUpdated, msgmodel.PartUpdatedEvent{
			SessionID: base.SessionID, Part: part, Time: uint64(s.now().UnixMilli()),
		})
	}
	return nil
}

func (s *Service) UpdatePartDelta(_ context.Context, input msgmodel.PartDeltaEvent) {
	s.publish(EventMessagePartDelta, input)
}

// Messages returns the session's messages oldest-first, each with its parts.
func (s *Service) Messages(ctx context.Context, sessionID string) ([]msgmodel.WithParts, error) {
	_ = ctx
	keys, err := s.store.List([]string{"message", sessionID})
	if err != nil {
		return nil, err
	}
	out := make([]msgmodel.WithParts, 0, len(keys))
	for _, key := range keys {
		var raw json.RawMessage
		if err := s.store.ReadInto(key, &raw); err != nil {
			return nil, err
		}
		info, err := msgmodel.UnmarshalInfo(raw)
		if err != nil {
			return nil, err
		}
		partKeys, err := s.store.List([]string{"part", sessionID, info.MessageID()})
		if err != nil {
			return nil, err
		}
		parts := make(msgmodel.Parts, 0, len(partKeys))
		for _, partKey := range partKeys {
			var partRaw json.RawMessage
			if err := s.store.ReadInto(partKey, &partRaw); err != nil {
				return nil, err
			}
			part, err := msgmodel.UnmarshalPart(partRaw)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		}
		out = append(out, msgmodel.WithParts{Info: info, Parts: parts})
	}
	return out, nil
}

func (s *Service) publish(def bus.Definition, properties any) {
	if s.bus != nil {
		s.bus.Publish(def, properties)
	}
}

type createdEvent struct {
	SessionID string `json:"sessionID"`
	Info      Info   `json:"info"`
}

func sessionKey(id string) []string { return []string{"session", id} }
func messageKey(sessionID, messageID string) []string {
	return []string{"message", sessionID, messageID}
}
func partKey(sessionID, messageID, partID string) []string {
	return []string{"part", sessionID, messageID, partID}
}

func messageSessionID(info msgmodel.Info) string {
	switch item := info.(type) {
	case msgmodel.User:
		return item.SessionID
	case msgmodel.Assistant:
		return item.SessionID
	default:
		return ""
	}
}
