package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/probe"
)

// run dispatches one verb and returns the process exit code. Exactly one
// compact JSON object goes to stdout per invocation.
func run(argv []string) int {
	if len(argv) == 0 {
		usage()
		return failBadRequestMsg("verb required")
	}
	verb, rest := argv[0], argv[1:]
	switch verb {
	case "contract":
		return emit(true, contractSchema(), "", "")
	case "prepare":
		return runPrepare(rest)
	case "start":
		return runStart(rest)
	case "observe":
		return runObserve(rest)
	case "act":
		return runAct(rest)
	case "wait":
		return runWait(rest)
	case "finish":
		return runFinish(rest)
	case "fixture-prepare", "fixture-reset":
		return runFixture(verb, rest)
	case "record":
		return runRecord(rest)
	case "record-outcome":
		return runRecordOutcome(rest)
	default:
		usage()
		return failBadRequestMsg("unknown verb " + verb)
	}
}

func runPrepare(argv []string) int {
	bin, src := "", ""
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--bin":
			i++
			if i < len(argv) {
				bin = argv[i]
			}
		case "--source":
			i++
			if i < len(argv) {
				src = argv[i]
			}
		default:
			return failBadRequestMsg("prepare: unknown flag " + argv[i])
		}
	}
	if bin == "" && src == "" {
		return failBadRequestMsg("prepare: give --bin <path> or --source <dir>")
	}
	if bin != "" && src != "" {
		return failBadRequestMsg("prepare: --bin and --source are exclusive")
	}
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	var b probe.BuildIdentity
	if src != "" {
		if b, err = buildFromSource(m, src); err != nil {
			return failBadRequestMsg(err.Error())
		}
	} else {
		if b, err = buildIdentity(bin, src); err != nil {
			return failBadRequestMsg(err.Error())
		}
	}
	pd, err := m.SetBuild(b)
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	return emit(true, pd, "", "")
}

func runStart(argv []string) int {
	id, profile, bin := "", "", ""
	var extra []string
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--session":
			i++
			if i < len(argv) {
				id = argv[i]
			}
		case "--profile":
			i++
			if i < len(argv) {
				profile = argv[i]
			}
		case "--bin":
			i++
			if i < len(argv) {
				bin = argv[i]
			}
		case "--arg":
			i++
			if i < len(argv) {
				extra = append(extra, argv[i])
			}
		default:
			return failBadRequestMsg("start: unknown flag " + argv[i])
		}
	}
	if id == "" || profile == "" || bin == "" {
		return failBadRequestMsg("start: --session, --profile and --bin are required")
	}
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	args := append([]string{"chat"}, extra...)
	sd, err := m.Start(id, bin, profile, args, nil, probe.Dims{})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return failBadRequestMsg(err.Error())
		}
		return failBadRequestMsg(err.Error())
	}
	return emit(true, sd, "", "")
}

func runObserve(argv []string) int {
	id, wantDiff := "", false
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--session":
			i++
			if i < len(argv) {
				id = argv[i]
			}
		case "--diff":
			wantDiff = true
		default:
			return failBadRequestMsg("observe: unknown flag " + argv[i])
		}
	}
	if id == "" {
		return failBadRequestMsg("observe: --session required")
	}
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	obs, err := probe.RecordedObserve(m, id, wantDiff, loadPrev(m, id))
	if err != nil {
		return failNoSession(err)
	}
	savePrev(m, id, obs.Snapshot)
	return emit(true, obs, "", "")
}

func runAct(argv []string) int {
	id, text, keys, resize, waitArg, expectRev := "", "", "", "", "", ""
	expectSet := false
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--session":
			i++
			if i < len(argv) {
				id = argv[i]
			}
		case "--text":
			i++
			if i < len(argv) {
				text = argv[i]
			}
		case "--keys":
			i++
			if i < len(argv) {
				keys = argv[i]
			}
		case "--resize":
			i++
			if i < len(argv) {
				resize = argv[i]
			}
		case "--wait":
			i++
			if i < len(argv) {
				waitArg = argv[i]
			}
		case "--expect-revision":
			i++
			if i < len(argv) {
				expectRev = argv[i]
				expectSet = true
			}
		default:
			return failBadRequestMsg("act: unknown flag " + argv[i])
		}
	}
	if id == "" {
		return failBadRequestMsg("act: --session required")
	}
	req := probe.ActRequest{Text: text, Keys: keys}
	if resize != "" {
		w, h, err := parseDims(resize)
		if err != nil {
			return failBadRequestMsg(err.Error())
		}
		req.Resize = &probe.Resize{Width: w, Height: h}
	}
	if waitArg != "" {
		q, t, err := parseWait(waitArg)
		if err != nil {
			return failBadRequestMsg(err.Error())
		}
		req.Wait = &probe.ActWait{QuietMs: q, TimeoutMs: t}
	}
	if expectSet {
		n, err := strconv.Atoi(expectRev)
		if err != nil {
			return failBadRequestMsg("--expect-revision must be an int")
		}
		req.ExpectRevision = &n
	}
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	data, obs, aerr := probe.RecordedAct(m, id, req)
	if aerr != nil {
		if errors.Is(aerr, probe.ErrWaitTimeout) {
			// The keys went through; only the bounded wait timed out.
			// TIMEOUT, not BAD_REQUEST.
			return fail(probe.CodeTimeout, aerr.Error())
		}
		return failBadRequestMsg(aerr.Error())
	}
	if data.Stale {
		return fail(probe.CodeStaleRevision, "revision did not match; nothing was sent")
	}
	return emit(true, actObservation{ActData: data, Observation: obs}, "", "")
}

