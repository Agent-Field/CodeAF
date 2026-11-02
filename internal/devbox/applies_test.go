package devbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// R6.1: the non-secret install version is present and unchanged through all three phases.
// R5.4: the clear is a third guarded apply with no second confirmation.
// R3.1: no offer or secrets exist until the confirmed first apply has returned.
// R3.2: the second apply follows the settle wait and a variables-only guard.
func TestDevboxR31R32R54R61ThreeAppliesSeparateCreationPairingAndClear(t *testing.T) {
	t.Setenv("TF_VAR_pair_code", "inherited-code")
	t.Setenv("TF_VAR_remote_key", "inherited-key")
	t.Setenv("TF_VAR_remote_devices", "inherited-devices")
	dir := filepath.Join(t.TempDir(), "box")
	var out bytes.Buffer
	f := fixtureFlow(t, dir, &out)
	f.Interactive = true
	f.In = strings.NewReader("y\n")
	var offered atomic.Bool
	firstApplied, settled := false, false
	joined := make(chan struct{})
	var order []string
	const version = "dev-20261030-aaaaaaaaaaaa"
	f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
		order = append(order, args[0])
		raw, err := os.ReadFile(filepath.Join(dir, "codeaf.auto.tfvars.json"))
		if err != nil {
			t.Fatal(err)
		}
		var vars map[string]string
		if err := json.Unmarshal(raw, &vars); err != nil {
			t.Fatal(err)
		}
		if vars["install_version"] != version {
			t.Fatalf("R6.1 version during %s = %q, want %q", args[0], vars["install_version"], version)
		}
		if !firstApplied {
			if offered.Load() {
				t.Error("offer started before first apply returned")
			}
			for _, entry := range env {
				if strings.HasPrefix(entry, "TF_VAR_") {
					t.Errorf("phase A secret environment: %s", strings.SplitN(entry, "=", 2)[0])
				}
			}
			if vars["via"] != "" {
				t.Error("phase A has a relay")
			}
			for _, key := range []string{"pair_code", "remote_key", "remote_devices"} {
				if _, found := vars[key]; found {
					t.Errorf("phase A contains %s", key)
				}
			}
		} else if args[0] == "plan" && !settled {
			t.Error("second plan before settle")
		}
		err = RunTerraform(ctx, path, dir, args, env, stdout, stderr)
		if args[0] == "apply" && err == nil {
			if !firstApplied {
				firstApplied = true
			} else if slices.Contains(env, "TF_VAR_pair_code=42-123-456") {
				close(joined)
			}
		}
		return err
	}
	f.Settle = func(_ context.Context, token, service, environment string) error {
		if !firstApplied || offered.Load() || token != "fixture-token" || service != "service" || environment != "environment" {
			t.Error("wrong settle order or inputs")
		}
		order = append(order, "settle")
		settled = true
		return nil
	}
	f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
		offered.Store(true)
		if !settled {
			t.Error("offer before settle")
		}
		ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
		select {
		case <-joined:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := f.Run(context.Background(), "up", Options{Name: "box", Dir: dir, Version: version}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "init,plan,apply,output,settle,plan,show,apply,plan,show,apply" {
		t.Fatalf("order: %s", got)
	}
	if strings.Count(out.String(), "Create these on your Railway account? [y/N]") != 1 {
		t.Fatalf("confirmation: %s", &out)
	}
	if strings.Contains(out.String(), "show-only-sensitive-value") {
		t.Fatal("plan JSON streamed")
	}
	if _, err := os.Stat(filepath.Join(dir, "terraform.tfstate.backup")); !os.IsNotExist(err) {
		t.Fatal("R5.4 backup remains")
	}
	for _, name := range []string{"terraform.tfstate"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("state is not private: %s %v", name, err)
		}
	}
}

