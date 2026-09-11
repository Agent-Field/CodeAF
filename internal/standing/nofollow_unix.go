//go:build unix

package standing

import "syscall"

// noFollow refuses to open a link where a file was expected ([excerpt]).
const noFollow = syscall.O_NOFOLLOW
