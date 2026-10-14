package space

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type workspace struct {
	db        *sql.DB
	name      string
	publicURL string
	signIn    string
	mailer    Mailer
	providers map[string]Provider
	clock     func() time.Time
	secrets   map[string]string
	oauth     map[string]oauthHold
	sockets   *sockSet
	mu        sync.Mutex
}

type oauthHold struct {
	ID, Secret string
	Expires    time.Time
}

type members struct{ s *Service }

// OpenWorkspace opens space.db and prints a setup code the first time this
// home has no Owner.
func OpenWorkspace(cfg svc.Config, all *svc.Services, opt WorkspaceOptions) (*Service, error) {
	name := strings.ToLower(strings.TrimSpace(opt.Name))
	if sanitizeHandle(name) != name || name == "" {
		return nil, invalid("workspace name must be a short lowercase word")
	}
	home := opt.Home
	if home == "" {
		return nil, invalid("workspace home is required")
	}
	signIn := opt.SignIn
	if signIn == "" {
		signIn = "email"
	}
	switch signIn {
	case "email", "google", "github":
	default:
		return nil, invalid("sign-in must be email, google, or github")
	}
	db, err := openDB(home)
	if err != nil {
		return nil, err
	}
	w := &workspace{
		db: db, name: name, publicURL: strings.TrimRight(opt.PublicURL, "/"), signIn: signIn,
		mailer: opt.Mailer, providers: opt.Providers, clock: opt.Now,
		secrets: map[string]string{}, oauth: map[string]oauthHold{}, sockets: newSockSet(),
	}
	if w.mailer == nil {
		w.mailer = LogMailer{}
	}
	if w.providers == nil {
		w.providers = map[string]Provider{}
	}
	s := &Service{all: all, cfg: cfg, kr: osKeyring{}, pend: map[string]pendingJoin{}, ws: w}
	if err := s.ensureSetup(opt.SetupOut); err != nil {
		_ = db.Close()
		return nil, err
	}
	if cfg.Ctx != nil {
		context.AfterFunc(cfg.Ctx, func() { _ = db.Close() })
	}
	return s, nil
}

func (w *workspace) now() time.Time {
	if w.clock != nil {
		return w.clock().UTC()
	}
	return time.Now().UTC()
}

func (w *workspace) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Service) ensureSetup(out io.Writer) error {
	var code string
	var fresh bool
	s.ws.mu.Lock()
	err := s.ws.tx(context.Background(), func(tx *sql.Tx) error {
		used, err := metaGet(tx, "setup_used")
		if err != nil || used == "1" {
			return err
		}
		code, err = metaGet(tx, "setup_code")
		if err != nil || code != "" {
			return err
		}
		code, err = randomToken()
		if err != nil {
			return err
		}
		if err := metaSet(tx, "setup_hash", hashSecret(code)); err != nil {
			return err
		}
		fresh = true
		return metaSet(tx, "setup_code", code)
	})
	s.ws.mu.Unlock()
	if err != nil || !fresh || out == nil {
		return err
	}
	_, err = fmt.Fprintf(out, "workspace %s setup code: %s\n", s.ws.name, code)
	return err
}

func (s *Service) workspaceInfo(ctx context.Context, caller svc.Caller) (bridge.SpaceInfo, error) {
	w := s.ws
	var me bridge.Member
	w.mu.Lock()
	err := w.tx(ctx, func(tx *sql.Tx) error {
		row, err := memberByID(tx, caller.Member)
		if err != nil {
			return err
		}
		me = row.wire()
		return nil
	})
	w.mu.Unlock()
	if err != nil {
		return bridge.SpaceInfo{}, err
	}
	var n int
	if s.all != nil && s.all.Store != nil {
		apps, err := s.all.Store.List(ctx, caller)
		if err != nil {
			return bridge.SpaceInfo{}, err
		}
		n = len(apps)
	}
	return bridge.SpaceInfo{ID: w.name, Name: w.name, Kind: "workspace", Address: urlHost(w.publicURL), Me: me, Apps: n}, nil
}