// R3.2: only the variable collection may change; unsafe or unreadable plans never apply or leak JSON.
func TestDevboxR32GuardRefusesOtherResourcesAndDiscardsSensitiveJSON(t *testing.T) {
	for _, mode := range []string{"service", "project", "replacement", "read", "malformed", "show-failed"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "box")
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			address, actions := "railway_service.box", `["update"]`
			if mode == "project" {
				address = "railway_project.box"
			}
			if mode == "replacement" {
				actions = `["delete","create"]`
			}
			if mode == "read" {
				actions = `["read"]`
			}
			raw := `{"resource_changes":[{"address":"railway_variable_collection.box","change":{"actions":["update"],"after":{"secret":"show-only-sensitive-value"}}},{"address":"` + address + `","change":{"actions":` + actions + `,"after":{"secret":"show-only-sensitive-value"}}}]}`
			if mode == "malformed" {
				raw = `{"resource_changes":["show-only-sensitive-value`
			}
			t.Setenv("GUARD_JSON", raw)
			if mode == "show-failed" {
				t.Setenv("FAIL_ACTION", "show")
			}
			f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
				ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
				<-ctx.Done()
				return ctx.Err()
			}
			err := f.Run(context.Background(), "up", Options{Name: "box", Dir: dir, Yes: true})
			if err == nil {
				t.Fatal("guard accepted unsafe plan")
			}
			all := out.String() + err.Error()
			for _, secret := range []string{"fixture-token", "42-123-456", "fixture-remote-key", "fixture-remote-devices", "show-only-sensitive-value"} {
				if strings.Contains(all, secret) {
					t.Errorf("leaked %s", secret)
				}
			}
			if mode != "malformed" && mode != "show-failed" && !strings.Contains(err.Error(), address) {
				t.Fatalf("missing refused address: %v", err)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Count(string(calls), "apply -input=false") != 1 {
				t.Fatalf("applied guarded plan: %s", calls)
			}
			plans, _ := filepath.Glob(filepath.Join(dir, ".codeaf-plan-*"))
			if len(plans) != 0 {
				t.Fatal("sensitive plan retained")
			}
		})
	}
}

// R5.3: remote credentials never change across retries; only the pairing code is fresh.
// R3.3: interrupted or expired joins get at most three fresh codes; other errors end immediately.
// R3.4: reaching the settle bound still proceeds with a single notice.
func TestDevboxR33R34R53FreshCodesReuseRemoteCredentials(t *testing.T) {
	for _, mode := range []string{"joined-on-second", "three-interruptions", "expired", "refused", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "box")
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			f.Settle = func(context.Context, string, string, string) error { return errors.New("wait ended") }
			var attempts atomic.Int32
			prepared := 0
			f.Prepare = func(context.Context, string) (Offer, error) {
				prepared++
				if attempts.Load() != 0 {
					t.Error("R5.3 prepared remote files after the first code")
				}
				return Offer{Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"}, nil
			}
			var joined atomic.Bool
			f.Probe = func(context.Context, string) bool { return joined.Load() }
			applied := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
				if args[0] == "plan" && attempts.Load() > 0 && !joined.Load() {
					for key, want := range map[string]string{"TF_VAR_pair_code": fmt.Sprintf("fixture-TF_VAR_pair_code-%d", attempts.Load()), "TF_VAR_remote_key": "fixture-remote-key", "TF_VAR_remote_devices": "fixture-remote-devices"} {
						if !slices.Contains(env, key+"="+want) {
							t.Errorf("R5.3 incorrect %s across retries", key)
						}
					}
				}
				err := RunTerraform(ctx, path, dir, args, env, stdout, stderr)
				if args[0] == "apply" && attempts.Load() > 0 && !joined.Load() {
					applied <- struct{}{}
				}
				return err
			}
			f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
				n := attempts.Add(1)
				ready(Offer{Code: fmt.Sprintf("fixture-TF_VAR_pair_code-%d", n), Via: "https://relay.example", RemoteKey: "", RemoteDevices: ""})
				select {
				case <-applied:
				case <-ctx.Done():
					return ctx.Err()
				}
				if mode == "joined-on-second" && n == 2 {
					joined.Store(true)
					return nil
				}
				if mode == "refused" {
					return errors.New("refused on the other device")
				}
				if mode == "cancelled" {
					cancel()
					return context.Canceled
				}
				if mode == "expired" {
					return context.DeadlineExceeded
				}
				return ErrJoinInterrupted
			}
			err := f.Run(ctx, "up", Options{Name: "box", Dir: dir, Yes: true})
			wantAttempts := int32(3)
			if mode == "joined-on-second" {
				wantAttempts = 2
			} else if mode == "refused" || mode == "cancelled" {
				wantAttempts = 1
			}
			if prepared != 1 {
				t.Fatalf("R5.3 prepared remote files %d times", prepared)
			}
			if attempts.Load() != wantAttempts {
				t.Fatalf("attempts %d, want %d (error %v)", attempts.Load(), wantAttempts, err)
			}
			if (err == nil) != (mode == "joined-on-second") {
				t.Fatalf("result: %v", err)
			}
			if err != nil {
				reason := "joining was interrupted or the pairing code expired"
				if mode == "refused" {
					reason = "refused on the other device"
				}
				if mode == "cancelled" {
					reason = "context canceled"
				}
				want := "box did not join: " + reason + ". It is on your Railway account; remove it with `codeaf devbox down box --dir " + shellQuote(dir) + "`, then run up again."
				if err.Error() != want {
					t.Fatalf("failure: %v", err)
				}
			}
			if strings.Count(out.String(), "Railway did not settle") != 1 {
				t.Fatalf("settle notice: %s", &out)
			}
			for n := int32(2); n <= wantAttempts; n++ {
				if !strings.Contains(out.String(), fmt.Sprintf("box did not finish joining; sending a new code (%d of 3).", n)) {
					t.Fatalf("retry receipt: %s", &out)
				}
			}
			if mode != "joined-on-second" && strings.Contains(out.String(), "box is up") {
				t.Fatal("claimed join")
			}
			if strings.Contains(out.String(), "fixture-TF_VAR") {
				t.Fatal("fresh secret leaked")
			}
			plans, _ := filepath.Glob(filepath.Join(dir, ".codeaf-plan-*"))
			if len(plans) != 0 {
				t.Fatal("sensitive plan retained")
			}
		})
	}
}

