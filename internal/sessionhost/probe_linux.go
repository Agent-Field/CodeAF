//go:build linux

package sessionhost

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
	"golang.org/x/sys/unix"
)

func Probe(cfg Config) Report {
	status, _ := readStatus("/proc/self/status")
	noNewPrivs, _ := strconv.Atoi(status["NoNewPrivs"])
	seccomp, _ := strconv.Atoi(status["Seccomp"])
	ptrace, _ := os.ReadFile("/proc/sys/kernel/yama/ptrace_scope")
	var info unix.Utsname
	_ = unix.Uname(&info)
	report := Report{
		Kernel: unix.ByteSliceToString(info.Release[:]), EUID: os.Geteuid(),
		CapEff: status["CapEff"], CapBnd: status["CapBnd"],
		CapNamesEff: capabilityNames(status["CapEff"]),
		NoNewPrivs:  noNewPrivs, Seccomp: seccomp, PtraceScope: strings.TrimSpace(string(ptrace)),
		Require: cfg.Require,
	}
	if cfg.ForceProbe == "fail" {
		report.UIDSwitch.Error = "forced to fail by CODEAF_SESSIONHOST_FORCE_PROBE"
		return report.decide()
	}

	report.UIDSwitch = probeSwitch(cfg)
	caps := executor.Probe()
	report.Landlock = LandlockReport{Available: caps.Landlock, ABI: caps.LandlockABI}
	report.UserNS = caps.UserNS
	return report.decide()
}

func probeSwitch(cfg Config) SwitchReport {
	if os.Geteuid() != 0 {
		return SwitchReport{Error: "not running as root"}
	}
	launcher, err := newLauncher(cfg)
	if err != nil {
		return SwitchReport{Error: "could not find the chat process executable"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := launcher.command(ctx, cfg.UIDBase, "/", helperEnv(), launcher.self, "sessionhost", "whoami")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	switch {
	case err == nil && probeIdentity(string(out), cfg.UIDBase):
		return SwitchReport{OK: true}
	case strings.TrimSpace(stderr.String()) == boundingError:
		return SwitchReport{Error: strings.TrimSuffix(boundingError, ".")}
	default:
		return SwitchReport{Error: "could not switch to the chat's process identity"}
	}
}

func capabilityNames(hex string) []string {
	bits, _ := strconv.ParseUint(hex, 16, 64)
	names := []string{}
	for i, name := range [...]string{
		"CHOWN", "DAC_OVERRIDE", "DAC_READ_SEARCH", "FOWNER", "FSETID", "KILL", "SETGID", "SETUID",
		"SETPCAP", "LINUX_IMMUTABLE", "NET_BIND_SERVICE", "NET_BROADCAST", "NET_ADMIN", "NET_RAW", "IPC_LOCK", "IPC_OWNER",
		"SYS_MODULE", "SYS_RAWIO", "SYS_CHROOT", "SYS_PTRACE", "SYS_PACCT", "SYS_ADMIN", "SYS_BOOT", "SYS_NICE",
		"SYS_RESOURCE", "SYS_TIME", "SYS_TTY_CONFIG", "MKNOD", "LEASE", "AUDIT_WRITE", "AUDIT_CONTROL", "SETFCAP",
		"MAC_OVERRIDE", "MAC_ADMIN", "SYSLOG", "WAKE_ALARM", "BLOCK_SUSPEND", "AUDIT_READ", "PERFMON", "BPF", "CHECKPOINT_RESTORE",
	} {
		if bits&(uint64(1)<<i) != 0 {
			names = append(names, "CAP_"+name)
		}
	}
	return names
}
