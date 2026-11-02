// Package devbox owns the readable Terraform folders for a person's computers.
package devbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/guard"
)

const TokenNeeded = "codeaf devbox needs a Railway token: create one at https://railway.com/account/tokens and set RAILWAY_TOKEN."
const TerraformNeeded = "codeaf devbox needs Terraform: install it from https://developer.hashicorp.com/terraform/install."
const FreshFor = 5 * time.Minute
const probeWithin = 3 * time.Second
const maxPairingCodes = 3
const reachableEvery = 5 * time.Second
const reachableWithin = 3 * time.Minute

var errNotReachable = errors.New("the pinned remote key did not answer within three minutes")

// An acknowledged offer needs a distinct recovery receipt when reachability fails.
type reachabilityError struct{ error }

// ErrJoinInterrupted allows a fresh offer after a deployment swap or an expired code.
var ErrJoinInterrupted = errors.New("joining was interrupted or the pairing code expired")

// Options describes one command, including the folder the person owns.
type Options struct {
	Name, Dir, Region, Workspace, Channel, Version string
	Yes                                            bool
}

type Device struct {
	ID       string
	Name     string
	LastSeen time.Time
	Removed  bool
	Online   *bool
}

// Offer carries configuration secrets only in memory until Terraform consumes them.
type Offer struct {
	Code, Via, RemoteKey, RemoteDevices string
}

// Runner executes Terraform without putting credentials in its arguments.
type Runner func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error

// Flow connects provisioning to the existing identity and pairing doors.
type Flow struct {
	Home            string
	In              io.Reader
	Out, Err        io.Writer
	Interactive     bool
	StdoutTerminal  bool
	GraphQLEndpoint string
	Settle          func(ctx context.Context, token, serviceID, environmentID string) error
	Getenv          func(string) string
	LookPath        func(string) (string, error)
	RunTerraform    Runner
	Workspaces      func(context.Context, string) ([]Workspace, error)
	// Prepare creates and pins the remote configuration once, before any identity code.
	Prepare func(context.Context, string) (Offer, error)
	// Pair blocks until the identity offer ends. Ready supplies only a fresh code and relay.
	Pair func(ctx context.Context, name string, ready func(Offer)) error
	// Probe verifies a remembered remote key; failure falls back to directory presence.
	Probe   func(context.Context, string) bool
	Devices func(context.Context) ([]Device, error)
	Remove  func(context.Context, string, string) (bool, error)
	// ShareState sends this computer's saved provider keys and settings to the
	// identity once. Nil sends nothing.
	ShareState              func(context.Context) error
	Unavailable             func() string
	probeEvery, readyWithin time.Duration
}

func (f Flow) Run(ctx context.Context, action string, o Options) error {
	if f.Out == nil {
		f.Out = io.Discard
	}
	if f.Err == nil {
		f.Err = f.Out
	}
	if f.In == nil {
		f.In = strings.NewReader("")
	}
	if f.Getenv == nil {
		f.Getenv = os.Getenv
	}
	if f.LookPath == nil {
		f.LookPath = exec.LookPath
	}
	if f.RunTerraform == nil {
		f.RunTerraform = RunTerraform
	}
	if f.Workspaces == nil {
		f.Workspaces = LookupWorkspaces
	}
	if f.Settle == nil {
		f.Settle = func(ctx context.Context, token, serviceID, environmentID string) error {
			return settleDeployments(ctx, f.GraphQLEndpoint, token, serviceID, environmentID)
		}
	}
	var token string
	if action == "up" || action == "down" {
		token = strings.TrimSpace(f.Getenv("RAILWAY_TOKEN"))
		if token == "" {
			token = strings.TrimSpace(f.Getenv("RAILWAY_API_TOKEN"))
		}
		if token == "" {
			return errors.New(TokenNeeded)
		}
	}
	terraform, err := f.LookPath("terraform")
	if err != nil {
		return errors.New(TerraformNeeded)
	}
	if o.Name != "" {
		if err := ValidateName(o.Name); err != nil {
			return err
		}
	}
	if o.Dir == "" {
		o.Dir = filepath.Join(f.Home, "devbox", o.Name)
	}
	switch action {
	case "up":
		return f.up(ctx, terraform, token, o)
	case "down":
		return f.down(ctx, terraform, token, o)
	case "status":
		return f.status(ctx, terraform, o)
	default:
		return errors.New("usage: codeaf devbox up <name> | status [<name>] | down <name>")
	}
}

