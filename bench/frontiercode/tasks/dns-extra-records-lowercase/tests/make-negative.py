#!/usr/bin/env python3
"""Generate the partial-fix negative control for the dns task.

This is the labelled failure shape for this task's two blockers: record names
are lowercased on ONE of the two paths that write extra records into the
tailcfg DNS config (the runtime setter) while the other path (the conversion
of the configured DNS block) still passes mixed-case names through. That is
blocker 2: "a fix that lowercases in one place and leaves the other path
passing mixed-case names through is exactly the partial fix this criterion
exists to catch". It also fails blocker 1, the PR's regression test, because
that test exercises BOTH paths. Every step of the generation asserts, so a
drifted upstream tree fails this script at image build instead of quietly
producing a mislabelled control.

The script runs INSIDE the verifier image, in a checkout of the base commit.
All edits are described as transformations of the base tree's own text — no
upstream bytes are committed to the rig.
"""

import argparse
import pathlib
import subprocess
import sys

SET_ANCHOR = "\t\tc.TailcfgDNSConfig.ExtraRecords = records"
CONV_ANCHOR = "\tcfg.ExtraRecords = dns.ExtraRecords"
PARTIAL = """\t\tif len(records) == 0 {
\t\t\treturn
\t\t}
\t\tnormalized := make([]tailcfg.DNSRecord, len(records))
\t\tfor i, record := range records {
\t\t\trecord.Name = strings.ToLower(record.Name)
\t\t\tnormalized[i] = record
\t\t}
\t\tc.TailcfgDNSConfig.ExtraRecords = normalized"""


def git(repo: pathlib.Path, *args: str) -> str:
    out = subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, text=True
    )
    return out.stdout


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    repo = pathlib.Path("/solution/full")
    config_go = repo / "hscontrol" / "types" / "config.go"
    base = config_go.read_text()

    # Anchor 1: the setter path exists at base and passes records through
    # unchanged; the conversion path does the same. Neither is normalized.
    assert base.count(SET_ANCHOR) == 1, "setter anchor drifted"
    assert CONV_ANCHOR in base, "conversion-path anchor drifted"
    assert "lowercaseRecordNames" not in base, "base already normalizes names"

    patched = base.replace(SET_ANCHOR, PARTIAL, 1)
    assert patched != base, "no edit applied"
    assert patched.count("strings.ToLower") == 1
    # The conversion path is left untouched — that is the point of the control.
    assert CONV_ANCHOR in patched
    config_go.write_text(patched)

    git(repo, "add", "-A")
    diff = git(repo, "diff", "--binary", args.base)
    assert diff.strip(), "empty negative diff"
    # Blocker 2 by construction: only the setter path normalizes.
    assert "record.Name = strings.ToLower(record.Name)" in patched
    # Blocker 1 by construction: the regression test exercises both paths and
    # the conversion path still passes mixed-case names through.
    assert CONV_ANCHOR in patched
    pathlib.Path(args.out).write_text(diff)
    print(f"negative control written: {args.out} ({len(diff)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
