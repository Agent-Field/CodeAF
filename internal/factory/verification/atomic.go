package verification

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/shell"
)

// Publishing a complete file by rename lets readers distinguish an admitted
// running check from corrupt committed terminal evidence.
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".verification-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func atomicMoveCommand() string {
	// A tool call may deliberately replace PATH with its fixture tools. The
	// observer's publication primitive must still be the host's own executable.
	for _, path := range []string{"/bin/mv", "/usr/bin/mv"} {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return "command " + quote(shell.ListPath(path))
		}
	}
	path, err := exec.LookPath("mv")
	if err != nil {
		path = "mv"
	}
	return "command " + quote(shell.ListPath(path))
}
