// Package crewlink stores links from a crew to an app record. A link is never
// a copy: the record stays in the app, and unlinking keeps the history.
package crewlink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// Service is the crew ↔ record link store. Finish and Reopen are not on
// svc.CrewLinks; the crew HTTP handlers type-assert them after a crew closes
// or reopens, because that seam has no room for two more methods.
type Service struct {
	root string
	all  *svc.Services
	mu   sync.Mutex
	now  func() time.Time
}

var _ svc.CrewLinks = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	return &Service{root: cfg.Root, all: all, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Add(ctx context.Context, scope svc.Scope, p bridge.AddLinkParams) (bridge.CrewLink, error) {
	if err := ctx.Err(); err != nil {
		return bridge.CrewLink{}, err
	}
	if err := validateLink(scope.App, p.Crew, p.Type, p.ID); err != nil {
		return bridge.CrewLink{}, err
	}
	if s.root == "" {
		return bridge.CrewLink{}, bridge.Errorf(bridge.CodeInternal, "links have no home")
	}
	access, rec, def, summary, err := s.access(ctx, scope.Caller, scope.App, p.Type, p.ID)
	if err != nil {
		return bridge.CrewLink{}, err
	}
	if err := refuseLink(access, p.Type, p.ID); err != nil {
		return bridge.CrewLink{}, err
	}
	label, _ := presentRecord(def, rec)
	row := storedLink{
		Crew: p.Crew, CrewName: s.crewName(p.Crew), Type: p.Type, ID: p.ID,
		Label: label, By: scope.Caller.Member, LinkedAt: s.now().UTC().Format(time.RFC3339),
		State: bridge.CrewLinkOpen, AppName: summary.Name,
	}
	if err := s.upsert(scope.App, row); err != nil {
		return bridge.CrewLink{}, err
	}
	return s.emit(ctx, scope.Caller, scope.App, row)
}

func (s *Service) Remove(ctx context.Context, scope svc.Scope, p bridge.RemoveLinkParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLink(scope.App, p.Crew, p.Type, p.ID); err != nil {
		return err
	}
	row, err := s.unlink(scope.App, p)
	if err != nil {
		return err
	}
	_, err = s.emit(ctx, scope.Caller, scope.App, row)
	return err
}

func (s *Service) ForRecord(ctx context.Context, scope svc.Scope, p bridge.LinksForRecordParams) (bridge.ListLinksResult, error) {
	if err := ctx.Err(); err != nil {
		return bridge.ListLinksResult{}, err
	}
	if err := validateRecord(scope.App, p.Type, p.ID); err != nil {
		return bridge.ListLinksResult{}, err
	}
	return s.list(ctx, scope.Caller, func(app string, row storedLink) bool {
		return app == scope.App && row.Type == p.Type && row.ID == p.ID
	})
}

func (s *Service) ForCrew(ctx context.Context, caller svc.Caller, p bridge.LinksForCrewParams) (bridge.ListLinksResult, error) {
	if err := ctx.Err(); err != nil {
		return bridge.ListLinksResult{}, err
	}
	if !validPart(p.Crew) {
		return bridge.ListLinksResult{}, bridge.Errorf(bridge.CodeInvalid, "crew is required")
	}
	return s.list(ctx, caller, func(_ string, row storedLink) bool { return row.Crew == p.Crew })
}

// Finish marks every open link of the crew finished. Unlinked rows stay, so
// Discussed-in history is not rebuilt. Reopen puts those same finished links
// back to open and does not create new ones.
func (s *Service) Finish(ctx context.Context, crew string) error {
	return s.retarget(ctx, crew, bridge.CrewLinkOpen, bridge.CrewLinkFinished)
}

func (s *Service) Reopen(ctx context.Context, crew string) error {
	return s.retarget(ctx, crew, bridge.CrewLinkFinished, bridge.CrewLinkOpen)
}

func refuseLink(access bridge.CrewLinkAccess, typ, id string) error {
	switch access {
	case bridge.CrewLinkAccessOK:
		return nil
	case bridge.CrewLinkArchived:
		return bridge.Errorf(bridge.CodeConflict, "the app is archived")
	case bridge.CrewLinkNoAccess:
		return bridge.Errorf(bridge.CodeDenied, "no access to this record")
	default:
		return bridge.Errorf(bridge.CodeNotFound, "no "+typ+" "+id)
	}
}

func (s *Service) upsert(app string, row storedLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load(app)
	if err != nil {
		return err
	}
	for i, existing := range file.Links {
		if existing.Crew != row.Crew || existing.Type != row.Type || existing.ID != row.ID {
			continue
		}
		if existing.State == bridge.CrewLinkOpen {
			return bridge.Errorf(bridge.CodeConflict, "already linked to this crew")
		}
		file.Links[i] = row
		return s.save(app, file)
	}
	file.Links = append(file.Links, row)
	return s.save(app, file)
}

func (s *Service) unlink(app string, p bridge.RemoveLinkParams) (storedLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load(app)
	if err != nil {
		return storedLink{}, err
	}
	for i, existing := range file.Links {
		if existing.Crew != p.Crew || existing.Type != p.Type || existing.ID != p.ID {
			continue
		}
		// An unlinked row is already history. Removing it again is not a second event.
		if existing.State == bridge.CrewLinkUnlinked {
			break
		}
		existing.State = bridge.CrewLinkUnlinked
		file.Links[i] = existing
		if err := s.save(app, file); err != nil {
			return storedLink{}, err
		}
		return existing, nil
	}
	return storedLink{}, bridge.Errorf(bridge.CodeNotFound, "no such link")
}

func (s *Service) retarget(ctx context.Context, crew string, from, to bridge.CrewLinkState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validPart(crew) {
		return bridge.Errorf(bridge.CodeInvalid, "crew is required")
	}
	caller := svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}
	s.mu.Lock()
	changed, err := s.retargetLocked(crew, from, to)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	for _, hit := range changed {
		if _, err := s.emit(ctx, caller, hit.app, hit.row); err != nil {
			return err
		}
	}
	return nil
}

