#!/usr/bin/env python3
"""Reproduce furrow 0.3.6's first-pack open race on Linux, without codeaf.

Run: python3 internal/furrowbin/testdata/first_pack_race.py /path/to/furrow
Requires strace with syscall injection. It creates a fresh store and no keys.
"""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time

binary = str(Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix="furrow-first-pack-") as directory:
    root = Path(directory)
    tree = root / "tree"
    tree.mkdir()
    (tree / "note").write_text("first shared chat\n")
    data = root / "data"
    address = str(root / "e.sock")
    daemon = subprocess.Popen(
        [binary, "serve", "--socket", address],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    reader = None
    stopped_pid = None

    def request(verb, args):
        with socket.socket(socket.AF_UNIX) as connection:
            connection.settimeout(5)
            connection.connect(address)
            connection.sendall((json.dumps({
                "V": 1, "id": 1, "verb": verb,
                "target": {"data_dir": str(data), "tree": str(tree), "timeline": "chat"},
                "args": args,
            }) + "\n").encode())
            with connection.makefile() as response:
                value = json.loads(response.readline())
            assert "err" not in value, value
            return value["ok"]

    try:
        deadline = time.monotonic() + 10
        while not Path(address).exists():
            assert time.monotonic() < deadline, "daemon did not start"
            time.sleep(0.001)
        request("capture-plan", {})
        trace = root / "reader.trace"
        command = [binary, "--repo", str(tree), "--timeline", "chat", "--json", "capture-plan"]
        environment = {"PATH": os.defpath, "FURROW_DATA_DIR": str(data)}
        subprocess.run(["strace", "-o", str(trace), "-e", "trace=statx"] + command,
                       env=environment, check=True, stdout=subprocess.DEVNULL)
        pack = (data / "store-v1/packs/CURRENT").read_text().strip()
        calls = [line for line in trace.read_text().splitlines() if line.startswith("statx(")]
        ordinal = next(index for index, line in enumerate(calls, 1)
                       if f"/packs/{pack}" in line and "ENOENT" in line)
        trace.unlink()
        reader = subprocess.Popen([
            "strace", "-f", "-o", str(trace), "-e", "trace=statx",
            "-e", f"inject=statx:signal=SIGSTOP:when={ordinal}",
        ] + command, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        deadline = time.monotonic() + 10
        while True:
            text = trace.read_text() if trace.exists() else ""
            if "stopped by SIGSTOP" in text:
                stopped_pid = int(text.split()[0])
                break
            assert reader.poll() is None, "reader exited before the injected stop"
            assert time.monotonic() < deadline, "reader did not stop"
            time.sleep(0.001)
        assert f"/packs/{pack}" in text and "ENOENT" in text
        result = request("seal", {"turn": "first", "durable": True})
        assert result["durable"], result
        assert (data / "store-v1/packs" / pack).is_file()
        os.kill(stopped_pid, signal.SIGCONT)
        stopped_pid = None
        _, stderr = reader.communicate(timeout=5)
        print(stderr.decode().strip())
        assert reader.returncode != 0 and b"durable object pack is missing" in stderr
        print("Reproduced: pack exists, but its earlier absence was combined with the newer frontier.")
    finally:
        if stopped_pid is not None:
            os.kill(stopped_pid, signal.SIGKILL)
        if reader is not None and reader.poll() is None:
            reader.kill()
            reader.wait()
        daemon.terminate()
        daemon.wait(timeout=5)
