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

	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"gopkg.in/yaml.v3"
)

type factoryHostConfig struct {
	Listen      string   `yaml:"listen"`
	AccessToken string   `yaml:"access_token"`
	Repos       []string `yaml:"repos"`
}

// MarshalYAML keeps omission distinct from an explicit empty repository list,
// because only the latter is an instruction to clear the saved selection.
func (cfg factoryHostConfig) MarshalYAML() (any, error) {
	doc := map[string]any{"listen": cfg.Listen, "access_token": cfg.AccessToken}
	if cfg.Repos != nil {
		doc["repos"] = cfg.Repos
	}
	return doc, nil
}

func runFactoryToken(args []string) error {
	flags := commandFlags("factory token")
	rotate := flags.Bool("rotate", false, "replace the access token")
	path := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf factory token [--rotate] [--config path]")
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
	if strings.TrimSpace(cfg.AccessToken) != cfg.AccessToken || strings.ContainsAny(cfg.AccessToken, " \t\r\n") {
		return cfg, fmt.Errorf("factory config %s: access_token must contain no whitespace", path)
	}
	for _, repo := range cfg.Repos {
		if !store.RepoName(repo) {
			return cfg, fmt.Errorf("factory config %s: %q is not owner/name", path, repo)
		}
	}
	return cfg, nil
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
