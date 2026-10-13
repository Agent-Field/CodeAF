package apps

import (
	"github.com/Agent-Field/codeaf/internal/apps/approles"
	"github.com/Agent-Field/codeaf/internal/apps/audit"
	"github.com/Agent-Field/codeaf/internal/apps/signoff"
	"github.com/Agent-Field/codeaf/internal/apps/space"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func wireSpaces(cfg svc.Config, all *svc.Services) {
	spaces := space.New(cfg, all)
	all.Spaces = spaces
	all.Members = spaces.Members()
	all.AppRoles = approles.New(cfg, all)
	all.Audit = audit.New(cfg, all)
	all.Signoff = signoff.New(cfg, all)
}
