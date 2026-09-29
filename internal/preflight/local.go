package preflight

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/Agent-Field/codeaf/internal/inventory"
)

// serviceBinaries are the tools that also count as an available service.
var serviceBinaries = []string{"postgres", "psql", "mysqld", "redis-server"}

// Local gathers the capabilities of this machine.
func Local() Capabilities {
	c := Capabilities{
		Platform: Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		Tools:    map[string]string{},
		Services: map[string]string{},
		GPU:      localGPU(),
	}
	for name := range inventory.DefaultProbes {
		if path, err := exec.LookPath(name); err == nil {
			c.Tools[name] = inventory.Version(name, path)
		}
	}
	for _, name := range serviceBinaries {
		if v, ok := c.Tools[name]; ok {
			c.Services[name] = v
		}
	}
	return c
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
