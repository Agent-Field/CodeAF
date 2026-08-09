// COW filesystem probe — port of src/session/isolation.ts:119-160.
package isolation

import (
	"os"
	"path/filepath"
)

func runCowProbe(baseDir string) bool {
	dir, err := os.MkdirTemp(baseDir, "codeaf-cow-")
	if err != nil {
		return false
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	srcPath := filepath.Join(dir, "src")
	dstPath := filepath.Join(dir, "dst")
	if err := os.WriteFile(srcPath, []byte("cow-probe"), 0o600); err != nil {
		return false
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return false
	}
	defer src.Close()
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return false
	}
	defer dst.Close()

	return cloneFile(dst, src) == nil
}
