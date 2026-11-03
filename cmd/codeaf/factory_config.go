package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"gopkg.in/yaml.v3"
)

type factoryHostConfig struct {
	Mode           string   `yaml:"mode,omitempty"`
	OperatorLogin  string   `yaml:"operator_login,omitempty"`
	Name           string   `yaml:"name,omitempty"`
	Listen         string   `yaml:"listen"`
	AccessToken    string   `yaml:"access_token"`
	Repos          []string `yaml:"repos"`
	GitHubClientID string   `yaml:"github_client_id,omitempty"`
	Team           []string `yaml:"team,omitempty"`
	// TLS is auto, files or off; empty is auto off loopback and off on it
	// ([resolveFactoryTLS] decides once the listen address is final).
	TLS      string `yaml:"tls,omitempty"`
	Domain   string `yaml:"domain,omitempty"`
	CertFile string `yaml:"cert_file,omitempty"`
	KeyFile  string `yaml:"key_file,omitempty"`
	CacheDir string `yaml:"cache_dir,omitempty"`
	// TrustedProxies are addresses or CIDR ranges whose forwarding headers
	// the service believes.
	TrustedProxies []string `yaml:"trusted_proxies,omitempty"`

	// secure is the HTTPS keys resolved against the final listen address by
	// `factory serve`; nil serves plain HTTP the way a window does.
	secure           *factoryTLS
	operatorLinks    bool
	operatorRecovery bool
	// githubLimit is the team server's view of GitHub's rate limit; nil elsewhere.
	githubLimit *githubLimit
}

// MarshalYAML keeps omission distinct from an explicit empty repository list,
// because only the latter is an instruction to clear the saved selection.
func (cfg factoryHostConfig) MarshalYAML() (any, error) {
	doc := map[string]any{"listen": cfg.Listen, "access_token": cfg.AccessToken}
	if cfg.Mode != "" {
		doc["mode"] = cfg.Mode
	}
	if cfg.OperatorLogin != "" {
		doc["operator_login"] = cfg.OperatorLogin
	}
	if cfg.Name != "" {
		doc["name"] = cfg.Name
	}
	if cfg.GitHubClientID != "" {
		doc["github_client_id"] = cfg.GitHubClientID
	}
	if len(cfg.Team) > 0 {
		doc["team"] = cfg.Team
	}
	if cfg.Repos != nil {
		doc["repos"] = cfg.Repos
	}
	// Every HTTPS key survives a token rotation, which rewrites this file.
	for key, value := range map[string]string{"tls": cfg.TLS, "domain": cfg.Domain,
		"cert_file": cfg.CertFile, "key_file": cfg.KeyFile, "cache_dir": cfg.CacheDir} {
		if value != "" {
			doc[key] = value
		}
	}
	if len(cfg.TrustedProxies) > 0 {
		doc["trusted_proxies"] = cfg.TrustedProxies
	}
	return doc, nil
}

func runFactoryToken(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "add":
			return runFactoryTokenAdd(args[1:])
		case "list":
			return runFactoryTokenList(args[1:])
		}
	}
	flags := commandFlags("factory token")
	rotate := flags.Bool("rotate", false, "replace the access token")
	path := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf factory token [--rotate] [--config path], token add or token list")
	}
	cfg, err := factoryHostConfigAt(*path, *rotate)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, cfg.AccessToken)
	return err
}

// factoryHostConfigAt mints a missing token once under a lock, so starting a
// service and asking for its token at the same time cannot choose two secrets.
func factoryHostConfigAt(path string, rotate bool) (factoryHostConfig, error) {
	release, err := factoryFileLock(path+".lock", false)
	defer release()
	if err != nil {
		return factoryHostConfig{}, err
	}
	cfg, err := readFactoryHostConfig(path)
	if err != nil {
		return cfg, err
	}
	if cfg.AccessToken != "" && !rotate {
		return cfg, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return cfg, err
	}
	cfg.AccessToken = hex.EncodeToString(secret)
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	return cfg, writeFactoryPrivate(path, body)
}

func readFactoryHostConfig(path string) (factoryHostConfig, error) {
	cfg := factoryHostConfig{Listen: factoryDefaultListen}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
		return cfg, fmt.Errorf("factory config %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return cfg, fmt.Errorf("factory config %s: expected one YAML document", path)
	}
	if strings.TrimSpace(cfg.Listen) == "" {
		cfg.Listen = factoryDefaultListen
	}
	if cfg.Mode != "" && cfg.Mode != "solo" && cfg.Mode != "team" {
		return cfg, fmt.Errorf("factory config %s: mode must be solo or team", path)
	}
	if cfg.OperatorLogin != "" && !factoryHandleOK(factory.NormalizeHandle(cfg.OperatorLogin)) {
		return cfg, fmt.Errorf("factory config %s: operator_login must be a GitHub handle", path)
	}
	cfg.Name, err = factory.NormalizeName(cfg.Name)
	if err != nil {
		return cfg, fmt.Errorf("factory config %s: %w", path, err)
	}
	if strings.TrimSpace(cfg.AccessToken) != cfg.AccessToken || strings.ContainsAny(cfg.AccessToken, " \t\r\n") {
		return cfg, fmt.Errorf("factory config %s: access_token must contain no whitespace", path)
	}
	for _, member := range cfg.Team {
		if !factoryHandleOK(factory.NormalizeHandle(member)) {
			return cfg, fmt.Errorf("factory config %s: team must contain GitHub handles", path)
		}
	}
	for _, repo := range cfg.Repos {
		if !store.RepoName(repo) {
			return cfg, fmt.Errorf("factory config %s: %q is not owner/name", path, repo)
		}
	}
	if err := checkFactoryTLSConfig(cfg); err != nil {
		return cfg, fmt.Errorf("factory config %s: %w", path, err)
	}
	return cfg, nil
}

// factoryHandleOK is a GitHub login after NormalizeHandle: letters, digits
// and hyphens, at most 39 characters.
func factoryHandleOK(handle string) bool {
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

// writeFactoryPrivate publishes a complete private file by rename so readers
// never see half a token or half a discovery receipt after an interrupted write.
func writeFactoryPrivate(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".factory-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(body); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
