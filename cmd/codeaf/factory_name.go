package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"gopkg.in/yaml.v3"
)

func runFactoryName(args []string) error {
	flags := commandFlags("factory name")
	path := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	var text *string
	if flags.NArg() > 0 {
		words := strings.Join(flags.Args(), " ")
		text = &words
	}
	name, err := factoryNameAt(*path, text)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, name)
	return err
}

// factoryNameAt shares the token writer's lock so a name edit cannot erase a
// concurrent token rotation. Reading a default never saves that derived name.
func factoryNameAt(path string, text *string) (string, error) {
	release, err := factoryFileLock(path+".lock", false)
	defer release()
	if err != nil {
		return "", err
	}
	cfg, err := readFactoryHostConfig(path)
	if err != nil {
		return "", err
	}
	if text != nil {
		cfg.Name, err = factory.NormalizeName(*text)
		if err != nil {
			return "", err
		}
		body, err := yaml.Marshal(cfg)
		if err != nil {
			return "", err
		}
		if err := writeFactoryPrivate(path, body); err != nil {
			return "", err
		}
	}
	repos := store.CleanRepos(cfg.Repos)
	if cfg.Name == "" && cfg.Repos == nil {
		st, err := store.Open(v3FactoryRoot())
		if err != nil {
			return "", err
		}
		repos, err = st.Repos()
		if err != nil {
			return "", err
		}
	}
	return factory.DisplayName(cfg.Name, repos), nil
}
