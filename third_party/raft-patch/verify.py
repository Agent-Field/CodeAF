#!/usr/bin/env python3
"""Check the R14 source boundary without git, downloads or the caller's home."""
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

meta = Path(__file__).resolve().parent
repo = meta.parent.parent
pin = json.loads((meta / "SOURCES.json").read_text())
source = repo / "third_party/raft"
expected = pin["upstream_files"]
actual = {str(p.relative_to(source)) for p in source.rglob("*") if p.is_file()}
assert actual == set(expected), "Raft source inventory changed"
for name, digest in expected.items():
    if name == "raft.go":
        continue
    assert hashlib.sha256((source / name).read_bytes()).hexdigest() == digest, name
# Reverse the exact recorded patch into a throwaway file and compare with the
# independently pinned upstream bytes. The working tree is never modified.
with tempfile.TemporaryDirectory(prefix="p02-pin-", dir="/tmp") as temp:
    path = Path(temp) / "raft.go"
    path.write_bytes((source / "raft.go").read_bytes())
    subprocess.run(["patch", "--silent", "--batch", "--reverse", str(path),
                    str(meta / "election-random.patch")], check=True)
    assert hashlib.sha256(path.read_bytes()).hexdigest() == expected["raft.go"]
rootmod = (repo / "go.mod").read_text()
rootsum = (repo / "go.sum").read_text()
assert "replace go.etcd.io/raft/v3 => ./third_party/raft" in rootmod
for key in ["raft", "protobuf"]:
    item = pin[key]
    assert f'{item["module"]} {item["version"]}' in rootmod
assert f'google.golang.org/protobuf v1.36.11 {pin["protobuf"]["sum"]}' in rootsum
assert (meta / "PROTOBUF-LICENSE").read_text().startswith("Copyright")
print("R14 source, patch, protobuf pins and notices: PASS")
