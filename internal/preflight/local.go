package preflight

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/Agent-Field/codeaf/internal/inventory"
)

// serviceBinaries are the tools that also count as an available service.
var serviceBinaries = []string{"postgres", "psql", "mysqld", "redis-server"}

// Local gathers the capabilities of this machine for the tools the probe table
// knows.
func Local() Capabilities { return LocalFor(inventory.Inventory{}) }

// LocalFor is Local that also looks for every tool and service the inventory
// names, so a tool outside the probe table is found when it is really there.
func LocalFor(inv inventory.Inventory) Capabilities {
	c := Capabilities{
		Platform: Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		Tools:    map[string]string{},
		Services: map[string]string{},
		GPU:      localGPU(),
	}
	for _, name := range lookupNames(inv) {
		if path, err := exec.LookPath(name); err == nil {
			c.Tools[name] = inventory.Version(name, path)
		}
	}
	for _, name := range append(serviceBinaries, serviceNames(inv)...) {
		if v, ok := c.Tools[name]; ok {
			c.Services[name] = v
		}
	}
	return c
}

func lookupNames(inv inventory.Inventory) []string {
	var names []string
	for name := range inventory.DefaultProbes {
		names = append(names, name)
	}
	for _, t := range inv.Tools {
		names = append(names, t.Name)
	}
	return append(names, serviceNames(inv)...)
}

func serviceNames(inv inventory.Inventory) []string {
	var names []string
	for _, s := range inv.Services {
		names = append(names, s.Name)
	}
	return names
}

func localGPU() bool {
	if runtime.GOOS == "darwin" {
		return true // Metal
	}
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		return true
	}
	_, err := os.Stat("/dev/nvidia0")
	return err == nil
}
