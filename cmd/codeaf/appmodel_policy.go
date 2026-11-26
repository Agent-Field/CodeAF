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

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/provider"
)

const appModelPolicyFile = "app-model-policy.json"
const appModelPolicyMax = 64 << 10

var errAppModelRestricted = errors.New("This app has restricted or PII fields. Choose a model and endpoint approved for its data class.")

type appModelPolicy struct {
	profile string
	classes func(context.Context) ([]schema.DataClass, error)
	audit   func(string) error
}

// authorize is the Personal policy for every app model request. A run can
// contain whole previous step results and a builder can read draft files, so
// the policy refuses the full call when any class in scope lacks approval.
func (p appModelPolicy) authorize(ctx context.Context, destination provider.ModelDestination) error {
	classes, err := p.classes(ctx)
	if err == nil && len(classes) == 0 {
		return nil
	}
	if err == nil {
		routes, readErr := readAppModelPolicy(filepath.Join(p.profile, appModelPolicyFile))
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
		return errors.New("Could not record the app's model policy decision.")
	}
	return errAppModelRestricted
}

func (b *appBackend) appModelAuthorizer(app string, draft bool) func(context.Context, provider.ModelDestination) error {
	policy := appModelPolicy{
		profile: b.localProfile(),
		classes: func(ctx context.Context) ([]schema.DataClass, error) {
			if draft {
				return builder.ModelDataClasses(ctx)
			}
			s, err := b.dataStore("")
			if err != nil {
				return nil, err
			}
			return s.ModelDataClasses(app), nil
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