var projectName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

func ValidateName(name string) error {
	clean, err := devname.Clean(name)
	if err != nil {
		return err
	}
	if clean != name || !projectName.MatchString(name) {
		return errors.New("a dev box name uses lowercase letters, digits and hyphens, starting and ending with a letter or digit")
	}
	return nil
}

func (f Flow) devices(ctx context.Context) ([]Device, error) {
	if f.Devices == nil {
		return nil, nil
	}
	return f.Devices(ctx)
}

func (f Flow) up(ctx context.Context, terraform, token string, o Options) error {
	if o.Name == "" {
		return errors.New("usage: codeaf devbox up <name>")
	}
	if o.Channel == "" {
		o.Channel = "dev"
	}
	installURL, binary, err := Channel(o.Channel)
	if err != nil {
		return err
	}
	exists, err := hasProject(o.Dir)
	if err != nil {
		return err
	}
	if exists {
		if f.probe(ctx, o.Name) {
			secrets, err := bootstrapSecrets(o.Dir)
			if err != nil {
				return f.clearFailed(o, err, token)
			}
			if len(secrets) > 0 {
				id, err := readDeviceID(o.Dir)
				if err != nil {
					return f.clearFailed(o, err, append(secrets, token)...)
				}
				if id == "" {
					if err := f.recordDevice(ctx, o); err != nil {
						return f.clearFailed(o, err, append(secrets, token)...)
					}
				}
				if err := f.clearBootstrap(ctx, terraform, token, o, secrets); err != nil {
					return err
				}
				f.sayUp(o)
				return nil
			}
		}
		return fmt.Errorf("%s is already up — `%s`", o.Name, f.command("status", o))
	}
	if f.Unavailable != nil {
		if reason := f.Unavailable(); reason != "" {
			return errors.New(reason)
		}
	}
	devices, err := f.devices(ctx)
	if err != nil {
		return err
	}
	for _, d := range devices {
		if !d.Removed && strings.EqualFold(d.Name, o.Name) {
			return fmt.Errorf("a device is already called %s — choose another dev box name", o.Name)
		}
	}
	workspace, err := resolveWorkspace(ctx, token, o.Workspace, f.Workspaces)
	if err != nil {
		return err
	}
	if f.Pair == nil || f.Prepare == nil {
		return errors.New("pairing is unavailable here")
	}
	// Create the infrastructure before offering a code, so deployment swaps cannot spend it.
	vars := map[string]string{"name": o.Name, "workspace_id": workspace, "region": o.Region, "image": DefaultImage, "install_url": installURL, "binary": binary, "install_version": o.Version, "via": ""}
	if err := WriteFolder(o.Dir, vars); err != nil {
		return err
	}
	env := []string{"RAILWAY_TOKEN=" + token}
	if err := f.terraform(ctx, terraform, o.Dir, []string{"init", "-input=false"}, env, token, ""); err != nil {
		return err
	}
	if err := f.applyPlan(ctx, terraform, o.Dir, env, []string{token}, false, func() error {
		return f.confirm(ctx, o.Yes, "Create these on your Railway account? [y/N]")
	}); err != nil {
		return err
	}
	outputs, err := f.outputs(ctx, terraform, o.Dir, env, token)
	if err != nil {
		return f.pairingFailed(o, err)
	}
	if outputs["service_id"] == "" || outputs["environment_id"] == "" {
		return f.pairingFailed(o, errors.New("Terraform did not return the service and environment ids"))
	}
	fmt.Fprintln(f.Out, "Waiting for Railway to finish starting the box.")
	if err := f.Settle(ctx, token, outputs["service_id"], outputs["environment_id"]); err != nil {
		if ctx.Err() != nil {
			return f.pairingFailed(o, ctx.Err())
		}
		fmt.Fprintln(f.Out, "Railway did not settle before the wait ended; sending the pairing code anyway.")
	}
	remote, err := f.Prepare(ctx, o.Name)
	if err != nil {
		return f.pairingFailed(o, err)
	}
	secrets := []string{token, remote.RemoteKey, remote.RemoteDevices}
	for attempt := 1; attempt <= maxPairingCodes; attempt++ {
		if err := ctx.Err(); err != nil {
			return f.pairingFailed(o, err)
		}
		err := f.sendPairingCode(ctx, terraform, token, o, vars, remote, &secrets)
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			err = ErrJoinInterrupted
		}
		if err == nil || errors.Is(err, ErrJoinInterrupted) && f.probe(ctx, o.Name) {
			f.shareState(ctx, o.Name)
			if err := f.recordDevice(ctx, o); err != nil {
				return f.clearFailed(o, err, secrets...)
			}
			if err := f.clearBootstrap(ctx, terraform, token, o, secrets); err != nil {
				return err
			}
			f.sayUp(o)
			return nil
		}
		var unreachable reachabilityError
		if errors.As(err, &unreachable) {
			return fmt.Errorf("%s took its pairing code but is not reachable yet: %s. It is on your Railway account; check it with %s, or remove it with %s.", o.Name, reason(err), f.command("status", o), f.command("down", o))
		}
		if ctx.Err() != nil {
			return f.pairingFailed(o, ctx.Err())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = ErrJoinInterrupted
		}
		if !errors.Is(err, ErrJoinInterrupted) || attempt == maxPairingCodes {
			return f.pairingFailed(o, errors.New(redact(err.Error(), secrets...)))
		}
		fmt.Fprintf(f.Out, "%s did not finish joining; sending a new code (%d of %d).\n", o.Name, attempt+1, maxPairingCodes)
	}
	return f.pairingFailed(o, ErrJoinInterrupted)
}

