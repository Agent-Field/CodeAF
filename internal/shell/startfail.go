package shell

import (
	"errors"
	"syscall"
)

// StartFailure explains why goos refused to start a command. The reason is a
// short clause written to follow "Failed to start command: ", short enough that
// a one-line summary of the failure still carries it; the advice is one line for
// whoever reads the whole result. Both are empty when the error has no more
// specific explanation, and the caller keeps its own wording. Callers pass
// runtime.GOOS; tests pass "windows" to check the Windows wording anywhere.
func StartFailure(goos string, err error) (reason, advice string) {
	if goos != "windows" {
		return "", ""
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return "", ""
	}
	// The numbers are Windows system error codes. The syscall package names
	// them only in its Windows build, and these have to compare on every build.
	const (
		accessDenied    syscall.Errno = 5
		filenameTooLong syscall.Errno = 206
		virusInfected   syscall.Errno = 225
		virusDeleted    syscall.Errno = 226
		securityAdvice                = "Check Protection history in Windows Security. Tell the person what you were trying to run rather than rewording the command to get past the security block."
	)
	switch errno {
	case virusInfected, virusDeleted:
		return "Microsoft Defender or another antivirus blocked it.", securityAdvice
	case accessDenied:
		return "Microsoft Defender likely blocked it (access denied).", securityAdvice
	case filenameTooLong:
		return "too long for Windows' 32,767-character command line.", "Write the script to a file in the working folder and run that file."
	}
	return "", ""
}