func (s *Service) audit(ctx context.Context, actor, action, object, text string) {
	if s.all == nil || s.all.Audit == nil || s.ws == nil {
		return
	}
	_, _ = s.all.Audit.Append(ctx, bridge.AuditEntry{
		Space: s.ws.name, Member: actor, Action: action, Object: object, Decision: "allow",
		Activity: &bridge.ActivityLine{Text: text},
	})
}

func (s *Service) publish(list bridge.MembersListResult) {
	if s.all != nil && s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicSpaceMembers, list)
	}
}

func (s *Service) finishGone(ctx context.Context, member string) {
	defer s.ws.sockets.close(member)
	memberGone(ctx, member)
}

func (w *workspace) listResult(tx *sql.Tx) (bridge.MembersListResult, error) {
	rows, err := listMembers(tx)
	if err != nil {
		return bridge.MembersListResult{}, err
	}
	invites, err := listInvites(tx)
	if err != nil {
		return bridge.MembersListResult{}, err
	}
	out := bridge.MembersListResult{Members: []bridge.Member{}, Invites: []bridge.Invite{}}
	for _, row := range rows {
		out.Members = append(out.Members, row.wire())
	}
	now := w.now()
	for _, row := range invites {
		out.Invites = append(out.Invites, bridge.Invite{
			ID: row.ID, Email: row.Email, Role: row.Role, Link: w.linkFor(row), By: row.By,
			Expires: row.Expires, State: inviteState(row, now),
		})
	}
	return out, nil
}

func (w *workspace) linkFor(row inviteRow) string {
	secret := w.secrets[row.ID]
	if secret == "" || row.dead(w.now()) {
		return ""
	}
	return buildInvite(w.publicURL, row.ID, secret)
}

func inviteState(row inviteRow, now time.Time) string {
	switch {
	case row.Cancelled:
		return "cancelled"
	case row.Used:
		return "used"
	case !now.Before(row.Expires):
		return "expired"
	default:
		return "pending"
	}
}

func (w *workspace) snapshot(ctx context.Context) bridge.MembersListResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out bridge.MembersListResult
	_ = w.tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = w.listResult(tx)
		return err
	})
	if out.Members == nil {
		out.Members = []bridge.Member{}
	}
	if out.Invites == nil {
		out.Invites = []bridge.Invite{}
	}
	return out
}

func splitInviteRef(ref string) (id, secret string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", invalid("invite is required")
	}
	if strings.HasPrefix(ref, "codeaf://") {
		_, id, secret, err = parseInvite(ref)
		return id, secret, err
	}
	id, secret, ok := strings.Cut(ref, ".")
	if !ok || id == "" || secret == "" {
		return ref, "", nil
	}
	return id, secret, nil
}

func (w *workspace) liveInvite(tx *sql.Tx, id, secret string) (inviteRow, error) {
	row, err := inviteByID(tx, id)
	if err != nil {
		return inviteRow{}, err
	}
	if secret != "" && !hashEqual(row.SecretHash, secret) {
		return inviteRow{}, inviteExpired(row.By)
	}
	if row.dead(w.now()) {
		return inviteRow{}, inviteExpired(row.By)
	}
	return row, nil
}

func issueSession(tx *sql.Tx, member string, now time.Time) (string, time.Time, error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(sessionTTL)
	_, err = tx.Exec(`INSERT INTO sessions (token_hash, member, expires) VALUES (?, ?, ?)`, hashSecret(token), member, stamp(expires))
	return token, expires, err
}

