package devbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// R5.3: an acknowledgement alone is insufficient; only the pinned probe can announce up.
func TestDevboxR53UpRequiresReachabilityAndFindsLostAcknowledgement(t *testing.T) {
	for _, mode := range []string{"not-reachable", "lost-ack", "interrupted-ack", "delayed-probe"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "custom folder")
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			var applied, cancelled atomic.Bool
			f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
				err := RunTerraform(ctx, path, dir, args, env, stdout, stderr)
				if args[0] == "apply" && strings.Contains(strings.Join(env, " "), "TF_VAR_pair_code=") {
					applied.Store(true)
				}
				return err
			}
			var probes atomic.Int32
			f.Probe = func(ctx context.Context, name string) bool {
				if name != "box" {
					t.Error("R5.3 wrong pinned alias")
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > probeWithin {
					t.Error("R5.3 unbounded probe")
				}
				n := probes.Add(1)
				return applied.Load() && mode != "not-reachable" && (mode != "delayed-probe" || n >= 2)
			}
			f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
				defer func() {
					if mode == "lost-ack" && ctx.Err() != nil {
						cancelled.Store(true)
					}
				}()
				ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
				for !applied.Load() {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(time.Millisecond):
					}
				}
				if mode == "lost-ack" {
					<-ctx.Done()
					cancelled.Store(true)
					return ctx.Err()
				}
				if mode == "interrupted-ack" {
					return ErrJoinInterrupted
				}
				return nil
			}
			f.probeEvery = time.Millisecond
			f.readyWithin = 25 * time.Millisecond
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := f.Run(ctx, "up", Options{Name: "box", Dir: dir, Yes: true})
			if mode == "not-reachable" {
				want := "box took its pairing code but is not reachable yet: the pinned remote key did not answer within three minutes. It is on your Railway account; check it with codeaf devbox status box --dir '" + dir + "', or remove it with codeaf devbox down box --dir '" + dir + "'."
				if err == nil || err.Error() != want || strings.Contains(out.String(), "box is up") {
					t.Fatalf("R5.3/R5.7 premature up or receipt: %v %s", err, &out)
				}
			} else if err != nil || !strings.Contains(out.String(), "box is up") || mode == "lost-ack" && !cancelled.Load() {
				t.Fatalf("R5.3 readiness: %v, cancelled %v, %s", err, cancelled.Load(), &out)
			}
			if probes.Load() == 0 {
				t.Fatal("R5.3 never probed")
			}
		})
	}
}

