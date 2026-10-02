#!/usr/bin/env python3
"""B's wall clock minus A's, in ms, by ping-pong over one open ssh channel.

  clockoff.py <ControlPath>  ->  {"offset_ms": .., "rtt_ms": .., "samples": ..}

A tiny loop on B answers each line with its clock at once, so the round trip is
the network and one ssh channel, not a process start. The reported offset is the
midpoint estimate from the lowest-round-trip sample; its error is at most half
that round trip, which is printed beside it.
"""
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import rigenv  # noqa: E402

REMOTE = 'import sys,time\nwhile sys.stdin.readline():\n    print(int(time.time()*1000),flush=True)\n'


def main(control):
    p = subprocess.Popen(["ssh", "-o", "BatchMode=yes", "-o", "ControlMaster=auto", "-o", f"ControlPath={control}",
                          "-o", "ControlPersist=20m", rigenv.get("CODEAF_SECOND_HOST"), "python3", "-u", "-c", "'" + REMOTE.replace("'", "") + "'"],
                         stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
    best = None
    for _ in range(40):
        t1 = time.time() * 1000
        p.stdin.write("x\n"); p.stdin.flush()
        tb = int(p.stdout.readline())
        t2 = time.time() * 1000
        rtt = t2 - t1
        if best is None or rtt < best[0]:
            best = (rtt, tb - (t1 + t2) / 2)
    p.stdin.close(); p.wait()
    print(json.dumps({"offset_ms": round(best[1]), "rtt_ms": round(best[0], 1), "samples": 40}))


main(sys.argv[1])
