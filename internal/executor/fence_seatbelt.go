package executor

import "strings"

// fence_seatbelt.go renders the fence (fence.go) as a macOS sandbox profile.
// It is plain text, so it is built and tested on every system; only running
// it needs macOS (fence_darwin.go). Later rules win, so the read-only paths,
// the hidden paths and the refused sockets come after what they narrow.

// darwinTemp is the system's own temporary and per-user cache folders. They
// stay writable because the compilers and Xcode's tools keep their module
// caches there whatever TMPDIR says, and a cgo build that cannot write them
// fails. Nothing installed lives in them.
var darwinTemp = []string{"/private/tmp", "/private/var/folders"}

// keychainServices are the system services a keychain read goes through.
// Certificate trust (trustd) is not among them, so TLS keeps working.
var keychainServices = []string{"com.apple.SecurityServer", "com.apple.securityd", "com.apple.security.keychain-circle-notification"}

// fenceProfile is the whole profile for one fence.
func fenceProfile(f *Fence) string {
	writable := append(append([]string{f.Root, f.Tmp, f.Cache}, f.Writable...), darwinTemp...)
	parts := []string{
		"(version 1)",
		"(deny default)",
		runtimePlumbing,
		"(allow file-read*)",
		clause("allow file-write*", subpaths(writable...)+literals(devSinks...)),
		"(allow network*)",
	}
	if len(f.ReadOnly) > 0 {
		parts = append(parts, clause("deny file-write*", subpaths(f.ReadOnly...)))
	}
	if len(f.Hidden) > 0 {
		parts = append(parts, clause("deny file-read* file-write*", subpaths(f.Hidden...)))
	}
	if len(f.Sockets) > 0 {
		parts = append(parts, clause("deny network-outbound", socketFilters(f.Sockets)))
		parts = append(parts, clause("deny file-read* file-write*", literals(f.Sockets...)))
	}
	parts = append(parts, clause("deny mach-lookup", globalNames(keychainServices)))
	return strings.Join(parts, "\n") + "\n"
}

func socketFilters(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		b.WriteString("    (remote unix-socket (path-literal " + quote(p) + "))\n")
	}
	return b.String()
}

func globalNames(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString("    (global-name " + quote(n) + ")\n")
	}
	return b.String()
}