func uniqueHandle(tx *sql.Tx, want string) (string, error) {
	if want == "" {
		want = "member"
	}
	handle := want
	for n := 2; ; n++ {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM members WHERE id = ?`, handle).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return handle, nil
		}
		if n > 1000 {
			return "", conflict("that name is taken")
		}
		handle = want + fmt.Sprintf("%d", n)
	}
}

func (w *workspace) acceptInvite(tx *sql.Tx, inv inviteRow, email, handle string, now time.Time) (memberRow, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if inv.Email != "" && !strings.EqualFold(inv.Email, email) {
		return memberRow{}, conflict("Use the address the invite was sent to.")
	}
	if _, ok, err := memberByEmail(tx, email); err != nil {
		return memberRow{}, err
	} else if ok && email != "" {
		return memberRow{}, conflict("that email is already in " + w.name)
	}
	res, err := tx.Exec(`UPDATE invites SET used = 1 WHERE id = ? AND used = 0 AND cancelled = 0`, inv.ID)
	if err != nil {
		return memberRow{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return memberRow{}, err
	}
	if n != 1 {
		return memberRow{}, inviteExpired(inv.By)
	}
	if handle == "" {
		handle = handleFromEmail(email)
	}
	handle, err = uniqueHandle(tx, sanitizeHandle(handle))
	if err != nil {
		return memberRow{}, err
	}
	if inv.Role == string(svc.WorkspaceOwner) {
		if _, err := tx.Exec(`UPDATE members SET role = ? WHERE role = ?`, string(svc.WorkspaceAdmin), string(svc.WorkspaceOwner)); err != nil {
			return memberRow{}, err
		}
	}
	row := memberRow{ID: handle, Handle: handle, Email: email, Role: inv.Role, Joined: now, LastSeen: now}
	if err := insertMember(tx, row); err != nil {
		return memberRow{}, err
	}
	return row, nil
}

func handleFromEmail(email string) string {
	local, _, _ := strings.Cut(email, "@")
	if h := sanitizeHandle(local); h != "" {
		return h
	}
	return "member"
}

func (s *Service) claim(ctx context.Context, setup, name, handle, email string) (string, bridge.Member, time.Time, error) {
	w := s.ws
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "" && name != w.name {
		return "", bridge.Member{}, time.Time{}, invalid("that setup code is for " + w.name)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if handle == "" {
		handle = localHandle()
	}
	handle = sanitizeHandle(handle)
	if handle == "" {
		handle = "owner"
	}
	var token string
	var expires time.Time
	var member bridge.Member
	w.mu.Lock()
	err := w.tx(ctx, func(tx *sql.Tx) error {
		used, err := metaGet(tx, "setup_used")
		if err != nil {
			return err
		}
		if used == "1" {
			return conflict("this workspace already has an Owner")
		}
		sum, err := metaGet(tx, "setup_hash")
		if err != nil {
			return err
		}
		if sum == "" || !hashEqual(sum, setup) {
			return denied("that setup code is not valid")
		}
		owners, err := countRole(tx, svc.WorkspaceOwner)
		if err != nil {
			return err
		}
		if owners > 0 {
			return conflict("this workspace already has an Owner")
		}
		handle, err = uniqueHandle(tx, handle)
		if err != nil {
			return err
		}
		now := w.now()
		row := memberRow{ID: handle, Handle: handle, Email: email, Role: string(svc.WorkspaceOwner), Joined: now, LastSeen: now}
		if err := insertMember(tx, row); err != nil {
			return err
		}
		token, expires, err = issueSession(tx, row.ID, now)
		if err != nil {
			return err
		}
		if err := metaSet(tx, "setup_used", "1"); err != nil {
			return err
		}
		if err := metaSet(tx, "setup_code", ""); err != nil {
			return err
		}
		member = row.wire()
		return nil
	})
	w.mu.Unlock()
	if err != nil {
		return "", bridge.Member{}, time.Time{}, err
	}
	s.audit(ctx, member.ID, "spaces.new", "member/"+member.ID, "@"+member.Name+" claimed "+w.name+" as Owner")
	s.publish(w.snapshot(ctx))
	return token, member, expires, nil
}

// loadInvite resolves a link, id.secret, or bare id. A bare id does not check the secret.
func (w *workspace) loadInvite(tx *sql.Tx, ref string, checkSecret bool) (inviteRow, string, error) {
	id, secret, err := splitInviteRef(ref)
	if err != nil {
		return inviteRow{}, "", err
	}
	if !checkSecret {
		secret = ""
	}
	row, err := w.liveInvite(tx, id, secret)
	return row, secret, err
}
