package crewagent

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) capabilityLine(ctx context.Context, scope svc.Scope, def *spec.Bundle) string {
	var reads, writes []string
	for typ, access := range def.App.Capabilities.Records {
		if access != spec.AccessRead && access != spec.AccessReadWrite {
			continue
		}
		raw, _ := json.Marshal(bridge.QueryParams{Type: typ})
		if s.all.Caps.Allow(ctx, scope, bridge.MethodRecordsQuery, raw) != nil {
			continue
		}
		name := typ
		if rt := def.App.Records[typ]; rt != nil && rt.Plural != "" {
			name = strings.ToLower(rt.Plural)
		}
		reads = append(reads, name)
		if access == spec.AccessReadWrite && svc.RoleOf(scope.Caller) != svc.RoleViewer {
			writes = append(writes, name)
		}
	}
	sort.Strings(reads)
	sort.Strings(writes)
	var lines []string
	if len(reads) > 0 {
		lines = append(lines, "read "+strings.Join(reads, ", "))
	}
	if len(writes) > 0 {
		lines = append(lines, "create and change "+strings.Join(writes, ", "))
	}
	if svc.RoleOf(scope.Caller) != svc.RoleViewer {
		lines = append(lines, connectorLines(def.App.Capabilities)...)
	}
	if len(lines) == 0 {
		return ""
	}
	return "can " + strings.Join(lines, ", ")
}

func connectorLines(caps spec.Capabilities) []string {
	set := map[string]bool{}
	for name, connector := range caps.Connectors {
		for _, verb := range connector.Can {
			noun := name
			if name == "gmail" || name == "email" {
				noun = "emails"
			}
			set[strings.ReplaceAll(verb, "_", " ")+" "+noun] = true
		}
	}
	for _, tool := range caps.Tools {
		set[strings.ReplaceAll(tool, ".", " ")] = true
	}
	lines := make([]string, 0, len(set))
	for line := range set {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return lines
}
