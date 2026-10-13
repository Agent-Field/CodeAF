package shape

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) Health(ctx context.Context, scope svc.Scope, _ bridge.AppsHealthParams) (bridge.AppsHealthResult, error) {
	if s.cfg.Root == "" {
		return bridge.AppsHealthResult{}, bridge.NotBuilt("shape")
	}
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return bridge.AppHealth{}, err
	}
	health, err := s.readHealth(scope.App)
	if health.Version != detail.Version {
		health = bridge.AppHealth{Version: detail.Version}
	}
	return health, err
}

func (s *Service) readHealth(app string) (bridge.AppHealth, error) {
	data, err := os.ReadFile(filepath.Join(s.cfg.Root, app, "health.json"))
	if errors.Is(err, os.ErrNotExist) {
		return bridge.AppHealth{}, nil
	}
	if err != nil {
		return bridge.AppHealth{}, err
	}
	var health bridge.AppHealth
	err = json.Unmarshal(data, &health)
	return health, err
}

type healthRows map[string][]bridge.Record

func (rows healthRows) Generate(context.Context, *spec.Bundle) (map[string][]bridge.Record, error) {
	return rows, nil
}

// Render uses Check's renderer with a read-only snapshot as its Sample seam.
// Runs and Records are absent from this isolated checker, so behaviour cannot
// start runs, reseed data or write through the real service.
func (s *Service) checkHealth(ctx context.Context, app string, version, previous int, request, member string, files spec.Files) error {
	prior, err := s.readHealth(app)
	if err != nil {
		return err
	}
	rows, err := records.ShapeSnapshot(ctx, filepath.Join(s.cfg.Root, app, "records.db"))
	if err != nil {
		return err
	}
	aliases, err := records.ShapeAliases(files)
	if err != nil {
		return err
	}
	files, err = store.ShapeValidationFiles(files)
	if err != nil {
		return err
	}
	b, err := spec.Decode(files)
	if err != nil {
		return err
	}
	projected := healthRows{}
	for typ, def := range b.App.Records {
		for _, row := range rows[typ] {
			fields := map[string]any{}
			for field, definition := range def.Fields {
				source := field
				if target := aliases[typ][field]; target != "" {
					source = target
				}
				fields[field] = row.Fields[source]
				if fields[field] == nil && len(definition.Default) > 0 {
					var value any
					if err := json.Unmarshal(definition.Default, &value); err != nil {
						return err
					}
					if value == "$me" {
						value = member
					}
					fields[field] = value
				}
			}
			row.Fields = fields
			projected[typ] = append(projected[typ], row)
		}
	}
	checker := check.New(s.cfg, &svc.Services{Sample: projected})
	report, err := checker.Run(ctx, app, files, 1)
	if err != nil {
		return err
	}
	issues := make([]spec.Issue, 0, len(report.Issues))
	for _, issue := range report.Issues {
		if issue.Rule == "render/bind" && strings.HasPrefix(issue.Message, "no sample records for ") {
			continue
		}
		issues = append(issues, issue)
	}
	health := bridge.AppHealth{Version: version}
	if len(issues) > 0 {
		lastGood := previous
		if prior.Broken != nil {
			lastGood = prior.Broken.LastGood
		}
		issue := issues[0]
		affected := s.affectedRows(ctx, files, projected, issue.File, issue.Path)
		health.Broken = &bridge.Broken{Version: version, Request: request, Error: issue.Message, File: issue.File, Line: healthLine(files[issue.File], issue.Path), Affected: affected, LastGood: lastGood}
	}
	if err := writeHealth(filepath.Join(s.cfg.Root, app, "health.json"), health); err != nil {
		return err
	}
	s.publish(bridge.TopicHealth(app), health)
	return nil
}

func (s *Service) affectedRows(ctx context.Context, files spec.Files, rows healthRows, file, path string) int {
	typ := healthRecordType(files[file], path)
	affected := 0
	for _, row := range rows[typ] {
		single := healthRows{}
		for name, list := range rows {
			single[name] = list
		}
		single[typ] = []bridge.Record{row}
		report, err := check.New(s.cfg, &svc.Services{Sample: single}).Run(ctx, "", files, 1)
		if err != nil {
			continue
		}
		for _, issue := range report.Issues {
			if issue.File == file && issue.Path == path {
				affected++
				break
			}
		}
	}
	return affected
}

func healthRecordType(data []byte, path string) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return ""
	}
	typ := ""
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, part := range append(parts, "") {
		switch node := value.(type) {
		case map[string]any:
			if bind, ok := node["bind"].(map[string]any); ok {
				if name, ok := bind["records"].(string); ok {
					typ = name
				}
				if name, ok := bind["type"].(string); ok {
					typ = name
				}
			}
			value = node[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return typ
			}
			value = node[index]
		default:
			return typ
		}
	}
	return typ
}

// Paths point into JSON nodes; locate the final property for the desk's file
// link without claiming a line when the source does not contain it.
func healthLine(data []byte, path string) int {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return 0
	}
	key := parts[len(parts)-1]
	quoted, _ := json.Marshal(key)
	found := 0
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, string(quoted)+":") {
			if found != 0 {
				return 0
			}
			found = i + 1
		}
	}
	return found
}

func writeHealth(path string, health bridge.AppHealth) error {
	data, err := json.MarshalIndent(health, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".health-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
