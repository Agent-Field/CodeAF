#!/usr/bin/env python3
"""Supplemental Validated interface check; never replaces the corpus grade.

Run inside the repository's prepared Python environment:
  python validated-contract.py --repo /app --out /logs/type-contract

The negative caller must be rejected at its argument boundary. The positive
caller must type-check and run. Source checking must also pass, so unrelated
errors cannot make a broken negative probe look successful.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

NEGATIVE = '''from typing_extensions import Never
from returns.interfaces.failable import FailableN
from returns.validated import Invalid

def recover(container: FailableN[int, int, Never]) -> object:
    return container.lash(lambda error: container.from_value(error + 1))

print(recover(Invalid((1, 2))))
'''
POSITIVE = '''from typing_extensions import Never
from returns.interfaces.failable import FailableN
from returns.validated import Invalid, Valid

def recover(container: FailableN[int, tuple[int, ...], Never]) -> object:
    return container.lash(lambda errors: container.from_value(errors[0] + 1))

assert recover(Invalid((1, 2))) == Valid(2)
print("tuple recovery passed")
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    repo, out = args.repo.resolve(), args.out.resolve()
    if not (repo / 'returns' / 'validated.py').is_file():
        parser.error('the repository must contain the implemented returns/validated.py')
    out.mkdir(parents=True, exist_ok=False)
    negative, positive = out / 'negative.py', out / 'positive.py'
    negative.write_text(NEGATIVE)
    positive.write_text(POSITIVE)
    env = os.environ.copy()
    env['PYTHONPATH'] = str(repo)
    rows = {}

    def run(name, command):
        try:
            result = subprocess.run(command, cwd=repo, env=env, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                    timeout=120)
            code, output = result.returncode, result.stdout
        except subprocess.TimeoutExpired as error:
            code, output = 124, error.stdout or b''
            if isinstance(output, bytes):
                output = output.decode(errors='replace')
        (out / (name + '.log')).write_text(output)
        rows[name] = {'exit': code, 'command': command}
        return code, output

    mypy = [sys.executable, '-m', 'mypy', '--no-incremental',
            '--cache-dir', os.devnull, '--show-error-codes', 'returns']
    source_code, _ = run('source', mypy)
    negative_code, negative_output = run('negative-type', mypy + [str(negative)])
    positive_code, _ = run('positive-type', mypy + [str(positive)])
    runtime_code, runtime_output = run('positive-runtime', [sys.executable, str(positive)])
    # Require the error at this call site, not an import failure or a source error.
    call_line = next(i for i, line in enumerate(NEGATIVE.splitlines(), 1)
                     if line.startswith('print(recover('))
    rejected = negative_code == 1 and any(
        re.search(r'negative\.py:' + str(call_line) + r'(?::\d+)?: error:', line)
        and '[arg-type]' in line for line in negative_output.splitlines())
    passed = (source_code == positive_code == runtime_code == 0 and rejected
              and 'tuple recovery passed' in runtime_output)
    report = {'passed': passed, 'negative_call_rejected': rejected, 'checks': rows}
    (out / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return 0 if passed else 1


if __name__ == '__main__':
    raise SystemExit(main())
