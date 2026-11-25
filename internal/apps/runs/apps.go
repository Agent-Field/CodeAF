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
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"gopkg.in/yaml.v3"
)

type App = schema.App

// Files loads live runs from the app directory layout. Check and Preview can
// instead supply their own definition loader and use a scratch journal.
type Files string

type appMeta struct {
	Archived  bool      `json:"archived"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (f Files) open(app string) (*os.Root, appMeta, error) {
	var meta appMeta
	if !validID(app) || strings.HasPrefix(app, "_") {
		return nil, meta, ErrNotFound
	}
	root, err := os.OpenRoot(string(f))
	if err != nil {
		return nil, meta, err
	}
	defer root.Close()
	appRoot, err := root.OpenRoot(app)
	if err != nil {
		return nil, meta, err
	}
	defer appRoot.Close()
	encoded, err := appRoot.ReadFile("meta.json")
	if err != nil {
		return nil, meta, err
	}
	if err := json.Unmarshal(encoded, &meta); err != nil {
		return nil, meta, err
	}
	if meta.Archived {
		return nil, meta, ErrNotFound
	}
	current, err := appRoot.OpenRoot("current")
	return current, meta, err
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
	root, _, err := f.open(id)
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
	root, _, err := f.open(app)
	if err != nil {
		return Definition{}, err
	}
	defer root.Close()
	manifest, err := readApp(root, app)
	if err != nil {
		return Definition{}, err
	}
	raw, err := root.ReadFile(filepath.Join("runs", run+".yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		raw, err = root.ReadFile(filepath.Join("runs", run+".yml"))
	}
	if err != nil {
		return Definition{}, err
	}
	definition, err := Parse(raw)
	if err != nil {
		return Definition{}, err
	}
	if manifest.Budget != nil && manifest.Budget.PerRun > 0 {
		cap := manifest.Budget.PerRun
		if definition.Budget == nil || definition.Budget.PerRun == 0 || cap < definition.Budget.PerRun {
			definition.Budget = &schema.RunBudget{PerRun: cap}
		}
	}
	return definition, nil
}

func (f Files) AgentPrompt(id string) (string, string, error) {
	root, _, err := f.open(id)
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	app, err := readApp(root, id)
	if err != nil {
		return "", "", err
	}
	if app.Agent == nil || app.Agent.File != "agent.md" {
		return "", "", errors.New("the app has no agent.md")
	}
	prompt, err := root.ReadFile("agent.md")
	if err != nil {
		return "", "", err
	}
	model := app.Agent.Model
	body := strings.ReplaceAll(string(prompt), "\r\n", "\n")
	if front, ok := strings.CutPrefix(body, "---\n"); ok {
		metadata, rest, found := strings.Cut(front, "\n---\n")
		if !found {
			return "", "", errors.New("agent.md front matter needs a closing delimiter")
		}
		var header struct {
			Model string `yaml:"model"`
		}
		if err := yaml.Unmarshal([]byte(metadata), &header); err != nil {
			return "", "", err
		}
		if model == "" {
			model = header.Model
		}
		body = rest
	}
	if body = strings.TrimSpace(body); body == "" {
		return "", "", errors.New("agent.md needs instructions")
	}
	return body, model, nil
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
		app, since, loadErr := f.triggerApp(entry.Name())
		switch {
		case errors.Is(loadErr, fs.ErrNotExist), errors.Is(loadErr, ErrNotFound):
			continue
		case loadErr != nil:
			err = errors.Join(err, fmt.Errorf("app %s: %w", entry.Name(), loadErr))
			continue
		}
		for _, trigger := range app.Triggers {
			triggers = append(triggers, Trigger{
				App: app.ID, ID: trigger.ID, On: trigger.On, Run: trigger.Run, Cron: trigger.Cron,
				Type: trigger.Type, When: trigger.When, Since: since,
			})
		}
	}
	return triggers, err
}

func (f Files) triggerApp(id string) (App, time.Time, error) {
	root, meta, err := f.open(id)
	if err != nil {
		return App{}, time.Time{}, err
	}
	defer root.Close()
	app, err := readApp(root, id)
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	return app, meta.UpdatedAt, err
}

func allowsTool(a App, service, op string, aliases ...string) (allowed, ask bool) {
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
