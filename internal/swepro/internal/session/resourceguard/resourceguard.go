// Package resourceguard ports src/session/resource-guard.ts:1-59 from swe-pro
// (commit 3b25a1a).
//
// The guard is deliberately fail-open: callers ask before expensive workspace
// clones, leaf dispatch, builds, and audits, but an inability to measure must
// never be the event that breaks a run. A zero floor disables the guard.
package resourceguard

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// DefaultDiskFloorGB is the free-space floor used when the environment
// override is unset or invalid.
const DefaultDiskFloorGB = 5

// DiskEnvelope mirrors the TS interface and its property order.
type DiskEnvelope struct {
	FreeBytes jscompat.JSNumber `json:"freeBytes"`
	FreeGB    jscompat.JSNumber `json:"freeGB"`
	FloorGB   jscompat.JSNumber `json:"floorGB"`
	OK        bool              `json:"ok"`
}

// EnvLookup is the process.env seam used by tests.
type EnvLookup func(key string) (value string, present bool)

// StatFSResult contains the two statfs fields the source multiplies.
// float64 is intentional: Node exposes these as JavaScript numbers.
type StatFSResult struct {
	Bavail float64
	Bsize  float64
}

// StatFSFunc is the injectable statfs operation.
type StatFSFunc func(path string) (StatFSResult, error)

// DFExecFunc is the source's secondary `df -k path` measurement seam.
type DFExecFunc func(path string) (stdout string, err error)

// CheckOptions supplies side-effect seams. Nil fields use the real process
// environment, statfs syscall, and df executable.
type CheckOptions struct {
	LookupEnv EnvLookup
	StatFS    StatFSFunc
	DF        DFExecFunc
}

// DiskFloorGB resolves CODEAF_DISK_FLOOR_GB exactly like Number(raw):
// unset uses 5, finite non-negative values (including empty -> 0) are accepted,
// and negative/non-finite/invalid values use 5.
func DiskFloorGB() float64 {
	return diskFloorGB(os.LookupEnv)
}

func diskFloorGB(lookup EnvLookup) float64 {
	raw, present := lookup("CODEAF_DISK_FLOOR_GB")
	if !present {
		return DefaultDiskFloorGB
	}
	n := jsNumber(raw)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return DefaultDiskFloorGB
	}
	return n
}

var decimalNumber = regexp.MustCompile(`^[+-]?(?:Infinity|(?:(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?))$`)

// jsNumber is Number(string) for environment/df text. jscompat.ToNumber covers
// common cases but strconv accepts Go-only forms such as 1_000/Inf/NaN and its
// radix path is uint64-limited, so this seam validates the full string grammar
// and uses big.Int for arbitrary-size 0x/0o/0b input.
func jsNumber(value string) float64 {
	text := jscompat.Trim(value)
	if text == "" {
		return 0
	}

	if len(text) > 2 && text[0] == '0' {
		base := 0
		switch text[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, ok := new(big.Int).SetString(text[2:], base)
			if !ok {
				return math.NaN()
			}
			result, _ := new(big.Float).SetInt(n).Float64()
			return result
		}
	}

	if !decimalNumber.MatchString(text) {
		return math.NaN()
	}
	switch text {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	n, err := strconv.ParseFloat(text, 64)
	if err == nil {
		return n
	}
	var numErr *strconv.NumError
	if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
		return n
	}
	return math.NaN()
}

// CheckDiskEnvelope measures the containing volume and compares it to the
// configured floor. It never returns an error. With no options it uses real
// I/O; the optional first value injects dependencies, and extra values are
// ignored like extra JavaScript arguments.
func CheckDiskEnvelope(path string, options ...CheckOptions) DiskEnvelope {
	opts := CheckOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}
	if opts.StatFS == nil {
		opts.StatFS = defaultStatFS
	}
	if opts.DF == nil {
		opts.DF = defaultDF
	}

	floorGB := diskFloorGB(opts.LookupEnv)
	freeBytes := -1.0
	stats, err := callStatFS(opts.StatFS, path)
	if err == nil {
		// Preserve the TS expression shape: coerce each operand to Number,
		// then multiply. Integer multiplication before conversion can round or
		// overflow differently.
		freeBytes = stats.Bavail * stats.Bsize
	} else if stdout, dfErr := callDF(opts.DF, path); dfErr == nil {
		if availKB, ok := parseDFAvailableKB(stdout); ok {
			freeBytes = availKB * 1024
		}
	}

	freeGB := -1.0
	if freeBytes >= 0 {
		freeGB = freeBytes / math.Pow(1024, 3)
	}
	ok := floorGB == 0 || freeBytes < 0 || freeGB > floorGB
	return DiskEnvelope{
		FreeBytes: jscompat.JSNumber(freeBytes),
		FreeGB:    jscompat.JSNumber(freeGB),
		FloorGB:   jscompat.JSNumber(floorGB),
		OK:        ok,
	}
}

func callStatFS(statfs StatFSFunc, path string) (result StatFSResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = StatFSResult{}
			err = fmt.Errorf("%v", recovered)
		}
	}()
	return statfs(path)
}

func callDF(df DFExecFunc, path string) (stdout string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stdout = ""
			err = fmt.Errorf("%v", recovered)
		}
	}()
	return df(path)
}

func parseDFAvailableKB(stdout string) (float64, bool) {
	text := jscompat.Trim(stdout)
	lines := strings.Split(text, "\n")
	last := ""
	if len(lines) > 0 {
		last = jscompat.Trim(lines[len(lines)-1])
	}
	fields := strings.FieldsFunc(last, isJSRegexWhitespace)
	if len(fields) < 4 {
		return 0, false
	}
	value := jsNumber(fields[3])
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isJSRegexWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func defaultDF(path string) (string, error) {
	cmd := exec.Command("df", "-k", path)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// The TS awaits proc.exited but never checks its value; any stdout from
		// a non-zero process is still parsed.
		err = nil
	}
	return stdout.String(), err
}
