package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/home"
)

// factoryTokensPath is tokens.json beside the host config. Add, list and
// revoke all use it, and the service is given the same path at start.
func factoryTokensPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "tokens.json")
}

func runFactoryTokenAdd(args []string) error {
	flags := commandFlags("factory token add")
	expires := flags.String("expires", "7d", "`duration` how long the token lasts, such as 7d or 24h")
	configPath := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf factory token add [--expires 7d]")
	}
	return printFactoryToken(os.Stdout, *configPath, *expires)
}

func runFactoryTokenList(args []string) error {
	flags := commandFlags("factory token list")
	configPath := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("codeaf factory token list takes no arguments")
	}
	return listFactoryTokens(os.Stdout, *configPath)
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

// printFactoryToken mints one access-token row and prints the secret alone.
func printFactoryToken(out io.Writer, configPath, expiresFlag string) error {
	ttl, err := parseTokenExpiry(expiresFlag)
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
	_, err = fmt.Fprintln(out, row.Token)
	return err
}

func parseTokenExpiry(raw string) (time.Duration, error) {
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
		if _, err := fmt.Fprintf(out, "%s%s\n", row.ID, when); err != nil {
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
