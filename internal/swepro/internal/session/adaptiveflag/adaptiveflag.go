// Package adaptiveflag ports src/session/adaptive-flag.ts — the single source
// of truth for the CODEAF_ADAPTIVE_CUTS master switch.
//
// The adaptive verification chain — root-cut fast path, programmatic
// admissibility retries, execution cross-check, capability tracker, HEFT, leaf
// outcomes — is gated behind this env var. After the 2026-07-20 flip the
// default is ON and the var is a KILL SWITCH: exactly "0" restores the
// pre-flip default-OFF behavior; unset, "1", or anything else leaves the
// adaptive chain ON.
//
// Every live read of the env var goes through this predicate; do not compare
// the raw string elsewhere.
package adaptiveflag

import "os"

// envVar is the one place the raw string lives, mirroring the TS module's
// "do not compare the raw string elsewhere" rule.
const envVar = "CODEAF_ADAPTIVE_CUTS"

// AdaptiveCutsEnabled mirrors adaptiveCutsEnabled().
//
// Read LIVE on every call — the TS reads process.env each time and callers
// (src/tool/shell.ts, src/cli/cmd/run.ts, src/session/capability.ts,
// src/session/leaf-outcome.ts) depend on a mid-process change taking effect.
// Nothing is cached here either.
//
// The comparison is EXACT and case-sensitive: only the one-character string
// "0" disables. "0 ", " 0", "00", "false", "off", "" and the empty/unset case
// all leave it enabled.
//
// os.Getenv collapses "unset" and "set to empty string" into "", where Node
// distinguishes undefined from "". Both compare unequal to "0", so the
// predicate is observably identical.
func AdaptiveCutsEnabled() bool {
	return os.Getenv(envVar) != "0"
}