func parseWait(s string) (int, int, error) {
	i := strings.IndexByte(s, ',')
	if i <= 0 {
		return 0, 0, nil
	}
	q, e1 := strconv.Atoi(s[:i])
	t, e2 := strconv.Atoi(s[i+1:])
	if e1 != nil || e2 != nil {
		return 0, 0, nil
	}
	return q, t, nil
}

func waitOrDefault(w *probe.ActWait) probe.ActWait {
	if w == nil {
		return probe.ActWait{QuietMs: 300, TimeoutMs: 10000}
	}
	return *w
}

func runWait(argv []string) int {
	id, quiet, timeout := "", "", ""
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--session":
			i++
			if i < len(argv) {
				id = argv[i]
			}
		case "--quiet":
			i++
			if i < len(argv) {
				quiet = argv[i]
			}
		case "--timeout":
			i++
			if i < len(argv) {
				timeout = argv[i]
			}
		default:
			return failBadRequestMsg("wait: unknown flag " + argv[i])
		}
	}
	if id == "" {
		return failBadRequestMsg("wait: --session required")
	}
	q, _ := strconv.Atoi(quiet)
	t, _ := strconv.Atoi(timeout)
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	settled, obs, err := m.Wait(id, probe.ActWait{QuietMs: q, TimeoutMs: t})
	if err != nil {
		return failNoSession(err)
	}
	wd := waitData{Settled: settled, Reason: "quiet", Revision: obs.Revision}
	if !settled {
		wd.Reason = "timeout"
	}
	return emit(true, wd, "", "")
}

func runFinish(argv []string) int {
	id := ""
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--session" {
			i++
			if i < len(argv) {
				id = argv[i]
			}
		} else {
			return failBadRequestMsg("finish: unknown flag " + argv[i])
		}
	}
	if id == "" {
		return failBadRequestMsg("finish: --session required")
	}
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	if err := m.Finish(id); err != nil {
		if strings.Contains(err.Error(), "no such probe session") {
			return failNoSession(err)
		}
		return failBadRequestMsg(err.Error())
	}
	clearPrev(m, id)
	if oerr := probe.RecordedOutcome(m, id, true, "finish"); oerr != nil {
		return failBadRequestMsg(oerr.Error())
	}
	return emit(true, map[string]bool{"removed": true, "recorded": true}, "", "")
}

func runFixture(verb string, argv []string) int {
	scenario := ""
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--scenario" {
			i++
			if i < len(argv) {
				scenario = argv[i]
			}
		}
	}
	if scenario == "" {
		return failBadRequestMsg("fixture: --scenario required")
	}
	root := filepath.Join(os.Getenv("CODEAF_PROBE_BASE"), defaultRoot())
	if base := os.Getenv("CODEAF_PROBE_BASE"); base == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".codeaf-probe", defaultRoot())
	}
	var st *probe.FixtureState
	var ferr error
	if verb == "fixture-reset" {
		st, ferr = probe.FixtureReset(root, scenario)
	} else {
		st, ferr = probe.FixturePrepare(root, scenario)
	}
	if ferr != nil {
		return failBadRequestMsg(ferr.Error())
	}
	return emit(true, probe.FixtureData{Scenario: st.Scenario, Home: st.Home, Seeded: st.Seeded}, "", "")
}

// runRecordOutcome appends a journey's terminal record to the session's
// evidence file: how the journey ended and why. A CI journey whose assertion
// fails calls this on its way out, then exits non-zero — a failure lands in
// the recording, it never silently passes.
// recordData is the data payload of record: the session's evidence file,
// read back whole, in order, as compact JSON.
type recordData struct {
	SessionID string         `json:"session_id"`
	Count     int            `json:"count"`
	Records   []probe.Record `json:"records"`
}

// runRecord reads a session's recording file back whole (redacted, compact
// JSONL rendered as one JSON object on stdout).
func runRecord(argv []string) int {
	id := ""
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--session" {
			i++
			if i < len(argv) {
				id = argv[i]
			}
		} else {
			return failBadRequestMsg("record: unknown flag " + argv[i])
		}
	}
	if id == "" {
		return failBadRequestMsg("record: --session required")
	}
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	recs, err := probe.ReadRecords(m, id)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			return failNoSession(fmt.Errorf("session %q: no recording yet", id))
		}
		return failBadRequestMsg(err.Error())
	}
	if recs == nil {
		recs = []probe.Record{}
	}
	return emit(true, recordData{SessionID: id, Count: len(recs), Records: recs}, "", "")
}

func runRecordOutcome(argv []string) int {
	id, outcome, reason := "", "", ""
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--session":
			i++
			if i < len(argv) {
				id = argv[i]
			}
		case "--outcome":
			i++
			if i < len(argv) {
				outcome = argv[i]
			}
		case "--reason":
			i++
			if i < len(argv) {
				reason = argv[i]
			}
		default:
			return failBadRequestMsg("record-outcome: unknown flag " + argv[i])
		}
	}
	if id == "" || outcome == "" {
		return failBadRequestMsg("record-outcome: --session and --outcome are required")
	}
	if outcome != "ok" && outcome != "failed" && outcome != "error" {
		return failBadRequestMsg("record-outcome: --outcome must be ok, failed or error")
	}
	m, err := probe.Open(defaultRoot())
	if err != nil {
		return failBadRequestMsg(err.Error())
	}
	if err := probe.RecordOutcome(m, id, outcome, reason); err != nil {
		return failBadRequestMsg(err.Error())
	}
	return emit(true, map[string]string{"recorded": outcome}, "", "")
}
