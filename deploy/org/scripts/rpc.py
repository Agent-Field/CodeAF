#!/usr/bin/env python3
"""usage: rpc.py <method> ['<json params>']   one call on the runner's engine wire; prints the reply."""
import json
import os
import socket
import sys

path = os.environ.get('RUNNER_SOCKET', '/data/engine.sock')
if len(sys.argv) < 2:
    raise SystemExit(__doc__)
with socket.socket(socket.AF_UNIX) as conn:
    conn.settimeout(300)
    conn.connect(path)
    params = json.loads(sys.argv[2]) if len(sys.argv) > 2 else {}
    conn.sendall((json.dumps({'id': 1, 'method': sys.argv[1], 'params': params}) + '\n').encode())
    with conn.makefile() as stream:
        reply = json.loads(stream.readline())
print(json.dumps(reply, indent=1))
sys.exit(1 if reply.get('error') else 0)
