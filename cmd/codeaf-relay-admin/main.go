// codeaf-relay-admin is an operator tool and is deliberately excluded from releases.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

const adminDomain = "codeaf/relay-admin/v1\n"
const adminPath = "/v1/admin/storage"

var requestCode = regexp.MustCompile(`^id_[0-9a-f]{32}$`)

type approval struct {
	V        int    `json:"v"`
	Host     string `json:"host"`
	Identity string `json:"identity"`
	Storage  string `json:"storage"`
	At       int64  `json:"at"`
	Nonce    string `json:"nonce"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, hc *http.Client) error {
	if len(args) == 0 {
		return errors.New("usage: codeaf-relay-admin approve|revoke <request-code> [--relay prod|staging|dev|<url>] [--key <path>], or public-key [--key <path>]")
	}
	action, args := args[0], args[1:]
	if action != "approve" && action != "revoke" && action != "public-key" {
		return errors.New("unknown admin command")
	}
	code := ""
	if action != "public-key" {
		if len(args) == 0 || !requestCode.MatchString(args[0]) {
			return errors.New("request code must be id_ followed by 32 lowercase hex digits")
		}
		code, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("codeaf-relay-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	relay := flags.String("relay", "prod", "relay name or URL")
	keyPath := flags.String("key", os.Getenv("CODEAF_RELAY_ADMIN_KEY"), "private seed file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected admin command argument")
	}
	if *keyPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*keyPath = filepath.Join(home, ".config", "codeaf-relay-admin", "admin.key")
	}
	key, err := readKey(*keyPath)
	if err != nil {
		return err
	}
	if action == "public-key" {
		_, err = fmt.Fprintln(out, base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
		return err
	}
	base, err := relayURL(*relay)
	if err != nil {
		return err
	}
	nonce := make([]byte, 24)
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	storage := "on"
	if action == "revoke" {
		storage = "off"
	}
	raw, err := json.Marshal(approval{1, base.Host, code, storage, time.Now().UnixMilli(), base64.RawURLEncoding.EncodeToString(nonce)})
	if err != nil {
		return err
	}
	signature := ed25519.Sign(key, append([]byte(adminDomain), raw...))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wireauth.Endpoint(base.String(), adminPath), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("codeaf-admin-signature", base64.RawURLEncoding.EncodeToString(signature))
	resp, err := hc.Do(req)
	if err != nil {
		return errors.New("relay request failed: " + err.Error())
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil {
		return err
	}
	if len(answer) > 4096 {
		return errors.New("relay answer is too large")
	}
	var reply struct {
		Identity string `json:"identity"`
		Storage  string `json:"storage"`
		Err      string `json:"err"`
	}
	if json.Unmarshal(answer, &reply) != nil {
		return fmt.Errorf("relay answered HTTP %d with an unreadable answer", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay refused: %s (HTTP %d)", reply.Err, resp.StatusCode)
	}
	if reply.Identity != code || reply.Storage != storage {
		return errors.New("relay answer does not match the request")
	}
	_, err = fmt.Fprintf(out, "%s storage %s\n", reply.Identity, reply.Storage)
	return err
}

func relayURL(raw string) (*url.URL, error) {
	switch raw {
	case "prod":
		raw = syncsetup.ProductionRelayURL
	case "staging":
		raw = syncsetup.StagingRelayURL
	case "dev":
		raw = syncsetup.DevRelayURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("relay must be prod, staging, dev or an HTTP URL")
	}
	return u, nil
}

func readKey(path string) (ed25519.PrivateKey, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("could not open admin key file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("admin key file must be a regular file readable only by its owner")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	seed, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("admin key file must contain one unpadded base64url Ed25519 seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
