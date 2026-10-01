#!/usr/bin/env bash
# PATH stubs replay failures without building the product or changing the real
# git worktree. Calls are recorded so a green cannot hide an unbounded retry.
set -euo pipefail
export GOFLAGS=-buildvcs=false
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d /tmp/codeaf-touched-verdict-test.XXXXXX)"
trap 'rm -rf -- "$tmp"' EXIT
mkdir "$tmp/bin"
cat >"$tmp/bin/stub" <<'PY'
#!/usr/bin/env python3
import json, os
from pathlib import Path
import sys

tool = Path(sys.argv[0]).name
directory = Path(os.environ['CASE_DIR'])
with (directory / 'calls').open('a') as out:
    out.write(json.dumps({'tool': tool, 'args': sys.argv[1:], 'cwd': os.getcwd()}) + '\n')
scenario = os.environ['SCENARIO']
package = 'example.invalid/fixture/pkg'
name = 'TestOne'

def emit(action, test=None, output=None, pkg=package):
    event = {'Action': action, 'Package': pkg}
    if test: event['Test'] = test
    if output: event['Output'] = output
    print(json.dumps(event), flush=True)

if tool == 'git':
    if sys.argv[1:3] == ['worktree', 'add']:
        Path(sys.argv[-2]).mkdir(parents=True)
    sys.exit(0)
if tool == 'gh':
    if scenario == 'api-malformed':
        print('null')
        sys.exit(0)
    if scenario == 'api-error':
        print('API unavailable', file=sys.stderr)
        sys.exit(1)
    if sys.argv[1:3] == ['issue', 'list']:
        assert sys.argv[sys.argv.index('--state') + 1] == 'open', sys.argv
        # A close title is not the exact standing issue.
        print(json.dumps([{'number': 6, 'title': 'touched packages: flaky or already-failing tests extra'},
                          {'number': 9, 'title': 'touched packages: flaky or already-failing tests'},
                          {'number': 7, 'title': 'touched packages: flaky or already-failing tests'}]
                         if scenario != 'create-issue' else []))
    elif sys.argv[1:3] == ['issue', 'create']:
        print('https://github.com/example/fixture/issues/8')
    elif sys.argv[1:3] == ['issue', 'comment']:
        body = Path(sys.argv[sys.argv.index('--body-file') + 1]).read_text()
        (directory / 'issue-body').write_text(body)
    sys.exit(0)
if tool == 'make':
    # No name-based skip may sneak back into the first run.
    assert not any('-skip' in arg for arg in sys.argv)
    assert 'KNOWN_RED=' in sys.argv and 'TEST_SKIP=' in sys.argv
    if scenario == 'killed-silent':
        emit('fail', name)
        sys.exit(137)
    if scenario in ('build', 'setup', 'timeout', 'panic', 'killed', 'shard'):
        emit('fail', name)
        prose = {'build': '[build failed]', 'setup': '[setup failed]',
                 'timeout': 'panic: test timed out after 15m', 'panic': 'panic: outside a test',
                 'killed': 'signal: killed', 'shard': 'shard-test: lost terminal test results'}[scenario]
        emit('output', output=prose)
    elif scenario == 'unnamed':
        emit('fail')
    elif scenario in ('cap', 'cap-five'):
        for i in range(6 if scenario == 'cap' else 5): emit('fail', 'Test' + str(i))
    elif scenario == 'subtest':
        emit('fail', 'TestOne/child.with+marks')
        emit('fail', 'TestOne')
    elif scenario == 'mixed':
        emit('fail', 'TestOne')
        emit('fail', 'TestTwo')
    elif scenario == 'green':
        emit('pass', name)
        emit('pass')
        sys.exit(0)
    elif scenario == 'readable-log':
        print('plain runner diagnostic', flush=True)
        emit('output', 'TestX', '--- FAIL: TestX (0.00s)\n')
        emit('fail', 'TestX')
    else:
        emit('output', name, 'FIRST FAILURE REMAINS IN THE LOG')
        emit('fail', name)
    emit('fail')
    sys.exit(1)
assert tool == 'go', tool
assert '-count=1' in sys.argv and '-run' in sys.argv, sys.argv
assert '-skip' not in ' '.join(sys.argv)
pattern = sys.argv[sys.argv.index('-run') + 1]
assert pattern.startswith('^(') and pattern.endswith(')$'), pattern
if scenario == 'subtest':
    assert pattern == r'^(TestOne)$/^(child\.with\+marks)$', pattern
    name = 'TestOne/child.with+marks'
if scenario == 'mixed' and pattern == '^(TestTwo)$': name = 'TestTwo'
if scenario == 'cap-five': name = pattern[2:-2]
if scenario == 'readable-log': name = 'TestX'
base = '/codeaf-touched-base-' in os.getcwd()
if base and scenario in ('absent-test', 'base-skipped'):
    if scenario == 'base-skipped': emit('skip', name)
    emit('pass')
    sys.exit(0)
if base and scenario == 'absent-package':
    emit('output', output='no required module provides package ' + package + '; [setup failed]')
    emit('fail')
    sys.exit(1)
if base and scenario == 'base-build':
    emit('output', output='[build failed]')
    emit('fail')
    sys.exit(1)
