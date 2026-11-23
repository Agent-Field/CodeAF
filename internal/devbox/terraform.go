package devbox

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultImage = "buildpack-deps:bookworm-scm"

//go:embed terraform/railway/*.tf terraform/boot.sh
var module embed.FS

func Channel(channel string) (installURL, binary string, err error) {
	switch channel {
	case "stable":
		binary = "codeaf"
	case "staging":
		binary = "stageaf"
	case "dev":
		binary = "devaf"
	default:
		return "", "", fmt.Errorf("channel must be stable, staging or dev")
	}
	return "https://agentfield.ai/get/" + binary, binary, nil
}

// WriteFolder replaces only the files shipped by codeaf, preserving local state and provider downloads.
func WriteFolder(dir string, variables map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"main.tf", "variables.tf", "outputs.tf", "boot.sh"} {
		path := "terraform/railway/" + name
		if name == "boot.sh" {
			path = "terraform/boot.sh"
		}
		raw, err := module.ReadFile(path)
		if err != nil {
			return err
		}
		if name == "main.tf" && variables["region"] != "" {
			raw = []byte(strings.Replace(string(raw), "regions = null", "regions = [{ region = var.region, num_replicas = 1 }]", 1))
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(variables, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "codeaf.auto.tfvars.json"), append(raw, '\n'), 0o600)
}

func RunTerraform(ctx context.Context, path, dir string, args, extraEnv []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Disable Terraform debug logs and inherited argument injection: plans carry the remote key and single-use code.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "RAILWAY_TOKEN" || key == "RAILWAY_API_TOKEN" || key == "TF_VAR_pair_code" || key == "TF_VAR_remote_key" || key == "TF_VAR_remote_devices" || strings.HasPrefix(key, "TF_LOG") || strings.HasPrefix(key, "TF_CLI_ARGS") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd.Run()
}

// redactor withholds enough trailing bytes to cover secrets split across writes.
type redactor struct {
	out     io.Writer
	secrets []string
	pending string
	keep    int
}

func newRedactor(out io.Writer, secrets ...string) *redactor {
	r := &redactor{out: out}
	for _, secret := range secrets {
		if secret != "" {
			r.secrets = append(r.secrets, secret)
			r.keep = max(r.keep, len(secret)-1)
		}
	}
	return r
}
func redact(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}
func (r *redactor) Write(p []byte) (int, error) {
	r.pending += string(p)
	n := len(r.pending) - r.keep
	if n > 0 {
		// Move the cut before any secret spanning it.
		for _, s := range r.secrets {
			start := strings.LastIndex(r.pending[:min(len(r.pending), n+len(s)-1)], s)
			if start >= 0 && start < n && start+len(s) > n {
				n = start
			}
		}
		if n > 0 {
			if _, err := io.WriteString(r.out, redact(r.pending[:n], r.secrets...)); err != nil {
				return 0, err
			}
			r.pending = r.pending[n:]
		}
	}
	return len(p), nil
}
func (r *redactor) Flush() {
	_, _ = io.WriteString(r.out, redact(r.pending, r.secrets...))
	r.pending = ""
}

// Every command goes through the same output flags and state permissions, including failed writes.
func (f Flow) runTerraform(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
	if !f.StdoutTerminal {
		args = append([]string{args[0], "-no-color"}, args[1:]...)
	}
	runErr := f.RunTerraform(ctx, path, dir, args, env, stdout, stderr)
	var stateErr error
	switch args[0] {
	case "init", "plan", "apply", "destroy":
		for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup"} {
			if err := os.Chmod(filepath.Join(dir, name), 0o600); err != nil && !os.IsNotExist(err) {
				stateErr = errors.Join(stateErr, fmt.Errorf("keep Terraform state private: %w", err))
			}
		}
	}
	return errors.Join(runErr, stateErr)
}

func (f Flow) applyPlan(ctx context.Context, terraform, dir string, env, secrets []string, guarded bool, beforeApply func() error) error {
	plan, err := os.CreateTemp(dir, ".codeaf-plan-*")
	if err != nil {
		return err
	}
	defer os.Remove(plan.Name())
	if err := plan.Close(); err != nil {
		return err
	}
	name := filepath.Base(plan.Name())
	if err := f.terraform(ctx, terraform, dir, []string{"plan", "-input=false", "-out=" + name}, env, secrets...); err != nil {
		return err
	}
	if guarded {
		if err := f.guardPlan(ctx, terraform, dir, name, env); err != nil {
			return err
		}
	}
	if err := beforeApply(); err != nil {
		return err
	}
	return f.terraform(ctx, terraform, dir, []string{"apply", "-input=false", name}, env, secrets...)
}

func (f Flow) guardPlan(ctx context.Context, terraform, dir, plan string, env []string) error {
	// Show exposes sensitive values in plain text. Neither stream nor its errors may leave memory.
	var raw bytes.Buffer
	if err := f.runTerraform(ctx, terraform, dir, []string{"show", "-json", plan}, env, &raw, io.Discard); err != nil {
		return errors.New("could not inspect the pairing plan")
	}
	var planJSON struct {
		Resources []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(raw.Bytes(), &planJSON); err != nil {
		return errors.New("could not read the pairing plan")
	}
	var refused []string
	for _, resource := range planJSON.Resources {
		if len(resource.Change.Actions) == 1 && resource.Change.Actions[0] == "no-op" {
			continue
		}
		if resource.Address != "railway_variable_collection.box" {
			refused = append(refused, strconv.Quote(resource.Address))
		}
	}
	if len(refused) != 0 {
		return fmt.Errorf("refusing a pairing plan that changes %s", strings.Join(refused, ", "))
	}
	return nil
}

func (f Flow) outputs(ctx context.Context, terraform, dir string, env []string, secrets ...string) (map[string]string, error) {
	var raw bytes.Buffer
	errs := newRedactor(f.Err, secrets...)
	err := f.runTerraform(ctx, terraform, dir, []string{"output", "-json"}, env, &raw, errs)
	errs.Flush()
	if err != nil {
		return nil, fmt.Errorf("terraform output failed: %s", redact(err.Error(), secrets...))
	}
	var values map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw.Bytes(), &values); err != nil {
		return nil, errors.New("could not read Terraform outputs")
	}
	result := make(map[string]string, len(values))
	for key, output := range values {
		var value string
		_ = json.Unmarshal(output.Value, &value)
		result[key] = value
	}
	return result, nil
}
