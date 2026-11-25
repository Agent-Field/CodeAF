package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type App struct {
	ID    string `json:"id"`
	Agent struct {
		File  string `json:"file"`
		Model string `json:"model"`
	} `json:"agent"`
	Capabilities struct {
		Records    map[string]string `json:"records"`
		Connectors map[string]struct {
			Can      []string `json:"can"`
			Approve  []string `json:"approve"`
			Channels []string `json:"channels"`
		} `json:"connectors"`
		Tools []string `json:"tools"`
	} `json:"capabilities"`
	Triggers []Trigger `json:"triggers"`
	Budget   Budget    `json:"budget"`
}

// Files loads live runs from the app directory layout. Check and Preview can
// instead supply their own definition loader and use a scratch journal.
type Files string

func (f Files) open(app string) (*os.Root, error) {
	if !validID(app) || strings.HasPrefix(app, "_") {
		return nil, ErrNotFound
	}
	root, err := os.OpenRoot(string(f))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	appRoot, err := root.OpenRoot(app)
	if err != nil {
		return nil, err
	}
	defer appRoot.Close()
	encoded, err := appRoot.ReadFile("meta.json")
	if err != nil {
		return nil, err
	}
	var meta struct{ Archived bool }
	if err := json.Unmarshal(encoded, &meta); err != nil {
		return nil, err
	}
	if meta.Archived {
		return nil, ErrNotFound
	}
	return appRoot.OpenRoot("current")
}

func readApp(root *os.Root, id string) (App, error) {
	var app App
	raw, err := root.ReadFile("app.json")
	if err != nil {
		return app, err
	}
	if err := json.Unmarshal(raw, &app); err != nil {
		return app, err
	}
	if app.ID != id {
		return app, errors.New("app id does not match its directory")
	}
	return app, nil
}

func (f Files) App(id string) (App, error) {
	root, err := f.open(id)
	if err != nil {
		return App{}, err
	}
	defer root.Close()
	return readApp(root, id)
}

func (f Files) Load(app, run string) (Definition, error) {
	if !validID(run) {
		return Definition{}, ErrNotFound
	}
	root, err := f.open(app)
	if err != nil {
		return Definition{}, err
	}
	defer root.Close()
	manifest, err := readApp(root, app)
	if err != nil {
		return Definition{}, err
	}
	raw, err := root.ReadFile(filepath.Join("runs", run+".yaml"))
	if err != nil {
		return Definition{}, err
	}
	definition, err := Parse(raw)
	if err != nil {
		return Definition{}, err
	}
	if cap := manifest.Budget.PerRun; cap > 0 && (definition.Budget.PerRun == 0 || cap < definition.Budget.PerRun) {
		definition.Budget.PerRun = cap
	}
	return definition, nil
}

func (f Files) AgentPrompt(id string) (string, string, error) {
	root, err := f.open(id)
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	app, err := readApp(root, id)
	if err != nil {
		return "", "", err
	}
	if app.Agent.File != "agent.md" {
		return "", "", errors.New("the app has no agent.md")
	}
	prompt, err := root.ReadFile("agent.md")
	return string(prompt), app.Agent.Model, err
}

func (f Files) Triggers() ([]Trigger, error) {
	entries, err := os.ReadDir(string(f))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var triggers []Trigger
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		app, loadErr := f.App(entry.Name())
		switch {
		case errors.Is(loadErr, fs.ErrNotExist), errors.Is(loadErr, ErrNotFound):
			continue
		case loadErr != nil:
			err = errors.Join(err, fmt.Errorf("app %s: %w", entry.Name(), loadErr))
			continue
		}
		for _, trigger := range app.Triggers {
			trigger.App = app.ID
			triggers = append(triggers, trigger)
		}
	}
	return triggers, err
}

func (a App) allowsTool(service, op string, aliases ...string) (allowed, ask bool) {
	connector, present := a.Capabilities.Connectors[service]
	if present {
		for _, verb := range append(aliases, op) {
			if verb == "" {
				continue
			}
			if slices.Contains(connector.Can, verb) {
				allowed = true
			}
			if slices.Contains(connector.Approve, verb) {
				ask = true
			}
		}
	}
	return allowed || slices.Contains(a.Capabilities.Tools, service+"."+op), ask
}
