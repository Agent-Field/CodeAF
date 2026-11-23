package cellstore

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/home"
	"golang.org/x/sys/windows"
)

const maxSocketBytes = 108

// socketFor puts the daemon under the user's local profile. Hashing the full
// home and binary path isolates homes, engine upgrades and development engines.
func socketFor(bin string) string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserCacheDir()
	}
	root := strings.ToLower(canonicalTree(home.Dir()))
	sum := sha256.Sum256([]byte(root + "\x00" + bin))
	socket := filepath.Join(shortSocketBase(base), "codeaf", fmt.Sprintf("e-%x.sock", sum[:8]))
	if base != "" && len(socket) < maxSocketBytes {
		return socket
	}
	// Short names cannot bound directory depth. Keep a private, home/build-
	// keyed directory under the user's own temp folder, never a shared system
	// folder or a drive root. With no bounded private place, saved history turns
	// itself off for this engine with one sentence instead of failing each seal.
	temp := shortSocketBase(os.TempDir())
	if !filepath.IsAbs(temp) || filepath.Dir(temp) == filepath.Clean(temp) || ownedSocketDir(temp) != nil {
		return ""
	}
	socket = filepath.Join(temp, fmt.Sprintf("codeaf-e-%x", sum[:8]), fmt.Sprintf("e-%x.sock", sum[:8]))
	if filepath.IsAbs(socket) && len(socket) < maxSocketBytes && privateSocketDir(filepath.Dir(socket)) == nil {
		return socket
	}
	return ""
}

func shortSocketBase(base string) string {
	if path, err := windows.UTF16PtrFromString(base); err == nil {
		buf := make([]uint16, 32768)
		if n, err := windows.GetShortPathName(path, &buf[0], uint32(len(buf))); err == nil && n > 0 && n < uint32(len(buf)) {
			return windows.UTF16ToString(buf[:n])
		}
	}
	return base
}

// Windows checks the private fallback while selecting the socket address.
func validateSocket(string) error {
	return nil
}

// privateSocketDir protects a short fallback before either dialing or pruning.
// Windows ignores Mkdir's mode, so creation needs a non-inherited user ACL.
func privateSocketDir(dir string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err := windows.CreateDirectory(path, &attrs); err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return err
	}
	if err := ownedSocketDir(dir); err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// ownedSocketDir rejects shared temp bases and preclaimed fallback directories.
func ownedSocketDir(dir string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	flags, err := windows.GetFileAttributes(path)
	if err != nil {
		return err
	}
	if flags&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || flags&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return fmt.Errorf("saving-engine socket directory is not private")
	}
	existing, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("saving-engine socket directory has no owner")
	}
	owner, _, err := existing.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(user.User.Sid) {
		return fmt.Errorf("saving-engine socket directory belongs to another user")
	}
	return nil
}