fails = scenario in ('inherited', 'cap-five', 'regression', 'absent-test', 'absent-package', 'base-build', 'base-skipped')
if scenario == 'mixed': fails = name == 'TestTwo'
if base and scenario in ('regression', 'mixed'): fails = False
emit('fail' if fails else 'pass', name)
emit('fail' if fails else 'pass')
sys.exit(1 if fails else 0)
PY
chmod +x "$tmp/bin/stub"
for tool in make go git gh; do ln -s stub "$tmp/bin/$tool"; done
export PATH="$tmp/bin:$PATH"

run_case() {
	local scenario="$1" expected="$2" go_calls="$3" verdict="$4" status
	export SCENARIO="$scenario" CASE_DIR="$tmp/$scenario"
	mkdir "$CASE_DIR"
	export GITHUB_STEP_SUMMARY="$CASE_DIR/summary"
	if "$root/scripts/touched-verdict.sh" run --base base-sha --report "$CASE_DIR/report.json" ./pkg >"$CASE_DIR/log" 2>&1; then status=0; else status=$?; fi
	if [ "$status" -ne "$expected" ]; then cat "$CASE_DIR/log" >&2; printf '%s: status %s, want %s\n' "$scenario" "$status" "$expected" >&2; exit 1; fi
	python3 - "$CASE_DIR" "$go_calls" "$verdict" <<'PY'
import json, sys
from pathlib import Path
directory = Path(sys.argv[1])
calls = [json.loads(line) for line in (directory / 'calls').read_text().splitlines()]
assert len([call for call in calls if call['tool'] == 'make']) == 1, calls
assert len([call for call in calls if call['tool'] == 'go']) == int(sys.argv[2]), calls
assert sys.argv[3] in (directory / 'log').read_text(), (directory / 'log').read_text()
assert sys.argv[3] in (directory / 'summary').read_text(), (directory / 'summary').read_text()
json.loads((directory / 'report.json').read_text())
PY
}

run_case green 0 0 'Every selected test passed'
run_case flaky 0 1 flaky
grep -q 'FIRST FAILURE REMAINS IN THE LOG' "$CASE_DIR/log"
grep -q '::warning::example.invalid/fixture/pkg: TestOne: flaky' "$CASE_DIR/log"
run_case readable-log 0 1 flaky
grep -q '^--- FAIL: TestX (0.00s)$' "$CASE_DIR/log"
grep -q '^plain runner diagnostic$' "$CASE_DIR/log"
if grep -q '{"Action"' "$CASE_DIR/log"; then cat "$CASE_DIR/log" >&2; exit 1; fi
run_case inherited 0 2 'already failing on the base'
run_case regression 1 2 'introduced by this change'
grep -q '::error::example.invalid/fixture/pkg: TestOne' "$CASE_DIR/log"
run_case build 1 0 'build failure'
run_case setup 1 0 'package setup failure'
run_case timeout 1 0 'package-level timeout'
run_case panic 1 0 'panic outside a test'
run_case killed 1 0 'killed process'
run_case killed-silent 1 0 'killed process'
run_case shard 1 0 'shard runner error'
run_case unnamed 1 0 'without an attributable test name'
run_case cap 1 0 'exceed the cap of 5'
run_case cap-five 0 10 'already failing on the base'
run_case absent-test 1 2 'absent'
run_case absent-package 1 2 'introduced by this change'
run_case base-build 1 2 'base could not run'
run_case base-skipped 1 2 'skipped'
run_case subtest 0 1 flaky
run_case mixed 1 3 'introduced by this change'

# API success and failure belong to the aggregate job, after test results are
# fixed. One comment contains every leg's finding; a fork makes no API call.
mkdir "$tmp/reports"
cp "$tmp/flaky/report.json" "$tmp/reports/tui3.json"
cp "$tmp/inherited/report.json" "$tmp/reports/session.json"
export GITHUB_REPOSITORY=example/fixture GITHUB_RUN_ID=42 GITHUB_SHA=head-sha
for scenario in api-error api-malformed existing-issue create-issue fork; do
	export SCENARIO="$scenario" CASE_DIR="$tmp/$scenario" TOUCHED_REPORT_ISSUE=true
	mkdir "$CASE_DIR"
	if [ "$scenario" = fork ]; then TOUCHED_REPORT_ISSUE=false; export TOUCHED_REPORT_ISSUE; fi
	"$root/scripts/touched-verdict.sh" report-issues "$tmp/reports" >"$CASE_DIR/log" 2>&1
	python3 - "$CASE_DIR" "$scenario" <<'PY'
import json, sys
from pathlib import Path
directory, scenario = Path(sys.argv[1]), sys.argv[2]
if scenario == 'fork':
    assert not (directory / 'calls').exists()
else:
    calls = [json.loads(line) for line in (directory / 'calls').read_text().splitlines()]
    if scenario in ('api-error', 'api-malformed'):
        assert '::warning::' in (directory / 'log').read_text()
    else:
        comments = [call for call in calls if call['args'][:2] == ['issue', 'comment']]
        assert len(comments) == 1, calls
        assert comments[0]['args'][2] == ('8' if scenario == 'create-issue' else '7'), calls
        body = (directory / 'issue-body').read_text()
        assert 'flaky' in body and 'already failing on the base' in body, body
        assert '/actions/runs/42' in body and 'head-sha' in body, body
        assert not any('close' in call['args'] for call in calls), calls
PY
done
printf 'touched-verdict acceptance: ok\n'
