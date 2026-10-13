package records

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) shapeDatabaseFilename(scope svc.Scope) (string, error) {
	switch scope.Data {
	case svc.DataReal:
		return "records.db", nil
	case svc.DataSample:
		return "sample.db", nil
	case svc.DataDraft:
		return s.draftDatabaseFilename(scope)
	default:
		return "", invalid("invalid data mode")
	}
}

func (s *Service) draftDatabaseFilename(scope svc.Scope) (string, error) {
	member := scope.Caller.Member
	if member == "" || member == "." || member == ".." || strings.ContainsAny(member, "/\\") || filepath.IsAbs(member) {
		return "", invalid("invalid draft member")
	}
	filename := filepath.Join("drafts", member, "overlay.db")
	info, err := os.Stat(filepath.Join(s.cfg.Root, scope.App, filename))
	if os.IsNotExist(err) {
		return "", bridge.Errorf(bridge.CodeNotFound, "no draft")
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", bridge.Errorf(bridge.CodeNotFound, "no draft")
	}
	// SQLite must open an existing scratch copy; opening a draft never seeds real data.
	return filename, nil
}
