#!/usr/bin/env python3
"""Generate docs/design/header-spacing-ref-gpt-image.png via OpenAI images API."""
import json
import os
import sys
from pathlib import Path

from openai import OpenAI

PROMPT = (
    'Flat 2D mockup of a dark terminal UI screenshot in crisp monospace type on a '
    'near-black background. Exactly four header rows at the top: first row a top nav '
    'reading "● codeaf   home  teams  chats  sessions  spend  settings" with a '
    'right-aligned status "2 want you · $1.20  thu 10:31pm"; second row COMPLETELY '
    'BLANK, one full empty text line; third row a chat strip "● harbor ▾   ◆ Manager ×   '
    'Refactor the rail… ×   +   ▦ All"; fourth row a full-width rule of dash glyphs. '
    'Beneath the rule, dim gray placeholder prose. Precise, aligned, screenshot-flat, '
    'no photorealism, no shadows, no garbled glyphs.'
)

OUT = Path("docs/design/header-spacing-ref-gpt-image.png")
OUT.parent.mkdir(parents=True, exist_ok=True)

client = OpenAI(api_key=os.environ["OPENAI_API_KEY"])

# First attempt with the literal requested model name.
model = "gpt-image"
resp = None
try:
    resp = client.images.generate(
        model=model,
        prompt=PROMPT,
        size="1792x896",  # ~2:1
        n=1,
    )
    print(f"model '{model}' accepted")
except Exception as e:
    print(f"model '{model}' REJECTED: {type(e).__name__}: {e}", file=sys.stderr)
    body = getattr(getattr(e, "response", None), "text", "")
    print(f"RESPONSE BODY:\n{body}", file=sys.stderr)
    if "400" not in str(e) and "400" not in str(getattr(getattr(e, "response", None), "status_code", "")):
        raise SystemExit(1)
    # 400 error: retry once with closest model of same family per task instructions.
    model = "gpt-image-1"
    print(f"retrying once with '{model}'", file=sys.stderr)
    try:
        resp = client.images.generate(
            model=model,
            prompt=PROMPT,
            size="1792x896",
            n=1,
        )
        print(f"model '{model}' accepted")
    except Exception as e2:
        body2 = getattr(getattr(e2, "response", None), "text", "")
        print(f"second model REJECTED: {type(e2).__name__}: {e2}\n{body2}", file=sys.stderr)
        raise SystemExit(1)

if not resp or not resp.data:
    raise SystemExit("no image data in response")

data = resp.data[0]
if data.b64_json:
    import base64
    OUT.write_bytes(base64.b64decode(data.b64_json))
    print(f"wrote b64 -> {OUT} ({OUT.stat().st_size} bytes)")
elif data.url:
    import urllib.request
    urllib.request.urlretrieve(data.url, OUT)
    print(f"downloaded -> {OUT} ({OUT.stat().st_size} bytes)")
else:
    raise SystemExit("no b64_json and no url in response")
print(json.dumps({"model": model, "path": str(OUT), "size": OUT.stat().st_size}))