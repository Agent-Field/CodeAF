#!/usr/bin/env python3
"""Generate docs/design/header-spacing-ref-imagen2.png via Google Vertex AI imagen.

Tries model literal name "imagen"; on 400, reads the rejection's accepted model
values and retries once with the closest same-family model.
"""
import json
import os
import re
import subprocess
import sys
from pathlib import Path

from google import genai

PROMPT = (
    'A pixel-sharp screenshot of a dark terminal application, perfectly flat '
    'solid background color with NO texture, NO gradient, NO photographic noise, '
    'rendered typography with hard edges like a real terminal screenshot. Four '
    'header text rows: (1) nav line: codeaf home teams chats sessions spend '
    'settings, right-aligned status at the edge; (2) one entire empty row of pure '
    'background, zero characters; (3) chat tab strip: harbor Manager Refactor the '
    'rail plus plus; (4) a full-width horizontal rule of dash glyphs. The empty '
    'second row must be a flat clean band separating nav from strip. Below, dim '
    'gray placeholder prose on the same flat background. Flat vector-like '
    'precision, screenshot aesthetic.'
)

OUT = Path("docs/design/header-spacing-ref-imagen2.png")


def access_token() -> str:
    """Vertex needs OAuth; prefer ADC, fall back to gcloud user token."""
    env = dict(os.environ)
    env.pop("GOOGLE_APPLICATION_CREDENTIALS", None)
    try:
        from google.auth import default
        creds, _ = default()
        creds.refresh(__import__("google.auth.transport.requests", fromlist=["Request"]).Request())
        return creds.token
    except Exception:
        tok = subprocess.run(
            ["gcloud", "auth", "application-default", "print-access-token"],
            capture_output=True, text=True,
        ).stdout.strip()
        if tok:
            return tok
        tok = subprocess.run(
            ["gcloud", "auth", "print-access-token"], capture_output=True, text=True,
        ).stdout.strip()
        if tok:
            return tok
        raise


def accepted_models(err) -> list:
    try:
        m = re.search(r"[a-z0-9-]*(?:imagen|image)[a-z0-9-]*", str(err), re.I)
        return [m.group(0)] if m else []
    except Exception:
        return []


def generate(client, model: str):
    resp = client.models.generate_images(
        model=model,
        prompt=PROMPT,
        config={"aspect_ratio": "2:1"},
    )
    if not resp.generated_images:
        raise SystemExit(f"model '{model}': no generated images")
    OUT.write_bytes(resp.generated_images[0].image.image_bytes)
    print(json.dumps({"model": model, "path": str(OUT), "size": OUT.stat().st_size}))


def main():
    token = access_token()  # OAuth2 token from gcloud user login
    from google.auth.credentials import Credentials as _BaseCreds

    class _TokenCreds(_BaseCreds):
        def __init__(self, tok):
            self.token = tok
            self.expired = False
            self.valid = True
            self.quota_project_id = None

        def refresh(self, request):
            pass

    creds = _TokenCreds(token)
    client = genai.Client(
        vertexai=True,
        project="model-development-504013",
        location="us-central1",
        credentials=creds,
    )

    first = "imagen"
    try:
        generate(client, first)
        return
    except Exception as e:
        print(f"model '{first}' REJECTED: {type(e).__name__}: {e}", file=sys.stderr)
        if "400" not in str(e):
            raise
        models = accepted_models(e)
        if not models:
            # one last probe: ask the API for the accepted list and parse it
            try:
                body = getattr(getattr(e, "_response", None), "text", "") or str(e)
                models = re.findall(r"[a-z0-9-]*(?:imagen|image)[a-z0-9-]*", body)
            except Exception:
                models = []
        if not models:
            raise SystemExit(f"could not parse accepted models from 400: {e}")
        second = models[-1] if "imagen" in models[-1] else models[0]
        print(f"retrying once with '{second}'", file=sys.stderr)
        try:
            generate(client, second)
        except Exception as e2:
            raise SystemExit(f"second model REJECTED: {type(e2).__name__}: {e2}")


if __name__ == "__main__":
    main()