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
	"strings"
	"testing"
	"time"
)

// Contract 1: prerequisites refuse before any local or external side effect.
func TestDevboxMissingPrerequisitesCreateNothing(t *testing.T) {
	for _, action := range []string{"up", "down"} {
		t.Run(action, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "box")
			var out bytes.Buffer
			f := Flow{Out: &out, Getenv: func(string) string { return "" }, LookPath: func(string) (string, error) { t.Fatal("terraform looked up before token"); return "", nil }}
			err := f.Run(context.Background(), action, Options{Name: "box", Dir: dir})
			if err == nil || err.Error() != TokenNeeded {
				t.Fatalf("refusal = %v", err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("folder created: %v", err)
			}
			if out.Len() != 0 || strings.Count(TokenNeeded, "\n") != 0 {
				t.Fatal("extra output")
			}
		})
	}
}

// Contract 1: a missing executable has the same absence of side effects.
func TestDevboxMissingTerraformCreatesNothing(t *testing.T) {
	for _, action := range []string{"up", "down"} {
		dir := filepath.Join(t.TempDir(), "box")
		f := Flow{Getenv: func(string) string { return "token" }, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
		if err := f.Run(context.Background(), action, Options{Name: "box", Dir: dir}); err == nil || err.Error() != TerraformNeeded {
			t.Fatalf("refusal %v", err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("folder created")
		}
	}
}

func fakeTerraform(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "terraform")
	script := `#!/bin/sh
set -eu
action="$1"
printf '%s\n' "$*" >> calls
shift
positional=
for arg in "$@"; do
 case "$arg" in
  -*) [ -z "$positional" ] || exit 54 ;;
  *) positional=1 ;;
 esac
done
if [ "${1:-}" = -no-color ]; then shift; fi
set -- "$action" "$@"
if [ ! -f terraform.tfstate ]; then
 [ -z "${TF_VAR_pair_code:-}" ] || exit 51
 [ -z "${TF_VAR_remote_key:-}" ] || exit 52
 [ -z "${TF_VAR_remote_devices:-}" ] || exit 53
fi
if [ "$action" != output ]; then
 [ "${RAILWAY_TOKEN:-}" = fixture-token ] || exit 41
 [ -z "${RAILWAY_API_TOKEN:-}" ] || exit 42
 [ -z "${TF_LOG:-}" ] || exit 43
fi
if [ -n "${TF_VAR_pair_code:-}" ]; then
 [ -n "${TF_VAR_remote_key:-}" ] || exit 47
 [ -n "${TF_VAR_remote_devices:-}" ] || exit 48
else
 [ -z "${TF_VAR_remote_key:-}" ] || exit 49
 [ -z "${TF_VAR_remote_devices:-}" ] || exit 50
fi
if [ "${FAIL_ACTION:-}" = "$action" ]; then echo 'fixture failure' >&2; exit 44; fi
case "$action" in
 init) ;;
 plan)
  for arg in "$@"; do case "$arg" in -out=*) plan="${arg#-out=}";; esac; done
  printf 'saved plan' > "$plan"
  printf 'Create project codeaf-box\nSensitive: fixture-token %s %s %s\n' "${TF_VAR_pair_code:-}" "${TF_VAR_remote_key:-}" "${TF_VAR_remote_devices:-}"
  ;;
 show)
  printf '%s' "${GUARD_JSON:-}"
  if [ -z "${GUARD_JSON:-}" ]; then
   printf '%s' '{"format_version":"1.2","resource_changes":[{"address":"railway_project.box","change":{"actions":["no-op"]}},{"address":"railway_service.box","change":{"actions":["no-op"]}},{"address":"railway_variable_collection.box","change":{"actions":["update"],"after":{"variables":[{"name":"secret","value":"show-only-sensitive-value"}]},"after_sensitive":{"variables":true}}}]}'
  fi
  ;;
 apply)
  test -f "$3"
  if [ -f terraform.tfstate ]; then cp terraform.tfstate terraform.tfstate.backup; fi
  printf '{"resources":[{"type":"railway_project","instances":[{"attributes":{"id":"project"}}]},{"type":"railway_variable_collection","instances":[{"attributes":{"variables":[{"name":"CODEAF_DEVBOX_CODE","value":"%s"},{"name":"CODEAF_DEVBOX_REMOTE_KEY","value":"%s"},{"name":"CODEAF_DEVBOX_REMOTE_DEVICES","value":"%s"}]}}]}]}' "${TF_VAR_pair_code:-}" "${TF_VAR_remote_key:-}" "${TF_VAR_remote_devices:-}" > terraform.tfstate
  if [ ! -f terraform.tfstate.backup ]; then cp terraform.tfstate terraform.tfstate.backup; fi
  chmod 644 terraform.tfstate terraform.tfstate.backup
  ;;
 destroy) printf '{"resources":[]}' > terraform.tfstate; chmod 644 terraform.tfstate ;;
 output) printf '{"project_name":{"value":"codeaf-box"},"region":{"value":"europe-west4"},"service_id":{"value":"service"},"environment_id":{"value":"environment"}}' ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureFlow(t *testing.T, dir string, out *bytes.Buffer) Flow {
	t.Helper()
	executable := fakeTerraform(t)
	return Flow{Home: t.TempDir(), Out: out, Prepare: func(context.Context, string) (Offer, error) {
		return Offer{Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"}, nil
	}, Probe: func(context.Context, string) bool { return true }, StdoutTerminal: true, Settle: func(context.Context, string, string, string) error { return nil }, Getenv: func(key string) string {
		if key == "RAILWAY_TOKEN" {
			return "fixture-token"
		}
		return ""
	}, LookPath: func(string) (string, error) { return executable, nil }, Workspaces: func(context.Context, string) ([]Workspace, error) {
		return []Workspace{{ID: "workspace", Name: "Personal"}}, nil
	}}
}

// R5.3/R5.4: standalone files, secret isolation, guarded clear and a reachable receipt.
// R6.1: an up without a matching release keeps its install version empty.
func TestDevboxR53R54UpWaitsForJoinAndKeepsOnlyNonsecretInputs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "box")
	var out bytes.Buffer
	f := fixtureFlow(t, dir, &out)
	applied := make(chan struct{})
	join := make(chan struct{})
	f.Probe = func(context.Context, string) bool {
		select {
		case <-join:
			return true
		default:
			return false
		}
	}
	done := make(chan error, 1)
	f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
		if strings.Contains(strings.Join(args, " "), "fixture-") || strings.Contains(strings.Join(args, " "), "42-123-456") {
			t.Error("secret argument")
		}
		err := RunTerraform(ctx, path, dir, args, env, stdout, stderr)
		if args[0] == "apply" && strings.Contains(strings.Join(env, " "), "TF_VAR_pair_code=") {
			close(applied)
		}
		return err
	}
	f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
		ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
		select {
		case <-join:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	go func() {
		done <- f.Run(context.Background(), "up", Options{Name: "box", Dir: dir, Yes: true, Channel: "staging"})
	}()
	select {
	case <-applied:
	case <-time.After(5 * time.Second):
		t.Fatal("apply never ran")
	}
	select {
	case err := <-done:
		t.Fatalf("reported before join: %v", err)
	default:
	}
	close(join)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "box is up") || !strings.Contains(out.String(), "codeaf chat --at box") {
		t.Fatal(out.String())
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 10 || lines[0] != "init -input=false" || !strings.HasPrefix(lines[1], "plan -input=false -out=") || !strings.HasPrefix(lines[2], "apply -input=false ") || lines[3] != "output -json" || !strings.HasPrefix(lines[4], "plan -input=false -out=") || !strings.HasPrefix(lines[5], "show -json ") || !strings.HasPrefix(lines[6], "apply -input=false ") {
		t.Fatalf("order: %s", calls)
	}
	for _, name := range []string{"main.tf", "variables.tf", "outputs.tf", "boot.sh", "codeaf.auto.tfvars.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "fixture-") || strings.Contains(string(raw), "42-123-456") {
			t.Fatalf("secret in %s", name)
		}
	}
	vars, _ := os.ReadFile(filepath.Join(dir, "codeaf.auto.tfvars.json"))
	var choices map[string]string
	if err := json.Unmarshal(vars, &choices); err != nil {
		t.Fatal(err)
	}
	if version, present := choices["install_version"]; !present || version != "" {
		t.Fatalf("R6.1 an unpinned up must save an empty version, got %q (present %v)", version, present)
	}
	for _, want := range []string{"https://relay.example", "https://agentfield.ai/get/stageaf", `"binary": "stageaf"`, `"name": "box"`} {
		if !strings.Contains(string(vars), want) {
			t.Errorf("missing %s: %s", want, vars)
		}
	}
	if strings.Contains(out.String(), "fixture-") || strings.Contains(out.String(), "42-123-456") {
		t.Fatal("secret in output")
	}
	plans, _ := filepath.Glob(filepath.Join(dir, ".codeaf-plan-*"))
	if len(plans) != 0 {
		t.Fatal("plan retained")
	}
	f.Pair = func(context.Context, string, func(Offer)) error { t.Fatal("second offer"); return nil }
	if err := f.Run(context.Background(), "up", Options{Name: "box", Dir: dir, Yes: true}); err == nil || !strings.Contains(err.Error(), "already up") {
		t.Fatalf("second up: %v", err)
	}
}

// R6.1: the service gets the optional first-install version through the empty-value filter.
func TestDevboxR61VariableCollectionFiltersEmptyVersion(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFolder(dir, map[string]string{"name": "box"}); err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile(filepath.Join(dir, "main.tf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`{ name = "CODEAF_DEVBOX_VERSION", value = var.install_version }`, `] : variable if variable.value != ""]`} {
		if !strings.Contains(string(main), want) {
			t.Fatalf("R6.1 module is missing %s", want)
		}
	}
}

// Contract 2: existing device names and unsupported names refuse before folder writes.
func TestDevboxUpRefusesDuplicateAndInvalidNames(t *testing.T) {
	for _, name := range []string{"box", "../box", "UPPER", "bad name", "-box", "box-", strings.Repeat("a", 33)} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "new")
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			f.Devices = func(context.Context) ([]Device, error) { return []Device{{Name: "BOX"}}, nil }
			f.Pair = func(context.Context, string, func(Offer)) error { t.Fatal("offer started"); return nil }
			if err := f.Run(context.Background(), "up", Options{Name: name, Dir: dir, Yes: true}); err == nil {
				t.Fatal("accepted")
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("created folder")
			}
		})
	}
}

// R5.6: destruction precedes removal, and a failed destroy retains access.
func TestDevboxR56DownRemovesDeviceOnlyAfterSuccessfulDestroy(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			dir := t.TempDir()
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			if err := WriteFolder(dir, map[string]string{"name": "box"}); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAIL_ACTION", map[bool]string{true: "destroy", false: ""}[fail])
			removed := false
			f.Remove = func(context.Context, string, string) (bool, error) {
				calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
				if !strings.Contains(string(calls), "destroy") {
					t.Fatal("removed before destroy")
				}
				removed = true
				return true, nil
			}
			err := f.Run(context.Background(), "down", Options{Name: "box", Dir: dir, Yes: true})
			if (err != nil) != fail || removed == fail {
				t.Fatalf("err=%v removed=%v", err, removed)
			}
			if _, err := os.Stat(filepath.Join(dir, "main.tf")); err != nil {
				t.Fatal("folder removed")
			}
		})
	}
}

// R5.3: a failed probe retains observed presence and unknown fields stay absent.
func TestDevboxR53StatusUsesDevicePresenceAndOmitsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	f := fixtureFlow(t, dir, &out)
	f.Probe = nil
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfstate"), []byte(`{"resources":[{"type":"railway_project","instances":[{}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	online := true
	f.Devices = func(context.Context) ([]Device, error) {
		return []Device{{Name: "box", Online: &online, LastSeen: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}}, nil
	}
	if err := f.Run(context.Background(), "status", Options{Name: "box", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"box: up", "region europe-west4", "last seen 2026-10-30T12:00:00Z"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	out.Reset()
	f.Devices = nil
	f.RunTerraform = func(context.Context, string, string, []string, []string, io.Writer, io.Writer) error { return nil }
	// Empty output is an error, not a fabricated region or status.
	if err := f.Run(context.Background(), "status", Options{Name: "box", Dir: dir}); err == nil {
		t.Fatal("invalid outputs accepted")
	}
	f.RunTerraform = func(_ context.Context, _, _ string, _, _ []string, out, _ io.Writer) error {
		_, err := io.WriteString(out, `{}`)
		return err
	}
	if err := f.Run(context.Background(), "status", Options{Name: "box", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "box: not up\n" {
		t.Fatalf("unknown fields: %q", out.String())
	}
}

// Contract 2: rewrites preserve state and initialized providers.
func TestDevboxFolderRewriteKeepsStateAndProviderFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".terraform"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup", ".terraform/keep"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFolder(dir, map[string]string{"name": "box"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("folder is not private: %v %v", info, err)
	}
	for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup", ".terraform/keep"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(raw) != "keep" {
			t.Fatalf("lost %s", name)
		}
	}
}

// Contract 2: declining a plan or losing the offer never applies or leaves its code on disk.
func TestDevboxUpDeclineAndPairingFailureCleanPlans(t *testing.T) {
	for _, mode := range []string{"decline", "nonterminal", "plan-failure", "apply-failure", "expired", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "box")
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			f.Interactive = mode != "nonterminal"
			f.Probe = nil
			f.In = strings.NewReader("n\n")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			applied := make(chan struct{})
			f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, out, errs io.Writer) error {
				err := RunTerraform(ctx, path, dir, args, env, out, errs)
				if args[0] == "apply" && strings.Contains(strings.Join(env, " "), "TF_VAR_pair_code=") {
					close(applied)
				}
				return err
			}
			f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
				ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
				if mode == "expired" || mode == "cancelled" {
					select {
					case <-applied:
						if mode == "cancelled" {
							cancel()
							<-ctx.Done()
							return ctx.Err()
						}
						return errors.New("offer expired")
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				<-ctx.Done()
				return ctx.Err()
			}
			if mode == "plan-failure" {
				t.Setenv("FAIL_ACTION", "plan")
			}
			if mode == "apply-failure" {
				t.Setenv("FAIL_ACTION", "apply")
			}
			yes := mode != "decline" && mode != "nonterminal"
			err := f.Run(ctx, "up", Options{Name: "box", Dir: dir, Yes: yes})
			if err == nil {
				t.Fatal("reported up")
			}
			if (mode == "expired" || mode == "cancelled") && !strings.Contains(err.Error(), "codeaf devbox down box") {
				t.Fatalf("missing cleanup: %v", err)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if (mode == "decline" || mode == "nonterminal" || mode == "plan-failure") && strings.Contains(string(calls), "apply") {
				t.Fatal("applied without consent or plan")
			}
			if strings.Contains(string(calls), "destroy") || strings.Contains(out.String(), "box is up") {
				t.Fatal("destroyed automatically or reported success")
			}
			plans, _ := filepath.Glob(filepath.Join(dir, ".codeaf-plan-*"))
			if len(plans) > 0 {
				t.Fatal("plan retained")
			}
		})
	}
}

// Contract 1: token preference and fallback are passed only as the canonical child environment.
func TestDevboxTokenPreferenceAndTerraformEnvironment(t *testing.T) {
	for _, primary := range []bool{true, false} {
		t.Run(fmt.Sprint(primary), func(t *testing.T) {
			dir := t.TempDir()
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			WriteFolder(dir, map[string]string{"name": "box"})
			f.Getenv = func(key string) string {
				if key == "RAILWAY_API_TOKEN" || primary && key == "RAILWAY_TOKEN" {
					return "fixture-token"
				}
				return ""
			}
			t.Setenv("RAILWAY_API_TOKEN", "inherited-token")
			t.Setenv("RAILWAY_TOKEN", "inherited-token")
			t.Setenv("TF_VAR_pair_code", "inherited-code")
			t.Setenv("TF_VAR_remote_key", "inherited-key")
			t.Setenv("TF_VAR_remote_devices", "inherited-devices")
			t.Setenv("TF_LOG", "TRACE")
			t.Setenv("TF_CLI_ARGS", "bad-injected-argument")
			if err := f.Run(context.Background(), "down", Options{Name: "box", Dir: dir, Yes: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Contract 2: the embedded module contains one project, one service, and one persistent volume.
func TestDevboxStandaloneModuleHasOneComputerAndOptionalRegion(t *testing.T) {
	for _, region := range []string{"", "europe-west4"} {
		dir := t.TempDir()
		if err := WriteFolder(dir, map[string]string{"name": "box", "region": region}); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "main.tf"))
		hcl := string(raw)
		for _, resource := range []string{`resource "railway_project"`, `resource "railway_service"`, `volume = {`} {
			if strings.Count(hcl, resource) != 1 {
				t.Fatalf("resource %s: %s", resource, hcl)
			}
		}
		if strings.Contains(hcl, "railway_service_domain") || strings.Contains(hcl, "railway_tcp_proxy") {
			t.Fatal("inbound endpoint")
		}
		if (region != "") != strings.Contains(hcl, "region = var.region") {
			t.Fatalf("region %q: %s", region, hcl)
		}
		if _, err := os.Stat(filepath.Join(dir, ".terraform.lock.hcl")); !os.IsNotExist(err) {
			t.Fatal("lockfile shipped")
		}
	}
}

// Contract 6: all default folders are listed, with empty state rendered as not up.
func TestDevboxStatusListsEveryDefaultFolderWithoutToken(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"one", "two"} {
		os.MkdirAll(filepath.Join(home, "devbox", name), 0o700)
	}
	var out bytes.Buffer
	f := fixtureFlow(t, "", &out)
	f.Home = home
	f.Getenv = func(string) string { t.Fatal("status read token"); return "" }
	if err := f.Run(context.Background(), "status", Options{}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "one: not up\ntwo: not up\n" {
		t.Fatal(out.String())
	}
}

// Contract 2: secrets remain hidden even when process writes split through their middle.
func TestDevboxTerraformOutputRedactsSplitSecrets(t *testing.T) {
	var out bytes.Buffer
	r := newRedactor(&out, "fixture-token", "42-123-456", "fixture-remote-key", "fixture-remote-devices")
	for _, part := range []string{"before fi", "xture-t", "oken and 42-", "123-456 and fixture-remo", "te-key and fixture-remote-", "devices after"} {
		if _, err := io.WriteString(r, part); err != nil {
			t.Fatal(err)
		}
	}
	r.Flush()
	if out.String() != "before [redacted] and [redacted] and [redacted] and [redacted] after" {
		t.Fatal(out.String())
	}
}

// Contract 5: a destructive confirmation always names the folder's actual box.
func TestDevboxDownConfirmsAndRejectsADifferentFoldersName(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	f := fixtureFlow(t, dir, &out)
	if err := WriteFolder(dir, map[string]string{"name": "box"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Run(context.Background(), "down", Options{Name: "different", Dir: dir, Yes: true}); err == nil || !strings.Contains(err.Error(), "belongs to box") {
		t.Fatalf("wrong folder: %v", err)
	}
	f.Interactive = true
	f.In = strings.NewReader("n\n")
	if err := f.Run(context.Background(), "down", Options{Name: "box", Dir: dir}); err == nil {
		t.Fatal("decline accepted")
	}
	if out.String() != "Destroy box and everything in project codeaf-box? [y/N] " {
		t.Fatalf("prompt: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(err) {
		t.Fatal("Terraform ran before confirmation")
	}
}

// Round 2: released serve is reachable even without a directory watch; failures retain the existing fallback.
func TestDevboxStatusProbesBeforeFallingBackToPresence(t *testing.T) {
	for _, reachable := range []bool{true, false} {
		t.Run(fmt.Sprint(reachable), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "terraform.tfstate"), []byte(`{"resources":[{"type":"railway_project","instances":[{}]}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			offline := false
			seen := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			f.Devices = func(context.Context) ([]Device, error) {
				if reachable {
					t.Fatal("reachable box depended on directory presence")
				}
				return []Device{{Name: "box", Online: &offline, LastSeen: seen}}, nil
			}
			probed := 0
			f.Probe = func(ctx context.Context, name string) bool {
				probed++
				deadline, ok := ctx.Deadline()
				if name != "box" || !ok || time.Until(deadline) > 5*time.Second {
					t.Error("probe has no short bound")
				}
				return reachable
			}
			before := time.Now().UTC().Truncate(time.Second)
			if err := f.Run(context.Background(), "status", Options{Name: "box", Dir: dir}); err != nil {
				t.Fatal(err)
			}
			if probed != 1 {
				t.Fatalf("probes: %d", probed)
			}
			if reachable {
				if !strings.HasPrefix(out.String(), "box: up;") || strings.Contains(out.String(), "2020-") {
					t.Fatal(out.String())
				}
				stamp := strings.TrimSpace(strings.Split(out.String(), "last seen ")[1])
				now, err := time.Parse(time.RFC3339, stamp)
				if err != nil || now.Before(before) || now.After(time.Now()) {
					t.Fatalf("last seen %s: %v", stamp, err)
				}
			} else if !strings.Contains(out.String(), "box: not up;") || !strings.Contains(out.String(), seen.Format(time.RFC3339)) {
				t.Fatal(out.String())
			}
		})
	}
}
