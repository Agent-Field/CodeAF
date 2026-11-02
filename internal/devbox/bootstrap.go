package devbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const deviceReceipt = "codeaf.devbox.json"

func (f Flow) recordDevice(ctx context.Context, o Options) error {
	if f.Devices == nil {
		return nil
	}
	devices, err := f.devices(ctx)
	if err != nil {
		return err
	}
	var id string
	for _, d := range devices {
		if !d.Removed && d.ID != "" && strings.EqualFold(d.Name, o.Name) {
			if id != "" && id != d.ID {
				return errors.New("more than one active device has the box's name")
			}
			id = d.ID
		}
	}
	if id == "" {
		return errors.New("the joined device is not in this identity's list yet")
	}
	raw, err := json.Marshal(struct {
		ID string `json:"device_id"`
	}{id})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(o.Dir, ".codeaf-device-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(raw, '\n'))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(o.Dir, deviceReceipt))
}

func readDeviceID(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, deviceReceipt))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var receipt struct {
		ID string `json:"device_id"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return "", err
	}
	return receipt.ID, nil
}

// State, including its previous copy after a failed clear, is the cleanup receipt.
// Read only the three bootstrap values, retaining them solely for redaction.
func bootstrapSecrets(dir string) ([]string, error) {
	var secrets []string
	for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.New("could not read the Terraform state for setup cleanup")
		}
		var state any
		if json.Unmarshal(raw, &state) != nil {
			return nil, errors.New("could not read the Terraform state for setup cleanup")
		}
		var visit func(any)
		visit = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if name, ok := v["name"].(string); ok && (name == "CODEAF_DEVBOX_CODE" || name == "CODEAF_DEVBOX_REMOTE_KEY" || name == "CODEAF_DEVBOX_REMOTE_DEVICES") {
					if secret, ok := v["value"].(string); ok && secret != "" {
						secrets = append(secrets, secret)
					}
				}
				for _, child := range v {
					visit(child)
				}
			case []any:
				for _, child := range v {
					visit(child)
				}
			}
		}
		visit(state)
	}
	return secrets, nil
}

func (f Flow) clearBootstrap(ctx context.Context, terraform, token string, o Options, secrets []string) error {
	secrets = append(secrets, token)
	fmt.Fprintln(f.Out, "Removing the one-time setup values from Railway.")
	if err := f.applyPlan(ctx, terraform, o.Dir, []string{"RAILWAY_TOKEN=" + token}, secrets, true, func() error { return ctx.Err() }); err != nil {
		return f.clearFailed(o, err, secrets...)
	}
	if err := os.Remove(filepath.Join(o.Dir, "terraform.tfstate.backup")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return f.clearFailed(o, err, secrets...)
	}
	return nil
}

func (f Flow) clearFailed(o Options, err error, secrets ...string) error {
	return fmt.Errorf("%s is up, but its one-time setup values are still on Railway and in %s: %s. Run %s again to finish, or %s to remove it.", o.Name, o.Dir, reason(errors.New(redact(err.Error(), secrets...))), f.command("up", o), f.command("down", o))
}
