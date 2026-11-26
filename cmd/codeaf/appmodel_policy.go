package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/provider"
)

const appModelPolicyFile = "app-model-policy.json"
const appModelPolicyMax = 64 << 10

var errAppModelRestricted = errors.New("This app can use restricted or PII data. Choose an approved model in codeaf, or ask the data owner to approve a destination.")

type appModelPolicy struct {
	personal func(provider.ModelDestination) bool
	profile  string
	classes  func(context.Context) ([]schema.DataClass, error)
	audit    func(string) error
}

// A run can contain previous step results from any type in scope. Check the
// actual destination before each send, including after routing changes.
func (p appModelPolicy) authorize(ctx context.Context, destination provider.ModelDestination) error {
	classes, err := p.classes(ctx)
	if err == nil && len(classes) == 0 {
		return nil
	}
	if err == nil && destination.Model != "" && destination.Endpoint != "" {
		routes, readErr := readAppModelPolicy(filepath.Join(p.profile, appModelPolicyFile))
		if errors.Is(readErr, os.ErrNotExist) && p.personal != nil && p.personal(destination) {
			return nil
		}
		if readErr == nil {
			for _, class := range classes {
				if !slices.Contains(routes[class], destination) {
					err = errAppModelRestricted
					break
				}
			}
			if err == nil {
				return nil
			}
		}
	}
	if err := p.audit("model_refused"); err != nil {
		return errors.New("Could not record the app's model policy decision. Check the profile's data directory and try again.")
	}
	return errAppModelRestricted
}

func (b *appBackend) appModelAuthorizer(app string) func(context.Context, provider.ModelDestination) error {
	policy := appModelPolicy{
		profile: b.localProfile(),
		classes: func(context.Context) ([]schema.DataClass, error) {
			s, err := b.dataStore("")
			if err != nil {
				return nil, err
			}
			classes := s.ModelDataClasses(app)
			sources, err := b.appConnections().List(app)
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(sources, func(source connections.Summary) bool { return source.Kind == connections.Google }) &&
				!slices.Contains(classes, schema.ClassPII) {
				classes = append(classes, schema.ClassPII)
			}
			return classes, nil
		},
		personal: func(destination provider.ModelDestination) bool {
			// A connected app can carry source data in its prior step results.
			// Keep approvals explicit whenever that provenance is possible.
			sources, err := b.appConnections().List(app)
			return err == nil && !slices.ContainsFunc(sources, func(source connections.Summary) bool {
				return source.Kind != connections.Google
			}) && personalProfileModel(b.localProfile(), destination)
		},
		audit: func(decision string) error {
			s, err := b.dataStore("")
			if err != nil {
				return err
			}
			return s.AuditPolicy(app, "", decision)
		},
	}
	return policy.authorize
}

// A choice comes from the profile or process configuration. App manifests,
// router picks, and outside harnesses cannot supply it.
func personalProfileModel(profile string, destination provider.ModelDestination) bool {
	settings, err := config.LoadKeyless()
	if err != nil || factoryProfile(settings.ProfileDir) != profile {
		return false
	}
	model := config.ChatModelAt(settings.ProfileDir)
	if model == "" {
		model = strings.TrimSpace(env.Get(config.ModelEnv))
	}
	matches := func(model, source string) bool {
		if model == "" {
			return false
		}
		client := settings.ClientConfig(model)
		return client.HTTPClient == nil && (source == "" || strings.EqualFold(client.SourceID, source)) &&
			client.CompletionDestination() == destination
	}
	if matches(model, "") {
		return true
	}
	for _, tier := range config.ModelTiers {
		seat := config.TierSeatAt(settings.ProfileDir, tier)
		if seat.Source == config.SeatPinned && matches(seat.Model, seat.Provider) {
			return true
		}
	}
	return false
}

func readAppModelPolicy(path string) (map[schema.DataClass][]provider.ModelDestination, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeAppModelPolicy(f)
}

func decodeAppModelPolicy(in io.Reader) (map[schema.DataClass][]provider.ModelDestination, error) {
	raw, err := io.ReadAll(io.LimitReader(in, appModelPolicyMax+1))
	if err != nil || len(raw) > appModelPolicyMax {
		return nil, errors.New("model approvals could not be read within the size limit")
	}
	var routes map[schema.DataClass][]provider.ModelDestination
	if err := decodeAppConnectionJSON(bytes.NewReader(raw), &routes); err != nil || routes == nil {
		return nil, errors.New("model approvals must be a JSON object keyed by data class")
	}
	for class, destinations := range routes {
		if !class.Restricted() {
			return nil, errors.New("the model approval class must be restricted or pii")
		}
		for _, d := range destinations {
			endpoint, err := url.Parse(d.Endpoint)
			if err != nil || d.Model == "" || endpoint.Hostname() == "" || endpoint.Path == "" ||
				endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
				(endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "::1" || endpoint.Hostname() == "localhost"))) {
				return nil, errors.New("each model approval needs an exact model and completion endpoint")
			}
		}
	}
	return routes, nil
}

func writeAppModelPolicy(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".app-model-policy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func appModelPolicyCommand(profile string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: codeaf app model-policy FILE.json")
	}
	routes, err := readAppModelPolicy(args[0])
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	return writeAppModelPolicy(filepath.Join(profile, appModelPolicyFile), raw)
}
