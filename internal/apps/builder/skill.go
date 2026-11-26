package builder

import (
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

//go:embed skill/SKILL.md
var skill string

const skillName = "workaf-apps"

func skillVersion(body string) int {
	body = strings.TrimPrefix(body, "\ufeff")
	if rest, ok := strings.CutPrefix(body, "---\n"); ok {
		_, body, _ = strings.Cut(rest, "\n---\n")
	}
	line, _, _ := strings.Cut(body, "\n")
	marker, ok := strings.CutPrefix(line, "<!-- "+skillName+" v")
	if !ok {
		return 0
	}
	digits, ok := strings.CutSuffix(marker, " -->")
	if !ok {
		return 0
	}
	version, _ := strconv.Atoi(digits)
	return version
}

// InstallSkill places the versioned skill beside the user's other skills,
// leaving an unmanaged file or a newer version alone.
func InstallSkill(login string) error {
	if login == "" {
		return errors.New("login home is unavailable")
	}
	dir := filepath.Join(login, ".codeaf", "skills", skillName)
	path := filepath.Join(dir, "SKILL.md")
	current, err := os.ReadFile(path)
	if err == nil && (skillVersion(string(current)) == 0 || skillVersion(string(current)) >= skillVersion(skill)) {
		return nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".skill-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(skill)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return fsreplace.Rename(f.Name(), path)
}
