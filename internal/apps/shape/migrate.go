package shape

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type migrationField struct {
	spec.Field
	Alias string `json:"alias,omitempty"`
}
type migrationType struct {
	Label  string                     `json:"label"`
	Plural string                     `json:"plural"`
	Fields map[string]*migrationField `json:"fields"`
}
type migrationApp struct {
	Records map[string]*migrationType `json:"records"`
}

func migrationDefinition(files spec.Files) (migrationApp, error) {
	var app migrationApp
	err := json.Unmarshal(files[spec.AppFile], &app)
	if err != nil {
		return app, err
	}
	if len(app.Records) == 0 {
		return app, bridge.Errorf(bridge.CodeInvalid, "app.json requires record types")
	}
	for _, typ := range app.Records {
		if typ == nil {
			return app, bridge.Errorf(bridge.CodeInvalid, "invalid record type")
		}
		for _, field := range typ.Fields {
			if field == nil {
				return app, bridge.Errorf(bridge.CodeInvalid, "invalid record field")
			}
		}
	}
	return app, nil
}

func sortedNames[T any](values map[string]T) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (s *Service) Preview(ctx context.Context, scope svc.Scope, _ bridge.ShapePreviewParams) (bridge.ShapePreviewResult, error) {
	if s.cfg.Root == "" {
		return bridge.ShapePreviewResult{}, bridge.NotBuilt("shape")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	detail, draft, files, err := s.applyCandidate(ctx, scope)
	if err != nil {
		return bridge.ApplyPreview{}, err
	}
	return s.previewCandidate(ctx, scope.App, detail, draft, files)
}

func (s *Service) previewCandidate(ctx context.Context, app string, base bridge.AppDetail, draft bridge.Draft, files spec.Files) (bridge.ApplyPreview, error) {
	old, err := spec.Decode(detailFiles(base))
	if err != nil {
		return bridge.ApplyPreview{}, err
	}
	next, err := spec.Decode(files)
	if err != nil {
		return bridge.ApplyPreview{}, err
	}
	migration, checked, err := s.migration(ctx, app, detailFiles(base), files, base.Version)
	if err != nil {
		return bridge.ApplyPreview{}, err
	}
	audience := "only you"
	if base.Space != "" && base.Space != "personal" {
		audience = fmt.Sprintf("everyone in %s gets v%d", base.Space, base.Version+1)
	}
	result := bridge.ApplyPreview{From: base.Version, To: base.Version + 1, Changes: []bridge.ApplyChange{}, Audience: audience, Needs: caps.Needs(next.App), NeedsChanged: !reflect.DeepEqual(caps.Needs(old.App), caps.Needs(next.App)), Migration: migration, RecordsChecked: checked}
	for _, change := range draft.Changes {
		if change.State == "passed" {
			result.Changes = append(result.Changes, bridge.ApplyChange{N: change.N, Request: change.Request})
		}
	}
	return result, nil
}

func (s *Service) migration(ctx context.Context, app string, before, after spec.Files, version int) ([]bridge.MigrationLine, int, error) {
	old, err := migrationDefinition(before)
	if err != nil {
		return nil, 0, err
	}
	next, err := migrationDefinition(after)
	if err != nil {
		return nil, 0, err
	}
	lines := []bridge.MigrationLine{}
	types := map[string]bool{}
	for typ := range old.Records {
		types[typ] = true
	}
	for typ := range next.Records {
		types[typ] = true
	}
	checked := 0
	path := filepath.Join(s.cfg.Root, app, "records.db")
	for _, typ := range sortedNames(types) {
		count, err := records.ShapeCounts(ctx, path, typ, "")
		if err != nil {
			return nil, 0, err
		}
		checked += count
		a, b := old.Records[typ], next.Records[typ]
		if a == nil {
			a = &migrationType{Fields: map[string]*migrationField{}}
		}
		if b == nil {
			b = &migrationType{Fields: map[string]*migrationField{}}
		}
		renamed := map[string]bool{}
		for _, field := range sortedNames(b.Fields) {
			f := b.Fields[field]
			if f == nil {
				return nil, 0, bridge.Errorf(bridge.CodeInvalid, "invalid migration field")
			}
			op, source := "", field
			switch {
			case a.Fields[field] != nil:
				if a.Fields[field].Type != f.Type {
					op = "retype"
				}
			case f.Alias != "" && a.Fields[f.Alias] != nil:
				op, source = "rename", f.Alias
				renamed[source] = true
			default:
				op = "add"
			}
			if op == "" {
				continue
			}
			line, err := migrationLine(ctx, path, typ, source, field, op, a, b, version)
			if err != nil {
				return nil, 0, err
			}
			lines = append(lines, line)
		}
		for _, field := range sortedNames(a.Fields) {
			if b.Fields[field] != nil || renamed[field] {
				continue
			}
			line, err := migrationLine(ctx, path, typ, field, "", "remove", a, b, version)
			if err != nil {
				return nil, 0, err
			}
			lines = append(lines, line)
		}
	}
	return lines, checked, nil
}

func migrationLine(ctx context.Context, path, typ, source, target, op string, old, next *migrationType, version int) (bridge.MigrationLine, error) {
	count, err := records.ShapeCounts(ctx, path, typ, source)
	if err != nil {
		return bridge.MigrationLine{}, err
	}
	plural := next.Plural
	if plural == "" {
		plural = old.Plural
	}
	if plural == "" {
		plural = typ + "s"
	}
	label := func(def *migrationType, field string) string {
		if f := def.Fields[field]; f != nil && f.Label != "" {
			return f.Label
		}
		return field
	}
	line := bridge.MigrationLine{Op: op, Type: typ, Field: source, Affected: count}
	switch op {
	case "rename":
		line.To = target
		line.Line = fmt.Sprintf("Rename “%s” to “%s” · %d %s", label(old, source), label(next, target), count, plural)
		line.Note = "nothing deleted · old name kept as alias · " + source + ": filters and runs keep working"
	case "remove":
		line.Line = fmt.Sprintf("Remove field “%s” · %d %s have a value", label(old, source), count, plural)
		line.Note = fmt.Sprintf("hidden, restorable · values stay stored · Restore v%d brings it back", version)
	case "add":
		line.Line = fmt.Sprintf("Add field “%s” · %d %s", label(next, target), count, plural)
		line.Note = "defaults applied lazily · nothing deleted"
	case "retype":
		line.To = string(next.Fields[target].Type)
		line.Line = fmt.Sprintf("Change field “%s” to %s · %d %s", label(next, target), line.To, count, plural)
		line.Note = "values stay stored · nothing deleted"
	}
	return line, nil
}

var summaryRename = regexp.MustCompile(`(?i)(?:alias|renamed?)\s+["“]?([\w -]+?)["”]?\s*(?:→|->|\bto\b)\s*["“]?([\w -]+)["”]?`)

// annotatedRenames persists explicit Builder rename summaries as field aliases;
// matching requires both a removed source and an added destination.
func annotatedRenames(before, after spec.Files, changes []bridge.Change) (spec.Files, error) {
	old, err := migrationDefinition(before)
	if err != nil {
		return nil, err
	}
	next, err := migrationDefinition(after)
	if err != nil {
		return nil, err
	}
	var app map[string]any
	if err := json.Unmarshal(after[spec.AppFile], &app); err != nil {
		return nil, err
	}
	types := app["records"].(map[string]any)
	for _, change := range changes {
		for _, match := range summaryRename.FindAllStringSubmatch(change.Summary, -1) {
			for typ, def := range next.Records {
				previous := old.Records[typ]
				if previous == nil {
					continue
				}
				source := findField(previous, strings.TrimSpace(match[1]))
				target := findField(def, strings.TrimSpace(match[2]))
				if source == "" || target == "" || def.Fields[source] != nil || previous.Fields[target] != nil {
					continue
				}
				fields := types[typ].(map[string]any)["fields"].(map[string]any)
				field := fields[target].(map[string]any)
				if _, explicit := field["alias"]; !explicit {
					field["alias"] = source
				}
			}
		}
	}
	data, err := json.MarshalIndent(app, "", "  ")
	copy := make(spec.Files, len(after))
	for name, data := range after {
		copy[name] = data
	}
	copy[spec.AppFile] = append(data, '\n')
	return copy, err
}
func findField(def *migrationType, text string) string {
	for _, name := range sortedNames(def.Fields) {
		if strings.EqualFold(name, text) || strings.EqualFold(def.Fields[name].Label, text) {
			return name
		}
	}
	return ""
}
