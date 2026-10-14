package space

import (
	"context"
	"io"
	"net/http"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the personal sidecar, or a workspace server when OpenWorkspace built it.
type Service struct {
	all    *svc.Services
	cfg    svc.Config
	kr     Keyring
	http   *http.Client
	handle string
	pendMu sync.Mutex
	pend   map[string]pendingJoin
	ws     *workspace
}

type pendingJoin struct {
	ID, Name, Origin, Address, Invite, Email, Kind, Role, By, URL, Handle string
	Expires                                                               time.Time
}

// PersonalOptions configures the sidecar. A nil Keyring uses the OS keychain.
type PersonalOptions struct {
	Keyring Keyring
	Handle  string
	HTTP    *http.Client
}

// WorkspaceOptions configures a workspace server. Home is the directory that
// holds space.db. PublicURL is the address invite links name.
type WorkspaceOptions struct {
	Name, Home, PublicURL, SignIn string
	Mailer                        Mailer
	Providers                     map[string]Provider
	Now                           func() time.Time
	SetupOut                      io.Writer
}

func New(cfg svc.Config, all *svc.Services) *Service {
	return &Service{all: all, cfg: cfg, kr: osKeyring{}, pend: map[string]pendingJoin{}}
}

// OpenPersonal is the sidecar with an explicit keyring. Tests pass a file keyring.
func OpenPersonal(cfg svc.Config, all *svc.Services, opt PersonalOptions) *Service {
	s := New(cfg, all)
	if opt.Keyring != nil {
		s.kr = opt.Keyring
	}
	s.handle = opt.Handle
	s.http = opt.HTTP
	return s
}

func (s *Service) Members() svc.Members {
	if s.ws != nil {
		return &members{s: s}
	}
	return &MemberService{}
}

func (s *Service) Info(ctx context.Context, caller svc.Caller) (bridge.SpaceInfo, error) {
	if s.ws != nil {
		return s.workspaceInfo(ctx, caller)
	}
	apps, err := s.all.Store.List(ctx, svc.PersonalCaller())
	return bridge.SpaceInfo{ID: svc.PersonalSpace, Name: "Personal", Kind: "personal", Me: personalMember(), Apps: len(apps)}, err
}

func personalMember() bridge.Member {
	return bridge.Member{ID: svc.Owner, Name: svc.Owner, Role: string(svc.WorkspaceOwner)}
}

func (s *Service) personalOnly() error {
	if s.ws != nil {
		return denied("this call is only on Personal")
	}
	return nil
}

func (s *Service) client() *http.Client {
	if s.http != nil {
		return s.http
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func localHandle() string {
	u, err := user.Current()
	if err != nil {
		return "owner"
	}
	if h := sanitizeHandle(u.Username); h != "" {
		return h
	}
	return "owner"
}

func sanitizeHandle(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	h := strings.Trim(b.String(), "-")
	if h == "" {
		return ""
	}
	if h[0] < 'a' || h[0] > 'z' {
		h = "m" + h
	}
	if len(h) > 32 {
		h = h[:32]
	}
	return h
}

var (
	_ svc.Spaces  = (*Service)(nil)
	_ svc.Members = (*members)(nil)
	_ svc.Members = (*MemberService)(nil)
)

func roleTitle(role string) string {
	switch svc.WorkspaceRole(role) {
	case svc.WorkspaceOwner:
		return "Owner"
	case svc.WorkspaceAdmin:
		return "Admin"
	default:
		return "Member"
	}
}
