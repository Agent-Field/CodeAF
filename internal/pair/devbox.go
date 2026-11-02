package pair

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"time"
)

// DevboxRemote prepares released serve's key and admissions as Terraform configuration.
// Running the existing writers in a private temporary folder keeps their formats identical.
func DevboxRemote(name, service string, client Device, label string) (key, devices []byte, known Known, err error) {
	dir, err := os.MkdirTemp("", "codeaf-devbox-remote-*")
	if err != nil {
		return nil, nil, Known{}, err
	}
	defer os.RemoveAll(dir)
	keeper := FileKeeper{Path: filepath.Join(dir, "device.key")}
	machine, err := ThisDevice(keeper)
	if err != nil {
		return nil, nil, Known{}, err
	}
	now := time.Now()
	bookPath := filepath.Join(dir, "devices.json")
	if err := BookAt(bookPath).Admit(Paired{Label: label, Key: base64.RawURLEncoding.EncodeToString(client.Public()), Since: now}); err != nil {
		return nil, nil, Known{}, err
	}
	key, err = os.ReadFile(keeper.Path)
	if err != nil {
		return nil, nil, Known{}, err
	}
	devices, err = os.ReadFile(bookPath)
	if err != nil {
		return nil, nil, Known{}, err
	}
	return key, devices, Known{Name: machine.Name(), Alias: name, Key: base64.RawURLEncoding.EncodeToString(machine.Public()), Service: service, Since: now}, nil
}

// ForgetAlias drops the creating laptop's pin after its dev box is destroyed.
func (b *Book) ForgetAlias(name string) error {
	known, err := b.Machines()
	if err != nil {
		return err
	}
	found := false
	for _, one := range known {
		if one.Alias == name {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	return b.update(func(known []Known) []Known {
		kept := known[:0]
		for _, one := range known {
			if one.Alias != name {
				kept = append(kept, one)
			}
		}
		return kept
	})
}