// R5.4: clear uses a guarded third apply without any bootstrap inputs, erases the backup and can resume.
func TestDevboxR54ClearSecretsAndResumeWithoutPairing(t *testing.T) {
	for _, mode := range []string{"success", "apply-fails", "apply-wrote-then-failed", "guard-refuses"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "custom folder")
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			var joined atomic.Bool
			f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
				ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
				for !joined.Load() {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(time.Millisecond):
					}
				}
				return nil
			}
			f.Probe = func(context.Context, string) bool { return joined.Load() }
			var clears, guards int
			failed := false
			f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
				clear := joined.Load()
				if clear {
					for _, entry := range env {
						if strings.HasPrefix(entry, "TF_VAR_pair_code=") || strings.HasPrefix(entry, "TF_VAR_remote_key=") || strings.HasPrefix(entry, "TF_VAR_remote_devices=") {
							t.Error("R5.4 clear contains bootstrap input")
						}
					}
				}
				if clear && args[0] == "show" {
					guards++
					if mode == "guard-refuses" && !failed {
						failed = true
						io.WriteString(stdout, `{"resource_changes":[{"address":"railway_service.box","change":{"actions":["delete","create"]}}]}`)
						return nil
					}
				}
				if clear && args[0] == "apply" {
					clears++
					if mode == "apply-fails" && !failed {
						failed = true
						io.WriteString(stderr, "fixture-remote-key 42-123-456 fixture-remote-devices")
						return errors.New("fixture-remote-key rejected")
					}
					if mode == "apply-wrote-then-failed" && !failed {
						failed = true
						if err := RunTerraform(ctx, path, dir, args, env, stdout, stderr); err != nil {
							return err
						}
						return errors.New("provider lost the clear reply")
					}
				}
				err := RunTerraform(ctx, path, dir, args, env, stdout, stderr)
				if args[0] == "apply" && strings.Contains(strings.Join(env, " "), "TF_VAR_pair_code=") {
					joined.Store(true)
				}
				return err
			}
			opts := Options{Name: "box", Dir: dir, Yes: true}
			err := f.Run(t.Context(), "up", opts)
			if mode != "success" {
				if err == nil || !strings.HasPrefix(err.Error(), "box is up, but its one-time setup values are still on Railway and in "+dir+": ") || !strings.Contains(err.Error(), "codeaf devbox up box --dir '"+dir+"' again to finish, or codeaf devbox down box --dir '"+dir+"' to remove it.") {
					t.Fatalf("R5.4/R5.7 clear failure: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "terraform.tfstate")); err != nil {
					t.Fatal("R5.4 lost state")
				}
				f.Pair = func(context.Context, string, func(Offer)) error { t.Fatal("R5.4 rerun re-paired"); return nil }
				f.Prepare = func(context.Context, string) (Offer, error) {
					t.Fatal("R5.4 rerun regenerated key")
					return Offer{}, nil
				}
				if err := f.Run(t.Context(), "up", opts); err != nil {
					t.Fatalf("R5.4 rerun: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantGuards, wantClears := 1, 1
			if mode != "success" {
				wantGuards = 2
			}
			if mode == "apply-fails" || mode == "apply-wrote-then-failed" {
				wantClears = 2
			}
			if guards != wantGuards || clears != wantClears {
				t.Fatalf("R5.4 clear guard/apply count: %d/%d, want %d/%d", guards, clears, wantGuards, wantClears)
			}
			secrets, err := bootstrapSecrets(dir)
			if err != nil || len(secrets) != 0 {
				t.Fatalf("R5.4 state still carries setup values: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "terraform.tfstate.backup")); !os.IsNotExist(err) {
				t.Fatalf("R5.4 secret backup remains: %v", err)
			}
			for _, secret := range []string{"fixture-token", "fixture-remote-key", "fixture-remote-devices", "42-123-456"} {
				if strings.Contains(out.String(), secret) {
					t.Fatalf("R5.4 leaked %s", secret)
				}
			}
			before := clears
			if err := f.Run(t.Context(), "up", opts); err == nil || !strings.Contains(err.Error(), "already up") {
				t.Fatalf("R5.4 clean rerun: %v", err)
			}
			if clears != before {
				t.Fatal("R5.4 clean rerun applied again")
			}
		})
	}
}

// Contract 3: a box created from a laptop that was a fleet of one receives that
// laptop's saved keys, because up sends them once the box answers and before it
// says the box is up. A failed send is said once and does not undo a working box.
func TestDevboxUpSendsThisComputersSavedKeysOnceTheBoxAnswers(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "sent", true: "failed"}[fail], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "box")
			var out bytes.Buffer
			f := fixtureFlow(t, dir, &out)
			var answered atomic.Bool
			f.Probe = func(context.Context, string) bool { answered.Store(true); return true }
			f.Pair = func(_ context.Context, _ string, ready func(Offer)) error {
				ready(Offer{Code: "42-123-456", Via: "https://relay.example"})
				return nil
			}
			var shares atomic.Int32
			f.ShareState = func(context.Context) error {
				shares.Add(1)
				if !answered.Load() || strings.Contains(out.String(), "box is up") {
					t.Error("the keys were sent before the box answered, or after up said it was up")
				}
				if fail {
					return errors.New("relay unreachable.")
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if err := f.Run(ctx, "up", Options{Name: "box", Dir: dir, Yes: true}); err != nil {
				t.Fatal(err)
			}
			if shares.Load() != 1 || !strings.Contains(out.String(), "box is up") {
				t.Fatalf("keys sent %d times; %s", shares.Load(), &out)
			}
			warning := "Could not send this computer's saved keys yet: relay unreachable. Open codeaf here once, or a chat on box can stop on a missing key."
			if strings.Contains(out.String(), warning) != fail {
				t.Fatalf("the one-line warning appeared = %v, want %v: %s", !fail, fail, &out)
			}
		})
	}
}