type appRow struct {
	app string
	row storedLink
}

func (s *Service) retargetLocked(crew string, from, to bridge.CrewLinkState) ([]appRow, error) {
	index, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	var changed []appRow
	seen := map[string]bool{}
	for _, ref := range index.Crews[crew] {
		if seen[ref.App] {
			continue
		}
		seen[ref.App] = true
		file, err := s.load(ref.App)
		if err != nil {
			return nil, err
		}
		dirty := false
		for i, row := range file.Links {
			if row.Crew != crew || row.State != from {
				continue
			}
			row.State = to
			file.Links[i] = row
			changed = append(changed, appRow{app: ref.App, row: row})
			dirty = true
		}
		if dirty {
			if err := s.save(ref.App, file); err != nil {
				return nil, err
			}
		}
	}
	return changed, nil
}

func (s *Service) list(ctx context.Context, caller svc.Caller, keep func(app string, row storedLink) bool) (bridge.ListLinksResult, error) {
	s.mu.Lock()
	rows, err := s.matching(keep)
	s.mu.Unlock()
	if err != nil {
		return bridge.ListLinksResult{}, err
	}
	links := make([]bridge.CrewLink, 0, len(rows))
	for _, hit := range rows {
		link, err := s.present(ctx, caller, hit.app, hit.row)
		if err != nil {
			return bridge.ListLinksResult{}, err
		}
		links = append(links, link)
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].LinkedAt != links[j].LinkedAt {
			return links[i].LinkedAt > links[j].LinkedAt
		}
		if links[i].App != links[j].App {
			return links[i].App < links[j].App
		}
		return links[i].ID < links[j].ID
	})
	return bridge.ListLinksResult{Links: links}, nil
}

func (s *Service) matching(keep func(string, storedLink) bool) ([]appRow, error) {
	index, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var rows []appRow
	for _, refs := range index.Crews {
		for _, ref := range refs {
			if seen[ref.App] {
				continue
			}
			seen[ref.App] = true
			file, err := s.load(ref.App)
			if err != nil {
				return nil, err
			}
			for _, row := range file.Links {
				if keep(ref.App, row) {
					rows = append(rows, appRow{app: ref.App, row: row})
				}
			}
		}
	}
	return rows, nil
}

func (s *Service) emit(ctx context.Context, caller svc.Caller, app string, row storedLink) (bridge.CrewLink, error) {
	link, err := s.present(ctx, caller, app, row)
	if err != nil {
		return bridge.CrewLink{}, err
	}
	if s.all != nil && s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicLinks(app), link)
	}
	return link, nil
}

func (s *Service) present(ctx context.Context, caller svc.Caller, app string, row storedLink) (bridge.CrewLink, error) {
	link := bridge.CrewLink{
		Crew: row.Crew, CrewName: row.CrewName, App: app, AppName: row.AppName,
		Type: row.Type, ID: row.ID, Label: row.Label, By: row.By,
		LinkedAt: row.LinkedAt, State: row.State,
	}
	if name := s.crewName(row.Crew); name != "" {
		link.CrewName = name
	}
	access, rec, def, summary, err := s.access(ctx, caller, app, row.Type, row.ID)
	if err != nil {
		return bridge.CrewLink{}, err
	}
	link.Access = access
	if summary.Name != "" {
		link.AppName = summary.Name
	}
	if access == bridge.CrewLinkAccessOK {
		if label, _ := presentRecord(def, rec); label != "" {
			link.Label = label
		}
	}
	return link, nil
}

// crewName reads the crew's display name from the profile that owns this apps
// root. A missing teams file leaves the stored name in place.
func (s *Service) crewName(id string) string {
	if s.root == "" {
		return ""
	}
	file, err := teams.Snapshot(filepath.Dir(s.root))
	if err != nil || file == nil {
		return ""
	}
	team, ok := file.Team(id)
	if !ok {
		return ""
	}
	return team.Name
}

func validateLink(app, crew, typ, id string) error {
	if err := validateRecord(app, typ, id); err != nil {
		return err
	}
	if !validPart(crew) {
		return bridge.Errorf(bridge.CodeInvalid, "crew, type and id are required")
	}
	return nil
}

func validateRecord(app, typ, id string) error {
	if !query.ValidName(app) || !query.ValidName(typ) || !validPart(id) {
		return bridge.Errorf(bridge.CodeInvalid, "crew, type and id are required")
	}
	return nil
}

func validPart(v string) bool {
	if v == "" || len(v) > 128 || strings.Contains(v, "..") {
		return false
	}
	for _, r := range v {
		if r < 0x21 || r > 0x7e || strings.ContainsRune(`/\:#?&%`, r) {
			return false
		}
	}
	return true
}

func codeOf(err error) bridge.ErrorCode {
	var bridgeErr *bridge.Error
	if errors.As(err, &bridgeErr) {
		return bridgeErr.Code
	}
	if errors.Is(err, os.ErrNotExist) {
		return bridge.CodeNotFound
	}
	return ""
}