// R5.4/R3.6: all three plans filter piped output and keep correct flags.
func TestDevboxR36R54TerraformOutputAndFlags(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			dir := t.TempDir()
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			f.StdoutTerminal = terminal
			applied := make(chan struct{})
			f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
				ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
				select {
				case <-applied:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			var commands []string
			f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
				commands = append(commands, args[0])
				if slices.Contains(args, "-no-color") == terminal || !terminal && (len(args) < 2 || args[1] != "-no-color") {
					t.Errorf("terminal %v: %v", terminal, args)
				}
				if args[0] == "plan" {
					for _, chunk := range []string{"\x1b[32mPlan: 3 to add.\x1b[0m\nTo perform exactly these actions, run the fol", "lowing command to apply:\n    terraform apply \"", "private-plan-name\"\n", "Next real line\n"} {
						_, _ = io.WriteString(stdout, chunk)
					}
				}
				err := RunTerraform(ctx, path, dir, args, env, stdout, stderr)
				if args[0] == "apply" && slices.Contains(env, "TF_VAR_pair_code=42-123-456") {
					close(applied)
				}
				return err
			}
			if err := f.Run(context.Background(), "up", Options{Name: "box", Dir: dir, Yes: true}); err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"To perform exactly", "terraform apply", "private-plan-name"} {
				if strings.Contains(out.String(), forbidden) {
					t.Errorf("unfiltered %q: %s", forbidden, &out)
				}
			}
			if !terminal && strings.Contains(out.String(), "\x1b") {
				t.Fatal("piped ANSI output")
			}
			if strings.Count(out.String(), "Next real line") != 3 {
				t.Fatal("filter lost real output")
			}
			f.Out = &out
			if err := f.Run(context.Background(), "status", Options{Name: "box", Dir: dir}); err != nil {
				t.Fatal(err)
			}
			if err := f.Run(context.Background(), "down", Options{Name: "box", Dir: dir, Yes: true}); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(commands, "output") || !slices.Contains(commands, "destroy") {
				t.Fatal("missed command flags")
			}
		})
	}
}

// R3.2: a failed Terraform write still makes both state files owner-only.
func TestDevboxR32FailedTerraformWriteProtectsState(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	f := fixtureFlow(t, dir, &out)
	if err := WriteFolder(dir, map[string]string{"name": "box"}); err != nil {
		t.Fatal(err)
	}
	f.RunTerraform = func(_ context.Context, _, dir string, _, _ []string, _, _ io.Writer) error {
		for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("sensitive state"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return errors.New("write failed")
	}
	if err := f.Run(context.Background(), "down", Options{Name: "box", Dir: dir, Yes: true}); err == nil {
		t.Fatal("failure hidden")
	}
	for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("state permissions: %s %v", name, err)
		}
	}
}
