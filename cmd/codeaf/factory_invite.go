package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/home"
)

// factoryTokensPath is tokens.json beside the host config. Invite, list and
// revoke all use it, and the service is given the same path at start.
func factoryTokensPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "tokens.json")
}

func runFactoryInvite(args []string) error {
	flags := commandFlags("factory invite")
	handle := flags.String("handle", "", "`name` who the link says invited them; the GitHub login when omitted")
	expires := flags.String("expires", "7d", "`duration` how long the token lasts, such as 7d or 24h")
	configPath := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf factory invite [--handle @name] [--expires 7d]")
	}
	return printFactoryInvite(os.Stdout, *configPath, *handle, *expires, func() (string, error) {
		link, err := factorygithub.Link(context.Background(), factoryProfile(""))
		if err != nil {
			return "", err
		}
		return link.Login, nil
	})
}

func runFactoryRevoke(args []string) error {
	flags := commandFlags("factory revoke")
	configPath := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return wrongCall("use codeaf factory revoke <id>")
	}
	return revokeFactoryToken(os.Stdout, *configPath, flags.Arg(0))
}

// printFactoryInvite mints one access-token row and prints the join link twice:
// once as codeaf://, which the app registers, and once as the same link with
// https:// so it can be copied where a custom scheme is not linked.
func printFactoryInvite(out io.Writer, configPath, handleFlag, expiresFlag string, lookup func() (string, error)) error {
	handle, err := factoryInviteHandle(handleFlag, lookup)
	if err != nil {
		return err
	}
	ttl, err := parseInviteExpiry(expiresFlag)
	if err != nil {
		return err
	}
	cfg, err := readFactoryHostConfig(configPath)
	if err != nil {
		return err
	}
	address, err := factoryInviteAddress(cfg.Listen)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var row tokens.Token
	err = withFactoryTokens(configPath, func(path string) error {
		for range 5 {
			minted, err := tokens.Mint()
			if err != nil {
				return err
			}
			minted.InvitedBy = handle
			minted.Created = now
			minted.Expires = now.Add(ttl)
			err = tokens.Add(path, minted)
			if errors.Is(err, tokens.ErrExists) {
				continue
			}
			if err != nil {
				return err
			}
			row = minted
			return nil
		}
		return errors.New("could not mint an access token id")
	})
	if err != nil {
		return err
	}
	link := factoryJoinLink("codeaf", address, row.Token, handle)
	if _, err := fmt.Fprintln(out, link); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, factoryJoinLink("https", address, row.Token, handle))
	return err
}

func factoryInviteHandle(flag string, lookup func() (string, error)) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return inviteHandle(flag)
	}
	if lookup == nil {
		return "", errors.New("codeaf factory invite needs --handle @name; this factory has no GitHub login")
	}
	login, err := lookup()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(login) == "" {
		return "", errors.New("codeaf factory invite needs --handle @name; this factory has no GitHub login")
	}
	return inviteHandle(login)
}

func inviteHandle(raw string) (string, error) {
	handle := factory.NormalizeHandle(raw)
	if !inviteHandleOK(handle) {
		return "", errors.New("use --handle @name")
	}
	return handle, nil
}

func inviteHandleOK(handle string) bool {
	if handle == "" || len(handle) > 39 {
		return false
	}
	for _, r := range handle {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func parseInviteExpiry(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	refuse := errors.New("expiry must be a duration like 7d or 24h")
	if raw == "" {
		return 0, refuse
	}
	if strings.HasSuffix(strings.ToLower(raw), "d") {
		n := strings.TrimSuffix(strings.ToLower(raw), "d")
		days := 0
		for _, r := range n {
			if r < '0' || r > '9' {
				return 0, refuse
			}
			days = days*10 + int(r-'0')
		}
		if days <= 0 {
			return 0, refuse
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, refuse
	}
	return d, nil
}

// factoryInviteAddress is the running service when one answers, otherwise the
// host config's listen address.
func factoryInviteAddress(listen string) (string, error) {
	if target, ok := liveFactoryDiscovery(); ok {
		return target.url, nil
	}
	return factoryURL("http://" + strings.TrimSpace(listen))
}

func factoryJoinLink(scheme, address, token, by string) string {
	return scheme + "://attach?url=" + url.QueryEscape(address) +
		"&token=" + url.QueryEscape(token) +
		"&by=" + url.QueryEscape(by)
}

func listFactoryTokens(out io.Writer, configPath string) error {
	rows, err := tokens.Load(factoryTokensPath(configPath))
	if err != nil {
		return err
	}
	for _, row := range rows {
		when := ""
		if !row.Expires.IsZero() {
			when = " " + row.Expires.UTC().Format(time.RFC3339)
		}
		if _, err := fmt.Fprintf(out, "%s %s%s\n", row.ID, row.InvitedBy, when); err != nil {
			return err
		}
	}
	return nil
}

func revokeFactoryToken(out io.Writer, configPath, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return wrongCall("use codeaf factory revoke <id>")
	}
	err := withFactoryTokens(configPath, func(path string) error {
		return tokens.Remove(path, id)
	})
	if errors.Is(err, tokens.ErrNotFound) {
		return fmt.Errorf("no access token %s", id)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "revoked "+id)
	return err
}

func withFactoryTokens(configPath string, fn func(path string) error) error {
	path := factoryTokensPath(configPath)
	release, err := factoryFileLock(path+".lock", false)
	defer release()
	if err != nil {
		return err
	}
	return fn(path)
}

// parseFactoryJoinLink reports whether raw is a join link from factory invite.
// A normal factory URL is not one. A join link missing its address or token
// is an error rather than an address named "attach".
func parseFactoryJoinLink(raw string) (address, token string, linked bool, err error) {
	u, parseErr := url.Parse(strings.TrimSpace(raw))
	if parseErr != nil || u.Scheme == "" {
		return "", "", false, nil
	}
	if u.Scheme != "codeaf" && !(u.Scheme == "https" && strings.EqualFold(u.Host, "attach")) {
		return "", "", false, nil
	}
	if u.Scheme == "codeaf" && !strings.EqualFold(u.Host, "attach") {
		return "", "", true, fmt.Errorf("%q is not a factory join link", raw)
	}
	q := u.Query()
	address, token = strings.TrimSpace(q.Get("url")), strings.TrimSpace(q.Get("token"))
	if address == "" || token == "" {
		return "", "", true, errors.New("a join link needs a factory address and a token")
	}
	address, err = factoryURL(address)
	if err != nil {
		return "", "", true, err
	}
	return address, token, true, nil
}