// shareState runs once the box answers. A laptop that was a fleet of one until
// this up offered its identity has never sent its saved keys anywhere, so the
// box would find none and the first `--at` chat would stop on a missing key. A
// failure is said once and is not fatal: codeaf sends them the next time it
// opens here.
func (f Flow) shareState(ctx context.Context, name string) {
	if f.ShareState == nil {
		return
	}
	if err := f.ShareState(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(f.Out, "Could not send this computer's saved keys yet: %s. Open codeaf here once, or a chat on %s can stop on a missing key.\n", reason(err), name)
	}
}

func (f Flow) sayUp(o Options) {
	fmt.Fprintf(f.Out, "%s is up. Open a chat on it: codeaf chat --at %s\nThe Terraform is in %s.\n", o.Name, o.Name, o.Dir)
}

func (f Flow) probe(ctx context.Context, name string) bool {
	if f.Probe == nil || ctx.Err() != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, probeWithin)
	defer cancel()
	return f.Probe(ctx, name)
}

func (f Flow) sendPairingCode(ctx context.Context, terraform, token string, o Options, vars map[string]string, remote Offer, secrets *[]string) error {
	offerCtx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	var pairErr error
	ready := make(chan Offer, 1)
	guard.Go("devbox pairing", func() {
		defer close(finished)
		pairErr = f.Pair(offerCtx, o.Name, func(offer Offer) {
			select {
			case ready <- offer:
			case <-offerCtx.Done():
			}
		})
	})
	defer func() { cancel(); <-finished }()
	var offer Offer
	select {
	case offer = <-ready:
	case <-finished:
		select {
		case offer = <-ready:
		default:
			if pairErr != nil {
				return pairErr
			}
			return errors.New("no device joined")
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if offer.Via != remote.Via {
		return errors.New("the pairing relay changed while setting up the box")
	}
	vars["via"] = remote.Via
	if err := WriteFolder(o.Dir, vars); err != nil {
		return err
	}
	*secrets = append(*secrets, offer.Code)
	env := []string{"RAILWAY_TOKEN=" + token, "TF_VAR_pair_code=" + offer.Code, "TF_VAR_remote_key=" + remote.RemoteKey, "TF_VAR_remote_devices=" + remote.RemoteDevices}
	if err := f.applyPlan(ctx, terraform, o.Dir, env, *secrets, true, func() error {
		select {
		case <-finished:
			if pairErr != nil {
				return pairErr
			}
		default:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(f.Out, "Sending %s its pairing code.\n", o.Name)
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintln(f.Out, "Waiting for the box to install codeaf, join and become reachable; this can take a few minutes.")
	return f.awaitReachable(ctx, o.Name, finished, func() error { return pairErr })
}

// Probe independently of the acknowledgement so a lost reply cannot strand a joined box.
func (f Flow) awaitReachable(ctx context.Context, name string, finished <-chan struct{}, result func() error) error {
	every, within := f.probeEvery, f.readyWithin
	if every <= 0 {
		every = reachableEvery
	}
	if within <= 0 {
		within = reachableWithin
	}
	probeCtx, cancel := context.WithCancel(ctx)
	done, reachable := make(chan struct{}), make(chan struct{})
	guard.Go("devbox reachability", func() {
		defer close(done)
		ticks := time.NewTicker(every)
		defer ticks.Stop()
		for probeCtx.Err() == nil {
			if f.probe(probeCtx, name) {
				close(reachable)
				return
			}
			select {
			case <-probeCtx.Done():
				return
			case <-ticks.C:
			}
		}
	})
	defer func() { cancel(); <-done }()
	var timeout <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-reachable:
			return nil
		case <-finished:
			err := result()
			if err != nil {
				if errors.Is(err, ErrJoinInterrupted) || errors.Is(err, context.DeadlineExceeded) {
					return ErrJoinInterrupted
				}
				return err
			}
			finished = nil
			timer = time.NewTimer(within)
			timeout = timer.C
		case <-timeout:
			return reachabilityError{errNotReachable}
		case <-ctx.Done():
			if finished == nil {
				return reachabilityError{ctx.Err()}
			}
			return ctx.Err()
		}
	}
}

func reason(err error) string {
	if err == nil {
		return "no device joined"
	}
	return strings.TrimRight(strings.Join(strings.Fields(err.Error()), " "), ".")
}

func (f Flow) pairingFailed(o Options, err error) error {
	return fmt.Errorf("%s did not join: %s. It is on your Railway account; remove it with `%s`, then run up again.", o.Name, reason(err), f.command("down", o))
}

// A recovery command must keep pointing to the folder that owns the paid resources.
func (f Flow) command(action string, o Options) string {
	command := "codeaf devbox " + action + " " + o.Name
	actual, _ := filepath.Abs(o.Dir)
	defaultDir, _ := filepath.Abs(filepath.Join(f.Home, "devbox", o.Name))
	if actual != defaultDir {
		command += " --dir " + shellQuote(o.Dir)
	}
	return command
}

var shellBare = regexp.MustCompile(`^[a-zA-Z0-9_./:@%+=,-]+$`)

func shellQuote(s string) string {
	if shellBare.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (f Flow) down(ctx context.Context, terraform, token string, o Options) error {
	if o.Name == "" {
		return errors.New("usage: codeaf devbox down <name>")
	}
	if _, err := os.Stat(filepath.Join(o.Dir, "main.tf")); err != nil {
		return fmt.Errorf("read the Terraform folder: %w", err)
	}
	if raw, err := os.ReadFile(filepath.Join(o.Dir, "codeaf.auto.tfvars.json")); err == nil {
		var vars struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &vars); err != nil {
			return fmt.Errorf("read the Terraform variables: %w", err)
		}
		if vars.Name != "" && vars.Name != o.Name {
			return fmt.Errorf("this Terraform folder belongs to %s; use `codeaf devbox down %s --dir %s`", vars.Name, vars.Name, shellQuote(o.Dir))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := f.confirm(ctx, o.Yes, fmt.Sprintf("Destroy %s and everything in project codeaf-%s? [y/N]", o.Name, o.Name)); err != nil {
		return err
	}
	if err := f.terraform(ctx, terraform, o.Dir, []string{"destroy", "-input=false", "-auto-approve"}, []string{"RAILWAY_TOKEN=" + token}, token, ""); err != nil {
		return err
	}
	fmt.Fprintf(f.Out, "The Terraform is still in %s.\n", o.Dir)
	if f.Remove != nil {
		id, err := readDeviceID(o.Dir)
		if err != nil {
			return fmt.Errorf("the box was destroyed, but its saved device id could not be read: %w", err)
		}
		removed, err := f.Remove(ctx, o.Name, id)
		if err != nil {
			return fmt.Errorf("the box was destroyed, but its device could not be removed: %w", err)
		}
		if !removed {
			fmt.Fprintf(f.Out, "No active device matches %s; no device was removed.\n", o.Name)
		}
	}
	fmt.Fprintf(f.Out, "%s is down.\n", o.Name)
	return nil
}

func (f Flow) confirm(ctx context.Context, yes bool, prompt string) error {
	if yes {
		return nil
	}
	if !f.Interactive {
		return errors.New("codeaf devbox needs --yes when stdin is not a terminal")
	}
	fmt.Fprint(f.Out, prompt+" ")
	answer := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(f.In).ReadString('\n'); answer <- strings.TrimSpace(line) }()
	select {
	case line := <-answer:
		if strings.EqualFold(line, "y") || strings.EqualFold(line, "yes") {
			return nil
		}
		return errors.New("cancelled")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func hasProject(dir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "terraform.tfstate"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var state struct {
		Resources []struct {
			Type      string
			Instances []json.RawMessage
		}
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return false, fmt.Errorf("read Terraform state: %w", err)
	}
	for _, r := range state.Resources {
		if r.Type == "railway_project" && len(r.Instances) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (f Flow) status(ctx context.Context, terraform string, o Options) error {
	names := []string{o.Name}
	if o.Name == "" {
		entries, err := os.ReadDir(o.Dir)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		names = nil
		for _, entry := range entries {
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
	}
	var devices []Device
	devicesLoaded := false
	for _, name := range names {
		dir := o.Dir
		if o.Name == "" {
			dir = filepath.Join(dir, name)
		}
		up, err := hasProject(dir)
		if err != nil {
			return err
		}
		if !up {
			fmt.Fprintf(f.Out, "%s: not up\n", name)
			continue
		}
		values, err := f.outputs(ctx, terraform, dir, nil)
		if err != nil {
			return err
		}
		get := func(key string) string { return values[key] }
		state := "not up"
		var last time.Time
		reached := false
		reached = f.probe(ctx, name)
		if reached {
			state = "up"
			last = time.Now()
		} else {
			if !devicesLoaded {
				var err error
				devices, err = f.devices(ctx)
				if err != nil {
					return err
				}
				devicesLoaded = true
			}
			for _, d := range devices {
				if !d.Removed && strings.EqualFold(d.Name, name) {
					last = d.LastSeen
					if d.Online != nil {
						if *d.Online {
							state = "up"
						}
					} else if !last.IsZero() && time.Since(last) <= FreshFor {
						state = "up (seen recently)"
					}
					break
				}
			}
		}
		fields := []string{name + ": " + state}
		if region := get("region"); region != "" {
			fields = append(fields, "region "+region)
		}
		if project := get("project_name"); project != "" {
			fields = append(fields, "project "+project)
		}
		if !last.IsZero() {
			fields = append(fields, "last seen "+last.UTC().Format(time.RFC3339))
		}
		fmt.Fprintln(f.Out, strings.Join(fields, "; "))
	}
	return nil
}

func (f Flow) terraform(ctx context.Context, path, dir string, args, env []string, secrets ...string) error {
	filteredOut := &terraformOutput{out: f.Out, stripColor: !f.StdoutTerminal}
	filteredErr := &terraformOutput{out: f.Err, stripColor: !f.StdoutTerminal}
	out := newRedactor(filteredOut, secrets...)
	errs := newRedactor(filteredErr, secrets...)
	err := f.runTerraform(ctx, path, dir, args, env, out, errs)
	out.Flush()
	errs.Flush()
	filteredOut.Flush()
	filteredErr.Flush()
	if err != nil {
		return fmt.Errorf("terraform %s failed: %s", args[0], redact(err.Error(), secrets...))
	}
	return nil
}
