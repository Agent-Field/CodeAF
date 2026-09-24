// Package main implements codeaf-probe: a thin CLI over internal/probe that
// emits exactly one compact JSON object on stdout per invocation. Human logs
// go to stderr; any error exits non-zero.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/probe"
)

// response mirrors internal/probe.Response so the CLI always prints the wire shape.
type response struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error *errObj     `json:"error,omitempty"`
}

type errObj struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// emit prints exactly one compact JSON object on stdout and sets the exit code.
func emit(ok bool, data interface{}, code, msg string) int {
	r := response{OK: ok}
	if ok {
		r.Data = data
	} else {
		r.Error = &errObj{Code: code, Message: msg}
	}
	b, err := json.Marshal(r)
	if err != nil { // unreachable for contract types, but never lie to the caller
		fmt.Fprintln(os.Stderr, "probe: marshal error:", err)
		return 2
	}
	fmt.Println(string(b))
	if !ok {
		return 1
	}
	return 0
}

// fail prints an error response and returns the exit code.
func fail(code, msg string) int { return emit(false, nil, code, msg) }

// usage writes the human summary to stderr.
func usage() {
	fmt.Fprintln(os.Stderr, `usage: codeaf-probe <verb> [flags]
verbs:
  prepare [--bin <path>] [--source <dir>]          build identity + reuse
  start --session <id> --profile <name> --bin <path> [--fixture <scenario>]
                                                   launch pinned codeaf chat in a persistent tmux session
  observe --session <id> [--diff]                  rendered snapshot/diff + revision + cursor + processes
  act --session <id> [--text s] [--keys ks] [--resize WxH] [--wait quietMs,timeoutMs] [--expect-revision N]
                                                   atomic act+wait+observe (stale revision -> STALE_REVISION)
  wait --session <id> --quiet <ms> --timeout <ms>  truthful settled/timeout reason
  record --session <id>                            read the session recording back as compact JSON
  finish --session <id>                            cleanup owned resources only
  fixture-prepare --scenario <name> | fixture-reset --scenario <name>
  contract                                         print the machine-readable JSON contract of all verbs`)
}

// contractSchema is the self-describing machine-readable contract: the JSON
// schema of every verb's request and response. Static by design — it must
// never drift from the wire types in internal/probe/contract.go.
func contractSchema() interface{} {
	return map[string]interface{}{
		"binary":  "codeaf-probe",
		"version": 1,
		"response": map[string]interface{}{
			"success": map[string]string{"ok": "true", "data": "verb-specific object"},
			"error":   map[string]interface{}{"ok": "false", "error": map[string]string{"code": "STALE_REVISION|NO_SESSION|TIMEOUT|NOT_OWNED|BAD_REQUEST", "message": "string"}},
		},
		"verbs": map[string]interface{}{
			"prepare": map[string]interface{}{
				"flags":   map[string]string{"--bin": "path to existing codeaf binary (optional)", "--source": "source dir to build from (default repo root)"},
				"request": map[string]string{"bin": "string?", "source": "string?"},
				"data":    map[string]interface{}{"build": map[string]string{"sha": "string", "dirty": "bool", "go_version": "string", "flags": "string", "binary": "path"}, "reused": "bool"},
			},
			"start": map[string]interface{}{
				"flags":   map[string]string{"--session": "required id", "--profile": "required profile name", "--bin": "required pinned binary path", "--fixture": "optional scenario to seed home"},
				"request": map[string]string{"session_id": "string", "profile": "string", "bin": "string", "fixture": "string?"},
				"data":    map[string]interface{}{"session_id": "string", "socket": "string", "profile": "string", "dims": "string"},
			},
			"observe": map[string]interface{}{
				"flags":   map[string]string{"--session": "required id", "--diff": "return diff vs previous observation"},
				"request": map[string]string{"session_id": "string", "diff": "bool?"},
				"data":    map[string]interface{}{"revision": "int", "snapshot": "string", "cursor": map[string]interface{}{"x": "int", "y": "int"}, "processes": "[]string", "ts": "string", "diff": "string?"},
			},
			"act": map[string]interface{}{
				"flags":   map[string]string{"--session": "required id", "--text": "text to type", "--keys": "tmux-style keys", "--resize": "WxH", "--wait": "quietMs,timeoutMs", "--expect-revision": "reject unless current revision matches"},
				"request": map[string]interface{}{"text": "string?", "keys": "string?", "resize": "string?", "wait": map[string]interface{}{"quiet_ms": "int?", "timeout_ms": "int?"}, "expect_revision": "int?"},
				"data":    map[string]interface{}{"accepted": "bool", "revision_before": "int", "revision_after": "int", "stale": "bool", "observation": "same as observe data"},
			},
			"wait": map[string]interface{}{
				"flags":   map[string]string{"--session": "required id", "--quiet": "quiet ms", "--timeout": "timeout ms"},
				"request": map[string]string{"session_id": "string", "quiet_ms": "int", "timeout_ms": "int"},
				"data":    map[string]interface{}{"settled": "bool", "reason": "string", "revision": "int"},
			},
			"finish": map[string]interface{}{
				"flags":   map[string]string{"--session": "required id"},
				"request": map[string]string{"session_id": "string"},
				"data":    map[string]interface{}{"removed": "bool", "killed": "[]string"},
			},
			"record": map[string]interface{}{
				"flags":   map[string]string{"--session": "required id"},
				"request": map[string]string{"session_id": "string"},
				"data":    map[string]interface{}{"session_id": "string", "count": "int", "records": "[]Record"},
			},
			"fixture-prepare": map[string]interface{}{
				"flags":   map[string]string{"--scenario": "required scenario name (e.g. clean)"},
				"request": map[string]string{"scenario": "string"},
				"data":    map[string]interface{}{"scenario": "string", "home": "string", "seeded": "bool"},
			},
			"fixture-reset": map[string]interface{}{
				"flags":   map[string]string{"--scenario": "required scenario name"},
				"request": map[string]string{"scenario": "string"},
				"data":    map[string]interface{}{"scenario": "string", "home": "string", "seeded": "bool"},
			},
			"contract": map[string]interface{}{
				"flags":   map[string]string{},
				"request": map[string]string{},
				"data":    "this document",
			},
		},
	}
}

func defaultRoot() string { return "default" }

func failBadRequestMsg(msg string) int { return fail(probe.CodeBadRequest, msg) }

func failNoSession(err error) int {
	return fail(probe.CodeNoSession, err.Error())
}
